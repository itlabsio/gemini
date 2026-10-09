package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

const mappingCols = `id, head_pairing_id, source_database_external_id, source_db_name,
	target_database_id, target_owner, s3_prefix, enabled, created_by, created_at, updated_at`

func scanMapping(row pgx.Row) (*model.DatabaseMapping, error) {
	var m model.DatabaseMapping
	err := row.Scan(&m.ID, &m.HeadPairingID, &m.SourceDatabaseExternalID, &m.SourceDBName,
		&m.TargetDatabaseID, &m.TargetOwner, &m.S3Prefix, &m.Enabled, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &m, nil
}

// MappingInput — данные сопоставления source-базы с локальной target-базой.
type MappingInput struct {
	HeadPairingID            string
	SourceDatabaseExternalID string
	SourceDBName             string
	TargetDatabaseID         string
	TargetOwner              string
	S3Prefix                 string
	Enabled                  bool
}

// UpsertMapping создаёт/обновляет сопоставление по (pairing, source external id).
func (s *Store) UpsertMapping(ctx context.Context, in MappingInput, createdBy string) (*model.DatabaseMapping, error) {
	return scanMapping(s.pool.QueryRow(ctx, `
		INSERT INTO database_mappings
			(head_pairing_id, source_database_external_id, source_db_name,
			 target_database_id, target_owner, s3_prefix, enabled, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (head_pairing_id, source_database_external_id) DO UPDATE
		SET source_db_name = EXCLUDED.source_db_name,
		    target_database_id = EXCLUDED.target_database_id,
		    target_owner = EXCLUDED.target_owner,
		    s3_prefix = EXCLUDED.s3_prefix,
		    enabled = EXCLUDED.enabled
		RETURNING `+mappingCols,
		in.HeadPairingID, in.SourceDatabaseExternalID, in.SourceDBName,
		in.TargetDatabaseID, in.TargetOwner, in.S3Prefix, in.Enabled, createdBy))
}

// ListMappings — для UI.
func (s *Store) ListMappings(ctx context.Context) ([]model.DatabaseMapping, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+mappingCols+` FROM database_mappings ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DatabaseMapping{}
	for rows.Next() {
		m, err := scanMapping(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// MappingBySourceExternalID нужен обработчику webhook backup-ready на DR.
func (s *Store) MappingBySourceExternalID(ctx context.Context, externalID string) (*model.DatabaseMapping, error) {
	return scanMapping(s.pool.QueryRow(ctx,
		`SELECT `+mappingCols+` FROM database_mappings
		 WHERE source_database_external_id = $1 AND enabled`, externalID))
}

// MappingByTargetDatabaseID нужен для обратного webhook restore-status на Source.
func (s *Store) MappingByTargetDatabaseID(ctx context.Context, targetDatabaseID string) (*model.DatabaseMapping, error) {
	return scanMapping(s.pool.QueryRow(ctx,
		`SELECT `+mappingCols+` FROM database_mappings
		 WHERE target_database_id = $1 ORDER BY created_at DESC LIMIT 1`, targetDatabaseID))
}

// EnabledMappings — активные сопоставления (для fallback-поллинга S3 на DR).
func (s *Store) EnabledMappings(ctx context.Context) ([]model.DatabaseMapping, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+mappingCols+` FROM database_mappings WHERE enabled ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DatabaseMapping{}
	for rows.Next() {
		m, err := scanMapping(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}
