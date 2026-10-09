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

// Store держит пул и box для шифрования секретов подключений.
type Store struct {
	pool *pgxpool.Pool
	box  *secretcrypto.Box
}

// New создаёт Store.
func New(pool *pgxpool.Pool, box *secretcrypto.Box) *Store {
	return &Store{pool: pool, box: box}
}

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

// WriteAudit пишет строку в audit_log (best-effort, ошибки только логируются вызывающим).
func (s *Store) WriteAudit(ctx context.Context, actor, action, target string, detailJSON []byte) error {
	if len(detailJSON) == 0 {
		detailJSON = []byte("{}")
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_log (actor, action, target, detail) VALUES ($1, $2, $3, $4)`,
		actor, action, target, string(detailJSON))
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
