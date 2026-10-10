package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/operator"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	runs, err := s.d.Store.ListRuns(r.Context(), store.RunFilter{
		DatabaseID: q.Get("database_id"),
		Kind:       model.RunKind(q.Get("kind")),
		Status:     model.RunStatus(q.Get("status")),
		Limit:      limit,
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
	runs, err := s.d.Store.ListActiveRuns(r.Context(), 0)
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
	writeJSON(w, http.StatusOK, run)
}

// cancelRun — остановка активного прогона: Job удаляется, прогон → failed.
func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
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
