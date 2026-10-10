// Package operator оркестрирует dump/restore-прогоны: резолвит креды, создаёт
// ephemeral k8s Secret, запускает Job, следит за его завершением, чистит секрет,
// пишет статус в BackupRun и дёргает межголовые webhook'и / нотификации.
package operator

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/k8s"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/notify"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/peer"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/vault"
)

// Operator держит зависимости оркестрации.
type Operator struct {
	cfg      *config.Config
	store    *store.Store
	k8s      *k8s.Client
	vault    *vault.Client
	buckets  *storage.Pool
	notifier *notify.Notifier
	peer     *peer.Client

	// baseOverrides — nodeSelector/tolerations/affinity/resources из values Helm-чарта.
	baseOverrides k8s.PodOverrides
}

// New собирает Operator. k8s может быть nil при локальной разработке —
// тогда запуск прогонов вернёт ошибку, но API останется работоспособным.
func New(cfg *config.Config, st *store.Store, kc *k8s.Client, vc *vault.Client,
	sc *storage.Pool, nt *notify.Notifier, pc *peer.Client) *Operator {
	ovr, err := k8s.ParsePodOverrides(cfg.JobPodOverridesJSON)
	if err != nil {
		log.Printf("operator: %v — job pod overrides ignored", err)
	}
	return &Operator{cfg: cfg, store: st, k8s: kc, vault: vc, buckets: sc, notifier: nt, peer: pc, baseOverrides: ovr}
}

// jobParams разрешает временное хранилище, ресурсы и TTL (секунды) для Job'ов базы:
// Database.Storage* → app_settings → config (env из Helm).
func (o *Operator) jobParams(ctx context.Context, db *model.Database) (storageType, storageSize string, overrides k8s.PodOverrides, ttl int32) {
	storageType = string(db.StorageType)
	storageSize = db.StorageSize
	overrides = o.baseOverrides

	if st, err := o.store.GetSettings(ctx); err == nil && st != nil {
		if storageType == "" {
			storageType = string(st.DefaultStorageType)
		}
		if storageSize == "" {
			storageSize = st.DefaultStorageSize
		}
		ttl = st.JobTTLMinutes * 60
		r := st.DefaultResources
		overrides = overrides.WithResources(r.Requests.CPU, r.Requests.Memory, r.Limits.CPU, r.Limits.Memory)
		if len(st.DefaultPodScheduling) > 0 {
			if sched, err := k8s.ParsePodOverrides(string(st.DefaultPodScheduling)); err == nil {
				overrides = overrides.MergeScheduling(sched)
			} else {
				log.Printf("operator: bad default_pod_scheduling in settings: %v", err)
			}
		}
	}
	if storageType == "" {
		storageType = o.cfg.JobStorageType
	}
	if storageSize == "" {
		storageSize = o.cfg.JobWorkDirSize
	}
	return
}

// ErrNoK8s — операция требует доступа к кластеру, которого нет.
var ErrNoK8s = errors.New("operator: kubernetes client not configured")

// safely выполняет один тик фонового цикла, превращая панику в лог.
// Фоновые горутины не должны ронять весь процесс из-за одной плохой записи в
// БД: до появления этой обёртки невалидный storage_size в databases/app_settings
// доезжал до PodSpec, паниковал в resource.MustParse и клал голову в CrashLoop,
// который сам не расходился (значение-то оставалось в БД).
func safely(what string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("operator: PANIC in %s: %v\n%s", what, r, debug.Stack())
		}
	}()
	f()
}

// resolveCreds достаёт username/password инстанса (plain или через Vault).
func (o *Operator) resolveCreds(ctx context.Context, instanceID string) (user, pass string, err error) {
	c, err := o.store.Credentials(ctx, instanceID)
	if err != nil {
		return "", "", err
	}
	switch c.AuthType {
	case model.AuthPlain:
		return c.Username, c.Password, nil
	case model.AuthVault:
		if o.vault == nil {
			return "", "", errors.New("operator: instance uses vault auth but Vault is disabled")
		}
		ds, err := o.vault.ReadDBSecret(ctx, c.VaultPath, c.VaultUsernameKey, c.VaultPasswordKey)
		if err != nil {
			return "", "", err
		}
		return ds.Username, ds.Password, nil
	default:
		return "", "", fmt.Errorf("operator: unknown auth_type %q", c.AuthType)
	}
}

