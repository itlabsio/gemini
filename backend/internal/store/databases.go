package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

// databaseCols ссылается на алиас d: все выборки идут FROM databases d. Пароль
// restore сюда не попадает НИКОГДА — он читается лишь DatabaseCredentials().
const databaseCols = `d.id, d.instance_id, d.db_name, d.enabled, d.schedule_cron, d.storage_type,
	d.storage_size, d.external_id, d.present, d.created_at, d.updated_at,
	COALESCE((SELECT c.username FROM database_credentials c WHERE c.database_id = d.id), '')`

func scanDatabase(row pgx.Row) (*model.Database, error) {
	var d model.Database
	err := row.Scan(&d.ID, &d.InstanceID, &d.DBName, &d.Enabled, &d.ScheduleCron,
		&d.StorageType, &d.StorageSize, &d.ExternalID, &d.Present, &d.CreatedAt, &d.UpdatedAt,
		&d.RestoreUsername)
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

// ListEnabledDatabases возвращает включённые базы (для scheduler).
func (s *Store) ListEnabledDatabases(ctx context.Context) ([]model.Database, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+databaseCols+` FROM databases d WHERE d.enabled AND d.present ORDER BY d.db_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectDatabases(rows)
}

// ListPresentDatabases возвращает присутствующие базы всех инстансов, кроме
// исключённых из discovery (postgres и instances.excluded_databases), —
// независимо от enabled; project != "" — только базы инстансов этого проекта
// (RBAC). Это список кандидатов для сопоставления на DR: решать,
// восстанавливать ли базу, должна галка на Target, а не на Source.
func (s *Store) ListPresentDatabases(ctx context.Context, project string) ([]model.Database, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+databaseCols+`
		FROM databases d
		WHERE d.present
		  AND ($1::text = '' OR d.instance_id IN (SELECT id FROM instances WHERE project = $1::text))
		  AND d.db_name <> 'postgres'
		  AND d.db_name <> ALL (
		      SELECT unnest(excluded_databases) FROM instances i WHERE i.id = d.instance_id)
		ORDER BY d.db_name`, project)
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
		`SELECT `+databaseCols+` FROM databases d WHERE d.id = $1`, id))
}

// GetDatabaseByExternalID нужен обработчикам webhook на DR.
func (s *Store) GetDatabaseByExternalID(ctx context.Context, externalID string) (*model.Database, error) {
	return scanDatabase(s.pool.QueryRow(ctx,
		`SELECT `+databaseCols+` FROM databases d WHERE d.external_id = $1`, externalID))
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
	q := `UPDATE databases d SET ` + set[2:] + ` WHERE d.id = $1 RETURNING ` + databaseCols
	return scanDatabase(s.pool.QueryRow(ctx, q, args...))
}

// SetDatabaseCredentials задаёт креды restore базы. Пустой password оставляет
// сохранённый пароль (как у инстанса) — но только если креды уже есть: записи
// без пароля не бывает.
func (s *Store) SetDatabaseCredentials(ctx context.Context, databaseID, username, password string) error {
	if password == "" {
		ct, err := s.pool.Exec(ctx,
			`UPDATE database_credentials SET username = $2 WHERE database_id = $1`, databaseID, username)
		if err != nil {
			return mapErr(err)
		}
		if ct.RowsAffected() == 0 {
			return ErrPasswordRequired
		}
		return nil
	}
	enc, err := s.box.Encrypt(password)
	if err != nil {
		return fmt.Errorf("encrypt password: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO database_credentials (database_id, username, password_encrypted)
		VALUES ($1, $2, $3)
		ON CONFLICT (database_id) DO UPDATE SET
			username = EXCLUDED.username,
			password_encrypted = EXCLUDED.password_encrypted`,
		databaseID, username, enc)
	return mapErr(err)
}

// DeleteDatabaseCredentials удаляет креды restore базы (отсутствие — не ошибка).
func (s *Store) DeleteDatabaseCredentials(ctx context.Context, databaseID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM database_credentials WHERE database_id = $1`, databaseID)
	return err
}

// DatabaseCredentials достаёт и расшифровывает креды restore базы.
// ErrNotFound — креды не заданы.
func (s *Store) DatabaseCredentials(ctx context.Context, databaseID string) (username, password string, err error) {
	var enc []byte
	err = s.pool.QueryRow(ctx,
		`SELECT username, password_encrypted FROM database_credentials WHERE database_id = $1`, databaseID,
	).Scan(&username, &enc)
	if err != nil {
		return "", "", mapErr(err)
	}
	if password, err = s.box.Decrypt(enc); err != nil {
		return "", "", fmt.Errorf("decrypt database %s restore password: %w", databaseID, err)
	}
	return username, password, nil
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

// DeleteAbsentDatabase удаляет запись базы, пропавшей из discovery, вместе с
// журналом запусков и кредами restore (каскад). Присутствующую базу или базу с
// активным прогоном не трогает — ErrConflict. Сопоставление на неё запрещает
// удаление на уровне FK; вызывающий проверяет его заранее.
func (s *Store) DeleteAbsentDatabase(ctx context.Context, id string) error {
	ct, err := s.pool.Exec(ctx, `
		DELETE FROM databases d
		WHERE d.id = $1 AND NOT d.present
		  AND NOT EXISTS (
		      SELECT 1 FROM backup_runs r
		      WHERE r.database_id = d.id AND r.status IN ('pending','running'))`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}
