package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/k8s"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

// Границы TTL завершённых Job'ов (минуты) — как CHECK в миграции 015. Минимум —
// чтобы watcher успел увидеть терминальный статус и забрать логи упавших подов.
const (
	minJobTTLMinutes = 5
	maxJobTTLMinutes = 7 * 24 * 60
)

// Границы таймаута старта пода (минуты) — как CHECK в миграции 016.
const (
	minPodStartTimeoutMinutes = 1
	maxPodStartTimeoutMinutes = 24 * 60
)

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.d.Store.GetSettings(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type settingsRequest struct {
	DefaultStorageType   string             `json:"default_storage_type"`
	DefaultStorageSize   string             `json:"default_storage_size"`
	DefaultResources     model.ResourceSpec `json:"default_resources"`
	DefaultPodScheduling json.RawMessage    `json:"default_pod_scheduling"`
	// 0 / не передан → текущее значение не меняется (старые клиенты).
	JobTTLMinutes             int32 `json:"job_ttl_minutes"`
	JobPodStartTimeoutMinutes int32 `json:"job_pod_start_timeout_minutes"`
}

// putSettings — админка: дефолты типа/размера временного хранилища, ресурсов и TTL Job'ов.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	switch model.StorageType(req.DefaultStorageType) {
	case model.StorageEphemeral, model.StorageEmptyDir:
	default:
		writeError(w, http.StatusBadRequest, "default_storage_type must be 'ephemeral' or 'emptydir'")
		return
	}
	if req.DefaultStorageSize == "" {
		writeError(w, http.StatusBadRequest, "default_storage_size is required (e.g. 20Gi)")
		return
	}
	if err := k8s.ValidStorageSize(req.DefaultStorageSize); err != nil {
		writeError(w, http.StatusBadRequest, "default_storage_size: "+err.Error())
		return
	}
	for label, v := range map[string]string{
		"default_resources.requests.cpu":    req.DefaultResources.Requests.CPU,
		"default_resources.requests.memory": req.DefaultResources.Requests.Memory,
		"default_resources.limits.cpu":      req.DefaultResources.Limits.CPU,
		"default_resources.limits.memory":   req.DefaultResources.Limits.Memory,
	} {
		if v == "" {
			continue
		}
		if err := k8s.ValidQuantity(v); err != nil {
			writeError(w, http.StatusBadRequest, label+" must be a k8s quantity: "+err.Error())
			return
		}
	}
	sched := req.DefaultPodScheduling
	if len(sched) > 0 && string(sched) != "null" {
		if _, err := k8s.ParsePodOverrides(string(sched)); err != nil {
			writeError(w, http.StatusBadRequest, "default_pod_scheduling: "+err.Error())
			return
		}
	} else {
		sched = nil
	}
	ttl, podStart := req.JobTTLMinutes, req.JobPodStartTimeoutMinutes
	if ttl == 0 || podStart == 0 {
		cur, err := s.d.Store.GetSettings(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if ttl == 0 {
			ttl = cur.JobTTLMinutes
		}
		if podStart == 0 {
			podStart = cur.JobPodStartTimeoutMinutes
		}
	}
	if ttl < minJobTTLMinutes || ttl > maxJobTTLMinutes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"job_ttl_minutes must be between %d and %d", minJobTTLMinutes, maxJobTTLMinutes))
		return
	}
	if podStart < minPodStartTimeoutMinutes || podStart > maxPodStartTimeoutMinutes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"job_pod_start_timeout_minutes must be between %d and %d", minPodStartTimeoutMinutes, maxPodStartTimeoutMinutes))
		return
	}
	id, _ := ctxIdentity(r)
	st, err := s.d.Store.UpdateSettings(r.Context(), store.SettingsInput{
		DefaultStorageType:        req.DefaultStorageType,
		DefaultStorageSize:        req.DefaultStorageSize,
		DefaultResources:          req.DefaultResources,
		DefaultPodScheduling:      sched,
		JobTTLMinutes:             ttl,
		JobPodStartTimeoutMinutes: podStart,
	}, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "settings.update", "app_settings", nil)
	writeJSON(w, http.StatusOK, st)
}
