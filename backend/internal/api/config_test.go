package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
)

// /api/config должен отвечать без авторизации — фронт зовёт его до логина.
func TestPublicConfigIsUnauthenticated(t *testing.T) {
	r := NewRouter(Deps{Cfg: &config.Config{Role: config.RoleTarget}})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		HeadRole string `json:"head_role"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.HeadRole != "target" {
		t.Fatalf("head_role = %q, want %q", body.HeadRole, "target")
	}
}
