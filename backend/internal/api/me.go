package api

import (
	"encoding/base64"
	"net/http"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
)

// me отдаёт личность и роль текущего пользователя — фронт использует это для
// guard'ов (скрыть/задизейблить действия). Окончательная проверка — всё равно на бэкенде.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	// best-effort кэш логина для аудита UI
	_ = s.d.Store.UpsertUser(r.Context(), id.Subject, id.Email, []string{string(id.Role)})

	// Публичный ключ головы — админ Target сверяет его вне системы с тем, что
	// показывает Source, чтобы убедиться, что при pairing запинился правильный.
	headPub := ""
	if pub, err := s.d.Store.HeadPublicKey(r.Context()); err == nil {
		headPub = base64.StdEncoding.EncodeToString(pub)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"subject":         id.Subject,
		"email":           id.Email,
		"role":            id.Role,
		"head_role":       s.d.Cfg.Role,
		"vault_enabled":   s.d.Cfg.Vault.Enabled,
		"head_public_key": headPub,
	})
}
