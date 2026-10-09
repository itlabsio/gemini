// Package jobrunner — код, исполняемый ВНУТРИ dump/restore-Job'а (подкоманды
// `gemini dump` / `gemini restore`). Работает по переменным окружения, которые
// backend кладёт в ephemeral k8s Secret и в env Job'а; наружу ничего не логирует
// из секретов.
package jobrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
)

// Env — распарсенное окружение Job'а.
type Env struct {
	PGHost, PGPort, PGUser, PGPassword, PGSSLMode, PGDatabase string

	S3ObjectKey, S3Prefix string

	// S3Buckets — все включённые бакеты (S3_BUCKETS из секрета кред): dump льёт
	// во все, restore качает из первого, где получилось. S3BucketID — бакет,
	// с которого restore начинает (там голова дамп уже видела).
	S3Buckets  []storage.JobBucket
	S3BucketID string

	TargetDBName   string
	TargetDBOwner  string // роль-владелец восстановленной базы (пусто → владелец = restorer)
	RestoreMode    string // "recreate" (RENAME→CREATE→DROP) | "in_place" (--clean поверх)
	ExpectedSHA256 string

	WorkDir string

	// pgMajor — мажор клиентских утилит для этого прогона, проставляется в
	// runDump/runRestore после определения версии сервера/архива (см. pgclient.go).
	// 0 → дефолтные бинари из PATH. Env копируется по значению, поэтому достаточно
	// выставить перед вызовом под-функций.
	pgMajor int
	// src — бакет, из которого restore скачал дамп (оттуда же .extensions).
	src storage.JobBucket
}

func readEnv() (Env, error) {
	e := Env{
		PGHost:         os.Getenv("PGHOST"),
		PGPort:         orDefault(os.Getenv("PGPORT"), "5432"),
		PGUser:         os.Getenv("PGUSER"),
		PGPassword:     os.Getenv("PGPASSWORD"),
		PGSSLMode:      orDefault(os.Getenv("PGSSLMODE"), "require"),
		PGDatabase:     os.Getenv("PGDATABASE"),
		S3ObjectKey:    os.Getenv("S3_OBJECT_KEY"),
		S3Prefix:       strings.Trim(os.Getenv("S3_PREFIX"), "/"),
		S3BucketID:     os.Getenv("S3_BUCKET_ID"),
		TargetDBName:   os.Getenv("TARGET_DB_NAME"),
		TargetDBOwner:  os.Getenv("TARGET_DB_OWNER"),
		RestoreMode:    orDefault(os.Getenv("RESTORE_MODE"), "recreate"),
		ExpectedSHA256: os.Getenv("EXPECTED_SHA256"),
		WorkDir:        orDefault(os.Getenv("WORKDIR"), "/work"),
	}
	if err := json.Unmarshal([]byte(os.Getenv("S3_BUCKETS")), &e.S3Buckets); err != nil {
		return e, fmt.Errorf("jobrunner: bad S3_BUCKETS: %w", err)
	}
	if len(e.S3Buckets) == 0 {
		return e, fmt.Errorf("jobrunner: S3_BUCKETS is empty — no S3 buckets configured")
	}
	return e, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// restoreOrder — бакеты для restore: сначала S3BucketID, затем остальные.
func (e Env) restoreOrder() []storage.JobBucket {
	out := make([]storage.JobBucket, 0, len(e.S3Buckets))
	for _, b := range e.S3Buckets {
		if b.ID == e.S3BucketID {
			out = append(out, b)
		}
	}
	for _, b := range e.S3Buckets {
		if b.ID != e.S3BucketID {
			out = append(out, b)
		}
	}
	return out
}

// pgEnv возвращает окружение для дочерних psql/pg_dump без утечки в родительский лог.
func (e Env) pgEnv(database string) []string {
	env := append(os.Environ(),
		"PGHOST="+e.PGHost,
		"PGPORT="+e.PGPort,
		"PGUSER="+e.PGUser,
		"PGPASSWORD="+e.PGPassword,
		"PGSSLMODE="+e.PGSSLMode,
		"PGCONNECT_TIMEOUT=15",
	)
	// verify-ca/verify-full требуют корневой CA. libpq по умолчанию ищет
	// ~/.postgresql/root.crt (нет при readOnlyRootFilesystem). PGSSLROOTCERT=system
	// (PG16+) переводит на системный trust store — в образе там уже есть Yandex CA
	// и публичные CA. Явно заданный PGSSLROOTCERT (напр. через ephemeral Secret)
	// не трогаем.
	if os.Getenv("PGSSLROOTCERT") == "" &&
		(e.PGSSLMode == "verify-ca" || e.PGSSLMode == "verify-full") {
		env = append(env, "PGSSLROOTCERT=system")
	}
	if database != "" {
		env = append(env, "PGDATABASE="+database)
	}
	return env
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// materializeRootCert записывает кастомный корневой CA (PGSSLROOTCERT_PEM, который
// голова кладёт в секрет кред инстанса) в файл под /tmp и выставляет PGSSLROOTCERT
// на него: у libpq это путь, а не содержимое, и корень контейнера смонтирован
// read-only (writable только /tmp и /work). Явно заданный PGSSLROOTCERT (в т.ч.
// "system") приоритетнее — PEM тогда игнорируется.
func materializeRootCert() error {
	pemText := os.Getenv("PGSSLROOTCERT_PEM")
	if pemText == "" || os.Getenv("PGSSLROOTCERT") != "" {
		return nil
	}
	path := filepath.Join(orDefault(os.Getenv("TMPDIR"), "/tmp"), "pg-root-ca.crt")
	if err := os.WriteFile(path, []byte(pemText), 0o600); err != nil {
		return fmt.Errorf("write custom CA to %s: %w", path, err)
	}
	return os.Setenv("PGSSLROOTCERT", path)
}

// Run диспетчеризует подкоманду ("dump" | "restore").
func Run(ctx context.Context, kind string) error {
	if err := materializeRootCert(); err != nil {
		return err
	}
	e, err := readEnv()
	if err != nil {
		return err
	}
	switch kind {
	case "dump":
		return runDump(ctx, e)
	case "restore":
		return runRestore(ctx, e)
	default:
		return fmt.Errorf("jobrunner: unknown kind %q", kind)
	}
}
