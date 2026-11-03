package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
)

func TestSanitize(t *testing.T) {
	cases := []struct {
		in     string
		leaked string // этой подстроки в результате быть не должно
	}{
		{`pg_dump: error: password=s3cr3t host=pg`, "s3cr3t"},
		{`PGPASSWORD: hunter2`, "hunter2"},
		{`connect pwd = qwerty`, "qwerty"},
		{`dial postgres://backup:p%40ss@db.internal:5432/app failed`, "p%40ss"},
		{`postgresql://u:topsecret@h/db`, "topsecret"},
		{`request header Authorization: Bearer-eyJhbGciOi`, "eyJhbGciOi"},
	}
	for _, c := range cases {
		got := Sanitize(c.in)
		if strings.Contains(got, c.leaked) {
			t.Errorf("Sanitize(%q) = %q — leaks %q", c.in, got, c.leaked)
		}
		if !strings.Contains(got, "[redacted]") {
			t.Errorf("Sanitize(%q) = %q — no redaction marker", c.in, got)
		}
	}
	// Обычный текст ошибки не трогаем.
	plain := `pg_restore: error: could not execute query: relation "orders" already exists`
	if got := Sanitize(plain); got != plain {
		t.Errorf("plain text changed: %q", got)
	}
}

func TestSanitizeTruncatesOnRuneBoundary(t *testing.T) {
	// Префикс подобран так, что байт 1500 приходится на середину кириллической
	// руны: старый s[:1500] давал здесь невалидный UTF-8.
	in := "xx" + strings.Repeat("ошибка ", 400)
	got := Sanitize(in)
	if !utf8.ValidString(got) {
		t.Fatal("truncated text is not valid UTF-8")
	}
	if !strings.HasSuffix(got, "…") || len(got) > maxErrorLen+len("…") {
		t.Fatalf("len = %d, suffix ok = %v", len(got), strings.HasSuffix(got, "…"))
	}
	if short := "коротко"; Sanitize(short) != short {
		t.Error("short text truncated")
	}
}

// capture — webhook-приёмник, запоминает последнее тело.
func capture(t *testing.T, status int) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

var ev = Event{HeadRole: "source", Kind: "dump", Instance: "pg1", Database: "app", RunID: "r1",
	Error: "password=s3cr3t boom", OccurredAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

func TestFailureSlackIsSanitized(t *testing.T) {
	srv, got := capture(t, http.StatusOK)
	n := New(config.NotifyConfig{Enabled: true, Provider: "slack", WebhookURL: srv.URL})
	if err := n.Failure(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	text, _ := (*got)["text"].(string)
	if strings.Contains(text, "s3cr3t") {
		t.Fatalf("secret leaked to webhook: %q", text)
	}
	for _, want := range []string{"FAILED", "Инстанс: pg1", "База: app", "Run: r1"} {
		if !strings.Contains(text, want) {
			t.Errorf("text has no %q: %q", want, text)
		}
	}
}

func TestTelegramPayload(t *testing.T) {
	srv, got := capture(t, http.StatusOK)
	n := New(config.NotifyConfig{Enabled: true, Provider: "telegram", WebhookURL: srv.URL,
		TelegramChatID: "-100", TelegramThreadID: "42"})
	if err := n.Success(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if (*got)["chat_id"] != "-100" || (*got)["message_thread_id"] != float64(42) {
		t.Errorf("payload = %v", *got)
	}
}

func TestSendErrorsAndNoop(t *testing.T) {
	srv, _ := capture(t, http.StatusBadGateway)
	n := New(config.NotifyConfig{Enabled: true, WebhookURL: srv.URL})
	if err := n.Success(context.Background(), ev); err == nil {
		t.Error("non-2xx webhook response not reported")
	}
	// Выключено / без URL / nil — молча ничего не шлём.
	for _, n := range []*Notifier{
		New(config.NotifyConfig{Enabled: false, WebhookURL: srv.URL}),
		New(config.NotifyConfig{Enabled: true}),
		nil,
	} {
		if err := n.Failure(context.Background(), ev); err != nil {
			t.Errorf("disabled notifier returned %v", err)
		}
	}
}