// credsSecretData — содержимое секрета кред инстанса (без имени базы: PGDATABASE
// прокидывается в Job отдельной переменной, секрет один на инстанс).
// S3_BUCKETS — все включённые бакеты с ключами: dump льёт во все, restore
// качает из первого, где дамп есть (S3_BUCKET_ID — предпочтительный).
func credsSecretData(inst *model.Instance, user, pass string, buckets []model.S3Bucket) map[string]string {
	jb := make([]storage.JobBucket, 0, len(buckets))
	for _, b := range buckets {
		jb = append(jb, storage.JobBucketOf(b))
	}
	data := map[string]string{
		"PGHOST":     inst.Host,
		"PGPORT":     fmt.Sprintf("%d", inst.Port),
		"PGUSER":     user,
		"PGPASSWORD": pass,
		"PGSSLMODE":  inst.SSLMode,
		"S3_BUCKETS": string(mustJSON(jb)),
	}
	// Кастомный корневой CA прокидываем PEM'ом: jobrunner материализует его в
	// файл и укажет на него PGSSLROOTCERT (у libpq это только путь, не содержимое).
	if inst.SSLRootCert != "" && (inst.SSLMode == "verify-ca" || inst.SSLMode == "verify-full") {
		data["PGSSLROOTCERT_PEM"] = inst.SSLRootCert
	}
	return data
}

// upsertInstanceCreds резолвит креды инстанса и обновляет его общий секрет.
func (o *Operator) upsertInstanceCreds(ctx context.Context, inst *model.Instance) error {
	user, pass, err := o.resolveCreds(ctx, inst.ID)
	if err != nil {
		return fmt.Errorf("resolve credentials: %w", err)
	}
	buckets, err := o.buckets.Buckets(ctx)
	if err != nil {
		return err
	}
	if len(buckets) == 0 {
		return storage.ErrNoBuckets
	}
	_, err = o.k8s.UpsertCredsSecret(ctx, inst.ID, credsSecretData(inst, user, pass, buckets))
	return err
}

// resolveDumpKey возвращает ключ самого свежего дампа базы в S3 (для прогонов,
// где раннер сам сгенерировал имя объекта — scheduled).
func (o *Operator) resolveDumpKey(ctx context.Context, db *model.Database) string {
	inst, err := o.store.GetInstance(ctx, db.InstanceID)
	if err != nil {
		return ""
	}
	objs, err := o.buckets.ListDumps(ctx, inst.Name+"/"+db.DBName+"/")
	if err != nil {
		return ""
	}
	return latest(objs)
}

// StartDump запускает ручной дамп базы (голова Source): гарантирует CronJob-шаблон
// и секрет кред, затем создаёт Job из этого шаблона — тем же путём, что и по расписанию.
func (o *Operator) StartDump(ctx context.Context, db *model.Database, trigger model.RunTrigger, initiatedBy string) (*model.BackupRun, error) {
	if o.k8s == nil {
		return nil, ErrNoK8s
	}
	inst, err := o.store.GetInstance(ctx, db.InstanceID)
	if err != nil {
		return nil, err
	}
	if err := o.upsertInstanceCreds(ctx, inst); err != nil {
		return nil, err
	}
	if err := o.ensureCronJob(ctx, "dump", db); err != nil {
		return nil, err
	}
	objectKey := fmt.Sprintf("%s/%s/%s.dump", inst.Name, db.DBName, time.Now().UTC().Format("20060102T150405Z"))

	run, err := o.store.CreateRun(ctx, store.NewRunInput{
		DatabaseID:  db.ID,
		Kind:        model.KindDump,
		Trigger:     trigger,
		S3ObjectKey: objectKey,
		InitiatedBy: initiatedBy,
	})
	if errors.Is(err, store.ErrConflict) {
		return nil, store.ErrConflict
	}
	if err != nil {
		return nil, err
	}

	jobName, err := o.k8s.TriggerJobFromCronJob(ctx, k8s.CronJobName("dump", db.ID), "gemini-dump-"+run.ID,
		map[string]string{k8s.LabelRunID: run.ID, k8s.LabelTrigger: "manual"},
		map[string]string{"GEMINI_RUN_ID": run.ID, "S3_OBJECT_KEY": objectKey},
	)
	if err != nil {
		o.failRun(ctx, run.ID, db.DBName, model.KindDump, err.Error())
		return nil, err
	}

	return o.store.UpdateRun(ctx, run.ID, store.RunUpdate{
		Status: model.StatusRunning, K8sJobName: &jobName, MarkStarted: true,
	})
}

