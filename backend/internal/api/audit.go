package api

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
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

// Выборка журнала: страница по умолчанию и максимум; экспорт — не больше
// auditExportMax строк (дальше — сузить период).
const (
	auditPageDefault = 50
	auditPageMax     = 200
	auditExportMax   = 100_000
)

// auditFilter разбирает фильтры из query: before (id), from/to (RFC3339),
// actor, action (через запятую), q.
func auditFilter(r *http.Request) (store.AuditFilter, string) {
	q := r.URL.Query()
	f := store.AuditFilter{Actor: q.Get("actor"), Query: q.Get("q")}
	if v := q.Get("before"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			return f, "before must be a positive record id"
		}
		f.BeforeID = id
	}
	for key, dst := range map[string]*time.Time{"from": &f.From, "to": &f.To} {
		if v := q.Get(key); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return f, key + " must be RFC3339 (e.g. 2026-10-29T00:00:00Z)"
			}
			*dst = t
		}
	}
	for a := range strings.SplitSeq(q.Get("action"), ",") {
		if a = strings.TrimSpace(a); a != "" {
			f.Actions = append(f.Actions, a)
		}
	}
	return f, ""
}

// listAudit — страница журнала (superuser / security). next_before — курсор
// следующей страницы, 0 — записей больше нет.
func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	f, msg := auditFilter(r)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	f.Limit = auditPageDefault
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > auditPageMax {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be 1..%d", auditPageMax))
			return
		}
		f.Limit = n
	}
	items, err := s.d.Store.ListAudit(r.Context(), f)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next int64
	if len(items) == f.Limit {
		next = items[len(items)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_before": next})
}

// listAuditActors — пользователи из журнала, для фильтра.
func (s *Server) listAuditActors(w http.ResponseWriter, r *http.Request) {
	actors, err := s.d.Store.AuditActors(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, actors)
}

// exportAudit отдаёт выборку CSV-потоком (те же фильтры, без пагинации).
// Сам экспорт попадает в журнал — с фильтрами, по которым выгружали.
func (s *Server) exportAudit(w http.ResponseWriter, r *http.Request) {
	f, msg := auditFilter(r)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	f.BeforeID = 0
	f.Limit = auditExportMax
	s.audit(r, "audit.export", "", map[string]any{"filter": r.URL.RawQuery})

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="gemini-audit-%s.csv"`, time.Now().UTC().Format("20060102T150405Z")))
	// BOM — чтобы Excel открыл UTF-8 без танцев с импортом.
	_, _ = w.Write([]byte("\ufeff"))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "time_utc", "actor", "action", "object", "target", "ip", "user_agent", "detail"})
	err := s.d.Store.EachAudit(r.Context(), f, func(rec model.AuditRecord) error {
		return cw.Write([]string{
			strconv.FormatInt(rec.ID, 10), rec.CreatedAt.UTC().Format(time.RFC3339), rec.Actor, rec.Action,
			rec.ObjectName, rec.Target, rec.IP, rec.UserAgent, string(rec.Detail),
		})
	})
	cw.Flush()
	if err != nil {
		// Заголовки уже ушли — код ответа не поменять; обрыв виден по логу.
		log.Printf("api: audit export: %v", err)
	}
}
