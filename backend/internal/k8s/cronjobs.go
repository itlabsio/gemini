package k8s

import (
	"context"
	"log"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// neverSchedule — синтаксически валидный cron, который никогда не срабатывает
// (31 февраля). Используется для suspend-CronJob'ов без расписания.
const neverSchedule = "0 0 31 2 *"

// defaultJobTTLSeconds — TTL завершённых Job'ов, если CronSpec его не задал.
const defaultJobTTLSeconds int32 = 300

// CronSpec — CronJob-шаблон прогона для одной базы. Создаётся ВСЕГДА для активной
// базы: dump — на Source по расписанию, restore — на Target всегда в suspend.
// «Запустить сейчас» триггерит Job из этого же шаблона (TriggerJobFromCronJob).
type CronSpec struct {
	Kind         string // "dump" | "restore"
	DatabaseID   string
	DatabaseName string
	Schedule     string // пусто → CronJob в suspend (neverSchedule)
	TimeZone     string
	Suspend      bool

	Image            string
	ServiceAccount   string
	ImagePullSecrets []string
	ImagePullPolicy  corev1.PullPolicy // пусто → IfNotPresent
	InstanceLabel    string
	CredsSecretName  string // стабильный секрет кред (envFrom)
	// Env — несекретные переменные (S3_PREFIX, SOURCE_DATABASE_EXTERNAL_ID, TARGET_DB_NAME, ...).
	Env map[string]string

	StorageType  string
	StorageClass string
	WorkDirSize  string

	// Overrides — nodeSelector / tolerations / affinity / resources подов.
	Overrides PodOverrides

	// TTLSecondsAfterFinished — spec.ttlSecondsAfterFinished Job'ов; 0 → defaultJobTTLSeconds.
	TTLSecondsAfterFinished int32
}

// CronJobName — детерминированное имя CronJob: gemini-<kind>-<databaseID>.
func CronJobName(kind, databaseID string) string {
	return "gemini-" + kind + "-" + databaseID
}

// parseCronJobName разбирает "gemini-<kind>-<databaseID>" обратно.
func parseCronJobName(name string) (kind, databaseID string, ok bool) {
	rest, found := strings.CutPrefix(name, "gemini-")
	if !found {
		return "", "", false
	}
	kind, databaseID, found = strings.Cut(rest, "-")
	return kind, databaseID, found
}

// ReconcileCronJob создаёт или обновляет CronJob-шаблон базы.
func (c *Client) ReconcileCronJob(ctx context.Context, s CronSpec) error {
	name := CronJobName(s.Kind, s.DatabaseID)
	if s.InstanceLabel == "" {
		s.InstanceLabel = c.instanceLabel
	}
	if s.ImagePullSecrets == nil {
		s.ImagePullSecrets = c.imagePullSecrets
	}
	if s.ImagePullPolicy == "" {
		s.ImagePullPolicy = c.imagePullPolicy
	}
	if s.ImagePullPolicy == "" {
		s.ImagePullPolicy = corev1.PullIfNotPresent
	}
	labels := ownerLabels(s.Kind, "", s.DatabaseID, s.InstanceLabel)
	labels[LabelTrigger] = "scheduled" // Job'ы, которые создаст сам CronJob
	tz := s.TimeZone
	if tz == "" {
		tz = "Asia/Yekaterinburg"
	}
	schedule := s.Schedule
	suspend := s.Suspend
	ttl := s.TTLSecondsAfterFinished
	if ttl <= 0 {
		ttl = defaultJobTTLSeconds
	}
	if schedule == "" {
		schedule = neverSchedule
		suspend = true
	}

	env := []corev1.EnvVar{
		{Name: "GEMINI_JOB_KIND", Value: s.Kind},
		{Name: "GEMINI_DATABASE_ID", Value: s.DatabaseID},
		{Name: "WORKDIR", Value: "/work"},
		{Name: "HOME", Value: "/tmp"},
	}
	for k, v := range s.Env {
		env = append(env, corev1.EnvVar{Name: k, Value: v})
	}

	var envFrom []corev1.EnvFromSource
	if s.CredsSecretName != "" {
		envFrom = []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: s.CredsSecretName},
			},
		}}
	}

	pod := corev1.PodSpec{
		RestartPolicy:                corev1.RestartPolicyNever,
		ServiceAccountName:           s.ServiceAccount,
		AutomountServiceAccountToken: boolPtr(false),
		ImagePullSecrets:             pullSecretRefs(s.ImagePullSecrets),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:   boolPtr(true),
			RunAsUser:      int64Ptr(10001),
			RunAsGroup:     int64Ptr(10001),
			FSGroup:        int64Ptr(10001),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		Containers: []corev1.Container{{
			Name:            s.Kind,
			Image:           s.Image,
			ImagePullPolicy: s.ImagePullPolicy,
			Args:            []string{s.Kind},
			Env:             env,
			EnvFrom:         envFrom,
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: boolPtr(false),
				ReadOnlyRootFilesystem:   boolPtr(true),
				Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			},
			Resources: s.Overrides.containerResources(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: "work", MountPath: "/work"},
				{Name: "tmp", MountPath: "/tmp"},
			},
		}},
		Volumes: []corev1.Volume{
			workVolume(s.StorageType, s.StorageClass, s.WorkDirSize),
			{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptrQuantity("256Mi")}}},
		},
	}
	s.Overrides.applyToPod(&pod)

	meta := objectMeta(name, labels)
	meta.OwnerReferences = c.ownerRefs()
	cj := &batchv1.CronJob{
		ObjectMeta: meta,
		Spec: batchv1.CronJobSpec{
			Schedule:                   schedule,
			TimeZone:                   &tz,
			Suspend:                    &suspend,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			StartingDeadlineSeconds:    int64Ptr(1800),
			SuccessfulJobsHistoryLimit: int32Ptr(3),
			FailedJobsHistoryLimit:     int32Ptr(5),
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: batchv1.JobSpec{
					BackoffLimit:            int32Ptr(1),
					ActiveDeadlineSeconds:   int64Ptr(6 * 3600),
					TTLSecondsAfterFinished: int32Ptr(ttl),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       pod,
					},
				},
			},
		},
	}

	existing, err := c.cs.BatchV1().CronJobs(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err = c.cs.BatchV1().CronJobs(c.namespace).Create(ctx, cj, metav1.CreateOptions{}); err != nil {
			return err
		}
		log.Printf("k8s: CronJob %s created (db=%s schedule=%q suspend=%t tz=%s)", name, s.DatabaseName, schedule, suspend, tz)
		return nil
	}
	if err != nil {
		return err
	}
	cj.ObjectMeta.ResourceVersion = existing.ResourceVersion
	if cj.OwnerReferences == nil {
		cj.OwnerReferences = existing.OwnerReferences // не терять привязку, если owner ещё не резолвнут
	}
	if _, err = c.cs.BatchV1().CronJobs(c.namespace).Update(ctx, cj, metav1.UpdateOptions{}); err != nil {
		return err
	}
	wasSuspended := existing.Spec.Suspend != nil && *existing.Spec.Suspend
	if existing.Spec.Schedule != schedule || wasSuspended != suspend {
		log.Printf("k8s: CronJob %s updated (db=%s schedule=%q→%q suspend=%t→%t)",
			name, s.DatabaseName, existing.Spec.Schedule, schedule, wasSuspended, suspend)
	}
	return nil
}

