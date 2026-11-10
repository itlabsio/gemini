package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/itlabsio/gemini/backend/internal/model"
)

const mappingCols = `id, head_pairing_id, source_database_external_id, source_db_name,
	target_database_id, target_owner, s3_prefix, created_by, created_at, updated_at`

func scanMapping(row pgx.Row) (*model.DatabaseMapping, error) {
	var m model.DatabaseMapping
	err := row.Scan(&m.ID, &m.HeadPairingID, &m.SourceDatabaseExternalID, &m.SourceDBName,
		&m.TargetDatabaseID, &m.TargetOwner, &m.S3Prefix, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt)
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
}

// UpsertMapping создаёт/обновляет сопоставление по (pairing, source external id).
func (s *Store) UpsertMapping(ctx context.Context, in MappingInput, createdBy string) (*model.DatabaseMapping, error) {
	return scanMapping(s.pool.QueryRow(ctx, `
		INSERT INTO database_mappings
			(head_pairing_id, source_database_external_id, source_db_name,
			 target_database_id, target_owner, s3_prefix, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (head_pairing_id, source_database_external_id) DO UPDATE
		SET source_db_name = EXCLUDED.source_db_name,
		    target_database_id = EXCLUDED.target_database_id,
		    target_owner = EXCLUDED.target_owner,
		    s3_prefix = EXCLUDED.s3_prefix
		RETURNING `+mappingCols,
		in.HeadPairingID, in.SourceDatabaseExternalID, in.SourceDBName,
		in.TargetDatabaseID, in.TargetOwner, in.S3Prefix, createdBy))
}

// ListMappings — для UI.
func (s *Store) ListMappings(ctx context.Context) ([]model.DatabaseMapping, error) {
	return s.queryMappings(ctx, `SELECT `+mappingCols+` FROM database_mappings ORDER BY created_at DESC`)
}

// GetMapping возвращает сопоставление по id.
func (s *Store) GetMapping(ctx context.Context, id string) (*model.DatabaseMapping, error) {
	return scanMapping(s.pool.QueryRow(ctx,
		`SELECT `+mappingCols+` FROM database_mappings WHERE id = $1`, id))
}

// MappingByPairingExternal — сопоставление по ключу upsert'а (pairing, source
// external id); им же webhook backup-ready на target находит базу.
func (s *Store) MappingByPairingExternal(ctx context.Context, pairingID, externalID string) (*model.DatabaseMapping, error) {
	return scanMapping(s.pool.QueryRow(ctx,
		`SELECT `+mappingCols+` FROM database_mappings
		 WHERE head_pairing_id = $1 AND source_database_external_id = $2`, pairingID, externalID))
}

// MappingByTargetDatabaseID нужен для обратного webhook restore-status на Source.
func (s *Store) MappingByTargetDatabaseID(ctx context.Context, targetDatabaseID string) (*model.DatabaseMapping, error) {
	return scanMapping(s.pool.QueryRow(ctx,
		`SELECT `+mappingCols+` FROM database_mappings
		 WHERE target_database_id = $1 ORDER BY created_at DESC LIMIT 1`, targetDatabaseID))
}

// AutoRestoreMappings — сопоставления, по которым идёт автовосстановление:
// целевая база присутствует, включена (флаг базы на Target = «авто») и её
// инстанс restorable. Для fallback-поллинга S3.
func (s *Store) AutoRestoreMappings(ctx context.Context) ([]model.DatabaseMapping, error) {
	return s.queryMappings(ctx, `
		SELECT `+mappingCols+` FROM database_mappings
		WHERE target_database_id IN (
			SELECT d.id FROM databases d JOIN instances i ON i.id = d.instance_id
			WHERE d.enabled AND d.present AND i.restorable)
		ORDER BY created_at DESC`)
}

// TemplateMappings — сопоставления, чьим целевым базам нужен restore-шаблон
// (CronJob) и секреты кред: база присутствует. Флаги не учитываются — ни
// автовосстановление (ручной restore работает и без него), ни restorable
// инстанса: Job восстановления принадлежит CronJob'у, и удаление шаблона
// каскадом убило бы уже идущий restore. Новые запуски отсекает StartRestore.
func (s *Store) TemplateMappings(ctx context.Context) ([]model.DatabaseMapping, error) {
	return s.queryMappings(ctx, `
		SELECT `+mappingCols+` FROM database_mappings
		WHERE target_database_id IN (SELECT id FROM databases WHERE present)
		ORDER BY created_at DESC`)
}

func (s *Store) queryMappings(ctx context.Context, sql string, args ...any) ([]model.DatabaseMapping, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
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

// DeleteMapping разрывает сопоставление. Restore-CronJob целевой базы убирает
// очередной reconcileTemplates — его больше нет среди TemplateMappings.
func (s *Store) DeleteMapping(ctx context.Context, id string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM database_mappings WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
