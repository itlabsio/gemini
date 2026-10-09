// Package discovery подключается к Instance и читает список пользовательских баз.
package discovery

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// BuiltinExcluded — базы, которые discovery отбрасывает всегда: postgres в
// Yandex MDB недоступна через пул, а бэкап её не нужен и в self-hosted.
var BuiltinExcluded = []string{"postgres"}

// Filter отбрасывает встроенные исключения и заданные для инстанса базы.
func Filter(names, instanceExcluded []string) []string {
	skip := make(map[string]struct{}, len(BuiltinExcluded)+len(instanceExcluded))
	for _, n := range BuiltinExcluded {
		skip[n] = struct{}{}
	}
	for _, n := range instanceExcluded {
		if n = strings.TrimSpace(n); n != "" {
			skip[n] = struct{}{}
		}
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if _, ok := skip[n]; ok {
			continue
		}
		out = append(out, n)
	}
	return out
}

// Target — параметры подключения к серверу БД для discovery.
type Target struct {
	Host     string
	Port     int
	User     string
	Password string
	SSLMode  string
	// RootCertPEM — PEM кастомного корневого CA (self-hosted инстанс с собственным
	// сертификатом). Пусто → системный trust store. Применяется при verify-ca/verify-full.
	RootCertPEM string
	// AdminDB — база, к которой подключаемся для чтения pg_database (обычно "postgres").
	AdminDB string
}

// adminDB — имя базы для подключения; пусто → "postgres".
func (t Target) adminDB() string {
	if t.AdminDB == "" {
		return "postgres"
	}
	return t.AdminDB
}

func (t Target) dsn() string {
	adminDB := t.adminDB()
	ssl := t.SSLMode
	if ssl == "" {
		ssl = "require"
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(t.User, t.Password),
		Host:   fmt.Sprintf("%s:%d", t.Host, t.Port),
		Path:   "/" + adminDB,
	}
	q := u.Query()
	q.Set("sslmode", ssl)
	q.Set("connect_timeout", "10")
	u.RawQuery = q.Encode()
	return u.String()
}

// connConfig разбирает DSN и, если задан кастомный CA, подменяет корневой пул
// доверия (libpq-семантику verify-ca/verify-full pgx уже выставил по sslmode).
func (t Target) connConfig() (*pgx.ConnConfig, error) {
	cfg, err := pgx.ParseConfig(t.dsn())
	if err != nil {
		return nil, err
	}
	if t.RootCertPEM != "" && cfg.TLSConfig != nil {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(t.RootCertPEM)) {
			return nil, fmt.Errorf("ssl_root_cert: no valid certificate in PEM")
		}
		cfg.TLSConfig.RootCAs = pool
	}
	return cfg, nil
}

// Result — что discovery узнал об инстансе за одно подключение.
type Result struct {
	// Databases — имена непшаблонных баз, доступных текущему пользователю.
	Databases []string
	// ServerVersionNum — server_version_num сервера (напр. 170004 = PG 17.4).
	// 0 → прочитать не удалось (не критично для discovery).
	ServerVersionNum int
}

// ListDatabases возвращает список баз инстанса и версию его сервера.
func ListDatabases(ctx context.Context, t Target) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	cfg, err := t.connConfig()
	if err != nil {
		return nil, fmt.Errorf("parse connection config: %w", err)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database %q: %w "+
			"(если базы %q нет — укажите существующую в discovery_db инстанса; "+
			"в Yandex Managed PostgreSQL базы postgres в пуле нет)",
			t.adminDB(), err, t.adminDB())
	}
	defer conn.Close(ctx)

	var res Result
	// Версия сервера — для UI и для выбора мажора pg_dump/pg_restore в Job'ах.
	// Ошибку глотаем: без версии discovery всё равно полезен.
	_ = conn.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).
		Scan(&res.ServerVersionNum)

	// Только базы, к которым текущий пользователь реально может подключиться —
	// служебные (postgres в Yandex MDB и т.п.) отсеиваются.
	rows, err := conn.Query(ctx, `
		SELECT datname FROM pg_database
		WHERE datistemplate = false
		  AND datallowconn = true
		  AND has_database_privilege(current_user, datname, 'CONNECT')
		ORDER BY datname`)
	if err != nil {
		return nil, fmt.Errorf("query pg_database: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		res.Databases = append(res.Databases, n)
	}
	return &res, rows.Err()
}

// ListRoles возвращает имена ролей кластера, кроме встроенных pg_* — кандидаты
// на владельца восстановленной базы (target-голова, поле сопоставления).
func ListRoles(ctx context.Context, t Target) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	cfg, err := t.connConfig()
	if err != nil {
		return nil, fmt.Errorf("parse connection config: %w", err)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database %q: %w", t.adminDB(), err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT rolname FROM pg_roles
		WHERE rolname NOT LIKE 'pg\_%'
		ORDER BY rolname`)
	if err != nil {
		return nil, fmt.Errorf("query pg_roles: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}
