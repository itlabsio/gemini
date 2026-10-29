package operator

import (
	"context"
	"log"
	"math/rand/v2"
	"time"
)

// retentionInterval — период очистки журнала аудита и истории прогонов.
const retentionInterval = time.Hour

// RunRetention (лидер) раз в час удаляет записи аудита и завершённые прогоны
// старше сроков из настроек (audit_retention_days / runs_retention_days).
func (o *Operator) RunRetention(ctx context.Context) {
	for {
		safely("pruneOnce", func() { o.pruneOnce(ctx) })
		select {
		case <-ctx.Done():
			return
		case <-time.After(retentionInterval + rand.N(templateReconcileJitter)):
		}
	}
}

func (o *Operator) pruneOnce(ctx context.Context) {
	st, err := o.store.GetSettings(ctx)
	if err != nil {
		log.Printf("operator: retention: read settings: %v", err)
		return
	}
	now := time.Now()
	days := func(n int32) time.Time { return now.AddDate(0, 0, -int(n)) }

	if n, err := o.store.PruneAudit(ctx, days(st.AuditRetentionDays)); err != nil {
		log.Printf("operator: retention: prune audit: %v", err)
	} else if n > 0 {
		log.Printf("operator: retention: removed %d audit record(s) older than %d days", n, st.AuditRetentionDays)
	}
	if n, err := o.store.PruneRuns(ctx, days(st.RunsRetentionDays)); err != nil {
		log.Printf("operator: retention: prune runs: %v", err)
	} else if n > 0 {
		log.Printf("operator: retention: removed %d run(s) older than %d days", n, st.RunsRetentionDays)
	}
}
