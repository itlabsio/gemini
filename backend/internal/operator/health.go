package operator

import (
	"context"
	"log"
	"math/rand/v2"
	"time"
)

const (
	// dependencyCheckInterval — период проверки доступности S3 и Vault.
	dependencyCheckInterval = 5 * time.Minute
	// dependencyCheckTimeout — потолок одной проверки.
	dependencyCheckTimeout = 30 * time.Second
)

// dependencyCheck — внешняя зависимость и её последнее известное состояние.
type dependencyCheck struct {
	name    string
	ping    func(context.Context) error
	failing bool
}

// run выполняет проверку и пишет в лог сбой (каждый раз) и восстановление (один раз).
func (d *dependencyCheck) run(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, dependencyCheckTimeout)
	defer cancel()
	err := d.ping(ctx)
	switch {
	case err != nil:
		log.Printf("operator: WARNING dependency %s is unavailable: %v", d.name, err)
	case d.failing:
		log.Printf("operator: dependency %s recovered", d.name)
	}
	d.failing = err != nil
}

// RunDependencyChecks (лидер) следит за S3 и Vault. В readiness их нет: сбой общий
// для всех реплик, а UI и приём webhook'ов без них продолжают работать.
func (o *Operator) RunDependencyChecks(ctx context.Context) {
	checks := []*dependencyCheck{{name: "s3", ping: o.buckets.Ping}}
	if o.vault != nil {
		checks = append(checks, &dependencyCheck{name: "vault", ping: o.vault.Ping})
	}
	for {
		for _, d := range checks {
			safely("dependencyCheck "+d.name, func() { d.run(ctx) })
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(dependencyCheckInterval + rand.N(templateReconcileJitter)):
		}
	}
}