// ensureCronJob обновляет CronJob-шаблон базы. Секрет кред инстанса — отдельно
// (upsertInstanceCreds), чтобы не дёргать его по каждой базе инстанса.
// dump: расписание из db.ScheduleCron (suspend, если пусто), коннект под саму базу.
// restore: всегда suspend; RESTORE_MODE берётся из инстанса, TARGET_DB_OWNER
// добавляется на запуск (он в сопоставлении, не в базе) — см. StartRestore.
func (o *Operator) ensureCronJob(ctx context.Context, kind string, db *model.Database) error {
	inst, err := o.store.GetInstance(ctx, db.InstanceID)
	if err != nil {
		return err
	}
	schedule := db.ScheduleCron
	// PGDATABASE прокидывается переменной (секрет кред — один на инстанс).
	env := map[string]string{
		"PGDATABASE": db.DBName,
	}
	switch kind {
	case "dump":
		env["S3_PREFIX"] = inst.Name + "/" + db.DBName
		env["SOURCE_DATABASE_EXTERNAL_ID"] = db.ExternalID
		env["INSTANCE_ID"] = db.InstanceID
	case "restore":
		schedule = ""
		// PGDATABASE = целевая база: in_place коннектится прямо в неё (на MDB
		// базы postgres в пуле нет); recreate для DDL уровня БД явно ходит в postgres.
		env["PGDATABASE"] = db.DBName
		env["TARGET_DB_NAME"] = db.DBName
		env["RESTORE_MODE"] = string(restoreModeOf(inst))
	}

	storageType, storageSize, overrides, ttl := o.jobParams(ctx, db)
	return o.k8s.ReconcileCronJob(ctx, k8s.CronSpec{
		Kind:            kind,
		DatabaseID:      db.ID,
		DatabaseName:    db.DBName,
		Schedule:        schedule,
		TimeZone:        o.cfg.JobTimezone,
		Suspend:         schedule == "",
		Image:           o.cfg.JobImage,
		ServiceAccount:  o.cfg.JobServiceAccount,
		CredsSecretName: k8s.CredsSecretName(inst.ID),
		StorageType:     storageType,
		StorageClass:    o.cfg.JobStorageClass,
		WorkDirSize:     storageSize,
		Overrides:       overrides,
		Env:             env,

		TTLSecondsAfterFinished: ttl,
	})
}

// restoreModeOf — стратегия restore инстанса, с дефолтом на recreate
// (старые строки до миграции 011 и любой пустой ввод).
func restoreModeOf(inst *model.Instance) model.RestoreMode {
	if inst.RestoreMode == model.RestoreInPlace {
		return model.RestoreInPlace
	}
	return model.RestoreRecreate
}

