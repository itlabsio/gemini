package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/operator"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	id := identity(r)
	runs, err := s.d.Store.ListRuns(r.Context(), store.RunFilter{
		DatabaseID:       q.Get("database_id"),
		Kind:             model.RunKind(q.Get("kind")),
		Status:           model.RunStatus(q.Get("status")),
		Limit:            limit,
		RestrictProjects: !id.Superuser,
		Projects:         id.ProjectsAtLeast(auth.RoleViewer),
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
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
