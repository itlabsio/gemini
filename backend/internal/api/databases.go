package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/k8s"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/operator"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

func (s *Server) listDatabases(w http.ResponseWriter, r *http.Request) {
	dbs, err := s.d.Store.ListDatabases(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dbs)
}

type databasePatchRequest struct {
	Enabled      *bool   `json:"enabled"`
	ScheduleCron *string `json:"schedule_cron"`
	StorageType  *string `json:"storage_type"`
	StorageSize  *string `json:"storage_size"`
}

// patchDatabase меняет enabled / schedule_cron / storage_*. schedule_cron — только на Source.
func (s *Server) patchDatabase(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req databasePatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ScheduleCron != nil {
		if s.d.Cfg.IsTarget() {
			writeError(w, http.StatusBadRequest, "schedule_cron is not used on target head")
			return
		}
		if *req.ScheduleCron != "" && !validCron(*req.ScheduleCron) {
			writeError(w, http.StatusBadRequest, "schedule_cron must have 5 fields")
			return
		}
	}
	if req.StorageType != nil && *req.StorageType != "" &&
		*req.StorageType != "ephemeral" && *req.StorageType != "emptydir" {
		writeError(w, http.StatusBadRequest, "storage_type must be '', 'ephemeral' or 'emptydir'")
		return
	}
	// Кривой размер доезжал до PodSpec и ронял реконсилер паникой — валидируем на входе.
	if req.StorageSize != nil && *req.StorageSize != "" {
		if err := k8s.ValidStorageSize(*req.StorageSize); err != nil {
			writeError(w, http.StatusBadRequest, "storage_size: "+err.Error())
			return
		}
	}
	db, err := s.d.Store.PatchDatabase(r.Context(), id, store.DatabasePatch{
		Enabled:      req.Enabled,
		ScheduleCron: req.ScheduleCron,
		StorageType:  req.StorageType,
		StorageSize:  req.StorageSize,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "database.patch", id, map[string]any{"enabled": req.Enabled, "schedule_cron": req.ScheduleCron})
	writeJSON(w, http.StatusOK, db)
}

// runDatabase — ручной запуск: dump на Source, restore на DR.
// Если для базы уже есть активный Job — запуск запрещён (409).
func (s *Server) runDatabase(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	db, err := s.d.Store.GetDatabase(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	idn, _ := ctxIdentity(r)

	if s.d.Cfg.IsSource() {
		run, err := s.d.Operator.StartDump(r.Context(), db, model.TriggerManual, idn)
		if err != nil {
			writeRunStartError(w, err)
			return
		}
		s.audit(r, "database.run.dump", id, nil)
		writeJSON(w, http.StatusAccepted, run)
		return
	}

	// DR: ручной restore из последнего доступного дампа по сопоставлению.
	m, err := s.d.Store.MappingByTargetDatabaseID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "no mapping for this target database")
		return
	}
	objects, err := s.d.Storage.ListDumps(r.Context(), strings.TrimSuffix(m.S3Prefix, "/")+"/")
	if errors.Is(err, storage.ErrNoBuckets) {
		writeError(w, http.StatusServiceUnavailable, "S3 storage is not configured")
		return
	}
	if err != nil || len(objects) == 0 {
		writeError(w, http.StatusNotFound, "no dumps found under mapping prefix")
		return
	}
	newest := objects[0]
	for _, o := range objects {
		if o.LastModified.After(newest.LastModified) {
			newest = o
		}
	}
	// Сумму берём только вместе с проверкой подписи Source: ручной запуск не
	// делает содержимое бакета доверенным.
	checksum, bucketID, err := s.d.Operator.VerifiedChecksum(r.Context(), newest.Key)
	if err != nil {
		writeError(w, http.StatusFailedDependency, "dump is not verifiably from Source: "+err.Error()+
			" — убедитесь, что пара с Source активна; дампы, снятые до установления пары, "+
			"подписываются автоматически в течение нескольких минут, либо запустите dump на Source заново")
		return
	}
	run, err := s.d.Operator.StartRestore(r.Context(), m, newest.Key, checksum, bucketID,
		model.TriggerManual, operator.EventKeyFor("restore", newest.Key)+":manual")
	if err != nil {
		writeRunStartError(w, err)
		return
	}
	s.audit(r, "database.run.restore", id, map[string]any{"s3_object_key": newest.Key})
	writeJSON(w, http.StatusAccepted, run)
}

func writeRunStartError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "a job for this database is already active")
	case errors.Is(err, operator.ErrNoK8s):
		writeError(w, http.StatusServiceUnavailable, "kubernetes client is not configured")
	case errors.Is(err, storage.ErrNoBuckets):
		writeError(w, http.StatusServiceUnavailable, "no S3 buckets configured")
	default:
		writeStoreError(w, err)
	}
}
