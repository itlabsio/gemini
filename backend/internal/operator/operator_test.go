package operator

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/peer"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
)

// fakeS3 — минимальный S3 для GET/HEAD объектов одного бакета: ровно то, что
// читает verifiedDumpIn (.sha256, .sig, .extensions). Нет объекта — 404 NoSuchKey.
func fakeS3(t *testing.T, objects map[string]string) *storage.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.URL.Path, "/dumps/")
		if ok && r.Method == http.MethodPut {
			objects[key] = "uploaded"
			w.Header().Set("ETag", `"x"`)
			return
		}
		body, found := objects[key]
		if !ok || !found {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code>` +
					`<Message>The specified key does not exist.</Message></Error>`))
			}
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("ETag", `"x"`)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	c, err := storage.New(model.S3Bucket{ID: "b1", Endpoint: strings.TrimPrefix(srv.URL, "http://"),
		Region: "us-east-1", Bucket: "dumps", AccessKeyID: "k", SecretAccessKey: "s", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const (
	dumpKey = "inst/db/20260101T000000Z.dump"
	sum     = "aa11bb22"
	exts    = "pg_trgm\nuuid-ossp\n"
)

func keypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// v2-подпись дампа так, как её ставит Source (signDump).
func signV2(priv ed25519.PrivateKey, key, checksum, extsRaw string, extsPresent bool) string {
	return peer.SignArtifact(priv, key, checksum, storage.ExtensionsDigest([]byte(extsRaw), extsPresent))
}

// v1-подпись (до списка расширений) — у старых дампов. Формат сообщения
// задан явно: функции подписи v1 в peer уже нет, только проверка.
func signV1(priv ed25519.PrivateKey, key, checksum string) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte("gemini-dump-v1\n"+key+"\n"+checksum)))
}

func TestVerifiedDumpIn(t *testing.T) {
	pub, priv := keypair(t)
	_, otherPriv := keypair(t)
	ctx := context.Background()

	cases := []struct {
		name     string
		objects  map[string]string
		wantErr  string // подстрока ошибки; "" — должно пройти
		wantExts []string
	}{
		{
			name: "v2 with extensions",
			objects: map[string]string{
				dumpKey + ".sha256":     sum + "  x.dump",
				dumpKey + ".extensions": exts,
				dumpKey + ".sig":        signV2(priv, dumpKey, sum, exts, true),
			},
			wantExts: []string{"pg_trgm", "uuid-ossp"},
		},
		{
			name: "v2 without extensions sidecar",
			objects: map[string]string{
				dumpKey + ".sha256": sum,
				dumpKey + ".sig":    signV2(priv, dumpKey, sum, "", false),
			},
		},
		{
			// v1 не покрывает список расширений: дамп принимаем, список — нет.
			name: "v1 signature drops unsigned extensions",
			objects: map[string]string{
				dumpKey + ".sha256":     sum,
				dumpKey + ".extensions": exts,
				dumpKey + ".sig":        signV1(priv, dumpKey, sum),
			},
		},
		{
			name: "not signed",
			objects: map[string]string{
				dumpKey + ".sha256": sum,
			},
			wantErr: "not signed",
		},
		{
			name: "empty checksum",
			objects: map[string]string{
				dumpKey + ".sha256": "   ",
				dumpKey + ".sig":    signV2(priv, dumpKey, "", "", false),
			},
			wantErr: "empty checksum",
		},
		{
			// Подменили дамп вместе с .sha256 — подпись старой суммы не сходится.
			name: "tampered checksum",
			objects: map[string]string{
				dumpKey + ".sha256": "ff00ff00",
				dumpKey + ".sig":    signV2(priv, dumpKey, sum, "", false),
			},
			wantErr: "signature mismatch",
		},
		{
			// Дописали расширение в sidecar — digest не тот, а v1 по этому ключу нет.
			name: "tampered extensions",
			objects: map[string]string{
				dumpKey + ".sha256":     sum,
				dumpKey + ".extensions": exts + "plpython3u\n",
				dumpKey + ".sig":        signV2(priv, dumpKey, sum, exts, true),
			},
			wantErr: "signature mismatch",
		},
		{
			// Подложили sidecar там, где Source подписал «расширений нет».
			name: "extensions added to dump signed without them",
			objects: map[string]string{
				dumpKey + ".sha256":     sum,
				dumpKey + ".extensions": exts,
				dumpKey + ".sig":        signV2(priv, dumpKey, sum, "", false),
			},
			wantErr: "signature mismatch",
		},
		{
			name: "signed by another key",
			objects: map[string]string{
				dumpKey + ".sha256": sum,
				dumpKey + ".sig":    signV2(otherPriv, dumpKey, sum, "", false),
			},
			wantErr: "signature mismatch",
		},
		{
			// Подпись чужого дампа (другой ключ S3) с той же суммой не переносится.
			name: "signature of another dump key",
			objects: map[string]string{
				dumpKey + ".sha256": sum,
				dumpKey + ".sig":    signV2(priv, "inst/other/20260101T000000Z.dump", sum, "", false),
			},
			wantErr: "signature mismatch",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := verifiedDumpIn(ctx, fakeS3(t, tc.objects), pub, dumpKey)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v.Checksum != sum || v.BucketID != "b1" {
				t.Errorf("got checksum=%q bucket=%q", v.Checksum, v.BucketID)
			}
			if !slices.Equal(v.Extensions, tc.wantExts) {
				t.Errorf("extensions = %v, want %v", v.Extensions, tc.wantExts)
			}
		})
	}
}

func TestSignCopy(t *testing.T) {
	ctx := context.Background()
	_, priv := keypair(t)
	const digest = storage.NoExtensionsDigest
	valid := peer.SignArtifact(priv, dumpKey, sum, digest)
	for _, tc := range []struct {
		name        string
		objects     map[string]string
		wantPresent bool
		wantUpload  bool
	}{
		{"valid signature is left alone", map[string]string{dumpKey + ".sha256": sum, dumpKey + ".sig": valid}, true, false},
		{"missing signature is uploaded", map[string]string{dumpKey + ".sha256": sum}, true, true},
		{"foreign signature is replaced", map[string]string{dumpKey + ".sha256": sum, dumpKey + ".sig": "bad"}, true, true},
		{"no dump in bucket", map[string]string{}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.objects[dumpKey+".sig"]
			present, err := signCopy(ctx, fakeS3(t, tc.objects), priv, dumpKey, sum, digest)
			if err != nil {
				t.Fatal(err)
			}
			if present != tc.wantPresent {
				t.Errorf("present = %v, want %v", present, tc.wantPresent)
			}
			if uploaded := tc.objects[dumpKey+".sig"] != before; uploaded != tc.wantUpload {
				t.Errorf("uploaded = %v, want %v", uploaded, tc.wantUpload)
			}
		})
	}
}

func TestDeliverToPeerDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-release
		got <- r.URL.Path
	}))
	defer srv.Close()
	defer close(release)

	o := &Operator{
		peer:      peer.NewClient(config.PeerRetryConfig{MaxAttempts: 1}, "source"),
		peerSlots: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	o.deliverToPeer(ctx, srv.URL, "/webhook/x", "secret", []byte("{}"))
	cancel() // запрос пользователя закончился — доставка продолжается
	release <- struct{}{}

	select {
	case path := <-got:
		if path != "/webhook/x" {
			t.Errorf("path = %q", path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("webhook was not delivered")
	}
}

func TestLatest(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	objs := []storage.Object{
		{Key: "a", LastModified: t0},
		{Key: "c", LastModified: t0.Add(2 * time.Hour)},
		{Key: "b", LastModified: t0.Add(time.Hour)},
	}
	if got := latest(objs); got != "c" {
		t.Errorf("latest = %q, want c", got)
	}
	if got := latest(nil); got != "" {
		t.Errorf("latest(nil) = %q", got)
	}
}

func TestEventKeyFor(t *testing.T) {
	// Ключ идемпотентности: один и тот же дамп — один restore (webhook и poller).
	if a, b := EventKeyFor("Restore", "k"), EventKeyFor("restore", "k"); a != b {
		t.Errorf("kind must be case-insensitive: %q vs %q", a, b)
	}
	if EventKeyFor("restore", "k1") == EventKeyFor("restore", "k2") {
		t.Error("different dumps share an event key")
	}
	if EventKeyFor("dump", "k") == EventKeyFor("restore", "k") {
		t.Error("dump and restore share an event key")
	}
}

func TestRestoreModeOf(t *testing.T) {
	cases := map[model.RestoreMode]model.RestoreMode{
		"":                        model.RestoreRecreate,
		"garbage":                 model.RestoreRecreate,
		model.RestoreRecreate:     model.RestoreRecreate,
		model.RestoreInPlace:      model.RestoreInPlace,
		model.RestoreInPlacePerDB: model.RestoreInPlacePerDB,
	}
	for in, want := range cases {
		if got := restoreModeOf(&model.Instance{RestoreMode: in}); got != want {
			t.Errorf("restoreModeOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCredsSecretData(t *testing.T) {
	inst := &model.Instance{Host: "pg", Port: 6432, SSLMode: "require", SSLRootCert: "PEM"}
	d := credsSecretData(inst, "u", "p", nil)
	if d["PGHOST"] != "pg" || d["PGPORT"] != "6432" || d["PGUSER"] != "u" || d["PGPASSWORD"] != "p" {
		t.Errorf("conn fields = %v", d)
	}
	// CA прокидывается только когда libpq его реально проверяет.
	if _, ok := d["PGSSLROOTCERT_PEM"]; ok {
		t.Error("root cert passed for ssl_mode=require")
	}
	inst.SSLMode = "verify-full"
	if credsSecretData(inst, "u", "p", nil)["PGSSLROOTCERT_PEM"] != "PEM" {
		t.Error("root cert missing for verify-full")
	}
}

// Флаг базы на Target гейтит только автоматические запуски; ручной — нет.
func TestIsAutoTrigger(t *testing.T) {
	for trig, want := range map[model.RunTrigger]bool{
		model.TriggerWebhook:      true,
		model.TriggerPollFallback: true,
		model.TriggerManual:       false,
	} {
		if got := isAutoTrigger(trig); got != want {
			t.Errorf("isAutoTrigger(%q) = %v, want %v", trig, got, want)
		}
	}
}
