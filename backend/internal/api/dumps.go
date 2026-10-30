package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
)

// dumpSuffix — расширение объекта дампа (см. jobrunner.runDump, operator.StartDump).
const dumpSuffix = ".dump"

// s3Timeout — потолок на обращения к S3 из обработчиков дампов.
const s3Timeout = 15 * time.Second

// dumpPrefix — префикс дампов базы в S3: <имя инстанса>/<имя базы>/.
func dumpPrefix(inst *model.Instance, db *model.Database) string {
	return inst.Name + "/" + db.DBName + "/"
}

// validDumpKey — ключ указывает на сам дамп базы: <prefix><имя>.dump без
// вложенных путей. Sidecar'ы (.sha256/.sig/.extensions) и чужие префиксы — нет.
func validDumpKey(prefix, key string) bool {
	name, ok := strings.CutPrefix(key, prefix)
	return ok && len(name) > len(dumpSuffix) && strings.HasSuffix(name, dumpSuffix) &&
		!strings.Contains(name, "/")
}

// dumpFilename — имя файла для браузера: ключ без "/" и с безопасными символами
// (уходит в кавычки Content-Disposition).
func dumpFilename(key string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '_'
	}, key)
}

type dumpBucket struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type dumpItem struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	// Buckets — downloadable-бакеты с готовой копией дампа. Пусто — дамп лежит
	// только в бакетах, из которых скачивание запрещено.
	Buckets []dumpBucket `json:"buckets"`
}

// authorizeDumps — admin проекта базы и скачивание включено на её инстансе.
func (s *Server) authorizeDumps(w http.ResponseWriter, r *http.Request, databaseID string) (*model.Database, *model.Instance, bool) {
	db, inst, ok := s.authorizeDatabase(w, r, databaseID, auth.RoleAdmin)
	if !ok {
		return nil, nil, false
	}
	if !inst.Downloadable {
		writeError(w, http.StatusForbidden, "dump downloads are disabled for instance "+inst.Name)
		return nil, nil, false
	}
	return db, inst, true
}

// listDumps — готовые дампы базы в S3 (свежие первыми) с бакетами для скачивания.
func (s *Server) listDumps(w http.ResponseWriter, r *http.Request) {
	db, inst, ok := s.authorizeDumps(w, r, mux.Vars(r)["id"])
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s3Timeout)
	defer cancel()
	prefix := dumpPrefix(inst, db)
	located, err := s.d.Storage.LocateDumps(ctx, prefix)
	if errors.Is(err, storage.ErrNoBuckets) {
		writeError(w, http.StatusServiceUnavailable, "S3 storage is not configured")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	items := []dumpItem{}
	for _, l := range located {
		if !l.Ready || !validDumpKey(prefix, l.Key) {
			continue
		}
		items = append(items, dumpItem{
			Key: l.Key, Size: l.Size, LastModified: l.LastModified, Buckets: downloadBuckets(l.ReadyIn),
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].LastModified.After(items[j].LastModified) })
	writeJSON(w, http.StatusOK, items)
}

// downloadBuckets — бакеты с готовой копией, из которых разрешено скачивание
// (порядок пула: встроенный первым). Выбирает из них пользователь.
func downloadBuckets(ready []*storage.Client) []dumpBucket {
	out := []dumpBucket{}
	for _, c := range ready {
		if c.Downloadable() {
			out = append(out, dumpBucket{ID: c.ID(), Name: c.Title()})
		}
	}
	return out
}

type dumpDownloadRequest struct {
	Key      string `json:"key"`
	BucketID string `json:"bucket_id"`
}

type dumpDownloadResponse struct {
	URL        string    `json:"url"`
	ExpiresAt  time.Time `json:"expires_at"`
	Filename   string    `json:"filename"`
	SHA256     string    `json:"sha256"`
	Extensions []string  `json:"extensions"`
}

// downloadDump выдаёт presigned-ссылку на дамп базы в выбранном бакете вместе с
// контрольной суммой и списком расширений (нужны для ручного pg_restore).
func (s *Server) downloadDump(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	db, inst, ok := s.authorizeDumps(w, r, id)
	if !ok {
		return
	}
	var req dumpDownloadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validDumpKey(dumpPrefix(inst, db), req.Key) {
		writeError(w, http.StatusBadRequest, "key is not a dump of this database")
		return
	}
	st, err := s.d.Store.GetSettings(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s3Timeout)
	defer cancel()
	c, err := s.d.Storage.Client(ctx, req.BucketID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !c.Downloadable() {
		writeError(w, http.StatusBadRequest, "downloads are disabled for bucket "+c.Title())
		return
	}
	// .sha256 — признак полной заливки и сумма для проверки скачанного файла.
	ready, err := c.ChecksumReady(ctx, req.Key)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if !ready {
		writeError(w, http.StatusNotFound, "dump is not ready in bucket "+c.Title())
		return
	}
	sum, err := c.ReadChecksum(ctx, req.Key)
	if err != nil {
		writeError(w, http.StatusBadGateway, "read checksum: "+err.Error())
		return
	}
	raw, _, err := c.ReadExtensions(ctx, req.Key)
	if err != nil {
		writeError(w, http.StatusBadGateway, "read extensions: "+err.Error())
		return
	}
	ttl := time.Duration(st.DownloadURLTTLMinutes) * time.Minute
	filename := dumpFilename(req.Key)
	u, err := c.PresignGet(ctx, req.Key, filename, ttl)
	if err != nil {
		writeError(w, http.StatusBadGateway, "presign: "+err.Error())
		return
	}
	s.audit(r, "database.dump.download", id, map[string]any{
		"key": req.Key, "bucket": c.Title(), "ttl_minutes": st.DownloadURLTTLMinutes,
	})
	exts := storage.ParseExtensions(raw)
	if exts == nil {
		exts = []string{}
	}
	sha, _, _ := strings.Cut(sum, " ")
	writeJSON(w, http.StatusOK, dumpDownloadResponse{
		URL: u, ExpiresAt: time.Now().Add(ttl).UTC(), Filename: filename, SHA256: sha, Extensions: exts,
	})
}
