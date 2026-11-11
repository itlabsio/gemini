package api

import (
	"encoding/base64"
	"net/http"
	"slices"

	"github.com/itlabsio/gemini/backend/internal/auth"
)

// me отдаёт личность, superuser-флаг и роли по проектам текущего пользователя — фронт использует это для
// guard'ов (скрыть/задизейблить действия). Окончательная проверка — всё равно на бэкенде.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	// best-effort кэш логина для аудита UI
	roles := []string{}
	if id.Superuser {
		roles = append(roles, "superuser")
	}
	if id.Security {
		roles = append(roles, "security")
	}
	for p, role := range id.Projects {
		roles = append(roles, p+":"+string(role))
	}
	slices.Sort(roles)
	_ = s.d.Store.UpsertUser(r.Context(), id.Subject, id.Email, roles)

	// Публичный ключ головы — админ Target сверяет его вне системы с тем, что
	// показывает Source, чтобы убедиться, что при pairing запинился правильный.
	headPub := ""
	if pub, err := s.d.Store.HeadPublicKey(r.Context()); err == nil {
		headPub = base64.StdEncoding.EncodeToString(pub)
	}

	// Для UI сопоставления: список пар (pairings) — только superuser, а узнать,
	// есть ли активная пара с Source, нужно и админу проекта.
	// sourceOffline — пара оффлайн (только ключ Source, см. initOfflinePairing):
	// UI берёт базы для сопоставления из бакета и подписывает это.
	sourcePaired, sourceOffline := false, false
	if s.d.Cfg.IsTarget() {
		p, err := s.d.Store.ActivePairing(r.Context(), "source")
		sourcePaired = err == nil
		sourceOffline = sourcePaired && p.PeerURL == ""
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"source_paired":   sourcePaired,
		"source_offline":  sourceOffline,
		"subject":         id.Subject,
		"email":           id.Email,
		"superuser":       id.Superuser,
		"security":        id.Security,
		"projects":        id.Projects,
		"head_role":       s.d.Cfg.Role,
		"vault_enabled":   s.d.Cfg.Vault.Enabled,
		"head_public_key": headPub,
		// Таймзона CronJob'ов (spec.timeZone) — подсказка к полю расписания.
		"job_timezone": s.d.Cfg.JobTimezone,
	})
}
