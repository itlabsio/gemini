package k8s

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
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

// Source и Target в одном namespace: reconcile каждой головы видит только свои
// объекты, иначе она удаляет CronJob'ы/Job'ы/секреты соседней как осиротевшие.
func TestListManagedFiltersByHeadRole(t *testing.T) {
	const ns = "gemini"
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	src := &Client{cs: cs, namespace: ns, headRole: "source"}
	tgt := &Client{cs: cs, namespace: ns, headRole: "target"}

	if err := src.ReconcileCronJob(ctx, CronSpec{Kind: "dump", DatabaseID: "db1"}); err != nil {
		t.Fatalf("source ReconcileCronJob: %v", err)
	}
	if err := tgt.ReconcileCronJob(ctx, CronSpec{Kind: "restore", DatabaseID: "db2"}); err != nil {
		t.Fatalf("target ReconcileCronJob: %v", err)
	}
	if _, err := src.UpsertCredsSecret(ctx, "i1", nil); err != nil {
		t.Fatalf("source UpsertCredsSecret: %v", err)
	}
	if _, err := tgt.UpsertCredsSecret(ctx, "i2", nil); err != nil {
		t.Fatalf("target UpsertCredsSecret: %v", err)
	}
	// Job, созданный до появления лейбла head-role: принадлежность — по component.
	legacy := map[string]string{LabelManagedBy: ManagedByValue, LabelComponent: "dump"}
	if _, err := cs.BatchV1().Jobs(ns).Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "gemini-dump-legacy", Labels: legacy},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create legacy job: %v", err)
	}

	for _, tc := range []struct {
		c          *Client
		wantCron   string
		wantSecret string
		wantJobs   int
	}{
		{src, CronJobName("dump", "db1"), "i1", 1},
		{tgt, CronJobName("restore", "db2"), "i2", 0},
	} {
		cjs, err := tc.c.ListManagedCronJobs(ctx)
		if err != nil {
			t.Fatalf("%s ListManagedCronJobs: %v", tc.c.headRole, err)
		}
		if len(cjs) != 1 || cjs[0].Name != tc.wantCron {
			t.Errorf("%s cronjobs = %+v, want only %s", tc.c.headRole, cjs, tc.wantCron)
		}
		secrets, err := tc.c.ListManagedCredsSecrets(ctx)
		if err != nil {
			t.Fatalf("%s ListManagedCredsSecrets: %v", tc.c.headRole, err)
		}
		if _, ok := secrets[tc.wantSecret]; len(secrets) != 1 || !ok {
			t.Errorf("%s secrets = %v, want only %s", tc.c.headRole, secrets, tc.wantSecret)
		}
		jobs, err := tc.c.ListManagedJobs(ctx)
		if err != nil {
			t.Fatalf("%s ListManagedJobs: %v", tc.c.headRole, err)
		}
		if len(jobs) != tc.wantJobs {
			t.Errorf("%s jobs = %+v, want %d", tc.c.headRole, jobs, tc.wantJobs)
		}
	}
}

// Принадлежность — по instance (релизу), даже если голов одной роли несколько;
// без известного instance головы — по head-role, у старых объектов — по component.
func TestOwns(t *testing.T) {
	src := &Client{headRole: "source", instanceLabel: "source"}
	src2 := &Client{headRole: "source", instanceLabel: "source-2"}
	tgt := &Client{headRole: "target", instanceLabel: "target"}
	noInst := &Client{headRole: "source"}
	creds := map[string]string{LabelInstance: "source", LabelComponent: "credentials"}
	dump := map[string]string{LabelInstance: "source", LabelHeadRole: "source", LabelComponent: "dump"}
	dumpNoInst := map[string]string{LabelComponent: "dump"}
	for _, tc := range []struct {
		name   string
		c      *Client
		labels map[string]string
		want   bool
	}{
		{"own legacy creds by instance", src, creds, true},
		{"other head's legacy creds", tgt, creds, false},
		{"same role, other release", src2, dump, false},
		{"own dump", src, dump, true},
		{"object without instance is foreign", src, dumpNoInst, false},
		{"no instance: legacy dump by component", noInst, dumpNoInst, true},
		{"no instance: role label wins", noInst, map[string]string{LabelHeadRole: "target", LabelComponent: "dump"}, false},
	} {
		if got := tc.c.owns(tc.labels); got != tc.want {
			t.Errorf("%s: owns = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// in_place_per_db: секрет кред базы подключается вторым envFrom с префиксом
// RESTORE_ — PGUSER/PGPASSWORD инстанса он не перекрывает.
func TestReconcileCronJobDBCredsSecret(t *testing.T) {
	const ns = "gemini"
	for _, tc := range []struct {
		name    string
		dbCreds string
		want    int
	}{
		{"instance creds only", "", 1},
		{"with db creds", DBCredsSecretName("db1"), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := fake.NewSimpleClientset()
			c := &Client{cs: cs, namespace: ns}
			if err := c.ReconcileCronJob(context.Background(), CronSpec{
				Kind: "restore", DatabaseID: "db1",
				CredsSecretName: CredsSecretName("inst1"), DBCredsSecretName: tc.dbCreds,
			}); err != nil {
				t.Fatalf("ReconcileCronJob: %v", err)
			}
			cj, err := cs.BatchV1().CronJobs(ns).Get(context.Background(), CronJobName("restore", "db1"), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get cronjob: %v", err)
			}
			ef := cj.Spec.JobTemplate.Spec.Template.Spec.Containers[0].EnvFrom
			if len(ef) != tc.want {
				t.Fatalf("envFrom = %+v, want %d entries", ef, tc.want)
			}
			if ef[0].Prefix != "" || ef[0].SecretRef.Name != CredsSecretName("inst1") {
				t.Errorf("envFrom[0] = %+v, want unprefixed instance secret", ef[0])
			}
			if tc.want == 2 && (ef[1].Prefix != DBCredsEnvPrefix || ef[1].SecretRef.Name != tc.dbCreds) {
				t.Errorf("envFrom[1] = %+v, want %s with prefix %s", ef[1], tc.dbCreds, DBCredsEnvPrefix)
			}
		})
	}
}
