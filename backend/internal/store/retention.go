package store

import (
	"context"
	"time"
)

// pruneBatch — сколько строк удаляется одним DELETE: короткие транзакции не
// держат долгих блокировок и не раздувают WAL одним куском.
const pruneBatch = 1000

// pruneLoop повторяет DELETE пачками, пока очередная не окажется неполной.
func (s *Store) pruneLoop(ctx context.Context, sql string, args ...any) (int64, error) {
	var total int64
	for {
		ct, err := s.pool.Exec(ctx, sql, append(args, pruneBatch)...)
		if err != nil {
			return total, err
		}
		total += ct.RowsAffected()
		if ct.RowsAffected() < pruneBatch {
			return total, nil
		}
	}
}

// PruneAudit удаляет записи журнала аудита старше before.
func (s *Store) PruneAudit(ctx context.Context, before time.Time) (int64, error) {
	return s.pruneLoop(ctx, `
		DELETE FROM audit_log WHERE id IN (
			SELECT id FROM audit_log WHERE created_at < $1 LIMIT $2)`, before)
}

// PruneRuns удаляет завершённые прогоны старше before. Последний прогон каждого
// вида по базе остаётся при любом возрасте: на его event_key держится
// идемпотентность webhook'а и поллера (иначе поллер принял бы последний дамп
// за новый и восстановил его повторно), по нему же строится полоса прогонов.
func (s *Store) PruneRuns(ctx context.Context, before time.Time) (int64, error) {
	return s.pruneLoop(ctx, `
		DELETE FROM backup_runs WHERE id IN (
			SELECT r.id FROM backup_runs r
			WHERE r.created_at < $1
			  AND r.status IN ('succeeded', 'failed')
			  AND EXISTS (
				SELECT 1 FROM backup_runs n
				WHERE n.database_id = r.database_id AND n.kind = r.kind
				  AND n.created_at > r.created_at)
			LIMIT $2)`, before)
}
