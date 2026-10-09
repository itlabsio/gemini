package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

// InstanceInput — данные для создания/обновления Instance.
type InstanceInput struct {
	Role              model.InstanceRole
	Name              string
	Host              string
	Port              int
	RestoreMode       model.RestoreMode
	SSLMode           string
	SSLRootCert       string
	DiscoveryDB       string
	ExcludedDatabases []string
	AuthType          model.AuthType
	PlainUsername     string
	PlainPassword     string // сырой пароль; шифруется здесь
	VaultPath         string
	VaultRole         string
	VaultUsernameKey  string
	VaultPasswordKey  string
}

// instanceCols — только адресная часть, из таблицы instances.
// Дескриптор кред подтягивается join'ом (см. instanceSelect); зашифрованный
// пароль в этих выборках не участвует НИКОГДА — он читается лишь Credentials().
const instanceCols = `i.id, i.role, i.name, i.host, i.port, i.restore_mode,
	i.ssl_mode, i.ssl_root_cert, i.discovery_db, i.excluded_databases, i.server_version_num,
	i.created_by, i.created_at, i.updated_at,
	c.auth_type, c.plain_username,
	c.vault_path, c.vault_role, c.vault_username_key, c.vault_password_key`

const instanceSelect = `SELECT ` + instanceCols + `
	FROM instances i JOIN instance_credentials c ON c.instance_id = i.id`

func scanInstance(row pgx.Row) (*model.Instance, error) {
	var i model.Instance
	err := row.Scan(&i.ID, &i.Role, &i.Name, &i.Host, &i.Port, &i.RestoreMode,
		&i.SSLMode, &i.SSLRootCert, &i.DiscoveryDB, &i.ExcludedDatabases, &i.ServerVersionNum,
		&i.CreatedBy, &i.CreatedAt, &i.UpdatedAt,
		&i.AuthType, &i.PlainUsername,
		&i.VaultPath, &i.VaultRole, &i.VaultUsernameKey, &i.VaultPasswordKey)
	if err != nil {
		return nil, mapErr(err)
	}
	if i.ExcludedDatabases == nil {
		i.ExcludedDatabases = []string{}
	}
	return &i, nil
}

