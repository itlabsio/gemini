package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/operator"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

// Размер страницы истории прогонов — как у журнала аудита.
const (
	runsPageDefault = 50
	runsPageMax     = 200
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// runCursor — курсор страницы для клиента: "<created_at в мкс>_<id>".
// Микросекунды — точность timestamptz, сравнение в SQL без потерь.
func runCursor(r model.BackupRun) string {
	return strconv.FormatInt(r.CreatedAt.UnixMicro(), 10) + "_" + r.ID
}

func parseRunCursor(s string) (*store.RunCursor, bool) {
	us, id, ok := strings.Cut(s, "_")
	if !ok || !uuidRe.MatchString(id) {
		return nil, false
	}
	n, err := strconv.ParseInt(us, 10, 64)
	if err != nil || n <= 0 {
		return nil, false
	}
	return &store.RunCursor{CreatedAt: time.UnixMicro(n), ID: id}, true
}

// listRuns — страница истории прогонов (от новых к старым) и курсор
// следующей: next_before пустой — дальше прогонов нет.
func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.RunFilter{
		DatabaseID: q.Get("database_id"),
		Kind:       model.RunKind(q.Get("kind")),
		Status:     model.RunStatus(q.Get("status")),
		Limit:      runsPageDefault,
	}
	if f.DatabaseID != "" && !uuidRe.MatchString(f.DatabaseID) {
		writeError(w, http.StatusBadRequest, "database_id must be a UUID")
		return
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > runsPageMax {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be 1..%d", runsPageMax))
			return
		}
		f.Limit = n
	}
	if v := q.Get("before"); v != "" {
		c, ok := parseRunCursor(v)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid before cursor")
			return
		}
		f.Before = c
	}
	id := identity(r)
	f.RestrictProjects, f.Projects = !id.Superuser, id.ProjectsAtLeast(auth.RoleViewer)
	runs, err := s.d.Store.ListRuns(r.Context(), f)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	next := ""
	if len(runs) == f.Limit {
		next = runCursor(runs[len(runs)-1])
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs, "next_before": next})
}

// listActiveRuns отдаёт все прогоны в статусе pending/running — таблица баз
// на странице инстанса опрашивает его, чтобы показывать «выполняется» и
// блокировать кнопку запуска, пока бэкап не закончится.
func (s *Server) listActiveRuns(w http.ResponseWriter, r *http.Request) {
	id := identity(r)
	runs, err := s.d.Store.ListActiveRunsInProjects(r.Context(), id.ProjectsAtLeast(auth.RoleViewer), id.Superuser)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

// listRecentRuns — последние per_db прогонов каждой базы инстанса: полоска
// успешных/упавших запусков в таблице баз.
func (s *Server) listRecentRuns(w http.ResponseWriter, r *http.Request) {
	instID := mux.Vars(r)["id"]
	if _, ok := s.authorizeInstance(w, r, instID, auth.RoleViewer); !ok {
		return
	}
	perDB, _ := strconv.Atoi(r.URL.Query().Get("per_db"))
	if perDB <= 0 || perDB > 50 {
		perDB = 20
	}
	runs, err := s.d.Store.RecentRunsByInstance(r.Context(), instID, perDB)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.d.Store.GetRun(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.authorizeRun(w, r, run, auth.RoleViewer) {
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// authorizeRun проверяет доступ к прогону через проект инстанса его базы.
func (s *Server) authorizeRun(w http.ResponseWriter, r *http.Request, run *model.BackupRun, min auth.Role) bool {
	_, _, ok := s.authorizeDatabase(w, r, run.DatabaseID, min)
	return ok
}

// cancelRun — остановка активного прогона: Job удаляется, прогон → failed.
func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	cur, err := s.d.Store.GetRun(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.authorizeRun(w, r, cur, auth.RoleOperator) {
		return
	}
	idn, _ := ctxIdentity(r)
	run, err := s.d.Operator.CancelRun(r.Context(), id, idn)
	if errors.Is(err, operator.ErrRunNotActive) {
		writeError(w, http.StatusConflict, "run is already finished")
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "run.cancel", id, nil)
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) listPairings(w http.ResponseWriter, r *http.Request) {
	// Ленивая уборка: протухший pending (код не использован в TTL) удаляется,
	// чтобы не висел в списке навсегда.
	if n, err := s.d.Store.PurgeExpiredPendingPairings(r.Context()); err != nil {
		log.Printf("api: purge expired pending pairings: %v", err)
	} else if n > 0 {
		log.Printf("api: purged %d expired pending pairing(s)", n)
	}

	items, err := s.d.Store.ListPairings(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
