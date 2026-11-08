package operator

import (
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/k8s"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
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

// signatureRetryInterval — период повтора несостоявшихся подписей на Source.
const signatureRetryInterval = 5 * time.Minute

// RunSignatureRetry (Source) периодически вызывает RetrySignatures.
func (o *Operator) RunSignatureRetry(ctx context.Context) {
	if !o.cfg.IsSource() {
		log.Printf("operator: signature retry disabled (not a source head)")
		return
	}
	for {
		safely("retrySignatures", func() { o.RetrySignatures(ctx) })
		select {
		case <-ctx.Done():
			return
		case <-time.After(signatureRetryInterval + rand.N(templateReconcileJitter)):
		}
	}
}

func (o *Operator) reconcileTemplates(ctx context.Context) {
	existing, err := o.k8s.ListManagedCronJobs(ctx)
	if err != nil {
		log.Printf("operator: list cronjobs: %v", err)
		return
	}

	knownHash := make(map[string]string, len(existing)) // имя CronJob'а → хэш спеки
	for _, cj := range existing {
		knownHash[cj.Name] = cj.SpecHash
	}

	wanted := map[string]bool{}          // имя CronJob'а
	wantedInstances := map[string]bool{} // instanceID с активной базой/сопоставлением
	wantedDBCreds := map[string]bool{}   // databaseID с секретом кред restore (in_place_per_db)

	if o.cfg.IsSource() {
		dbs, err := o.store.ListEnabledDatabases(ctx)
		if err != nil {
			log.Printf("operator: list enabled databases: %v", err)
		} else {
			for i := range dbs {
				// wanted — до ensure: если обновление не прошло (невалидный
				// schedule, сбой API), существующий CronJob остаётся со старым
				// spec и продолжает работать, а не удаляется ниже.
				name := k8s.CronJobName("dump", dbs[i].ID)
				wanted[name] = true
				wantedInstances[dbs[i].InstanceID] = true // секрет кред нужен сохранённому CronJob'у
				if err := o.ensureCronJob(ctx, "dump", &dbs[i], knownHash[name]); err != nil {
					log.Printf("operator: ensure dump cronjob for %s: %v", dbs[i].DBName, err)
				}
			}
		}
	}

	if o.cfg.IsTarget() {
		// Шаблон и креды — для всех присутствующих целевых баз (см. TemplateMappings).
		maps, err := o.store.TemplateMappings(ctx)
		if err != nil {
			log.Printf("operator: list mappings: %v", err)
		} else {
			for _, m := range maps {
				tgt, err := o.store.GetDatabase(ctx, m.TargetDatabaseID)
				if err != nil {
					continue
				}
				name := k8s.CronJobName("restore", tgt.ID)
				wanted[name] = true // до ensure — см. dump выше
				wantedInstances[tgt.InstanceID] = true
				if err := o.ensureCronJob(ctx, "restore", tgt, knownHash[name]); err != nil {
					log.Printf("operator: ensure restore cronjob for %s: %v", tgt.DBName, err)
					continue
				}

				inst, err := o.store.GetInstance(ctx, tgt.InstanceID)
				if err != nil || restoreModeOf(inst) != model.RestoreInPlacePerDB {
					continue
				}
				// Нет кредов — секрета нет; StartRestore откажет с понятной ошибкой.
				if err := o.upsertDatabaseCreds(ctx, tgt); err != nil {
					if !errors.Is(err, ErrNoRestoreCredentials) {
						log.Printf("operator: upsert restore creds for %s: %v", tgt.DBName, err)
					}
					continue
				}
				wantedDBCreds[tgt.ID] = true
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

	dbSecrets, err := o.k8s.ListManagedDBCredsSecrets(ctx)
	if err != nil {
		log.Printf("operator: list db creds secrets: %v", err)
		return
	}
	for databaseID := range dbSecrets {
		if !wantedDBCreds[databaseID] {
			_ = o.k8s.DeleteDBCredsSecret(ctx, databaseID)
		}
	}
}
