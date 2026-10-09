package api

import "net/http"

// publicConfig отдаёт неаутентифицированному фронту минимум для отрисовки шелла —
// сейчас это роль головы, чтобы бейдж source/target был верным ещё на странице
// логина, до того как появится токен для /api/me.
func (s *Server) publicConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"head_role": s.d.Cfg.Role,
	})
}
