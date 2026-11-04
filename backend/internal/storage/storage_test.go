package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

func TestLabels(t *testing.T) {
	b := JobBucket{ID: BuiltInID, Name: "helm", Endpoint: "storage.yandexcloud.net/", Bucket: "dumps"}
	if got, want := b.String(), "s3://dumps (storage.yandexcloud.net)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := b.Object("db/x.dump"), "s3://dumps/db/x.dump (storage.yandexcloud.net)"; got != want {
		t.Errorf("Object() = %q, want %q", got, want)
	}
}

func TestExtensionsDigest(t *testing.T) {
	if got := ExtensionsDigest(nil, false); got != NoExtensionsDigest {
		t.Errorf("missing sidecar digest = %q, want %q", got, NoExtensionsDigest)
	}
	empty := ExtensionsDigest([]byte{}, true)
	if empty == NoExtensionsDigest {
		t.Error("empty sidecar must differ from a missing one")
	}
	if a, b := ExtensionsDigest([]byte("pg_trgm\n"), true), ExtensionsDigest([]byte("pg_trgm\ndblink\n"), true); a == b {
		t.Error("different lists share a digest")
	}
}

func TestParseExtensions(t *testing.T) {
	got := ParseExtensions([]byte("pg_trgm\n\n  uuid-ossp \nbad;name\npostgis\n"))
	want := []string{"pg_trgm", "uuid-ossp", "postgis"}
	if len(got) != len(want) {
		t.Fatalf("ParseExtensions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParseExtensions = %v, want %v", got, want)
		}
	}
}

func TestPresignGet(t *testing.T) {
	c, err := New(model.S3Bucket{ID: "b1", Endpoint: "s3.example", Region: "ru-central1", Bucket: "dumps",
		AccessKeyID: "k", SecretAccessKey: "s", PathStyle: true, UseSSL: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.PresignGet(context.Background(), "pg/orders/x.dump", "pg_orders_x.dump", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "s3.example" || u.Path != "/dumps/pg/orders/x.dump" {
		t.Errorf("presigned url = %s", raw)
	}
	q := u.Query()
	if got := q.Get("response-content-disposition"); got != `attachment; filename="pg_orders_x.dump"` {
		t.Errorf("content-disposition = %q", got)
	}
	if q.Get("X-Amz-Expires") != "900" || q.Get("X-Amz-Signature") == "" {
		t.Errorf("signature params = %v", q)
	}
}

func TestDirectChild(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"inst/db/20260101.dump", true},
		{"inst/db/20260101.dump.sha256", true},
		{"inst/db/sub/20260101.dump", false}, // дамп базы "db/sub"
		{"inst/db/", false},
		{"inst/dbx/20260101.dump", false},
		{"other/db/20260101.dump", false},
	}
	for _, c := range cases {
		if got := DirectChild("inst/db/", c.key); got != c.want {
			t.Errorf("DirectChild(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

// TestListDumpsSidecars — готовность (.sha256) и подпись (.sig) берутся из того
// же листинга; sidecar'ы и вложенные ключи в результат не попадают.
func TestListDumpsSidecars(t *testing.T) {
	const prefix = "pg/orders/"
	keys := []string{
		prefix + "a.dump", prefix + "a.dump.sha256", prefix + "a.dump.sig",
		prefix + "b.dump", prefix + "b.dump.sha256",
		prefix + "c.dump",
		prefix + "nested/d.dump", prefix + "nested/d.dump.sha256",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") != "2" {
			http.NotFound(w, r)
			return
		}
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult><Name>dumps</Name>`)
		b.WriteString(`<Prefix>` + r.URL.Query().Get("prefix") + `</Prefix><IsTruncated>false</IsTruncated>`)
		for _, k := range keys {
			b.WriteString(`<Contents><Key>` + k + `</Key><LastModified>2026-11-01T00:00:00.000Z</LastModified>` +
				`<ETag>"x"</ETag><Size>10</Size><StorageClass>STANDARD</StorageClass></Contents>`)
		}
		b.WriteString(`</ListBucketResult>`)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()
	c, err := New(model.S3Bucket{ID: "b1", Endpoint: strings.TrimPrefix(srv.URL, "http://"),
		Region: "us-east-1", Bucket: "dumps", AccessKeyID: "k", SecretAccessKey: "s", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	objs, err := c.ListDumps(context.Background(), prefix)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]bool{}
	for _, o := range objs {
		got[o.Key] = [2]bool{o.Ready, o.Signed}
	}
	want := map[string][2]bool{
		prefix + "a.dump": {true, true},
		prefix + "b.dump": {true, false},
		prefix + "c.dump": {false, false},
	}
	if len(got) != len(want) {
		t.Fatalf("ListDumps = %v, want %v", got, want)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: ready/signed = %v, want %v", k, got[k], w)
		}
	}
}
