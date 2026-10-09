package k8s

import (
	"context"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Phase — упрощённый статус Job'а для watcher'а.
type Phase string

const (
	PhaseRunning   Phase = "running"
	PhaseSucceeded Phase = "succeeded"
	PhaseFailed    Phase = "failed"
)

// JobStatus — снимок состояния одного Job'а.
type JobStatus struct {
	Name       string
	RunID      string // из лейбла; пусто у scheduled-Job'ов, созданных CronJob'ом
	DatabaseID string
	Kind       string // "dump" | "restore"
	Trigger    string // "manual" | "scheduled" | ""
	Phase      Phase
	Message    string
}

// TriggerJobFromCronJob создаёт Job из шаблона CronJob'а базы — ручной запуск
// проходит тем же путём, что и запуск по расписанию. extraEnv доклеивается в
// первый контейнер, extraLabels — на Job и pod-template.
func (c *Client) TriggerJobFromCronJob(ctx context.Context, cronJobName, jobName string, extraLabels, extraEnv map[string]string) (string, error) {
	cj, err := c.cs.BatchV1().CronJobs(c.namespace).Get(ctx, cronJobName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get cronjob %s: %w", cronJobName, err)
	}
	tmpl := cj.Spec.JobTemplate
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:   jobName,
			Labels: mergeStrMap(tmpl.Labels, extraLabels),
			// Owner — сам CronJob (как у Job'ов по расписанию): виден под ним в
			// дереве, каскадно чистится, и контроллер CronJob'а учитывает его в
			// ConcurrencyPolicy/historyLimit. BlockOwnerDeletion=false — иначе
			// нужен доступ к cronjobs/finalizers.
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "batch/v1",
				Kind:               "CronJob",
				Name:               cj.Name,
				UID:                cj.UID,
				Controller:         boolPtr(true),
				BlockOwnerDeletion: boolPtr(false),
			}},
		},
		Spec: *tmpl.Spec.DeepCopy(),
	}
	job.Spec.Template.Labels = mergeStrMap(job.Spec.Template.Labels, extraLabels)
	if len(extraEnv) > 0 && len(job.Spec.Template.Spec.Containers) > 0 {
		ct := &job.Spec.Template.Spec.Containers[0]
		for k, v := range extraEnv {
			ct.Env = upsertEnvVar(ct.Env, k, v)
		}
	}
	created, err := c.cs.BatchV1().Jobs(c.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create job from cronjob %s: %w", cronJobName, err)
	}
	log.Printf("k8s: Job %s created from CronJob %s", created.Name, cronJobName)
	return created.Name, nil
}

func mergeStrMap(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func upsertEnvVar(env []corev1.EnvVar, name, value string) []corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			env[i].Value = value
			env[i].ValueFrom = nil
			return env
		}
	}
	return append(env, corev1.EnvVar{Name: name, Value: value})
}

// DeleteJob удаляет Job вместе с подами (propagation=Background).
func (c *Client) DeleteJob(ctx context.Context, name string) error {
	policy := metav1.DeletePropagationBackground
	err := c.cs.BatchV1().Jobs(c.namespace).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &policy})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err == nil {
		log.Printf("k8s: Job %s deleted", name)
	}
	return err
}

// ListManagedJobs возвращает статус всех Job'ов, помеченных Gemini.
func (c *Client) ListManagedJobs(ctx context.Context) ([]JobStatus, error) {
	list, err := c.cs.BatchV1().Jobs(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: LabelManagedBy + "=" + ManagedByValue,
	})
	if err != nil {
		return nil, err
	}
	out := make([]JobStatus, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, jobStatus(&list.Items[i]))
	}
	return out, nil
}

// FailureDetail достаёт настоящую причину падения Job'а из его подов: условие
// Job'а вида "BackoffLimitExceeded" саму ошибку (pg_dump/pg_restore) не несёт.
// Порядок: terminationMessage упавшего контейнера → хвост его логов с фильтром
// на строки, похожие на ошибку → код выхода. Пусто, если поды уже вычищены или
// ничего осмысленного не нашлось — тогда вызывающий оставляет условие Job'а.
func (c *Client) FailureDetail(ctx context.Context, jobName string) string {
	pods, err := c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}
	// Самый свежий под — последняя попытка Job'а.
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].CreationTimestamp.After(pods.Items[j].CreationTimestamp.Time)
	})
	for i := range pods.Items {
		pod := &pods.Items[i]
		for _, cs := range pod.Status.ContainerStatuses {
			t := cs.State.Terminated
			if t == nil || t.ExitCode == 0 {
				continue
			}
			if msg := strings.TrimSpace(t.Message); msg != "" {
				return msg
			}
			if logMsg := c.containerErrorLog(ctx, pod.Name, cs.Name); logMsg != "" {
				return logMsg
			}
			reason := t.Reason
			if reason == "" {
				reason = "Error"
			}
			return fmt.Sprintf("контейнер %s: %s, код выхода %d", cs.Name, reason, t.ExitCode)
		}
	}
	return ""
}

// containerErrorLog берёт хвост логов контейнера и выбирает строки, похожие на
// причину падения; если явных маркеров нет — последнюю непустую строку.
func (c *Client) containerErrorLog(ctx context.Context, podName, containerName string) string {
	tail := int64(50)
	req := c.cs.CoreV1().Pods(c.namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tail,
	})
	rc, err := req.Stream(ctx)
	if err != nil {
		return ""
	}
	defer rc.Close()
	data, _ := io.ReadAll(io.LimitReader(rc, 64*1024))
	return pickErrorLines(string(data))
}

// pickErrorLines выбирает из текста логов строки, похожие на причину падения
// (последние 5 — первопричина обычно ближе к концу); если маркеров нет —
// последнюю непустую строку.
func pickErrorLines(raw string) string {
	var hits []string
	var lastNonEmpty string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lastNonEmpty = line
		if looksLikeError(line) {
			hits = append(hits, line)
		}
	}
	if len(hits) > 5 {
		hits = hits[len(hits)-5:]
	}
	if len(hits) > 0 {
		return strings.Join(hits, "\n")
	}
	return lastNonEmpty
}

// Семантические маркеры, а не имена утилит: без -v pg_dump/pg_restore пишут в
// stderr только ошибки и предупреждения, но строку "pg_dump: dumping table …"
// (verbose) ловить не хотим.
var errorMarkers = []string{
	"error:", "error ", "fatal:", "fatal ", "panic:", "err=",
	"could not", "level=error", "level=fatal", "permission denied",
	"connection refused", "no such host", "timeout",
}

func looksLikeError(line string) bool {
	l := strings.ToLower(line)
	for _, m := range errorMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

func jobStatus(j *batchv1.Job) JobStatus {
	st := JobStatus{
		Name:       j.Name,
		RunID:      j.Labels[LabelRunID],
		DatabaseID: j.Labels[LabelDatabaseID],
		Kind:       j.Labels[LabelComponent],
		Trigger:    j.Labels[LabelTrigger],
		Phase:      PhaseRunning,
	}
	for _, cond := range j.Status.Conditions {
		if cond.Status != "True" {
			continue
		}
		switch cond.Type {
		case batchv1.JobComplete:
			st.Phase = PhaseSucceeded
		case batchv1.JobFailed:
			st.Phase = PhaseFailed
			st.Message = cond.Reason
			if cond.Message != "" {
				st.Message = cond.Reason + ": " + cond.Message
			}
		}
	}
	return st
}
