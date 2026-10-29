package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

// AuditFilter — выборка журнала аудита. Сортировка — от новых к старым,
// пагинация курсором по id (BeforeID), без OFFSET.
type AuditFilter struct {
	BeforeID int64 // 0 — с самых новых
	From, To time.Time
	Actor    string
	Actions  []string // точные коды действий; пусто — все
	Query    string   // подстрока в объекте, target или detail
	Limit    int      // 0 — без ограничения (экспорт)
}

// auditSelect — записи с именем объекта, на который указывает target. Имя ищется
// по сущности из префикса действия; target, не похожий на UUID, ни с чем не
// сравнивается (иначе ::uuid упал бы на «app_settings» и т.п.).
const auditSelect = `
	SELECT a.id, a.created_at, a.actor, a.action, a.target, a.detail::text, a.ip, a.user_agent,
	       COALESCE(obj.name, '') AS object_name
	FROM audit_log a
	LEFT JOIN LATERAL (
		SELECT CASE split_part(a.action, '.', 1)
			WHEN 'instance'  THEN (SELECT i.name FROM instances i WHERE i.id = u.id)
			WHEN 'database'  THEN (SELECT i.name || '/' || d.db_name FROM databases d
			                       JOIN instances i ON i.id = d.instance_id WHERE d.id = u.id)
			WHEN 'run'       THEN (SELECT i.name || '/' || d.db_name FROM backup_runs r
			                       JOIN databases d ON d.id = r.database_id
			                       JOIN instances i ON i.id = d.instance_id WHERE r.id = u.id)
			WHEN 's3_bucket' THEN (SELECT b.name FROM s3_buckets b WHERE b.id = u.id)
			WHEN 'mapping'   THEN (SELECT m.source_db_name FROM database_mappings m WHERE m.id = u.id)
			WHEN 'pairing'   THEN (SELECT p.peer_url FROM head_pairings p WHERE p.id = u.id)
		END AS name
		FROM (SELECT CASE WHEN a.target ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
		                  THEN a.target::uuid END AS id) u
	) obj ON TRUE`

func (f AuditFilter) sql() (string, []any) {
	var (
		where []string
		args  []any
	)
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.BeforeID > 0 {
		add("a.id < $%d", f.BeforeID)
	}
	if !f.From.IsZero() {
		add("a.created_at >= $%d", f.From)
	}
	if !f.To.IsZero() {
		add("a.created_at < $%d", f.To)
	}
	if f.Actor != "" {
		add("a.actor = $%d", f.Actor)
	}
	if len(f.Actions) > 0 {
		add("a.action = ANY($%d)", f.Actions)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+likeEscape(q)+"%")
		n := len(args)
		where = append(where, fmt.Sprintf(
			"(COALESCE(obj.name, '') ILIKE $%[1]d OR a.target ILIKE $%[1]d OR a.detail::text ILIKE $%[1]d)", n))
	}
	sql := auditSelect
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	sql += " ORDER BY a.id DESC"
	if f.Limit > 0 {
		args = append(args, f.Limit)
		sql += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	return sql, args
}

// likeEscape экранирует метасимволы LIKE: поиск — по подстроке как есть.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// EachAudit вызывает fn для каждой записи выборки (от новых к старым) — для
// страницы и для потокового экспорта без загрузки всего в память.
func (s *Store) EachAudit(ctx context.Context, f AuditFilter, fn func(model.AuditRecord) error) error {
	sql, args := f.sql()
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			rec    model.AuditRecord
			detail string
		)
		if err := rows.Scan(&rec.ID, &rec.CreatedAt, &rec.Actor, &rec.Action, &rec.Target, &detail,
			&rec.IP, &rec.UserAgent, &rec.ObjectName); err != nil {
			return err
		}
		rec.Detail = []byte(detail)
		if err := fn(rec); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ListAudit — страница журнала.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]model.AuditRecord, error) {
	out := []model.AuditRecord{}
	err := s.EachAudit(ctx, f, func(r model.AuditRecord) error {
		out = append(out, r)
		return nil
	})
	return out, err
}

// AuditActors — все пользователи, встречающиеся в журнале (для фильтра).
func (s *Store) AuditActors(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT actor FROM audit_log WHERE actor <> '' ORDER BY actor`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
