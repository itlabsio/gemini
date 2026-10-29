// Package store — репозитории поверх собственной БД метаданных головы.
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/secretcrypto"
)

// ErrNotFound возвращается, когда сущность не найдена.
var ErrNotFound = errors.New("not found")

// ErrConflict — нарушение уникального ограничения / бизнес-инварианта
// (например, уже есть активный прогон для базы).
var ErrConflict = errors.New("conflict")

// ErrPasswordRequired — креды задаются впервые, а пароль пустой («не менять»
// применим только к уже сохранённым).
var ErrPasswordRequired = errors.New("password is required")

// Store держит пул и box для шифрования секретов подключений.
type Store struct {
	pool *pgxpool.Pool
	box  *secretcrypto.Box

	// ownerHead — идентичность головы (UID Deployment'а backend'а), которой
	// CreateRun метит новые прогоны. Задаётся один раз на старте, до запуска
	// API и фоновых циклов.
	ownerHead string
}

// New создаёт Store.
func New(pool *pgxpool.Pool, box *secretcrypto.Box) *Store {
	return &Store{pool: pool, box: box}
}

// SetOwnerHead задаёт идентичность головы для новых прогонов (см. ownerHead).
func (s *Store) SetOwnerHead(id string) { s.ownerHead = id }

// OwnerHead — идентичность этой головы ("" — не определена).
func (s *Store) OwnerHead() string { return s.ownerHead }

// Pool отдаёт нижележащий пул (нужен фоновым компонентам под свои запросы).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// AuditEntry — строка журнала аудита.
type AuditEntry struct {
	Actor     string
	Action    string
	Target    string
	Detail    []byte // JSON; пусто → {}
	IP        string
	UserAgent string
}

// WriteAudit пишет строку в audit_log (best-effort, ошибки только логируются вызывающим).
func (s *Store) WriteAudit(ctx context.Context, e AuditEntry) error {
	if len(e.Detail) == 0 {
		e.Detail = []byte("{}")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_log (actor, action, target, detail, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.Actor, e.Action, e.Target, string(e.Detail), e.IP, e.UserAgent)
	return err
}

// UpsertUser обновляет кэш логина для аудита UI.
func (s *Store) UpsertUser(ctx context.Context, subject, email string, roles []string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (subject, email, roles, last_seen_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (subject) DO UPDATE
		   SET email = EXCLUDED.email, roles = EXCLUDED.roles, last_seen_at = NOW()`,
		subject, email, roles)
	return err
}