// DeleteCronJob удаляет CronJob-шаблон (kind, база) — при disable/удалении базы.
func (c *Client) DeleteCronJob(ctx context.Context, kind, databaseID string) error {
	name := CronJobName(kind, databaseID)
	err := c.cs.BatchV1().CronJobs(c.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err == nil {
		log.Printf("k8s: CronJob %s deleted", name)
	}
	return err
}

// ManagedCronJob — идентификатор CronJob'а Gemini для сверки при reconcile.
type ManagedCronJob struct {
	Name       string
	Kind       string
	DatabaseID string
}

// ListManagedCronJobs возвращает все CronJob'ы Gemini в namespace.
func (c *Client) ListManagedCronJobs(ctx context.Context) ([]ManagedCronJob, error) {
	list, err := c.cs.BatchV1().CronJobs(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: LabelManagedBy + "=" + ManagedByValue,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ManagedCronJob, 0, len(list.Items))
	for i := range list.Items {
		cj := &list.Items[i]
		kind := cj.Labels[LabelComponent]
		dbID := cj.Labels[LabelDatabaseID]
		if kind == "" || dbID == "" {
			if k, d, ok := parseCronJobName(cj.Name); ok {
				kind, dbID = k, d
			}
		}
		out = append(out, ManagedCronJob{Name: cj.Name, Kind: kind, DatabaseID: dbID})
	}
	return out, nil
}
