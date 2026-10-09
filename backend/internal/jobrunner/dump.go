package jobrunner

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
)

// listExtensions читает расширения исходной базы (кроме plpgsql — он есть всегда).
func listExtensions(ctx context.Context, e Env) ([]string, error) {
	cmd := exec.CommandContext(ctx, pgTool("psql", e.pgMajor), "--no-psqlrc", "--tuples-only",
		"--no-align", "--quiet", "--command",
		"SELECT extname FROM pg_extension WHERE extname <> 'plpgsql' ORDER BY extname")
	cmd.Env = e.pgEnv(e.PGDatabase)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var exts []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" && validExtName(line) {
			exts = append(exts, line)
		}
	}
	return exts, nil
}

// validExtName — консервативная проверка имени расширения (уходит в SQL restore'а).
func validExtName(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for _, r := range s {
		ok := r == '_' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// runDump: pg_dump -Fc → /work/db.dump → sha256 → в каждый бакет: upload дампа →
// <key>.extensions → <key>.sha256. Файл контрольной суммы заливается ПОСЛЕДНИМ и
// служит признаком готовности: DR ориентируется именно на его появление.
//
// Формат custom (-Fc): один сжатый файл, restore через pg_restore
// --single-transaction --role=<owner> (см. restore.go). --clean / --if-exists —
// опции restore, не dump, поэтому здесь их нет.
func runDump(ctx context.Context, e Env) error {
	if e.PGDatabase == "" {
		return fmt.Errorf("dump: PGDATABASE is required")
	}
	// Ручной запуск передаёт точный S3_OBJECT_KEY; scheduled — только S3_PREFIX,
	// ключ генерируем здесь (backend узнаёт его по факту, листингом префикса).
	if e.S3ObjectKey == "" {
		if e.S3Prefix == "" {
			return fmt.Errorf("dump: either S3_OBJECT_KEY or S3_PREFIX is required")
		}
		e.S3ObjectKey = fmt.Sprintf("%s/%s.dump", e.S3Prefix, time.Now().UTC().Format("20060102T150405Z"))
		log.Printf("dump: generated object key %s", e.S3ObjectKey)
	}
	dumpPath := filepath.Join(e.WorkDir, "db.dump")

	// Мажор pg_dump = мажор сервера-источника: архив ляжет в формате этой версии
	// и не потащит GUC, которых на нём нет. Не смогли прочитать версию — идём на
	// дефолтном бинаре.
	avail := availablePGMajors()
	var srcMajor int
	if n, err := serverVersionNum(ctx, e, e.PGDatabase); err != nil {
		log.Printf("dump: cannot read source server version (%v); using default pg_dump", err)
	} else {
		srcMajor = majorFromVersionNum(n)
	}
	if srcMajor > 0 && len(avail) > 0 && srcMajor > avail[len(avail)-1] {
		return fmt.Errorf("dump: source is PostgreSQL %d, image carries pg_dump up to %d — "+
			"add postgresql%d-client to backend/Dockerfile", srcMajor, avail[len(avail)-1], srcMajor)
	}
	e.pgMajor = pickPGMajor(srcMajor, avail)
	if e.pgMajor > 0 {
		log.Printf("dump: source PostgreSQL %d -> pg_dump %d", srcMajor, e.pgMajor)
	}

	// Имя базы НЕ передаём аргументом: pg_dump трактует позиционный параметр как
	// имя ИЛИ как conninfo-строку. pgEnv кладёт его в PGDATABASE, где libpq читает
	// значение только как имя базы.
	//   --no-owner/--no-privileges: владельца ставит restore (--role), гранты не
	//     нужны — роли source-контура на target не существуют.
	//   --exclude-extension='*': расширения на MDB ставятся только через консоль и
	//     принадлежат системной роли; их список едет отдельно в <key>.extensions,
	//     restore создаёт нужные до наката (ensureExtensions). Флаг появился в
	//     pg_dump 17 — для источника <17 EXTENSION-записи режутся на накате
	//     (pgRestore, --use-list).
	dumpArgs := []string{"--format=custom", "--no-owner", "--no-privileges", "--file", dumpPath}
	if e.pgMajor == 0 || e.pgMajor >= 17 {
		dumpArgs = append(dumpArgs, "--exclude-extension=*")
	} else {
		log.Printf("dump: pg_dump %d без --exclude-extension — EXTENSION вырежет restore", e.pgMajor)
	}
	pgDump := exec.CommandContext(ctx, pgTool("pg_dump", e.pgMajor), dumpArgs...)
	pgDump.Env = e.pgEnv(e.PGDatabase)
	pgDump.Stderr = os.Stderr

	log.Printf("dump: pg_dump -Fc %s -> %s", e.PGDatabase, dumpPath)
	if err := pgDump.Run(); err != nil {
		return fmt.Errorf("pg_dump: %w", err)
	}

	sum, err := sha256File(dumpPath)
	if err != nil {
		return err
	}
	log.Printf("dump: sha256=%s", sum)

	// Список расширений — один на все бакеты.
	var exts []string
	if exts, err = listExtensions(ctx, e); err != nil {
		log.Printf("dump: list extensions: %v (skip .extensions sidecar)", err)
	} else if len(exts) > 0 {
		log.Printf("dump: extensions: %s", strings.Join(exts, ", "))
	}

	// Fan-out: копия во все бакеты. Отказ части бакетов не роняет Job — дамп
	// всё равно доступен Target'ам из остальных; бакеты без копии голова Source
	// обнаружит при подписи и пришлёт нотификацию. Падаем, только если не удалось
	// никуда.
	var failed []string
	for _, b := range e.S3Buckets {
		if err := uploadDump(ctx, b, e.S3ObjectKey, dumpPath, sum, exts); err != nil {
			log.Printf("dump: bucket %s: %v", b.Name, err)
			failed = append(failed, b.Name)
		}
	}
	if len(failed) == len(e.S3Buckets) {
		return fmt.Errorf("dump: upload failed to every bucket (%s)", strings.Join(failed, ", "))
	}
	if len(failed) > 0 {
		log.Printf("dump: WARNING upload failed to %d of %d bucket(s): %s",
			len(failed), len(e.S3Buckets), strings.Join(failed, ", "))
	}
	log.Printf("dump: done")
	return nil
}

// uploadDump заливает дамп в один бакет: дамп → <key>.extensions → <key>.sha256.
// Файл контрольной суммы — последним: это признак готовности для Target.
func uploadDump(ctx context.Context, b storage.JobBucket, key, dumpPath, sum string, exts []string) error {
	mc, err := b.Minio()
	if err != nil {
		return err
	}
	f, err := os.Open(dumpPath)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}

	log.Printf("dump: uploading s3://%s/%s to %s (%d bytes)", b.Bucket, key, b.Name, fi.Size())
	if _, err := mc.PutObject(ctx, b.Bucket, key, f, fi.Size(), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	}); err != nil {
		return fmt.Errorf("upload dump: %w", err)
	}

	if len(exts) > 0 {
		body := []byte(strings.Join(exts, "\n") + "\n")
		if _, err := mc.PutObject(ctx, b.Bucket, key+".extensions",
			bytes.NewReader(body), int64(len(body)),
			minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
			return fmt.Errorf("upload extension list: %w", err)
		}
	}

	checksumBody := fmt.Appendf(nil, "%s  %s\n", sum, filepath.Base(dumpPath))
	if _, err := mc.PutObject(ctx, b.Bucket, key+".sha256",
		bytes.NewReader(checksumBody), int64(len(checksumBody)), minio.PutObjectOptions{
			ContentType: "text/plain",
		}); err != nil {
		return fmt.Errorf("upload checksum: %w", err)
	}
	return nil
}