// excludedArg — nil-слайс уехал бы в NULL и нарушил NOT NULL колонки; пустой
// список должен стать '{}'.
func excludedArg(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// writeCredentials пишет дескриптор кред инстанса. keepPassword=true оставляет
// сохранённый пароль (пустой ввод при auth_type=plain — «не менять»).
func writeCredentials(ctx context.Context, tx pgx.Tx, instanceID string, in InstanceInput,
	enc []byte, keepPassword bool) error {
	pwdSet, pwdArg := `plain_password_encrypted = EXCLUDED.plain_password_encrypted`, any(enc)
	if keepPassword {
		pwdSet = `plain_password_encrypted = instance_credentials.plain_password_encrypted`
		pwdArg = nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO instance_credentials
			(instance_id, auth_type, plain_username, plain_password_encrypted,
			 vault_path, vault_role, vault_username_key, vault_password_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (instance_id) DO UPDATE SET
			auth_type          = EXCLUDED.auth_type,
			plain_username     = EXCLUDED.plain_username,
			`+pwdSet+`,
			vault_path         = EXCLUDED.vault_path,
			vault_role         = EXCLUDED.vault_role,
			vault_username_key = EXCLUDED.vault_username_key,
			vault_password_key = EXCLUDED.vault_password_key`,
		instanceID, in.AuthType, in.PlainUsername, pwdArg,
		in.VaultPath, in.VaultRole, in.VaultUsernameKey, in.VaultPasswordKey)
	return mapErr(err)
}

// ListInstances возвращает все инстансы, опционально фильтруя по роли.
func (s *Store) ListInstances(ctx context.Context, role model.InstanceRole) ([]model.Instance, error) {
	q := instanceSelect
	args := []any{}
	if role != "" {
		q += ` WHERE i.role = $1`
		args = append(args, role)
	}
	q += ` ORDER BY i.created_at DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Instance{}
	for rows.Next() {
		i, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

// GetInstance возвращает один инстанс.
func (s *Store) GetInstance(ctx context.Context, id string) (*model.Instance, error) {
	return scanInstance(s.pool.QueryRow(ctx, instanceSelect+` WHERE i.id = $1`, id))
}

// InstanceCredentials — расшифрованные креды подключения (наружу не отдаются).
type InstanceCredentials struct {
	AuthType         model.AuthType
	Username         string
	Password         string
	VaultPath        string
	VaultRole        string
	VaultUsernameKey string
	VaultPasswordKey string
}

// Credentials достаёт и расшифровывает секрет подключения инстанса.
func (s *Store) Credentials(ctx context.Context, id string) (*InstanceCredentials, error) {
	var (
		authType model.AuthType
		username string
		encPass  []byte
		vpath    string
		vrole    string
		vukey    string
		vpkey    string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT auth_type, plain_username, plain_password_encrypted, vault_path, vault_role,
		        vault_username_key, vault_password_key
		   FROM instance_credentials WHERE instance_id = $1`, id,
	).Scan(&authType, &username, &encPass, &vpath, &vrole, &vukey, &vpkey)
	if err != nil {
		return nil, mapErr(err)
	}
	c := &InstanceCredentials{AuthType: authType, Username: username, VaultPath: vpath, VaultRole: vrole,
		VaultUsernameKey: vukey, VaultPasswordKey: vpkey}
	if authType == model.AuthPlain {
		pw, err := s.box.Decrypt(encPass)
		if err != nil {
			return nil, fmt.Errorf("decrypt instance %s password: %w", id, err)
		}
		c.Password = pw
	}
	return c, nil
}

// CreateInstance вставляет новый инстанс.
func (s *Store) CreateInstance(ctx context.Context, in InstanceInput, createdBy string) (*model.Instance, error) {
	enc, err := s.box.Encrypt(in.PlainPassword)
	if err != nil {
		return nil, fmt.Errorf("encrypt password: %w", err)
	}
	if in.Port == 0 {
		in.Port = 5432
	}
	if in.SSLMode == "" {
		in.SSLMode = "require"
	}
	if in.RestoreMode == "" {
		in.RestoreMode = model.RestoreRecreate
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO instances (role, name, host, port, restore_mode, ssl_mode, ssl_root_cert, discovery_db, excluded_databases, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id`,
		in.Role, in.Name, in.Host, in.Port, in.RestoreMode, in.SSLMode, in.SSLRootCert, in.DiscoveryDB,
		excludedArg(in.ExcludedDatabases), createdBy,
	).Scan(&id); err != nil {
		return nil, mapErr(err)
	}
	if err := writeCredentials(ctx, tx, id, in, enc, false); err != nil {
		return nil, err
	}
	inst, err := scanInstance(tx.QueryRow(ctx, instanceSelect+` WHERE i.id = $1`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return inst, nil
}

// UpdateInstance обновляет инстанс. Пустой PlainPassword при auth_type=plain
// оставляет сохранённый пароль без изменений.
func (s *Store) UpdateInstance(ctx context.Context, id string, in InstanceInput) (*model.Instance, error) {
	if in.Port == 0 {
		in.Port = 5432
	}
	if in.SSLMode == "" {
		in.SSLMode = "require"
	}
	if in.RestoreMode == "" {
		in.RestoreMode = model.RestoreRecreate
	}
	// Пустой пароль при auth_type=plain означает «оставить сохранённый».
	keepPassword := in.AuthType == model.AuthPlain && in.PlainPassword == ""
	var enc []byte
	if !keepPassword {
		var err error
		if enc, err = s.box.Encrypt(in.PlainPassword); err != nil {
			return nil, fmt.Errorf("encrypt password: %w", err)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	ct, err := tx.Exec(ctx, `
		UPDATE instances SET role=$2, name=$3, host=$4, port=$5, ssl_mode=$6, ssl_root_cert=$7,
			discovery_db=$8, excluded_databases=$9, restore_mode=$10
		WHERE id=$1`,
		id, in.Role, in.Name, in.Host, in.Port, in.SSLMode, in.SSLRootCert, in.DiscoveryDB,
		excludedArg(in.ExcludedDatabases), in.RestoreMode)
	if err != nil {
		return nil, mapErr(err)
	}
	if ct.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	if err := writeCredentials(ctx, tx, id, in, enc, keepPassword); err != nil {
		return nil, err
	}
	inst, err := scanInstance(tx.QueryRow(ctx, instanceSelect+` WHERE i.id = $1`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return inst, nil
}

// DeleteInstance удаляет инстанс (каскадно — его databases).
func (s *Store) DeleteInstance(ctx context.Context, id string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM instances WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
