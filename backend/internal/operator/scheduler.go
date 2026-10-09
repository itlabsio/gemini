package operator

import (
	"context"
	"log"
	"math/rand/v2"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/k8s"
)

const (
	// templateReconcileInterval — период сверки CronJob-шаблонов и секретов кред.
	// Каждый прогон делает GET/UPDATE на CronJob + Secret по каждой базе, поэтому
	// не чаще раза в 5 минут.
	templateReconcileInterval = 5 * time.Minute
	// templateReconcileJitter — случайный сдвиг каждого цикла, чтобы прогоны не
	// выстраивались ровно по сетке и не били k8s API залпом.
	templateReconcileJitter = time.Minute
)

// RunTemplateReconciler периодически приводит CronJob-шаблоны (dump на Source,
// restore на Target) и секреты кред в соответствие с активными базами/сопоставлениями.
func (o *Operator) RunTemplateReconciler(ctx context.Context) {
	if o.k8s == nil {
		log.Printf("operator: template reconciler disabled (no kubernetes client)")
		return
	}
	for {
		safely("reconcileTemplates", func() { o.reconcileTemplates(ctx) })
		delay := templateReconcileInterval + rand.N(templateReconcileJitter)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// signatureBackfillInterval — период фонового до-подписания дампов на Source.
const signatureBackfillInterval = 5 * time.Minute

// RunSignatureBackfill (Source) периодически до-подписывает успешные дампы, у
// которых в бакете ещё нет .sig: дамп мог завершиться раньше, чем появилась
// активная пара, а Target без подписи откажется его восстанавливать.
func (o *Operator) RunSignatureBackfill(ctx context.Context) {
	if !o.cfg.IsSource() {
		log.Printf("operator: signature backfill disabled (not a source head)")
		return
	}
	for {
		safely("backfillSignatures", func() { o.BackfillSignatures(ctx) })
		select {
		case <-ctx.Done():
			return
		case <-time.After(signatureBackfillInterval + rand.N(templateReconcileJitter)):
		}
	}
}

func (o *Operator) reconcileTemplates(ctx context.Context) {
	existing, err := o.k8s.ListManagedCronJobs(ctx)
	if err != nil {
		log.Printf("operator: list cronjobs: %v", err)
		return
	}

	wanted := map[string]bool{}          // имя CronJob'а
	wantedInstances := map[string]bool{} // instanceID с активной базой/сопоставлением

	if o.cfg.IsSource() {
		dbs, err := o.store.ListEnabledDatabases(ctx)
		if err != nil {
			log.Printf("operator: list enabled databases: %v", err)
		} else {
			for i := range dbs {
				if err := o.ensureCronJob(ctx, "dump", &dbs[i]); err != nil {
					log.Printf("operator: ensure dump cronjob for %s: %v", dbs[i].DBName, err)
					continue
				}
				wanted[k8s.CronJobName("dump", dbs[i].ID)] = true
				wantedInstances[dbs[i].InstanceID] = true
			}
		}
	}

	if o.cfg.IsTarget() {
		maps, err := o.store.EnabledMappings(ctx)
		if err != nil {
			log.Printf("operator: list mappings: %v", err)
		} else {
			for _, m := range maps {
				tgt, err := o.store.GetDatabase(ctx, m.TargetDatabaseID)
				if err != nil {
					continue
				}
				if err := o.ensureCronJob(ctx, "restore", tgt); err != nil {
					log.Printf("operator: ensure restore cronjob for %s: %v", tgt.DBName, err)
					continue
				}
				wanted[k8s.CronJobName("restore", tgt.ID)] = true
				wantedInstances[tgt.InstanceID] = true
			}
		}
	}

	// Секрет кред — один на инстанс, обновляем по разу за цикл.
	for instanceID := range wantedInstances {
		inst, err := o.store.GetInstance(ctx, instanceID)
		if err != nil {
			continue
		}
		if err := o.upsertInstanceCreds(ctx, inst); err != nil {
			log.Printf("operator: upsert creds for instance %s: %v", inst.Name, err)
		}
	}

	for _, cj := range existing {
		if wanted[cj.Name] || cj.Kind == "" || cj.DatabaseID == "" {
			continue
		}
		_ = o.k8s.DeleteCronJob(ctx, cj.Kind, cj.DatabaseID)
	}

	secrets, err := o.k8s.ListManagedCredsSecrets(ctx)
	if err != nil {
		log.Printf("operator: list creds secrets: %v", err)
		return
	}
	for instanceID := range secrets {
		if !wantedInstances[instanceID] {
			_ = o.k8s.DeleteCredsSecret(ctx, instanceID)
		}
	}
}