// StartRestore запускает restore целевой базы (голова DR). bucketID — бакет,
// где дамп точно лежит; Job начнёт с него и при ошибке перейдёт к остальным.
func (o *Operator) StartRestore(ctx context.Context, m *model.DatabaseMapping, s3Key, checksum, bucketID string, trigger model.RunTrigger, eventKey string) (*model.BackupRun, error) {
	if o.k8s == nil {
		return nil, ErrNoK8s
	}
	target, err := o.store.GetDatabase(ctx, m.TargetDatabaseID)
	if err != nil {
		return nil, err
	}
	inst, err := o.store.GetInstance(ctx, target.InstanceID)
	if err != nil {
		return nil, err
	}
	if err := o.upsertInstanceCreds(ctx, inst); err != nil {
		return nil, err
	}
	if err := o.ensureCronJob(ctx, "restore", target); err != nil {
		return nil, err
	}

	run, err := o.store.CreateRun(ctx, store.NewRunInput{
		DatabaseID:  target.ID,
		Kind:        model.KindRestore,
		Trigger:     trigger,
		S3ObjectKey: s3Key,
		Checksum:    checksum,
		EventKey:    eventKey,
		InitiatedBy: "webhook:source",
	})
	if errors.Is(err, store.ErrConflict) {
		return nil, store.ErrConflict
	}
	if err != nil {
		return nil, err
	}

	jobEnv := map[string]string{
		"GEMINI_RUN_ID":   run.ID,
		"S3_OBJECT_KEY":   s3Key,
		"EXPECTED_SHA256": checksum,
		"RESTORE_MODE":    string(restoreModeOf(inst)),
		"S3_BUCKET_ID":    bucketID,
	}
	if m.TargetOwner != "" {
		jobEnv["TARGET_DB_OWNER"] = m.TargetOwner
	}
	jobName, err := o.k8s.TriggerJobFromCronJob(ctx, k8s.CronJobName("restore", target.ID), "gemini-restore-"+run.ID,
		map[string]string{k8s.LabelRunID: run.ID, k8s.LabelTrigger: string(trigger)},
		jobEnv,
	)
	if err != nil {
		o.failRun(ctx, run.ID, target.DBName, model.KindRestore, err.Error())
		return nil, err
	}
	return o.store.UpdateRun(ctx, run.ID, store.RunUpdate{
		Status: model.StatusRunning, K8sJobName: &jobName, MarkStarted: true,
	})
}

func (o *Operator) failRun(ctx context.Context, runID, dbName string, kind model.RunKind, msg string) {
	msg = notify.Sanitize(msg)
	_, _ = o.store.UpdateRun(ctx, runID, store.RunUpdate{
		Status: model.StatusFailed, ErrorMessage: &msg, MarkFinished: true,
	})
	if o.notifier != nil {
		_ = o.notifier.Failure(ctx, notify.Event{
			HeadRole: string(o.cfg.Role), Kind: string(kind), Database: dbName,
			RunID: runID, Error: msg, OccurredAt: time.Now(),
		})
	}
}

// Watch раз в 10 секунд сверяет активные прогоны с состоянием Job'ов в кластере.
func (o *Operator) Watch(ctx context.Context) {
	if o.k8s == nil {
		log.Printf("operator: watcher disabled (no kubernetes client)")
		return
	}
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			safely("reconcileOnce", func() {
				if err := o.reconcileOnce(ctx); err != nil {
					log.Printf("operator: watch tick: %v", err)
				}
			})
		}
	}
}

// defaultPodStartTimeout — таймаут старта пода, если настройки не прочитались.
const defaultPodStartTimeout = 10 * time.Minute

// podStartTimeout — сколько Job может ждать старта пода (app_settings).
func (o *Operator) podStartTimeout(ctx context.Context) time.Duration {
	st, err := o.store.GetSettings(ctx)
	if err != nil || st == nil || st.JobPodStartTimeoutMinutes <= 0 {
		return defaultPodStartTimeout
	}
	return time.Duration(st.JobPodStartTimeoutMinutes) * time.Minute
}

// failIfPodNotStarted закрывает прогон, у Job'а которого за timeout так и не
// запустился под (FailedCreate из-за кривого PVC, Pending без нод, ErrImagePull):
// иначе прогон висел бы running до activeDeadlineSeconds. Job удаляется, чтобы
// контроллер перестал пытаться создать под.
func (o *Operator) failIfPodNotStarted(ctx context.Context, js k8s.JobStatus, timeout time.Duration) {
	stuck, reason := o.k8s.PodNotStarted(ctx, js.Name)
	if !stuck {
		return
	}
	run := o.runForJob(ctx, js)
	if run == nil || !run.Active() {
		return
	}
	if err := o.k8s.DeleteJob(ctx, js.Name); err != nil {
		log.Printf("operator: delete stuck job %s: %v", js.Name, err)
		return
	}
	o.onRunFailed(ctx, *run, fmt.Sprintf("под не запустился за %s — %s", timeout, reason))
}

// ErrRunNotActive — прогон уже завершён, останавливать нечего.
var ErrRunNotActive = errors.New("operator: run is not active")

