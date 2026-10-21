package k8s

import (
	"context"
	"log"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Тайминги аренды: лидер продлевает её каждые leaseRetry, а реплика перехватывает
// не раньше чем через leaseDuration после последнего продления.
const (
	leaseDuration = 15 * time.Second
	leaseRenew    = 10 * time.Second
	leaseRetry    = 2 * time.Second
)

// OwnerUID — UID Deployment'а backend'а ("" до успешного ResolveOwner).
// Идентичность головы: общая у всех её реплик, своя у каждого релиза, и не
// меняется при helm upgrade / sync Argo CD (Deployment патчится на месте).
func (c *Client) OwnerUID() string {
	if c.ownerRef == nil {
		return ""
	}
	return string(c.ownerRef.UID)
}

// RunAsLeader держит coordination.k8s.io Lease name и, пока эта реплика —
// лидер, крутит run(leaderCtx). Потеряв аренду, отменяет leaderCtx и снова
// встаёт в очередь. Возвращается по отмене ctx (аренда при этом отпускается,
// чтобы соседняя реплика подхватила её без ожидания leaseDuration).
func (c *Client) RunAsLeader(ctx context.Context, name, identity string, run func(ctx context.Context)) {
	lock := &resourcelock.LeaseLock{
		LeaseMeta:  metav1.ObjectMeta{Name: name, Namespace: c.namespace},
		Client:     c.cs.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: identity},
	}
	for ctx.Err() == nil {
		// OnStartedLeading библиотека запускает отдельной горутиной — ждём её
		// выхода, прежде чем снова бороться за аренду, иначе после быстрого
		// перезахвата старые и новые циклы на миг работали бы параллельно.
		var wg sync.WaitGroup
		leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
			Lock:            lock,
			LeaseDuration:   leaseDuration,
			RenewDeadline:   leaseRenew,
			RetryPeriod:     leaseRetry,
			ReleaseOnCancel: true,
			Name:            name,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(leaderCtx context.Context) {
					wg.Add(1)
					defer wg.Done()
					log.Printf("k8s: %s acquired lease %s — background loops started", identity, name)
					run(leaderCtx)
				},
				OnStoppedLeading: func() {
					log.Printf("k8s: %s lost lease %s — background loops stopped", identity, name)
				},
				OnNewLeader: func(id string) {
					if id != identity {
						log.Printf("k8s: lease %s is held by %s", name, id)
					}
				},
			},
		})
		wg.Wait()
	}
}
