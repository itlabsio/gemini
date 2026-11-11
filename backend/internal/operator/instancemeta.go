package operator

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/itlabsio/gemini/backend/internal/model"
	"github.com/itlabsio/gemini/backend/internal/peer"
	"github.com/itlabsio/gemini/backend/internal/storage"
)

// InstanceMeta — содержимое <инстанс>/meta.json. Source кладёт его в каждый
// бакет и подписывает своим ключом, чтобы Target без связи с Source (оффлайн-пара:
// известен только публичный ключ) узнал проект инстанса и пустил к
// сопоставлению его баз админов этого проекта, а не только superuser.
type InstanceMeta struct {
	Instance string `json:"instance"`
	Project  string `json:"project"`
	// GeneratedAt — когда файл переписан (только при изменении). Из нескольких
	// бакетов Target берёт самый свежий валидный файл.
	GeneratedAt time.Time `json:"generated_at"`
}

// instanceMetaInterval — период сверки meta.json на Source. Проект инстанса
// меняется редко, а после создания/правки инстанса сверка идёт сразу.
const instanceMetaInterval = time.Hour

// RunInstanceMetaSync (Source) периодически сверяет meta.json инстансов во всех
// бакетах: дописывает пропавшие, в новые бакеты и для инстансов, созданных до
// появления meta.json; переписывает при смене проекта.
func (o *Operator) RunInstanceMetaSync(ctx context.Context) {
	if !o.cfg.IsSource() {
		return
	}
	for {
		safely("syncInstanceMeta", func() { o.SyncInstanceMeta(ctx) })
		select {
		case <-ctx.Done():
			return
		case <-time.After(instanceMetaInterval + rand.N(templateReconcileJitter)):
		}
	}
}

// SyncInstanceMeta (Source) сверяет meta.json перечисленных инстансов, без
// аргументов — всех source-инстансов. Пишет только то, что отличается.
func (o *Operator) SyncInstanceMeta(ctx context.Context, insts ...model.Instance) {
	if !o.cfg.IsSource() {
		return
	}
	if len(insts) == 0 {
		var err error
		if insts, err = o.store.ListInstances(ctx, model.InstanceSource); err != nil {
			log.Printf("operator: instance meta: list instances: %v", err)
			return
		}
	}
	priv, err := o.store.HeadPrivateKey(ctx)
	if err != nil {
		log.Printf("operator: instance meta: no head key: %v", err)
		return
	}
	clients, _ := o.buckets.Clients(ctx)
	for _, inst := range insts {
		for _, c := range clients {
			written, err := syncInstanceMeta(ctx, c, priv, inst.Name, inst.Project)
			if err != nil {
				log.Printf("operator: instance meta %s: %v", c.Object(storage.InstanceMetaKey(inst.Name)), err)
			} else if written {
				log.Printf("operator: wrote %s", c.Object(storage.InstanceMetaKey(inst.Name)))
			}
		}
	}
}

func syncInstanceMeta(ctx context.Context, c *storage.Client, priv ed25519.PrivateKey, name, project string) (bool, error) {
	key := storage.InstanceMetaKey(name)
	if m := readInstanceMeta(ctx, c, priv.Public().(ed25519.PublicKey), name); m != nil && m.Project == project {
		return false, nil
	}
	body, err := json.Marshal(InstanceMeta{Instance: name, Project: project, GeneratedAt: time.Now().UTC()})
	if err != nil {
		return false, err
	}
	// Файл раньше подписи: читатель, попавший между ними, увидит несошедшуюся
	// подпись и просто не возьмёт файл до следующего чтения.
	if err := c.PutObject(ctx, key, "application/json", body); err != nil {
		return false, err
	}
	if err := c.PutObject(ctx, key+storage.SignatureSuffix, "text/plain",
		[]byte(peer.SignInstanceMeta(priv, key, body)+"\n")); err != nil {
		return false, fmt.Errorf("upload signature: %w", err)
	}
	return true, nil
}

// readInstanceMeta — meta.json инстанса из бакета, если подпись сходится с pub
// и файл про этот инстанс. Иначе nil (нет файла, подделка, не читается).
func readInstanceMeta(ctx context.Context, c *storage.Client, pub ed25519.PublicKey, name string) *InstanceMeta {
	key := storage.InstanceMetaKey(name)
	body, err := c.ReadObject(ctx, key)
	if err != nil {
		return nil
	}
	sig, err := c.ReadObject(ctx, key+storage.SignatureSuffix)
	if err != nil || !peer.VerifyInstanceMeta(pub, key, body, strings.TrimSpace(string(sig))) {
		return nil
	}
	var m InstanceMeta
	if json.Unmarshal(body, &m) != nil || m.Instance != name {
		return nil
	}
	return &m
}

// OfflineCandidate — база, найденная в бакетах без связи с Source.
type OfflineCandidate struct {
	Prefix   string // <инстанс>/<база>
	Instance string
	DBName   string
	// Project — из подписанного meta.json инстанса; ProjectKnown=false — файла
	// нет или он не прошёл проверку, проект неизвестен.
	Project      string
	ProjectKnown bool
}

// OfflineCandidates (Target, оффлайн-пара) собирает базы Source по содержимому
// бакетов: префиксы с готовыми подписанными дампами и проект из meta.json,
// проверенного запиненным ключом Source. Сами дампы здесь не проверяются — это
// делает VerifiedDump при restore, а подпись покрывает и ключ дампа, так что
// чужой дамп под настоящим префиксом не пройдёт.
func (o *Operator) OfflineCandidates(ctx context.Context) ([]OfflineCandidate, error) {
	raw, err := o.store.PeerPublicKey(ctx, "source")
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("no pinned Source public key")
	}
	pub := ed25519.PublicKey(raw)
	clients, _ := o.buckets.Clients(ctx)
	if len(clients) == 0 {
		return nil, storage.ErrNoBuckets
	}
	prefixes := map[string]bool{}
	var errs []error
	for _, c := range clients {
		ps, err := c.SignedDumpPrefixes(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
			continue
		}
		for p := range ps {
			prefixes[p] = true
		}
	}
	if len(errs) == len(clients) {
		return nil, errors.Join(errs...)
	}

	metas := map[string]*InstanceMeta{}
	out := make([]OfflineCandidate, 0, len(prefixes))
	for p := range prefixes {
		inst, db, _ := strings.Cut(p, "/")
		m, seen := metas[inst]
		if !seen {
			for _, c := range clients {
				if cur := readInstanceMeta(ctx, c, pub, inst); cur != nil && (m == nil || cur.GeneratedAt.After(m.GeneratedAt)) {
					m = cur
				}
			}
			metas[inst] = m
		}
		cand := OfflineCandidate{Prefix: p, Instance: inst, DBName: db}
		if m != nil {
			cand.Project, cand.ProjectKnown = m.Project, true
		}
		out = append(out, cand)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out, nil
}
