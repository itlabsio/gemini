package vault

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
)

// fakeVault отдаёт секреты по путям Vault API (/v1/<path>). Значение — уже
// содержимое поля data ответа: для KV v2 туда кладётся {"data": {...}}.
func fakeVault(t *testing.T, secrets map[string]map[string]any) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "t" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		data, ok := secrets[strings.TrimPrefix(r.URL.Path, "/v1/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[]}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	c, err := New(config.VaultConfig{Enabled: true, Address: srv.URL, AuthMethod: "token", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestReadDBSecret(t *testing.T) {
	c := fakeVault(t, map[string]map[string]any{
		"kv/data/v2":     {"data": map[string]any{"username": "u2", "password": "p2"}, "metadata": map[string]any{}},
		"kv1/plain":      {"user": "u1", "pwd": "p1"},
		"kv/data/custom": {"data": map[string]any{"PG_LOGIN": "cu", "PG_SECRET": "cp", "password": "decoy"}},
		"kv/data/num":    {"data": map[string]any{"username": "u", "password": 12345}},
		"kv/data/empty":  {"data": map[string]any{"username": "u", "password": ""}},
	})
	ctx := context.Background()

	cases := []struct {
		name, path, ukey, pkey string
		wantUser, wantPass     string
		wantErr                string
	}{
		{name: "kv v2 guessed", path: "kv/data/v2", wantUser: "u2", wantPass: "p2"},
		{name: "kv v1 guessed", path: "kv1/plain", wantUser: "u1", wantPass: "p1"},
		{name: "leading/trailing slashes", path: "/kv/data/v2/", wantUser: "u2", wantPass: "p2"},
		// Явное сопоставление важнее эвристики: "password" здесь — приманка.
		{name: "explicit keys", path: "kv/data/custom", ukey: "PG_LOGIN", pkey: "PG_SECRET", wantUser: "cu", wantPass: "cp"},
		{name: "explicit key missing", path: "kv/data/v2", ukey: "username", pkey: "nope", wantErr: `"nope"`},
		{name: "non-string password", path: "kv/data/num", wantErr: "password"},
		{name: "empty password", path: "kv/data/empty", wantErr: "password"},
		{name: "no guessable username", path: "kv/data/custom", wantErr: "username"},
		{name: "no secret", path: "kv/data/missing", wantErr: "no secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := c.ReadDBSecret(ctx, tc.path, tc.ukey, tc.pkey)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.Username != tc.wantUser || s.Password != tc.wantPass {
				t.Fatalf("got %q/%q", s.Username, s.Password)
			}
		})
	}
}

// SecretKeys отдаёт только имена полей — значения наружу не уходят.
func TestSecretKeysNamesOnly(t *testing.T) {
	c := fakeVault(t, map[string]map[string]any{
		"kv/data/x": {"data": map[string]any{"password": "TOPSECRET", "username": "u"}},
	})
	keys, err := c.SecretKeys(context.Background(), "kv/data/x")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(keys, []string{"password", "username"}) {
		t.Fatalf("keys = %v", keys)
	}
}

func TestGuessKeys(t *testing.T) {
	u, p := GuessKeys([]string{"login", "pass", "user"})
	if u != "user" || p != "pass" { // порядок приоритета: username, user, login
		t.Errorf("GuessKeys = %q, %q", u, p)
	}
	if u, p := GuessKeys([]string{"a", "b"}); u != "" || p != "" {
		t.Errorf("GuessKeys(unknown) = %q, %q", u, p)
	}
}
