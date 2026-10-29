package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

// actionAccessDenied — отказ в доступе (403) к /api/*.
const actionAccessDenied = "access.denied"

// audit пишет строку в audit_log от имени текущего пользователя (best-effort).
func (s *Server) audit(r *http.Request, action, target string, detail map[string]any) {
	id, _ := auth.FromContext(r.Context())
	s.writeAudit(r, id, action, target, detail)
}

func (s *Server) writeAudit(r *http.Request, id auth.Identity, action, target string, detail map[string]any) {
	actor := id.Email
	if actor == "" {
		actor = id.Subject
	}
	var raw []byte
	if detail != nil {
		raw, _ = json.Marshal(detail)
	}
	if err := s.d.Store.WriteAudit(r.Context(), store.AuditEntry{
		Actor: actor, Action: action, Target: target, Detail: raw,
		IP: clientIP(r), UserAgent: truncate(r.UserAgent(), 512),
	}); err != nil {
		log.Printf("api: audit write failed: %v", err)
	}
}

// clientIP — адрес пользователя. Браузерные запросы приходят либо через ingress
// (nginx ставит X-Real-IP, перезаписывая присланный клиентом), либо от
// frontend'а, который пересылает X-Real-IP входящего запроса (lib/api.ts).
// Без заголовка — адрес соединения.
func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" && net.ParseIP(ip) != nil {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// auditForbidden — отказ verifier'а валидному токену без доступа к Gemini
// (auth.Verifier.OnForbidden).
func (s *Server) auditForbidden(r *http.Request, id auth.Identity, reason string) {
	s.writeAudit(r, id, actionAccessDenied, r.Method+" "+r.URL.Path, map[string]any{"reason": reason})
}

// auditDenied пишет в журнал ответы 403 обработчиков /api/* (роль ниже
// требуемой, чужой проект). Ставится после verifier'а — Identity уже в контексте.
func (s *Server) auditDenied(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status != http.StatusForbidden {
			return
		}
		var body struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.body.Bytes(), &body)
		s.audit(r, actionAccessDenied, r.Method+" "+r.URL.Path, map[string]any{"reason": body.Error})
	})
}

// statusRecorder запоминает код ответа и, для 403, начало тела (причину отказа).
type statusRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.status == http.StatusForbidden && w.body.Len() < 1024 {
		w.body.Write(b[:min(len(b), 1024-w.body.Len())])
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap — для http.ResponseController (Flush при потоковой выдаче).
func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }
