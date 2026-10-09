package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

const runCols = `id, database_id, kind, trigger, status, k8s_job_name, s3_object_key,
	checksum, event_key, started_at, finished_at, error_message, initiated_by, created_at`

func scanRun(row pgx.Row) (*model.BackupRun, error) {
	var r model.BackupRun
	err := row.Scan(&r.ID, &r.DatabaseID, &r.Kind, &r.Trigger, &r.Status, &r.K8sJobName,
		&r.S3ObjectKey, &r.Checksum, &r.EventKey, &r.StartedAt, &r.FinishedAt,
		&r.ErrorMessage, &r.InitiatedBy, &r.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &r, nil
}

// SetPeerStatus (Source): сохраняет исход restore одного Target-контура рядом с
// dump-прогоном (по ключу объекта в S3) и id пары этого Target. Идемпотентно.
func (s *Store) SetPeerStatus(ctx context.Context, pairingID, peerURL, s3ObjectKey, status, errMsg string) error {
	var runID string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM backup_runs WHERE s3_object_key = $1 AND kind = 'dump'
		 ORDER BY created_at DESC LIMIT 1`, s3ObjectKey).Scan(&runID)
	if err != nil {
		return mapErr(err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO backup_run_peer_status (run_id, pairing_id, peer_url, status, error, reported_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (run_id, pairing_id) DO UPDATE
		SET peer_url = EXCLUDED.peer_url, status = EXCLUDED.status,
		    error = EXCLUDED.error, reported_at = NOW()`,
		runID, pairingID, peerURL, status, errMsg)
	return err
}

// loadPeerStatuses подгружает исходы restore по Target'ам к переданным прогонам.
func (s *Store) loadPeerStatuses(ctx context.Context, runs []model.BackupRun) error {
	if len(runs) == 0 {
		return nil
	}
	idx := make(map[string]int, len(runs))
	ids := make([]string, len(runs))
	for i := range runs {
		idx[runs[i].ID] = i
		ids[i] = runs[i].ID
	}
	rows, err := s.pool.Query(ctx, `
		SELECT run_id, peer_url, status, error, reported_at
		FROM backup_run_peer_status WHERE run_id = ANY($1::uuid[])
		ORDER BY reported_at`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var runID string
		var ps model.RunPeerStatus
		if err := rows.Scan(&runID, &ps.PeerURL, &ps.Status, &ps.Error, &ps.ReportedAt); err != nil {
			return err
		}
		if i, ok := idx[runID]; ok {
			runs[i].PeerStatuses = append(runs[i].PeerStatuses, ps)
		}
	}
	return rows.Err()
}

// RunFilter — параметры выборки истории.
type RunFilter struct {
	DatabaseID string
	Kind       model.RunKind
	Status     model.RunStatus
	Limit      int
}

// ListRuns возвращает историю прогонов с фильтрами.
func (s *Store) ListRuns(ctx context.Context, f RunFilter) ([]model.BackupRun, error) {
	q := `SELECT ` + runCols + ` FROM backup_runs`
	var where []string
	var args []any
	if f.DatabaseID != "" {
		args = append(args, f.DatabaseID)
		where = append(where, "database_id = $"+itoa(len(args)))
	}
	if f.Kind != "" {
		args = append(args, f.Kind)
		where = append(where, "kind = $"+itoa(len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, "status = $"+itoa(len(args)))
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at DESC"
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	args = append(args, f.Limit)
	q += " LIMIT $" + itoa(len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out := []model.BackupRun{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.loadPeerStatuses(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRun возвращает один прогон.
func (s *Store) GetRun(ctx context.Context, id string) (*model.BackupRun, error) {
	r, err := scanRun(s.pool.QueryRow(ctx, `SELECT `+runCols+` FROM backup_runs WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	one := []model.BackupRun{*r}
	if err := s.loadPeerStatuses(ctx, one); err != nil {
		return nil, err
	}
	return &one[0], nil
}

// ActiveRun возвращает активный (pending/running) прогон для (базы, вида), если есть.
func (s *Store) ActiveRun(ctx context.Context, databaseID string, kind model.RunKind) (*model.BackupRun, error) {
	return scanRun(s.pool.QueryRow(ctx, `
		SELECT `+runCols+` FROM backup_runs
		WHERE database_id = $1 AND kind = $2 AND status IN ('pending','running')`,
		databaseID, kind))
}

// NewRunInput — параметры создания прогона.
type NewRunInput struct {
	DatabaseID  string
	Kind        model.RunKind
	Trigger     model.RunTrigger
	S3ObjectKey string
	Checksum    string
	EventKey    string
	InitiatedBy string
}

// CreateRun вставляет прогон в статусе pending.
// Возвращает ErrConflict, если уже есть активный прогон (частичный unique index)
// или event_key уже обработан (идемпотентность webhook/poll).
func (s *Store) CreateRun(ctx context.Context, in NewRunInput) (*model.BackupRun, error) {
	r, err := scanRun(s.pool.QueryRow(ctx, `
		INSERT INTO backup_runs
			(database_id, kind, trigger, status, s3_object_key, checksum, event_key, initiated_by)
		VALUES ($1,$2,$3,'pending',$4,$5,$6,$7)
		RETURNING `+runCols,
		in.DatabaseID, in.Kind, in.Trigger, in.S3ObjectKey, in.Checksum, in.EventKey, in.InitiatedBy))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrConflict
	}
	return r, err
}

// ListActiveRuns возвращает прогоны в статусе pending/running, стартовавшие
// (или созданные) раньше now-minAge. Используется reaper'ом watcher'а для
// принудительного резолва «зависших» прогонов, чей Job уже исчез из кластера.
func (s *Store) ListActiveRuns(ctx context.Context, minAge time.Duration) ([]model.BackupRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+runCols+` FROM backup_runs
		WHERE status IN ('pending','running')
		  AND created_at < NOW() - make_interval(secs => $1)
		ORDER BY created_at`, minAge.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.BackupRun{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// ListSignableDumps возвращает успешные dump-прогоны с уже известными ключом и
// контрольной суммой за последние since. Источник кандидатов для (до)подписания
// на Source: дамп мог завершиться раньше, чем появилась активная пара, и остаться
// без <key>.sig (см. operator.BackfillSignatures).
func (s *Store) ListSignableDumps(ctx context.Context, since time.Duration) ([]model.BackupRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+runCols+` FROM backup_runs
		WHERE kind = 'dump' AND status = 'succeeded'
		  AND s3_object_key <> '' AND checksum <> ''
		  AND created_at > NOW() - make_interval(secs => $1)
		ORDER BY created_at DESC`, since.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.BackupRun{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// RunByEventKey — для идемпотентной обработки повторно пришедшего события.
func (s *Store) RunByEventKey(ctx context.Context, eventKey string) (*model.BackupRun, error) {
	return scanRun(s.pool.QueryRow(ctx,
		`SELECT `+runCols+` FROM backup_runs WHERE event_key = $1`, eventKey))
}

// RunUpdate — переход статуса прогона (используется watcher'ом).
type RunUpdate struct {
	Status       model.RunStatus
	K8sJobName   *string
	S3ObjectKey  *string
	Checksum     *string
	ErrorMessage *string
	MarkStarted  bool
	MarkFinished bool
}

// UpdateRun применяет переход статуса.
func (s *Store) UpdateRun(ctx context.Context, id string, u RunUpdate) (*model.BackupRun, error) {
	set := []string{"status = $2"}
	args := []any{id, u.Status}
	add := func(col string, val any) {
		args = append(args, val)
		set = append(set, col+" = $"+itoa(len(args)))
	}
	if u.K8sJobName != nil {
		add("k8s_job_name", *u.K8sJobName)
	}
	if u.S3ObjectKey != nil {
		add("s3_object_key", *u.S3ObjectKey)
	}
	if u.Checksum != nil {
		add("checksum", *u.Checksum)
	}
	if u.ErrorMessage != nil {
		add("error_message", *u.ErrorMessage)
	}
	if u.MarkStarted {
		set = append(set, "started_at = COALESCE(started_at, NOW())")
	}
	if u.MarkFinished {
		set = append(set, "finished_at = NOW()")
	}
	q := `UPDATE backup_runs SET ` + strings.Join(set, ", ") + ` WHERE id = $1 RETURNING ` + runCols
	return scanRun(s.pool.QueryRow(ctx, q, args...))
}