// CancelRun останавливает активный прогон: удаляет его Job (вместе с подами) и
// закрывает прогон как failed. Job ищется по имени из прогона, а для ручных
// запусков — ещё и по лейблу run-id (имя могло не успеть записаться).
func (o *Operator) CancelRun(ctx context.Context, runID, by string) (*model.BackupRun, error) {
	run, err := o.store.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if !run.Active() {
		return nil, ErrRunNotActive
	}
	if o.k8s != nil {
		names := map[string]bool{}
		if run.K8sJobName != "" {
			names[run.K8sJobName] = true
		}
		statuses, err := o.k8s.ListManagedJobs(ctx)
		if err != nil {
			return nil, err
		}
		for _, js := range statuses {
			if js.RunID == run.ID {
				names[js.Name] = true
			}
		}
		for name := range names {
			if err := o.k8s.DeleteJob(ctx, name); err != nil {
				return nil, fmt.Errorf("delete job %s: %w", name, err)
			}
		}
	}
	msg := "остановлен вручную"
	if by != "" {
		msg += ": " + by
	}
	o.onRunFailed(ctx, *run, msg)
	return o.store.GetRun(ctx, runID)
}

// stuckRunGrace — сколько прогон может «висеть» активным без Job'а в кластере,
// прежде чем reaper принудительно закроет его как failed.
const stuckRunGrace = 15 * time.Minute

func (o *Operator) reconcileOnce(ctx context.Context) error {
	statuses, err := o.k8s.ListManagedJobs(ctx)
	if err != nil {
		return err
	}
	podStartTimeout := time.Duration(-1) // читаем настройку лениво, раз за тик
	for _, js := range statuses {
		if js.Phase == k8s.PhaseRunning {
			if podStartTimeout < 0 {
				podStartTimeout = o.podStartTimeout(ctx)
			}
			if time.Since(js.Created) > podStartTimeout {
				o.failIfPodNotStarted(ctx, js, podStartTimeout)
			}
			continue // ждём терминального состояния
		}
		run := o.runForJob(ctx, js)
		if run == nil || !run.Active() {
			continue
		}
		switch js.Phase {
		case k8s.PhaseSucceeded:
			o.onRunSucceeded(ctx, *run)
		case k8s.PhaseFailed:
			// Условие Job'а ("BackoffLimitExceeded: …") саму ошибку не несёт —
			// вытаскиваем её из подов; если не вышло, оставляем условие Job'а.
			msg := js.Message
			if detail := o.k8s.FailureDetail(ctx, js.Name); detail != "" {
				msg = detail
			}
			o.onRunFailed(ctx, *run, msg)
		}
	}
	o.reapStuckRuns(ctx, statuses)
	return nil
}

// reapStuckRuns закрывает прогоны, которые в БД активны дольше stuckRunGrace, а
// соответствующего Job'а в кластере уже нет (удалён по TTL / вручную / пока
// backend лежал). Без этого такой прогон висит в pending/running вечно.
func (o *Operator) reapStuckRuns(ctx context.Context, statuses []k8s.JobStatus) {
	stale, err := o.store.ListActiveRuns(ctx, stuckRunGrace)
	if err != nil {
		log.Printf("operator: reaper list active runs: %v", err)
		return
	}
	if len(stale) == 0 {
		return
	}
	liveByRun := map[string]bool{}
	liveByName := map[string]bool{}
	for _, js := range statuses {
		liveByName[js.Name] = true
		if js.RunID != "" {
			liveByRun[js.RunID] = true
		}
	}
	for i := range stale {
		run := stale[i]
		if liveByRun[run.ID] || (run.K8sJobName != "" && liveByName[run.K8sJobName]) {
			continue
		}
		log.Printf("operator: reaper — run %s (%s, job=%q) active %s+ with no Job in cluster → failed",
			run.ID, run.Kind, run.K8sJobName, stuckRunGrace)
		o.onRunFailed(ctx, run, "k8s Job отсутствует в кластере — прогон принудительно закрыт")
	}
}

