package api

import (
	"testing"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
)

func TestValidDumpKey(t *testing.T) {
	const prefix = "pg-main/orders/"
	for key, want := range map[string]bool{
		"pg-main/orders/20261030T010203Z.dump":        true,
		"pg-main/orders/20261030T010203Z.dump.sha256": false,
		"pg-main/orders/20261030T010203Z.dump.sig":    false,
		"pg-main/orders/20261030T010203Z.extensions":  false,
		"pg-main/orders/.dump":                        false,
		"pg-main/orders/nested/x.dump":                false,
		"pg-main/orders-old/20261030T010203Z.dump":    false,
		"pg-main/billing/20261030T010203Z.dump":       false,
		"20261030T010203Z.dump":                       false,
		"":                                            false,
	} {
		if got := validDumpKey(prefix, key); got != want {
			t.Errorf("validDumpKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestDumpFilename(t *testing.T) {
	if got, want := dumpFilename(`pg main/orders"x/20261030T010203Z.dump`), "pg_main_orders_x_20261030T010203Z.dump"; got != want {
		t.Errorf("dumpFilename = %q, want %q", got, want)
	}
}

func TestDownloadBuckets(t *testing.T) {
	client := func(id, name string, downloadable bool) *storage.Client {
		c, err := storage.New(model.S3Bucket{ID: id, Name: name, Endpoint: "s3.example", Region: "ru-central1",
			Bucket: "dumps", AccessKeyID: "k", SecretAccessKey: "s", Downloadable: downloadable})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	got := downloadBuckets([]*storage.Client{
		client(storage.BuiltInID, "helm", true),
		client("b1", "minio-dc", false),
		client("b2", "yc", true),
	})
	if len(got) != 2 || got[0].ID != storage.BuiltInID || got[1] != (dumpBucket{ID: "b2", Name: "yc"}) {
		t.Fatalf("downloadBuckets = %+v", got)
	}
	if got := downloadBuckets(nil); got == nil || len(got) != 0 {
		t.Fatalf("no ready copies must give empty list, got %#v", got)
	}
}

func TestS3BucketRequestDownloadable(t *testing.T) {
	req := s3BucketRequest{Name: "dr", Endpoint: "s3.example", Bucket: "b", AccessKeyID: "k", SecretAccessKey: "s"}
	if in, _ := req.input(true); !in.Downloadable {
		t.Error("downloadable must default to true")
	}
	off := false
	req.Downloadable = &off
	if in, _ := req.input(true); in.Downloadable {
		t.Error("explicit downloadable=false ignored")
	}
}

func TestDownloadRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	// recent — свежие первыми, по одной выдаче в минуту.
	recent := func(n int) []time.Time {
		out := make([]time.Time, n)
		for i := range out {
			out[i] = now.Add(-time.Duration(i) * time.Minute)
		}
		return out
	}
	if got := downloadRetryAfter(recent(dumpDownloadLimit-1), now); got != 0 {
		t.Errorf("below limit: wait %s, want 0", got)
	}
	// Самая старая из лимитных выдач — 9 минут назад: ждать ещё минуту.
	if got := downloadRetryAfter(recent(dumpDownloadLimit), now); got != time.Minute {
		t.Errorf("at limit: wait %s, want 1m", got)
	}
	if got := downloadRetryAfter(recent(dumpDownloadLimit), now.Add(2*time.Minute)); got != 0 {
		t.Errorf("window passed: wait %s, want 0", got)
	}
}
