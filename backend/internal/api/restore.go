package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/itlabsio/gemini/backend/internal/auth"
	"github.com/itlabsio/gemini/backend/internal/model"
	"github.com/itlabsio/gemini/backend/internal/storage"
)

// restoreCandidatesLimit — сколько последних дампов предлагать на выбор при
// ручном restore.
const restoreCandidatesLimit = 7

type restoreCandidate struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	// Signed — рядом с дампом есть .sig (по листингу). Валидность подписи
	// проверяется при запуске restore.
	Signed bool `json:"signed"`
}

// mappingDumpPrefix — префикс дампов сопоставления с завершающим "/".
func mappingDumpPrefix(m *model.DatabaseMapping) string {
	return strings.TrimSuffix(m.S3Prefix, "/") + "/"
}

// restoreCandidates отбирает из листинга готовые дампы прямо под префиксом,
// свежие первыми, не больше limit.
func restoreCandidates(located []storage.LocatedDump, prefix string, limit int) []restoreCandidate {
	out := []restoreCandidate{}
	for _, l := range located {
		if !l.Ready || !validDumpKey(prefix, l.Key) {
			continue
		}
		out = append(out, restoreCandidate{
			Key: l.Key, Size: l.Size, LastModified: l.LastModified, Signed: l.Signed,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastModified.After(out[j].LastModified) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// listRestoreCandidates (Target, operator проекта) — последние дампы под
// префиксом сопоставления базы: из них выбирают дамп для ручного restore.
func (s *Server) listRestoreCandidates(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, _, ok := s.authorizeDatabase(w, r, id, auth.RoleOperator); !ok {
		return
	}
	m, err := s.d.Store.MappingByTargetDatabaseID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "no mapping for this target database")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s3Timeout)
	defer cancel()
	prefix := mappingDumpPrefix(m)
	located, err := s.d.Storage.LocateDumps(ctx, prefix)
	if errors.Is(err, storage.ErrNoBuckets) {
		writeError(w, http.StatusServiceUnavailable, "S3 storage is not configured")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, restoreCandidates(located, prefix, restoreCandidatesLimit))
}

type runRestoreRequest struct {
	// S3ObjectKey — выбранный дамп; пусто — самый свежий под префиксом.
	S3ObjectKey string `json:"s3_object_key"`
}

// runRestore — ручной restore целевой базы (Target): выбранный дамп или самый
// свежий. Ключ обязан лежать под префиксом сопоставления и быть подписан Source.
func (s *Server) runRestore(w http.ResponseWriter, r *http.Request, db *model.Database, initiatedBy string) {
	var req runRestoreRequest
	if r.ContentLength != 0 && !decodeJSON(w, r, &req) {
		return
	}
	m, err := s.d.Store.MappingByTargetDatabaseID(r.Context(), db.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "no mapping for this target database")
		return
	}
	prefix := mappingDumpPrefix(m)
	key := strings.TrimSpace(req.S3ObjectKey)
	if key != "" {
		// Пользователь выбирает объект бакета — граница та же, что у webhook'а:
		// только сам дамп прямо под префиксом этого сопоставления.
		if !validDumpKey(prefix, key) {
			writeError(w, http.StatusBadRequest, "s3_object_key must be a dump directly under the mapping prefix "+prefix)
			return
		}
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), s3Timeout)
		located, err := s.d.Storage.LocateDumps(ctx, prefix)
		cancel()
		if errors.Is(err, storage.ErrNoBuckets) {
			writeError(w, http.StatusServiceUnavailable, "S3 storage is not configured")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		newest := restoreCandidates(located, prefix, 1)
		if len(newest) == 0 {
			writeError(w, http.StatusNotFound, "no dumps found under mapping prefix")
			return
		}
		key = newest[0].Key
	}
	if len(s.d.Storage.ReadyClients(r.Context(), key)) == 0 {
		writeError(w, http.StatusNotFound, "dump not found in any S3 bucket configured on this side")
		return
	}
	// Сумму и список расширений берём только вместе с проверкой подписи
	// Source: ручной запуск не делает содержимое бакета доверенным.
	v, err := s.d.Operator.VerifiedDump(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusFailedDependency, "dump is not verifiably from Source: "+err.Error()+
			" — убедитесь, что пара с Source активна; дампы, снятые до установления пары, "+
			"подписываются автоматически в течение нескольких минут, либо запустите dump на Source заново")
		return
	}
	// Ручной запуск без event_key: ключ уникален навсегда (и для failed), так
	// что повтор того же дампа упирался бы в него. От параллельных запусков
	// защищает backup_runs_one_active.
	run, err := s.d.Operator.StartRestore(r.Context(), m, key, v, model.TriggerManual, "", initiatedBy)
	if err != nil {
		writeRunStartError(w, err)
		return
	}
	s.audit(r, "database.run.restore", db.ID, map[string]any{
		"s3_object_key": key, "selected": req.S3ObjectKey != "",
	})
	writeJSON(w, http.StatusAccepted, run)
}

type instanceRestorableRequest struct {
	Restorable *bool `json:"restorable"`
}

// setInstanceRestorable (Target, superuser) — предохранитель инстанса: при
// false restore в его базы запрещён полностью (и авто, и ручной).
func (s *Server) setInstanceRestorable(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req instanceRestorableRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Restorable == nil {
		writeError(w, http.StatusBadRequest, "restorable is required")
		return
	}
	inst, err := s.d.Store.SetInstanceRestorable(r.Context(), id, *req.Restorable)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	action := "instance.restore-disable"
	if *req.Restorable {
		action = "instance.restore-enable"
	}
	s.audit(r, action, id, nil)
	writeJSON(w, http.StatusOK, inst)
}
