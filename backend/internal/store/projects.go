package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

// ListProjects — проекты с числом привязанных инстансов.
func (s *Store) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.name, p.created_by, p.created_at, p.managed_by, COUNT(i.id)
		FROM projects p LEFT JOIN instances i ON i.project = p.name
		GROUP BY p.name ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Project{}
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.Name, &p.CreatedBy, &p.CreatedAt, &p.ManagedBy, &p.InstanceCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProjectExists сообщает, есть ли проект с таким именем.
func (s *Store) ProjectExists(ctx context.Context, name string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM projects WHERE name = $1)`, name).Scan(&ok)
	return ok, err
}

// CreateProject создаёт проект; дубль имени → ErrConflict. managedBy — см.
// InstanceInput.ManagedBy.
func (s *Store) CreateProject(ctx context.Context, name, createdBy, managedBy string) (*model.Project, error) {
	var p model.Project
	err := s.pool.QueryRow(ctx, `
		INSERT INTO projects (name, created_by, managed_by) VALUES ($1, $2, $3)
		RETURNING name, created_by, created_at, managed_by`, name, createdBy, managedBy,
	).Scan(&p.Name, &p.CreatedBy, &p.CreatedAt, &p.ManagedBy)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrConflict
		}
		return nil, err
	}
	return &p, nil
}

// ClearProjectManagedBy снимает метку управления.
func (s *Store) ClearProjectManagedBy(ctx context.Context, name string) error {
	ct, err := s.pool.Exec(ctx, `UPDATE projects SET managed_by = '' WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProject удаляет проект. Если к нему привязаны инстансы → ErrConflict.
func (s *Store) DeleteProject(ctx context.Context, name string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE name = $1`, name)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrConflict
		}
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
