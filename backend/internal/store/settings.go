package store

import (
	"context"
	"encoding/json"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

// GetSettings возвращает singleton-строку глобальных дефолтов.
func (s *Store) GetSettings(ctx context.Context) (*model.Settings, error) {
	var (
		st      model.Settings
		resJSON []byte
	)
	var schedJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT default_storage_type, default_storage_size, default_resources,
		       default_pod_scheduling, job_ttl_minutes, job_pod_start_timeout_minutes,
		       updated_at, updated_by
		FROM app_settings WHERE id = TRUE`,
	).Scan(&st.DefaultStorageType, &st.DefaultStorageSize, &resJSON, &schedJSON,
		&st.JobTTLMinutes, &st.JobPodStartTimeoutMinutes, &st.UpdatedAt, &st.UpdatedBy)
	if err != nil {
		return nil, mapErr(err)
	}
	if len(resJSON) > 0 {
		_ = json.Unmarshal(resJSON, &st.DefaultResources)
	}
	if len(schedJSON) > 0 && string(schedJSON) != "{}" {
		st.DefaultPodScheduling = schedJSON
	}
	return &st, nil
}

// SettingsInput — редактируемые из UI поля.
type SettingsInput struct {
	DefaultStorageType        string
	DefaultStorageSize        string
	DefaultResources          model.ResourceSpec
	DefaultPodScheduling      json.RawMessage // JSON nodeSelector/tolerations/affinity; nil → '{}'
	JobTTLMinutes             int32
	JobPodStartTimeoutMinutes int32
}

// UpdateSettings перезаписывает singleton-строку.
func (s *Store) UpdateSettings(ctx context.Context, in SettingsInput, updatedBy string) (*model.Settings, error) {
	resJSON, err := json.Marshal(in.DefaultResources)
	if err != nil {
		return nil, err
	}
	schedJSON := "{}"
	if len(in.DefaultPodScheduling) > 0 {
		schedJSON = string(in.DefaultPodScheduling)
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE app_settings
		SET default_storage_type = $1, default_storage_size = $2, default_resources = $3,
		    default_pod_scheduling = $4, job_ttl_minutes = $5,
		    job_pod_start_timeout_minutes = $6, updated_at = NOW(), updated_by = $7
		WHERE id = TRUE`,
		in.DefaultStorageType, in.DefaultStorageSize, string(resJSON), schedJSON,
		in.JobTTLMinutes, in.JobPodStartTimeoutMinutes, updatedBy)
	if err != nil {
		return nil, err
	}
	return s.GetSettings(ctx)
}
