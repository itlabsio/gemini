package k8s

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

// Две реплики на одном Lease: фоновые циклы крутит ровно одна, а после её
// остановки аренду подхватывает вторая.
func TestRunAsLeaderSingleActive(t *testing.T) {
	cs := fake.NewSimpleClientset()
	a := &Client{cs: cs, namespace: "gemini"}
	b := &Client{cs: cs, namespace: "gemini"}

	var active, maxActive atomic.Int32
	started := make(chan string, 4)
	run := func(id string) func(context.Context) {
		return func(ctx context.Context) {
			n := active.Add(1)
			for {
				m := maxActive.Load()
				if n <= m || maxActive.CompareAndSwap(m, n) {
					break
				}
			}
			started <- id
			<-ctx.Done()
			active.Add(-1)
		}
	}

	ctxA, cancelA := context.WithCancel(context.Background())
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	doneA := make(chan struct{})
	go func() { a.RunAsLeader(ctxA, "source-backend-leader", "a", run("a")); close(doneA) }()

	waitStarted := func(want string) {
		t.Helper()
		select {
		case id := <-started:
			if id != want {
				t.Fatalf("leader = %s, want %s", id, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s never became leader", want)
		}
	}
	waitStarted("a")
	go b.RunAsLeader(ctxB, "source-backend-leader", "b", run("b"))

	// b не должен перехватить аренду, пока a её продлевает.
	time.Sleep(3 * leaseRetry)
	if n := maxActive.Load(); n != 1 {
		t.Fatalf("%d replicas ran background loops at once", n)
	}

	cancelA()
	<-doneA
	waitStarted("b")
	if n := maxActive.Load(); n != 1 {
		t.Fatalf("%d replicas ran background loops at once", n)
	}
}