// runForJob сопоставляет Job с прогоном. Job с лейблом run-id — прогон, созданный
// заранее (ручной запуск/restore). Job без run-id (создан самим CronJob'ом по
// расписанию) — «усыновляем»: заводим строку BackupRun с trigger=scheduled,
// дедуп по имени Job'а (event_key).
func (o *Operator) runForJob(ctx context.Context, js k8s.JobStatus) *model.BackupRun {
	if js.RunID != "" {
		r, err := o.store.GetRun(ctx, js.RunID)
		if err != nil {
			return nil
		}
		return r
	}
	if js.Kind != string(model.KindDump) || js.DatabaseID == "" {
		return nil
	}
	eventKey := "job:" + js.Name
	if r, err := o.store.RunByEventKey(ctx, eventKey); err == nil {
		return r
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil
	}
	run, err := o.store.CreateRun(ctx, store.NewRunInput{
		DatabaseID:  js.DatabaseID,
		Kind:        model.KindDump,
		Trigger:     model.TriggerScheduled,
		EventKey:    eventKey,
		InitiatedBy: "cronjob",
	})
	if errors.Is(err, store.ErrConflict) {
		// уже есть активный прогон этой базы — параллельный Job лишний
		_ = o.k8s.DeleteJob(ctx, js.Name)
		return nil
	}
	if err != nil {
		log.Printf("operator: adopt scheduled job %s: %v", js.Name, err)
		return nil
	}
	updated, err := o.store.UpdateRun(ctx, run.ID, store.RunUpdate{
		Status: model.StatusRunning, K8sJobName: &js.Name, MarkStarted: true,
	})
	if err != nil {
		return nil
	}
	log.Printf("operator: adopted scheduled dump job %s → run %s (db=%s)", js.Name, run.ID, js.DatabaseID)
	return updated
}

func (o *Operator) onRunSucceeded(ctx context.Context, run model.BackupRun) {
	updated, err := o.store.UpdateRun(ctx, run.ID, store.RunUpdate{Status: model.StatusSucceeded, MarkFinished: true})
	if err != nil {
		log.Printf("operator: mark succeeded %s: %v", run.ID, err)
		return
	}
	log.Printf("operator: run %s (%s) succeeded — job %s", run.ID, run.Kind, run.K8sJobName)

	db, err := o.store.GetDatabase(ctx, run.DatabaseID)
	if err != nil {
		log.Printf("operator: lookup db for run %s: %v", run.ID, err)
		return
	}

	switch run.Kind {
	case model.KindDump:
		if o.cfg.IsSource() {
			// scheduled-прогон не знал ключ заранее (раннер сгенерировал сам) —
			// достаём его по самому свежему дампу в префиксе; sha — из <key>.sha256.
			key := updated.S3ObjectKey
			if key == "" {
				key = o.resolveDumpKey(ctx, db)
			}
			sum := updated.Checksum
			if key != "" && sum == "" {
				for _, c := range o.buckets.ReadyClients(ctx, key) {
					if raw, err := c.ReadChecksum(ctx, key); err == nil {
						sum = firstField(raw)
						break
					}
				}
			}
			if key != updated.S3ObjectKey || sum != updated.Checksum {
				if u, err := o.store.UpdateRun(ctx, run.ID, store.RunUpdate{
					Status: model.StatusSucceeded, S3ObjectKey: &key, Checksum: &sum,
				}); err == nil {
					updated = u
				}
			}
			o.signDump(ctx, db, updated)
			o.notifyPeerBackupReady(ctx, db, updated)
		}
	case model.KindRestore:
		if o.cfg.IsTarget() {
			o.notifyPeerRestoreStatus(ctx, updated, model.StatusSucceeded, "")
		}
	}
}

func (o *Operator) onRunFailed(ctx context.Context, run model.BackupRun, msg string) {
	if msg == "" {
		msg = "job failed"
	}
	msg = notify.Sanitize(msg)
	updated, err := o.store.UpdateRun(ctx, run.ID, store.RunUpdate{
		Status: model.StatusFailed, ErrorMessage: &msg, MarkFinished: true,
	})
	if err != nil {
		log.Printf("operator: mark failed %s: %v", run.ID, err)
		return
	}
	log.Printf("operator: run %s (%s) failed — job %s: %s", run.ID, run.Kind, run.K8sJobName, msg)

	db, _ := o.store.GetDatabase(ctx, run.DatabaseID)
	dbName := ""
	if db != nil {
		dbName = db.DBName
	}
	if o.notifier != nil {
		_ = o.notifier.Failure(ctx, notify.Event{
			HeadRole: string(o.cfg.Role), Kind: string(run.Kind), Database: dbName,
			RunID: run.ID, Error: msg, OccurredAt: time.Now(),
		})
	}
	if run.Kind == model.KindRestore && o.cfg.IsTarget() {
		o.notifyPeerRestoreStatus(ctx, updated, model.StatusFailed, msg)
	}
}

