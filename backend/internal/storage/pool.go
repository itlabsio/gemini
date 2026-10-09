package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

// BuiltInID — id бакета из env (Helm values.s3); в БД его нет.
const BuiltInID = "builtin"

// Loader отдаёт включённые бакеты из БД вместе с секретами.
type Loader func(ctx context.Context) ([]model.S3Bucket, error)

// Pool — набор бакетов головы: встроенный из env + включённые из БД. Состав
// читается из БД на каждый вызов (бакеты правятся из UI), minio-клиенты
// кэшируются по id+updated_at, чтобы не терять пул соединений.
type Pool struct {
	builtIn *model.S3Bucket
	load    Loader

	mu    sync.Mutex
	cache map[string]cachedClient
}

type cachedClient struct {
	updatedAt time.Time
	client    *Client
}

// NewPool собирает пул. builtIn может быть nil (S3_* в env не задан).
func NewPool(builtIn *model.S3Bucket, load Loader) *Pool {
	if builtIn != nil {
		builtIn.ID, builtIn.BuiltIn, builtIn.Enabled = BuiltInID, true, true
	}
	return &Pool{builtIn: builtIn, load: load, cache: map[string]cachedClient{}}
}

// BuiltIn отдаёт встроенный бакет (nil, если не задан).
func (p *Pool) BuiltIn() *model.S3Bucket { return p.builtIn }

// Buckets — включённые бакеты (встроенный первым) с секретами.
func (p *Pool) Buckets(ctx context.Context) ([]model.S3Bucket, error) {
	var out []model.S3Bucket
	if p.builtIn != nil {
		out = append(out, *p.builtIn)
	}
	dbBuckets, err := p.load(ctx)
	if err != nil {
		return out, fmt.Errorf("load s3 buckets: %w", err)
	}
	return append(out, dbBuckets...), nil
}

// Clients — клиенты включённых бакетов в порядке Buckets. Пустой список —
// S3 не настроен. Если БД недоступна, отдаёт хотя бы встроенный бакет.
func (p *Pool) Clients(ctx context.Context) ([]*Client, error) {
	buckets, loadErr := p.Buckets(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	live := map[string]bool{}
	out := make([]*Client, 0, len(buckets))
	for _, b := range buckets {
		live[b.ID] = true
		if c, ok := p.cache[b.ID]; ok && c.updatedAt.Equal(b.UpdatedAt) {
			out = append(out, c.client)
			continue
		}
		c, err := New(b)
		if err != nil {
			log.Printf("storage: %v", err)
			continue
		}
		p.cache[b.ID] = cachedClient{updatedAt: b.UpdatedAt, client: c}
		out = append(out, c)
	}
	if loadErr == nil {
		for id := range p.cache {
			if !live[id] {
				delete(p.cache, id)
			}
		}
	}
	return out, loadErr
}

// Client — клиент включённого бакета по id.
func (p *Pool) Client(ctx context.Context, id string) (*Client, error) {
	clients, err := p.Clients(ctx)
	for _, c := range clients {
		if c.ID() == id {
			return c, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("s3 bucket %s is not configured or disabled", id)
}

// ListDumps — объединение дампов под префиксом по всем бакетам (ключ один и тот
// же во всех бакетах; берём самый свежий LastModified). Недоступный бакет не
// мешает остальным — ошибка, только если не ответил ни один.
func (p *Pool) ListDumps(ctx context.Context, prefix string) ([]Object, error) {
	clients, _ := p.Clients(ctx)
	if len(clients) == 0 {
		return nil, ErrNoBuckets
	}
	byKey := map[string]Object{}
	var errs []error
	for _, c := range clients {
		objs, err := c.ListDumps(ctx, prefix)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
			continue
		}
		for _, o := range objs {
			if cur, ok := byKey[o.Key]; !ok || o.LastModified.After(cur.LastModified) {
				byKey[o.Key] = o
			}
		}
	}
	if len(errs) == len(clients) {
		return nil, errors.Join(errs...)
	}
	for _, err := range errs {
		log.Printf("storage: list %s: %v", prefix, err)
	}
	out := make([]Object, 0, len(byKey))
	for _, o := range byKey {
		out = append(out, o)
	}
	return out, nil
}

// ReadyClients — бакеты, где дамп уже полностью залит (есть <key>.sha256).
func (p *Pool) ReadyClients(ctx context.Context, dumpKey string) []*Client {
	clients, _ := p.Clients(ctx)
	var out []*Client
	for _, c := range clients {
		if ok, err := c.ChecksumReady(ctx, dumpKey); err != nil {
			log.Printf("storage: stat %s in %s: %v", dumpKey, c.Name(), err)
		} else if ok {
			out = append(out, c)
		}
	}
	return out
}

// Ping — readiness: ок, если бакетов нет или отвечает хотя бы один (fan-out
// переживает отказ части бакетов, под из-за этого выводить нельзя).
func (p *Pool) Ping(ctx context.Context) error {
	clients, _ := p.Clients(ctx)
	if len(clients) == 0 {
		return nil
	}
	var errs []error
	for _, c := range clients {
		if err := c.Ping(ctx); err != nil {
			errs = append(errs, err)
			continue
		}
		return nil
	}
	return errors.Join(errs...)
}

// ErrNoBuckets — ни одного включённого бакета.
var ErrNoBuckets = errors.New("no S3 buckets configured")
