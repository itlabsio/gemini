package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
)

func TestInitPairingRequiresHTTPS(t *testing.T) {
	s := &Server{d: Deps{Cfg: &config.Config{Role: config.RoleTarget, PublicURL: "https://dr.example.com"}}}
	for _, u := range []string{"http://source.example.com", "source.example.com", "ftp://x"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/pairing/init",
			strings.NewReader(`{"source_url":"`+u+`","code":"abc"}`))
		s.initPairing(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("initPairing(%q) = %d, want 400", u, rec.Code)
		}
	}
}
