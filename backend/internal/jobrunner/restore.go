package jobrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
)

// runRestore применяет проверенный дамп (pg_dump -Fc) к целевой базе через
// pg_restore.
//
// Общая часть (до любых деструктивных операций):
//  1. скачать дамп, сверить sha256 с EXPECTED_SHA256 (fail при несовпадении).
//
// Дальше по RESTORE_MODE:
//
//	recreate — оборвать сессии → ALTER DATABASE RENAME → CREATE DATABASE OWNER
//	           <owner> → CREATE EXTENSION из <key>.extensions → pg_restore →
//	           DROP old. При ошибке old остаётся, прогон failed. Требует прав на
//	           RENAME/CREATE DATABASE — self-hosted с суперюзером.
//	in_place — оборвать сессии → CREATE EXTENSION IF NOT EXISTS →
//	           pg_restore --clean --if-exists прямо в существующую базу. Прежней
//	           копии не остаётся. Для MDB: базу, роль-владельца и расширения
//	           заводят через консоль Yandex Cloud, restorer'у дают членство в
//	           роли-владельце, CREATE/ALTER DATABASE не нужны.
//
// Владелец (TARGET_DB_OWNER — заранее созданная роль) прокидывается в
// `pg_restore --role=<owner>` (SET ROLE после подключения). ALTER DATABASE OWNER
// отдельно не делаем: в recreate владелец задан при CREATE, в in_place базой
// владеет <owner> с момента её создания в консоли.
func runRestore(ctx context.Context, e Env) error {
	target := firstNonEmpty(e.TargetDBName, e.PGDatabase)
	if target == "" || e.S3ObjectKey == "" {
		return fmt.Errorf("restore: TARGET_DB_NAME and S3_OBJECT_KEY are required")
	}
	dumpPath := filepath.Join(e.WorkDir, "db.dump")
	src, err := downloadDump(ctx, e, dumpPath)
	if err != nil {
		return err
	}
	e.src = src

	// (1) сверка контрольной суммы — до любых деструктивных операций.
	//
	// Ожидаемую сумму берём ТОЛЬКО из EXPECTED_SHA256, который голова передала в
	// Job. Голова получила её либо по HMAC-подписанному webhook'у от Source, либо
	// из <key>.sha256 с проверкой подписи <key>.sig (operator.VerifiedChecksum).
	want := firstToken(e.ExpectedSHA256)
	if want == "" {
		return fmt.Errorf("restore: EXPECTED_SHA256 is empty, refusing to proceed")
	}
	got, err := sha256File(dumpPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(want, got) {
		return fmt.Errorf("restore: checksum mismatch: want %s got %s", want, got)
	}
	log.Printf("restore: checksum ok (%s)", got)

	// Префлайт мажоров: pg_restore обязан быть ≥ мажора архива (иначе не прочитает
	// формат) и ≤ мажора целевого сервера (иначе выставит GUC, которых на сервере
	// нет — напр. transaction_timeout из PG17). Значит накатываем клиентом мажора
	// архива, а сервер обязан быть не старше. «Вниз по мажорам» (архив новее
	// сервера) невозможен в принципе — падаем здесь с понятной ошибкой.
	avail := availablePGMajors()
	archiveMajor, err := archivePgDumpMajor(ctx, dumpPath, avail)
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	if len(avail) > 0 && archiveMajor > avail[len(avail)-1] {
		return fmt.Errorf("restore: archive is from pg_dump %d, image carries pg_restore up to %d — "+
			"add postgresql%d-client to backend/Dockerfile", archiveMajor, avail[len(avail)-1], archiveMajor)
	}

	// Версию сервера читаем best-effort: не смогли — не блокируем restore, просто
	// теряем защиту от «вниз по мажорам» (её всё равно поймает pg_restore).
	targetMajor := 0
	if n, verr := serverVersionNum(ctx, e, target); verr != nil {
		log.Printf("restore: cannot read target server version (%v); skipping major preflight", verr)
	} else {
		targetMajor = majorFromVersionNum(n)
	}
	if targetMajor != 0 && targetMajor < archiveMajor {
		return fmt.Errorf("restore: dump was produced by pg_dump %d, target server is PostgreSQL %d — "+
			"a newer dump cannot be restored into an older server. Recreate the target at PostgreSQL >= %d, "+
			"or re-pair with a PostgreSQL %d (or older) source.", archiveMajor, targetMajor, archiveMajor, targetMajor)
	}
	e.pgMajor = pickPGMajor(archiveMajor, avail)
	if e.pgMajor > 0 {
		log.Printf("restore: archive from pg_dump %d, target PostgreSQL %d -> pg_restore %d",
			archiveMajor, targetMajor, e.pgMajor)
	}

	switch e.RestoreMode {
	case "", "recreate":
		return restoreRecreate(ctx, e, target, dumpPath)
	case "in_place":
		return restoreInPlace(ctx, e, target, dumpPath)
	default:
		return fmt.Errorf("restore: unknown RESTORE_MODE %q (want recreate|in_place)", e.RestoreMode)
	}
}

// downloadDump качает дамп из первого бакета, где получилось (начиная с
// S3_BUCKET_ID). Подменённую копию в любом бакете отсечёт сверка sha256 ниже.
func downloadDump(ctx context.Context, e Env, dumpPath string) (storage.JobBucket, error) {
	var errs []error
	for _, b := range e.restoreOrder() {
		mc, err := b.Minio()
		if err == nil {
			log.Printf("restore: downloading %s", b.Object(e.S3ObjectKey))
			err = mc.FGetObject(ctx, b.Bucket, e.S3ObjectKey, dumpPath, minio.GetObjectOptions{})
		}
		if err == nil {
			return b, nil
		}
		log.Printf("restore: bucket %s: %v", b, err)
		errs = append(errs, fmt.Errorf("%s: %w", b, err))
	}
	return storage.JobBucket{}, fmt.Errorf("download dump: %w", errors.Join(errs...))
}

// restoreRecreate: RENAME старую базу, создать новую, накатить, выставить
// владельца, снести старую. Всё до DROP обратимо — при ошибке старая база
// остаётся под именем <target>_old_<ts>, прогон падает.
func restoreRecreate(ctx context.Context, e Env, target, dumpPath string) error {
	oldName := fmt.Sprintf("%s_old_%s", target, time.Now().UTC().Format("20060102t150405z"))

	if err := terminateBackends(ctx, e, "postgres", target); err != nil {
		return fmt.Errorf("terminate backends: %w", err)
	}

	if err := psqlExec(ctx, e, "postgres",
		fmt.Sprintf(`ALTER DATABASE %s RENAME TO %s;`, quoteIdent(target), quoteIdent(oldName)),
	); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", target, oldName, err)
	}
	log.Printf("restore: renamed %s -> %s", target, oldName)

	createSQL := fmt.Sprintf(`CREATE DATABASE %s`, quoteIdent(target))
	if e.TargetDBOwner != "" {
		createSQL += fmt.Sprintf(` OWNER %s`, quoteIdent(e.TargetDBOwner))
	}
	if err := psqlExec(ctx, e, "postgres", createSQL+";"); err != nil {
		return fmt.Errorf("create database %s (old kept as %s): %w", target, oldName, err)
	}
	if err := ensureExtensions(ctx, e, target); err != nil {
		return fmt.Errorf("ensure extensions in %s (old kept as %s): %w", target, oldName, err)
	}
	if err := pgRestore(ctx, e, target, dumpPath, false); err != nil {
		return fmt.Errorf("restore into %s (old kept as %s): %w", target, oldName, err)
	}

	if err := psqlExec(ctx, e, "postgres",
		fmt.Sprintf(`DROP DATABASE %s;`, quoteIdent(oldName)),
	); err != nil {
		log.Printf("restore: WARNING failed to drop %s: %v (restore itself succeeded)", oldName, err)
	}
	log.Printf("restore: done (recreate)")
	return nil
}

