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

// capture — приёмник Bot API: запоминает путь и последнее тело.
func capture(t *testing.T, status int, resp string) (*Notifier, *string, *map[string]any) {
	t.Helper()
	got, path := map[string]any{}, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	n := New()
	n.telegramURL = srv.URL
	return n, &path, &got
}

var ev = Event{HeadRole: "source", Kind: "dump", Instance: "pg1", Database: "app", RunID: "r1",
	Error: "password=s3cr3t boom", OccurredAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

const testToken = "123:abcdefghijklmnopqrstuvwxyz"

var tg = Channel{Name: "c", Provider: ProviderTelegram, Token: testToken, ChatID: "-100", ThreadID: "42"}

func TestFailureTextIsSanitized(t *testing.T) {
	text := FailureText(ev)
	if strings.Contains(text, "s3cr3t") {
		t.Fatalf("secret leaked: %q", text)
	}
	for _, want := range []string{"FAILED", "Инстанс: pg1", "База: app", "Run: r1"} {
		if !strings.Contains(text, want) {
			t.Errorf("text has no %q: %q", want, text)
		}
	}
}

func TestTelegramPayload(t *testing.T) {
	n, path, got := capture(t, http.StatusOK, `{"ok":true}`)
	if err := n.Send(context.Background(), tg, SuccessText(ev)); err != nil {
		t.Fatal(err)
	}
	if *path != "/bot"+testToken+"/sendMessage" {
		t.Errorf("path = %q", *path)
	}
	if (*got)["chat_id"] != "-100" || (*got)["message_thread_id"] != float64(42) {
		t.Errorf("payload = %v", *got)
	}
}

func TestSendReportsProviderDescription(t *testing.T) {
	n, _, _ := capture(t, http.StatusBadRequest, `{"ok":false,"description":"chat not found"}`)
	err := n.Send(context.Background(), tg, "x")
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("err = %v", err)
	}
}

// Ошибка транспорта содержит URL, а в нём токен: наружу (UI теста) он уйти не должен.
func TestSendErrorDoesNotLeakToken(t *testing.T) {
	n := New()
	n.telegramURL = "http://127.0.0.1:1"
	err := n.Send(context.Background(), tg, "x")
	if err == nil {
		t.Fatal("expected connection error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("token leaked: %v", err)
	}
}

func TestMaxRequest(t *testing.T) {
	var auth, chat, text string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		auth, chat, text = r.Header.Get("Authorization"), r.URL.Query().Get("chat_id"), b["text"]
		if r.URL.Path != "/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if chat == "-1" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"chat.denied","message":"no access"}`))
		}
	}))
	t.Cleanup(srv.Close)
	n := New()
	n.maxURL = srv.URL
	ch := Channel{Provider: ProviderMax, Token: "max-token", ChatID: "-100"}
	if err := n.Send(context.Background(), ch, "hello"); err != nil {
		t.Fatal(err)
	}
	if auth != "max-token" || chat != "-100" || text != "hello" {
		t.Errorf("auth=%q chat=%q text=%q", auth, chat, text)
	}
	ch.ChatID = "-1"
	if err := n.Send(context.Background(), ch, "x"); err == nil || !strings.Contains(err.Error(), "chat.denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestBotName(t *testing.T) {
	var auth string
	body := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if body == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	n := New()
	n.telegramURL, n.maxURL = srv.URL, srv.URL
	ctx := context.Background()

	body = `{"ok":true,"result":{"first_name":"G","username":"gemini_bot"}}`
	if got, err := n.BotName(ctx, ProviderTelegram, testToken); err != nil || got != "@gemini_bot" {
		t.Errorf("telegram: %q %v", got, err)
	}
	body = `{"user_id":1,"first_name":"Gemini","username":"gem","is_bot":true}`
	if got, err := n.BotName(ctx, ProviderMax, "max-token"); err != nil || got != "@gem" || auth != "max-token" {
		t.Errorf("max: %q %v auth=%q", got, err, auth)
	}
	body = `{"user_id":1,"first_name":"Gemini","username":null}`
	if got, err := n.BotName(ctx, ProviderMax, "t"); err != nil || got != "Gemini" {
		t.Errorf("max without username: %q %v", got, err)
	}
	body = ""
	if _, err := n.BotName(ctx, ProviderMax, "t"); err == nil {
		t.Error("invalid token accepted")
	}
	n.telegramURL = "http://127.0.0.1:1"
	if _, err := n.BotName(ctx, ProviderTelegram, testToken); err == nil || strings.Contains(err.Error(), testToken) {
		t.Errorf("token leaked or no error: %v", err)
	}
}

// УЦ Минцифры подключён только к клиенту MAX.
func TestMaxCAScopedToMaxClient(t *testing.T) {
	n := New()
	if n.http.Transport != nil {
		t.Error("shared client must use system roots")
	}
	tr, ok := n.maxHTTP.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs == nil {
		t.Fatal("max client has no dedicated CA pool")
	}
	if dt := http.DefaultTransport.(*http.Transport); dt.TLSClientConfig != nil && dt.TLSClientConfig.RootCAs != nil {
		t.Error("default transport was modified")
	}
}

func TestSendUnknownProvider(t *testing.T) {
	if err := New().Send(context.Background(), Channel{Provider: "slack"}, "x"); err == nil {
		t.Fatal("unsupported provider accepted")
	}
}

func TestValidation(t *testing.T) {
	for token, want := range map[string]bool{
		testToken: true, "": false, "abc": false, "1:short": false,
		"123:abcdefghijklmnopqrstuvwxyz/../../x": false, "123:abcdefghijklmnopqrstuvwxyz?a=1": false,
	} {
		if ValidToken(ProviderTelegram, token) != want {
			t.Errorf("ValidToken(%q) != %v", token, want)
		}
	}
	if ValidToken("slack", testToken) || ValidTarget("slack", "-100", "") {
		t.Error("unknown provider accepted")
	}
	if !ValidToken(ProviderMax, "abcdefghijklmnopqrstuvwxyz") || ValidToken(ProviderMax, "abc def ghi jkl mno pqr stu\r\nX: y") {
		t.Error("max token validation")
	}
	if !ValidTarget(ProviderMax, "-100", "") || ValidTarget(ProviderMax, "@chan", "") || ValidTarget(ProviderMax, "-100", "1") {
		t.Error("max target validation")
	}
	for _, c := range []struct {
		chat, thread string
		want         bool
	}{
		{"-100123", "", true}, {"@my_channel", "42", true},
		{"", "", false}, {"-100", "x", false}, {"@a", "", false}, {"1 2", "", false},
	} {
		if ValidTarget(ProviderTelegram, c.chat, c.thread) != c.want {
			t.Errorf("ValidTarget(%q,%q) != %v", c.chat, c.thread, c.want)
		}
	}
}

func TestAllowTest(t *testing.T) {
	n := New()
	if !n.AllowTest("a") || n.AllowTest("a") {
		t.Error("second test within interval must be rejected")
	}
	if !n.AllowTest("b") {
		t.Error("limit must be per channel")
	}
}
