package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

const databaseCols = `id, instance_id, db_name, enabled, schedule_cron, storage_type,
	storage_size, external_id, present, created_at, updated_at`

func scanDatabase(row pgx.Row) (*model.Database, error) {
	var d model.Database
	err := row.Scan(&d.ID, &d.InstanceID, &d.DBName, &d.Enabled, &d.ScheduleCron,
		&d.StorageType, &d.StorageSize, &d.ExternalID, &d.Present, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &d, nil
}

// ListDatabases возвращает базы инстанса, кроме исключённых из discovery:
// postgres (всегда) и заданные в instances.excluded_databases. Такие базы не
// бэкапятся и в UI их видеть незачем — даже если строка осталась от старого
// discovery. Планировщик читает ListEnabledDatabases и сюда не заглядывает.
func (s *Store) ListDatabases(ctx context.Context, instanceID string) ([]model.Database, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+databaseCols+`
		FROM databases d
		WHERE d.instance_id = $1
		  AND d.db_name <> 'postgres'
		  AND d.db_name <> ALL (
		      SELECT unnest(excluded_databases) FROM instances WHERE id = $1)
		ORDER BY d.db_name`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectDatabases(rows)
}

// ListEnabledDatabases возвращает включённые базы (для scheduler и для sync с DR).
func (s *Store) ListEnabledDatabases(ctx context.Context) ([]model.Database, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+databaseCols+` FROM databases WHERE enabled AND present ORDER BY db_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectDatabases(rows)
}

func collectDatabases(rows pgx.Rows) ([]model.Database, error) {
	out := []model.Database{}
	for rows.Next() {
		d, err := scanDatabase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// GetDatabase возвращает одну базу.
func (s *Store) GetDatabase(ctx context.Context, id string) (*model.Database, error) {
	return scanDatabase(s.pool.QueryRow(ctx,
		`SELECT `+databaseCols+` FROM databases WHERE id = $1`, id))
}

// GetDatabaseByExternalID нужен обработчикам webhook на DR.
func (s *Store) GetDatabaseByExternalID(ctx context.Context, externalID string) (*model.Database, error) {
	return scanDatabase(s.pool.QueryRow(ctx,
		`SELECT `+databaseCols+` FROM databases WHERE external_id = $1`, externalID))
}

// SyncDiscovered синхронизирует список баз инстанса с результатом discovery:
// добавляет новые (enabled=false), помечает исчезнувшие present=false,
// обновляет server_version_num инстанса (если discovery его прочитал),
// возвращает актуальный список.
func (s *Store) SyncDiscovered(ctx context.Context, instanceID string, dbNames []string, serverVersionNum int) ([]model.Database, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op после Commit

	if serverVersionNum > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE instances SET server_version_num = $2 WHERE id = $1`,
			instanceID, serverVersionNum); err != nil {
			return nil, err
		}
	}

	for _, name := range dbNames {
		if _, err := tx.Exec(ctx, `
			INSERT INTO databases (instance_id, db_name, present)
			VALUES ($1, $2, TRUE)
			ON CONFLICT (instance_id, db_name) DO UPDATE SET present = TRUE`,
			instanceID, name); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE databases SET present = FALSE
		WHERE instance_id = $1 AND NOT (db_name = ANY($2))`,
		instanceID, dbNames); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.ListDatabases(ctx, instanceID)
}

// DatabasePatch — изменяемые из UI поля базы.
type DatabasePatch struct {
	Enabled      *bool
	ScheduleCron *string
	StorageType  *string
	StorageSize  *string
}

// PatchDatabase применяет частичное обновление (enabled / schedule_cron / storage_*).
func (s *Store) PatchDatabase(ctx context.Context, id string, p DatabasePatch) (*model.Database, error) {
	set := ""
	args := []any{id}
	if p.Enabled != nil {
		args = append(args, *p.Enabled)
		set += ", enabled = $" + itoa(len(args))
	}
	if p.ScheduleCron != nil {
		args = append(args, *p.ScheduleCron)
		set += ", schedule_cron = $" + itoa(len(args))
	}
	if p.StorageType != nil {
		args = append(args, *p.StorageType)
		set += ", storage_type = $" + itoa(len(args))
	}
	if p.StorageSize != nil {
		args = append(args, *p.StorageSize)
		set += ", storage_size = $" + itoa(len(args))
	}
	if set == "" {
		return s.GetDatabase(ctx, id)
	}
	// срезаем ведущую ", "
	q := `UPDATE databases SET ` + set[2:] + ` WHERE id = $1 RETURNING ` + databaseCols
	return scanDatabase(s.pool.QueryRow(ctx, q, args...))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