// signDump кладёт рядом с дампом подпись манифеста (ключ + sha256) приватным
// ключом Source. Подписывает голова, а не dump-Job: приватник в поды не попадает.
// Подпись асимметричная и самодостаточная — один .sig проверяют все Target'ы;
// кладётся в каждый бакет, где дамп залит. Бакеты без дампа (заливка туда упала)
// — нотификация: копия дампа там не появится.
func (o *Operator) signDump(ctx context.Context, db *model.Database, run *model.BackupRun) {
	if run.S3ObjectKey == "" || run.Checksum == "" {
		return
	}
	priv, err := o.store.HeadPrivateKey(ctx)
	if err != nil {
		log.Printf("operator: no head key — dump %s left unsigned: %v", run.S3ObjectKey, err)
		return
	}
	sig := peer.SignArtifact(priv, run.S3ObjectKey, run.Checksum)
	clients, _ := o.buckets.Clients(ctx)
	var missing []string
	for _, c := range clients {
		ready, err := c.ChecksumReady(ctx, run.S3ObjectKey)
		if err != nil || !ready {
			missing = append(missing, c.Name())
			continue
		}
		if err := c.PutSignature(ctx, run.S3ObjectKey, sig); err != nil {
			log.Printf("operator: upload signature for %s: %v", c.Object(run.S3ObjectKey), err)
			continue
		}
		log.Printf("operator: signed dump %s", c.Object(run.S3ObjectKey))
	}
	if len(missing) > 0 && o.notifier != nil {
		_ = o.notifier.Failure(ctx, notify.Event{
			HeadRole: string(o.cfg.Role), Kind: string(model.KindDump), Database: db.DBName, RunID: run.ID,
			Error:      "дамп не залит в бакет(ы): " + strings.Join(missing, ", ") + " — см. лог dump-Job'а",
			OccurredAt: time.Now(),
		})
	}
}

// signatureBackfillWindow — насколько назад BackfillSignatures ищет неподписанные
// дампы. Дампы старше этого окна на Target'е всё равно не восстанавливают.
const signatureBackfillWindow = 30 * 24 * time.Hour

// BackfillSignatures (Source) до-подписывает успешные дампы, у которых в бакете
// ещё нет <key>.sig (дамп завершился раньше, чем сгенерировался ключ головы) или
// подпись старого формата (HMAC до перехода на Ed25519). Проходит по всем
// бакетам, где дамп лежит.
func (o *Operator) BackfillSignatures(ctx context.Context) {
	if !o.cfg.IsSource() {
		return
	}
	clients, _ := o.buckets.Clients(ctx)
	if len(clients) == 0 {
		return
	}
	priv, err := o.store.HeadPrivateKey(ctx)
	if err != nil {
		log.Printf("operator: signature backfill: no head key: %v", err)
		return
	}
	pub := priv.Public().(ed25519.PublicKey)
	runs, err := o.store.ListSignableDumps(ctx, signatureBackfillWindow)
	if err != nil {
		log.Printf("operator: signature backfill: list dumps: %v", err)
		return
	}
	var signed int
	for i := range runs {
		r := runs[i]
		for _, c := range clients {
			if sig, err := c.ReadSignature(ctx, r.S3ObjectKey); err == nil &&
				peer.VerifyArtifact(pub, r.S3ObjectKey, r.Checksum, sig) {
				continue // валидная подпись текущим ключом уже есть
			}
			if ready, err := c.ChecksumReady(ctx, r.S3ObjectKey); err != nil || !ready {
				continue // дампа в этом бакете нет — подписывать нечего
			}
			sig := peer.SignArtifact(priv, r.S3ObjectKey, r.Checksum)
			if err := c.PutSignature(ctx, r.S3ObjectKey, sig); err != nil {
				log.Printf("operator: signature backfill: put %s: %v", c.Object(r.S3ObjectKey), err)
				continue
			}
			signed++
		}
	}
	if signed > 0 {
		log.Printf("operator: signature backfill signed %d dump copy(ies)", signed)
	}
}

