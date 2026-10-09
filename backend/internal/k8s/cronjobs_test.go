package k8s

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// dump/restore-Job'ы должны тянуть образ по тем же правилам, что backend —
// иначе при :latest + PullAlways они застревают на закешированном образе.
func TestReconcileCronJobInheritsImagePullPolicy(t *testing.T) {
	const ns = "gemini"
	cs := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gemini-backend", Namespace: ns,
			Labels: map[string]string{LabelInstance: "gemini-source"},
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				ImagePullSecrets: []corev1.LocalObjectReference{{Name: "harbor"}},
				Containers: []corev1.Container{{
					Name: "backend", Image: "harbor/gemini:latest",
					ImagePullPolicy: corev1.PullAlways,
				}},
			}},
		},
	})
	c := &Client{cs: cs, namespace: ns}
	if err := c.ResolveOwner(context.Background(), "gemini-backend"); err != nil {
		t.Fatalf("ResolveOwner: %v", err)
	}

	if err := c.ReconcileCronJob(context.Background(), CronSpec{
		Kind: "dump", DatabaseID: "db1", Image: "harbor/gemini:latest",
	}); err != nil {
		t.Fatalf("ReconcileCronJob: %v", err)
	}

	cj, err := cs.BatchV1().CronJobs(ns).Get(context.Background(), CronJobName("dump", "db1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get cronjob: %v", err)
	}
	got := cj.Spec.JobTemplate.Spec.Template.Spec.Containers[0].ImagePullPolicy
	if got != corev1.PullAlways {
		t.Errorf("ImagePullPolicy = %q, want %q", got, corev1.PullAlways)
	}
	if secs := cj.Spec.JobTemplate.Spec.Template.Spec.ImagePullSecrets; len(secs) != 1 || secs[0].Name != "harbor" {
		t.Errorf("ImagePullSecrets = %v, want [harbor]", secs)
	}
}

// TTL Job'ов берётся из CronSpec (настройка в UI), без него — дефолт 5 минут.
func TestReconcileCronJobTTL(t *testing.T) {
	const ns = "gemini"
	for _, tc := range []struct {
		name string
		in   int32
		want int32
	}{
		{"default", 0, defaultJobTTLSeconds},
		{"custom", 30 * 60, 30 * 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := fake.NewSimpleClientset()
			c := &Client{cs: cs, namespace: ns}
			if err := c.ReconcileCronJob(context.Background(), CronSpec{
				Kind: "dump", DatabaseID: "db1", TTLSecondsAfterFinished: tc.in,
			}); err != nil {
				t.Fatalf("ReconcileCronJob: %v", err)
			}
			cj, err := cs.BatchV1().CronJobs(ns).Get(context.Background(), CronJobName("dump", "db1"), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get cronjob: %v", err)
			}
			if got := cj.Spec.JobTemplate.Spec.TTLSecondsAfterFinished; got == nil || *got != tc.want {
				t.Errorf("TTLSecondsAfterFinished = %v, want %d", got, tc.want)
			}
		})
	}
}