// restoreInPlace: pg_restore --clean --if-exists прямо в существующую базу.
// Сессии обрываем, подключаясь к самой базе — базы postgres в пуле MDB нет.
func restoreInPlace(ctx context.Context, e Env, target, dumpPath string) error {
	if err := terminateBackends(ctx, e, target, target); err != nil {
		return fmt.Errorf("terminate backends: %w", err)
	}
	if err := ensureExtensions(ctx, e, target); err != nil {
		return fmt.Errorf("ensure extensions in %s: %w", target, err)
	}
	if err := pgRestore(ctx, e, target, dumpPath, true); err != nil {
		return fmt.Errorf("restore into %s: %w", target, err)
	}
	log.Printf("restore: done (in_place)")
	return nil
}

// ensureExtensions создаёт расширения из <key>.extensions ДО наката дампа, как
// restorer (не под SET ROLE — расширения принадлежат системной роли).
//   - self-hosted: restorer-суперюзер создаёт что угодно;
//   - MDB: CREATE EXTENSION IF NOT EXISTS — no-op, если включено в консоли;
//     иначе понятная ранняя ошибка вместо падения посреди дампа.
//
// Файл отсутствует (старый дамп / нет расширений) — тихо пропускаем.
func ensureExtensions(ctx context.Context, e Env, database string) error {
	names, err := downloadExtensionList(ctx, e)
	if err != nil {
		return err
	}
	for _, n := range names {
		if err := psqlExec(ctx, e, database,
			fmt.Sprintf(`CREATE EXTENSION IF NOT EXISTS %s;`, quoteIdent(n))); err != nil {
			return fmt.Errorf("create extension %s: %w", n, err)
		}
	}
	if len(names) > 0 {
		log.Printf("restore: ensured %d extension(s): %s", len(names), strings.Join(names, ", "))
	}
	return nil
}