// VerifiedChecksum (Target) возвращает sha256 дампа и бакет, где он лежит, ТОЛЬКО
// если подпись рядом сходится с запиненным публичным ключом Source. Это
// единственный доверенный способ узнать сумму из бакета: сам .sha256
// подделывается с дампом. Бакеты перебираются по порядку до первого успеха.
func (o *Operator) VerifiedChecksum(ctx context.Context, s3Key string) (sum, bucketID string, err error) {
	pub, err := o.store.PeerPublicKey(ctx, "source")
	if err != nil || len(pub) == 0 {
		return "", "", errors.New("no pinned Source public key — pair with Source first")
	}
	clients, _ := o.buckets.Clients(ctx)
	if len(clients) == 0 {
		return "", "", storage.ErrNoBuckets
	}
	var errs []error
	for _, c := range clients {
		sum, err := verifiedChecksumIn(ctx, c, pub, s3Key)
		if err == nil {
			return sum, c.ID(), nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
	}
	return "", "", errors.Join(errs...)
}

func verifiedChecksumIn(ctx context.Context, c *storage.Client, pub ed25519.PublicKey, s3Key string) (string, error) {
	raw, err := c.ReadChecksum(ctx, s3Key)
	if err != nil {
		return "", fmt.Errorf("read checksum for %s: %w", s3Key, err)
	}
	sum := firstField(raw)
	if sum == "" {
		return "", fmt.Errorf("empty checksum for %s", s3Key)
	}
	sig, err := c.ReadSignature(ctx, s3Key)
	if err != nil {
		return "", fmt.Errorf("dump %s is not signed by Source: %w", s3Key, err)
	}
	if !peer.VerifyArtifact(pub, s3Key, sum, sig) {
		return "", fmt.Errorf("signature mismatch for %s — refusing to restore", s3Key)
	}
	return sum, nil
}

func (o *Operator) notifyPeerBackupReady(ctx context.Context, db *model.Database, run *model.BackupRun) {
	pairings, err := o.store.ActivePairings(ctx, "target")
	if err != nil || len(pairings) == 0 {
		log.Printf("operator: no active target pairing, backup-ready webhook skipped (targets' fallback poll will pick it up)")
		return
	}
	body := mustJSON(map[string]any{
		"source_database_external_id": db.ExternalID,
		"source_db_name":              db.DBName,
		"s3_object_key":               run.S3ObjectKey,
		"sha256":                      run.Checksum,
		"timestamp":                   time.Now().UTC().Format(time.RFC3339),
	})
	for i := range pairings {
		p := pairings[i]
		sec, err := o.store.PairingSecretByID(ctx, p.ID)
		if err != nil || sec == "" {
			continue
		}
		if err := o.peer.Post(ctx, p.PeerURL, "/webhook/backup-ready", sec, body); err != nil {
			log.Printf("operator: backup-ready webhook to %s failed: %v", p.PeerURL, err)
		}
	}
}

func (o *Operator) notifyPeerRestoreStatus(ctx context.Context, run *model.BackupRun, status model.RunStatus, errMsg string) {
	// Source называет базу своим external_id — берём его из сопоставления по target-базе.
	m, err := o.store.MappingByTargetDatabaseID(ctx, run.DatabaseID)
	if err != nil {
		log.Printf("operator: no mapping for target db %s, restore-status webhook skipped", run.DatabaseID)
		return
	}
	secret, err := o.store.PairingSecret(ctx, "source")
	if err != nil || secret == "" {
		log.Printf("operator: no active Source pairing, restore-status webhook skipped")
		return
	}
	p, err := o.store.ActivePairing(ctx, "source")
	if err != nil {
		return
	}
	body := mustJSON(map[string]any{
		"source_database_external_id": m.SourceDatabaseExternalID,
		"s3_object_key":               run.S3ObjectKey,
		"status":                      status,
		"timestamp":                   time.Now().UTC().Format(time.RFC3339),
		"error_message":               notify.Sanitize(errMsg),
	})
	if err := o.peer.Post(ctx, p.PeerURL, "/webhook/restore-status", secret, body); err != nil {
		log.Printf("operator: restore-status webhook to %s failed: %v", p.PeerURL, err)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // сериализуем только собственные map[string]any — ошибка невозможна
	}
	return b
}

// EventKeyFor строит бизнес-ключ события для идемпотентности (webhook + poll).
func EventKeyFor(kind, s3Key string) string {
	return strings.ToLower(kind) + ":" + s3Key
}
