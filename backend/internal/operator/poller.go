package operator

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

// RunPoller (голова DR) — fallback-поллинг S3: каждые cfg.PollInterval обходит
// s3_prefix активных сопоставлений и запускает restore по новым дампам, для
// которых уже появился .sha256-файл. Идемпотентность — через event_key.
func (o *Operator) RunPoller(ctx context.Context) {
	if o.k8s == nil {
		log.Printf("operator: poller disabled (kubernetes client missing)")
		return
	}
	t := time.NewTicker(o.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			safely("pollOnce", func() { o.pollOnce(ctx) })
		}
	}
}

func (o *Operator) pollOnce(ctx context.Context) {
	mappings, err := o.store.EnabledMappings(ctx)
	if err != nil {
		log.Printf("operator: poller list mappings: %v", err)
		return
	}
	for i := range mappings {
		m := mappings[i]
		prefix := strings.TrimSuffix(m.S3Prefix, "/")
		if prefix == "" {
			continue
		}
		objects, err := o.buckets.ListDumps(ctx, prefix+"/")
		if errors.Is(err, storage.ErrNoBuckets) {
			return
		}
		if err != nil {
			log.Printf("operator: poller list %s: %v", prefix, err)
			continue
		}
		newest := latest(objects)
		if newest == "" {
			continue
		}
		eventKey := EventKeyFor("restore", newest)
		if _, err := o.store.RunByEventKey(ctx, eventKey); err == nil {
			continue // уже обработано (webhook'ом или прошлым поллингом)
		} else if !errors.Is(err, store.ErrNotFound) {
			log.Printf("operator: poller event-key check: %v", err)
			continue
		}

		if len(o.buckets.ReadyClients(ctx, newest)) == 0 {
			continue // дамп ещё дозаписывается — ждём .sha256
		}
		// Поллинг — единственный путь, где метаданные дампа приходят не по
		// аутентифицированному каналу, а из бакета. Поэтому применяем только то,
		// что подписано секретом пары: иначе запись в бакет = выполнение любого
		// SQL в Target-контуре.
		checksum, bucketID, err := o.VerifiedChecksum(ctx, newest)
		if err != nil {
			log.Printf("operator: poller refused %s: %v", newest, err)
			continue
		}

		_, err = o.StartRestore(ctx, &m, newest, checksum, bucketID, model.TriggerPollFallback, eventKey)
		switch {
		case err == nil:
			log.Printf("operator: poller started restore for %s (%s)", m.SourceDBName, newest)
		case errors.Is(err, store.ErrConflict):
			// уже есть активный restore для этой базы — норм
		default:
			log.Printf("operator: poller start restore for %s: %v", newest, err)
		}
	}
}

// latest возвращает ключ самого свежего дампа по времени модификации.
func latest(objs []storage.Object) string {
	var key string
	var ts time.Time
	for _, o := range objs {
		if key == "" || o.LastModified.After(ts) {
			key, ts = o.Key, o.LastModified
		}
	}
	return key
}

func firstField(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}
