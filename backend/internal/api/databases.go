package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/k8s"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/operator"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

func (s *Server) listDatabases(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeInstance(w, r, mux.Vars(r)["id"], auth.RoleViewer); !ok {
		return
	}
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
	// RestoreUsername / RestorePassword — креды restore базы (только Target,
	// режим инстанса in_place_per_db). Пустой username удаляет креды; пустой
	// пароль при заданном username — «не менять сохранённый».
	RestoreUsername *string `json:"restore_username"`
	RestorePassword *string `json:"restore_password"`
}

// patchDatabase меняет enabled / schedule_cron / storage_* / restore-креды.
// schedule_cron — только на Source, restore-креды — только на Target.
func (s *Server) patchDatabase(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, _, ok := s.authorizeDatabase(w, r, id, auth.RoleAdmin); !ok {
		return
	}
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
	if req.RestorePassword != nil && req.RestoreUsername == nil {
		writeError(w, http.StatusBadRequest, "restore_password requires restore_username")
		return
	}
	if req.RestoreUsername != nil {
		if !s.d.Cfg.IsTarget() {
			writeError(w, http.StatusBadRequest, "restore credentials are used only on target head")
			return
		}
		user := strings.TrimSpace(*req.RestoreUsername)
		pass := ""
		if req.RestorePassword != nil {
			pass = *req.RestorePassword
		}
		// Иначе для несуществующей базы INSERT упал бы на FK как 500.
		if _, err := s.d.Store.GetDatabase(r.Context(), id); err != nil {
			writeStoreError(w, err)
			return
		}
		var err error
		switch {
		case user == "":
			err = s.d.Store.DeleteDatabaseCredentials(r.Context(), id)
		case !validRoleName(user):
			writeError(w, http.StatusBadRequest, "restore_username is not a valid PostgreSQL role name")
			return
		default:
			err = s.d.Store.SetDatabaseCredentials(r.Context(), id, user, pass)
		}
		if err != nil {
			writeStoreError(w, err)
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
	// Пароль в аудит не пишем — только факт его смены.
	s.audit(r, "database.patch", id, map[string]any{"enabled": req.Enabled, "schedule_cron": req.ScheduleCron,
		"restore_username": req.RestoreUsername, "restore_password_changed": req.RestorePassword != nil && *req.RestorePassword != ""})
	writeJSON(w, http.StatusOK, db)
}

// runDatabase — ручной запуск: dump на Source, restore на DR.
// Если для базы уже есть активный Job — запуск запрещён (409).
func (s *Server) runDatabase(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	db, _, ok := s.authorizeDatabase(w, r, id, auth.RoleOperator)
	if !ok {
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
	// Сумму и список расширений берём только вместе с проверкой подписи
	// Source: ручной запуск не делает содержимое бакета доверенным.
	v, err := s.d.Operator.VerifiedDump(r.Context(), newest.Key)
	if err != nil {
		writeError(w, http.StatusFailedDependency, "dump is not verifiably from Source: "+err.Error()+
			" — убедитесь, что пара с Source активна; дампы, снятые до установления пары, "+
			"подписываются автоматически в течение нескольких минут, либо запустите dump на Source заново")
		return
	}
	// Ручной запуск без event_key: ключ уникален навсегда (и для failed), так
	// что повтор того же дампа упирался бы в него. От параллельных запусков
	// защищает backup_runs_one_active.
	run, err := s.d.Operator.StartRestore(r.Context(), m, newest.Key, v, model.TriggerManual, "")
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
	case errors.Is(err, operator.ErrDatabaseDisabled):
		writeError(w, http.StatusConflict, "restore is disabled for this target database")
	case errors.Is(err, operator.ErrNoRestoreCredentials):
		writeError(w, http.StatusConflict,
			"restore user is not set for this target database — instance is in in_place_per_db mode, set it on the instance page")
	case errors.Is(err, storage.ErrNoBuckets):
		writeError(w, http.StatusServiceUnavailable, "no S3 buckets configured")
	default:
		writeStoreError(w, err)
	}
}
