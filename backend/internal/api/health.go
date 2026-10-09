package api

import (
	"log"
	"net/http"
)

// livez — liveness: процесс жив и обслуживает HTTP. Никаких внешних зависимостей.
func (s *Server) livez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"role":   string(s.d.Cfg.Role),
	})
}

// readyz — readiness: доступность своей БД, S3 и (если включён) Vault (см. Deps.Ready).
func (s *Server) readyz(w http.ResponseWriter, _ *http.Request) {
	if s.d.Ready != nil {
		if err := s.d.Ready(); err != nil {
			log.Printf("api: readyz failed: %v", err)
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
