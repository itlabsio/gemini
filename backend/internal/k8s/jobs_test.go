package k8s

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPickErrorLines(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "pg_dump connection error",
			raw: "starting dump db=postgres\n" +
				"pg_dump: error: connection to server at \"c-xxx.ro.mdb.yandexcloud.net\" (10.247.27.69), port 6432 failed: " +
				"ERROR:  odyssey: c179d4d3a45c9: route for 'postgres.dr-dumper' is not found\n",
			want: "pg_dump: error: connection to server at \"c-xxx.ro.mdb.yandexcloud.net\" (10.247.27.69), port 6432 failed: " +
				"ERROR:  odyssey: c179d4d3a45c9: route for 'postgres.dr-dumper' is not found",
		},
		{
			name: "real dump failure — picks the pg_dump line, drops log noise",
			raw: "2026/09/06 16:46:53 dump: pg_dump -Fc postgres -> /work/db.dump\n" +
				"pg_dump: error: connection to server at \"c-xxx.ro.mdb.yandexcloud.net\" (10.247.27.69), port 6432 failed: " +
				"ERROR:  odyssey: c179d4d3a45c9: route for 'postgres.dr-dumper' is not found\n" +
				"2026/09/06 16:46:53 dump: pg_dump: exit status 1\n",
			want: "pg_dump: error: connection to server at \"c-xxx.ro.mdb.yandexcloud.net\" (10.247.27.69), port 6432 failed: " +
				"ERROR:  odyssey: c179d4d3a45c9: route for 'postgres.dr-dumper' is not found",
		},
		{
			name: "no markers falls back to last line",
			raw:  "line one\nline two\nline three\n",
			want: "line three",
		},
		{
			name: "keeps only last five hits",
			raw: "pg_dump: error: a\npg_dump: error: b\npg_dump: error: c\n" +
				"pg_dump: error: d\npg_dump: error: e\npg_dump: error: f\n",
			want: "pg_dump: error: b\npg_dump: error: c\npg_dump: error: d\npg_dump: error: e\npg_dump: error: f",
		},
		{
			name: "verbose progress lines are not errors",
			raw: "pg_dump: dumping contents of table \"public.users\"\n" +
				"pg_dump: last built-in OID is 16383\n" +
				"done\n",
			want: "done",
		},
		{
			name: "empty input",
			raw:  "",
			want: "",
		},
		{
			name: "trailing CR stripped",
			raw:  "ok\r\npg_restore: error: could not execute query\r\n",
			want: "pg_restore: error: could not execute query",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickErrorLines(tc.raw); got != tc.want {
				t.Errorf("pickErrorLines()\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestLooksLikeError(t *testing.T) {
	yes := []string{
		"pg_dump: error: connection failed",
		"FATAL: password authentication failed",
		"panic: runtime error",
		"could not connect to server",
		`time=... level=error msg="upload failed"`,
	}
	no := []string{
		"starting dump",
		"pg_dump: dumping contents of table public.users",
		"uploaded 42 MB to s3",
	}
	for _, s := range yes {
		if !looksLikeError(s) {
			t.Errorf("looksLikeError(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeError(s) {
			t.Errorf("looksLikeError(%q) = true, want false", s)
		}
	}
}

func TestPodNotStarted(t *testing.T) {
	const ns, job = "gemini", "gemini-dump-r1"
	pod := func(phase corev1.PodPhase, waiting string) *corev1.Pod {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: job + "-x", Namespace: ns, Labels: map[string]string{"job-name": job}},
			Status:     corev1.PodStatus{Phase: phase},
		}
		if waiting != "" {
			p.Status.ContainerStatuses = []corev1.ContainerStatus{{
				Name: "runner", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: waiting}},
			}}
		}
		return p
	}
	failedCreate := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "ev1", Namespace: ns},
		InvolvedObject: corev1.ObjectReference{Kind: "Job", Name: job},
		Type:           corev1.EventTypeWarning,
		Reason:         "FailedCreate",
		Message:        "spec.volumes[0].ephemeral: storage must be at least 1Gi",
	}
	for _, tc := range []struct {
		name       string
		objs       []runtime.Object
		wantStuck  bool
		wantReason string
	}{
		{"no pods, FailedCreate event", []runtime.Object{failedCreate}, true, "FailedCreate"},
		{"pending pod, image pull", []runtime.Object{pod(corev1.PodPending, "ImagePullBackOff")}, true, "ImagePullBackOff"},
		{"running pod", []runtime.Object{pod(corev1.PodRunning, "")}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{cs: fake.NewSimpleClientset(tc.objs...), namespace: ns}
			stuck, reason := c.PodNotStarted(context.Background(), job)
			if stuck != tc.wantStuck || !strings.Contains(reason, tc.wantReason) {
				t.Errorf("PodNotStarted = (%v, %q), want (%v, ~%q)", stuck, reason, tc.wantStuck, tc.wantReason)
			}
		})
	}
}
