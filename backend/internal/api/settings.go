package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"

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

// Границы сроков хранения аудита и истории прогонов (дни) — как CHECK в миграции 023.
const (
	minRetentionDays = 14
	maxRetentionDays = 3650
)

// settingsResponse — настройки плюс то, что действует при пустых полях: значения
// из values чарта (job.*, JOB_POD_OVERRIDES). UI показывает их подсказками.
type settingsResponse struct {
	*model.Settings
	// ChartPodScheduling — nodeSelector/tolerations/affinity чарта; nil, если не заданы.
	ChartPodScheduling *k8s.PodOverrides `json:"chart_pod_scheduling,omitempty"`
	// ChartResources — ресурсы чарта, а без них — встроенные дефолты бэкенда.
	ChartResources model.ResourceSpec `json:"chart_resources"`
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.d.Store.GetSettings(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	resp := settingsResponse{Settings: st}
	o, _ := k8s.ParsePodOverrides(s.d.Cfg.JobPodOverridesJSON) // битый JSON не даст стартовать оператору
	resp.ChartResources = resourceSpecOf(o.ContainerResources())
	if len(o.NodeSelector) > 0 || len(o.Tolerations) > 0 || o.Affinity != nil {
		o.Resources = nil // у ресурсов своя карточка — chart_resources
		resp.ChartPodScheduling = &o
	}
	writeJSON(w, http.StatusOK, resp)
}

// resourceSpecOf — requests/limits CPU и памяти в строковом виде (500m, 2Gi).
func resourceSpecOf(rr corev1.ResourceRequirements) model.ResourceSpec {
	q := func(l corev1.ResourceList) model.ResourceQuantities {
		var out model.ResourceQuantities
		if v, ok := l[corev1.ResourceCPU]; ok {
			out.CPU = v.String()
		}
		if v, ok := l[corev1.ResourceMemory]; ok {
			out.Memory = v.String()
		}
		return out
	}
	return model.ResourceSpec{Requests: q(rr.Requests), Limits: q(rr.Limits)}
}

type settingsRequest struct {
	DefaultStorageType   string             `json:"default_storage_type"`
	DefaultStorageSize   string             `json:"default_storage_size"`
	DefaultResources     model.ResourceSpec `json:"default_resources"`
	DefaultPodScheduling json.RawMessage    `json:"default_pod_scheduling"`
	// 0 / не передан → текущее значение не меняется (старые клиенты).
	JobTTLMinutes             int32 `json:"job_ttl_minutes"`
	JobPodStartTimeoutMinutes int32 `json:"job_pod_start_timeout_minutes"`
	// nil / не передан → текущее значение не меняется (старые клиенты).
	NotifyOnFailure *bool `json:"notify_on_failure"`
	NotifyOnSuccess *bool `json:"notify_on_success"`
	// 0 / не передан → текущее значение не меняется (старые клиенты).
	AuditRetentionDays int32 `json:"audit_retention_days"`
	RunsRetentionDays  int32 `json:"runs_retention_days"`
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
	// Поля накладываются на ресурсы чарта поштучно — проверяем итог, а не только ввод.
	base, _ := k8s.ParsePodOverrides(s.d.Cfg.JobPodOverridesJSON)
	dr := req.DefaultResources
	if err := base.WithResources(dr.Requests.CPU, dr.Requests.Memory, dr.Limits.CPU, dr.Limits.Memory).
		ValidateResources(); err != nil {
		writeError(w, http.StatusBadRequest, "default_resources: "+err.Error())
		return
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
	onFailure, onSuccess := cur.NotifyOnFailure, cur.NotifyOnSuccess
	if req.NotifyOnFailure != nil {
		onFailure = *req.NotifyOnFailure
	}
	if req.NotifyOnSuccess != nil {
		onSuccess = *req.NotifyOnSuccess
	}
	if ttl < minJobTTLMinutes || ttl > maxJobTTLMinutes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"job_ttl_minutes must be between %d and %d", minJobTTLMinutes, maxJobTTLMinutes))
		return
	}
	auditDays, runsDays := req.AuditRetentionDays, req.RunsRetentionDays
	if auditDays == 0 {
		auditDays = cur.AuditRetentionDays
	}
	if runsDays == 0 {
		runsDays = cur.RunsRetentionDays
	}
	for label, v := range map[string]int32{"audit_retention_days": auditDays, "runs_retention_days": runsDays} {
		if v < minRetentionDays || v > maxRetentionDays {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"%s must be between %d and %d", label, minRetentionDays, maxRetentionDays))
			return
		}
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
		NotifyOnFailure:           onFailure,
		NotifyOnSuccess:           onSuccess,
		AuditRetentionDays:        auditDays,
		RunsRetentionDays:         runsDays,
	}, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "settings.update", "app_settings", nil)
	writeJSON(w, http.StatusOK, st)
}