func downloadExtensionList(ctx context.Context, e Env) ([]string, error) {
	mc, err := e.src.Minio()
	if err != nil {
		return nil, err
	}
	obj, err := mc.GetObject(ctx, e.src.Bucket, e.S3ObjectKey+".extensions", minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	b, err := io.ReadAll(io.LimitReader(obj, 64<<10))
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, nil // sidecar'а нет — не ошибка
		}
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line = strings.TrimSpace(line); line != "" && validExtName(line) {
			names = append(names, line)
		}
	}
	return names, nil
}

// terminateBackends обрывает чужие сессии к targetDB, подключаясь к connectDB.
func terminateBackends(ctx context.Context, e Env, connectDB, targetDB string) error {
	return psqlExec(ctx, e, connectDB, fmt.Sprintf(
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		 WHERE datname = %s AND pid <> pg_backend_pid();`, quoteLit(targetDB),
	))
}

// pgRestore накатывает архив pg_dump -Fc в database.
//
//   - --single-transaction: весь накат — одна транзакция. При ошибке откат
//     целиком (для in_place: половины базы не останется); подразумевает
//     exit-on-error; SET ROLE держится через пул MDB в любом режиме — это
//     буквально одна транзакция. Несовместимо с параллелизмом (-j), нам норм.
//   - --role=<owner>: pg_restore шлёт `SET ROLE <owner>` после подключения, и
//     объекты создаются под владельцем, а не под restorer'ом. Работает и на MDB
//     без суперюзера — нужно лишь членство restorer'а в роли (выдаётся в консоли
//     Yandex Cloud).
//   - --no-owner/--no-privileges: в архиве их и так нет (--exclude-extension при
//     dump'е + --no-owner/-x), это подстраховка.
//   - clean → --clean --if-exists: снести существующие объекты перед созданием
//     (in_place). Для recreate база пустая — не нужно.
//   - --use-list: если архив снят pg_dump <17 (без --exclude-extension), из TOC
//     вырезаются EXTENSION-записи — их DROP под MDB in_place падает "must be
//     owner of extension". Нужные расширения уже создал ensureExtensions.
func pgRestore(ctx context.Context, e Env, database, dumpPath string, clean bool) error {
	listPath, err := extensionFreeTOCList(ctx, e, dumpPath)
	if err != nil {
		return fmt.Errorf("build restore TOC list: %w", err)
	}

	args := []string{
		"--no-owner", "--no-privileges",
		"--single-transaction",
		"--dbname", database,
	}
	if e.TargetDBOwner != "" {
		args = append(args, "--role", e.TargetDBOwner)
	}
	if clean {
		args = append(args, "--clean", "--if-exists")
	}
	if listPath != "" {
		args = append(args, "--use-list", listPath)
	}
	args = append(args, dumpPath)

	cmd := exec.CommandContext(ctx, pgTool("pg_restore", e.pgMajor), args...)
	cmd.Env = e.pgEnv(database)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	return cmd.Run()
}

func psqlExec(ctx context.Context, e Env, database, sql string) error {
	cmd := exec.CommandContext(ctx, pgTool("psql", e.pgMajor), "--set", "ON_ERROR_STOP=1", "--quiet",
		"--no-psqlrc", "--tuples-only", "--command", sql)
	cmd.Env = e.pgEnv(database)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	return cmd.Run()
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// quoteIdent экранирует SQL-идентификатор двойными кавычками.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// quoteLit экранирует строковый литерал одинарными кавычками.
func quoteLit(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}
