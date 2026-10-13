package jobrunner

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Клиентские утилиты PostgreSQL в образе разложены по мажорам:
// /usr/libexec/postgresql<N>/{pg_dump,pg_restore,psql}. Набор мажоров — в
// backend/Dockerfile (postgresql<N>-client), сейчас 16/17/18.
//
// Зачем несколько мажоров: архив `pg_dump -Fc` несёт формат своей мажорной
// версии и список GUC, которые pg_restore выставляет на целевом сервере.
// pg_dump 18, натравленный на сервер 16/17, кладёт в архив `SET
// transaction_timeout` (GUC появился в PG17) — и restore в сервер <17 падает на
// «unrecognized configuration parameter». Поэтому dump снимаем pg_dump'ом
// мажора источника, а restore накатываем pg_restore'ом мажора архива.
//
// Restore «вниз по мажорам» (архив из PG N в сервер PG M < N) невозможен: архив
// в формате N, pg_restore обязан быть ≥ N и всё равно выставит GUC из N.
// runRestore ловит это на префлайте и падает с понятной ошибкой.

// pgLibexec — где apk раскладывает versioned-бинари. Переменная — для тестов.
var pgLibexec = "/usr/libexec"

// availablePGMajors — мажоры установленных клиентов по возрастанию. Пусто → в
// образе только дефолтные бинари из PATH (dev/CI).
func availablePGMajors() []int {
	entries, err := os.ReadDir(pgLibexec)
	if err != nil {
		return nil
	}
	var majors []int
	for _, e := range entries {
		if !e.IsDir() {
			continue // симлинк postgresql -> postgresql<N> сюда не попадает
		}
		var n int
		if _, err := fmt.Sscanf(e.Name(), "postgresql%d", &n); err == nil && n > 0 {
			majors = append(majors, n)
		}
	}
	sort.Ints(majors)
	return majors
}

// pgTool — путь к утилите нужного мажора; нет такого каталога (или major=0) →
// голое имя из PATH.
func pgTool(name string, major int) string {
	if major > 0 {
		p := filepath.Join(pgLibexec, fmt.Sprintf("postgresql%d", major), name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return name
}

// pickPGMajor выбирает мажор клиента для работы с сервером/архивом мажора want:
// точное совпадение, иначе ближайший доступный не ниже want (pg_dump/pg_restore
// умеют работать со «старшими» относительно себя данными, но не с «младшими»),
// иначе самый старший. want ≤ 0 → 0 (PATH). avail отсортирован по возрастанию.
func pickPGMajor(want int, avail []int) int {
	if want <= 0 || len(avail) == 0 {
		return 0
	}
	for _, m := range avail {
		if m >= want {
			return m
		}
	}
	return avail[len(avail)-1]
}

func majorFromVersionNum(n int) int { return n / 10000 }

// serverVersionNum читает server_version_num сервера (напр. 170004 = PG 17.4).
// Пробуем дефолтным psql — простой SHOW работает с сервером любой версии.
func serverVersionNum(ctx context.Context, e Env, database string) (int, error) {
	cmd := exec.CommandContext(ctx, pgTool("psql", 0),
		"--no-psqlrc", "--tuples-only", "--no-align", "--quiet",
		"--command", "SHOW server_version_num")
	cmd.Env = e.pgEnv(database)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// serverVersionNumRetry — serverVersionNum с несколькими попытками: разовый
// сетевой сбой (переключение мастера MDB, connect timeout) не должен ронять dump.
func serverVersionNumRetry(ctx context.Context, e Env, database string) (int, error) {
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		var n int
		if n, err = serverVersionNum(ctx, e, database); err == nil {
			return n, nil
		}
		log.Printf("dump: read server version (attempt %d/3): %v", attempt, err)
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Duration(attempt) * 5 * time.Second):
		}
	}
	return 0, err
}

var pgDumpVersionRe = regexp.MustCompile(`(?i)dumped by pg_dump version:\s*(\d+)`)

// archivePgDumpMajor — мажор pg_dump, снявшего архив -Fc, из его заголовка
// (`pg_restore --list`). Читаем самым старшим доступным pg_restore: он осилит
// любой архив не новее себя.
func archivePgDumpMajor(ctx context.Context, dumpPath string, avail []int) (int, error) {
	major := 0
	if len(avail) > 0 {
		major = avail[len(avail)-1]
	}
	cmd := exec.CommandContext(ctx, pgTool("pg_restore", major), "--list", dumpPath)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("pg_restore --list %s: %w", filepath.Base(dumpPath), err)
	}
	m := pgDumpVersionRe.FindSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("archive header has no 'Dumped by pg_dump version'")
	}
	return strconv.Atoi(string(m[1]))
}

// tocExtensionEntry матчит строки `pg_restore --list`, относящиеся к расширениям:
//
//	2; 3079 16385 EXTENSION - pg_trgm
//	3499; 0 0 COMMENT - EXTENSION pg_trgm
//	1234; 0 0 ACL - EXTENSION pg_trgm
var tocExtensionEntry = regexp.MustCompile(`^\d+;\s+\d+\s+\d+\s+(EXTENSION\b|(?:COMMENT|ACL)\s+-\s+EXTENSION\b)`)

// extensionFreeTOCList строит для `pg_restore --use-list` копию TOC архива без
// EXTENSION-записей: их DROP под MDB in_place (--clean) падает "must be owner of
// extension", а нужные расширения создаёт ensureExtensions до наката. Архивы
// pg_dump ≥17 снимаются с --exclude-extension — EXTENSION-записей нет, функция
// возвращает "" и --use-list не нужен.
//
// Вместе с EXTENSION вырезаются TABLE DATA / SEQUENCE SET конфиг-таблиц
// расширений (pg_extension.extconfig, напр. postgis spatial_ref_sys): pg_dump <17
// кладёт их данные в архив, а под --role=<owner> COPY в таблицу, созданную
// restorer'ом в ensureExtensions, падает "permission denied". pg_dump ≥17 с
// --exclude-extension их тоже не дампит — поведение одинаковое. Список
// конфиг-таблиц читаем из database (расширения там уже созданы).
func extensionFreeTOCList(ctx context.Context, e Env, database, dumpPath string) (string, error) {
	out, err := exec.CommandContext(ctx, pgTool("pg_restore", e.pgMajor), "--list", dumpPath).Output()
	if err != nil {
		return "", fmt.Errorf("pg_restore --list: %w", err)
	}
	lines := strings.Split(string(out), "\n")
	if !slices.ContainsFunc(lines, tocExtensionEntry.MatchString) {
		return "", nil
	}
	cfg, err := extensionConfigTables(ctx, e, database)
	if err != nil {
		return "", fmt.Errorf("list extension config tables: %w", err)
	}
	kept, droppedExt, droppedData := filterTOC(lines, cfg)
	path := filepath.Join(e.WorkDir, "restore.toc")
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600); err != nil {
		return "", err
	}
	log.Printf("restore: TOC filter dropped %d EXTENSION entr(y/ies) (archive from pg_dump without --exclude-extension)", droppedExt)
	if len(droppedData) > 0 {
		log.Printf("restore: TOC filter dropped data of extension config table(s): %s", strings.Join(droppedData, ", "))
	}
	return path, nil
}

// tocDataEntry матчит записи с данными таблиц/последовательностей:
//
//	5432; 0 16400 TABLE DATA public spatial_ref_sys postgres
//	5433; 0 0 SEQUENCE SET public some_seq postgres
var tocDataEntry = regexp.MustCompile(`^\d+;\s+\d+\s+\d+\s+(?:TABLE DATA|SEQUENCE SET)\s+(\S+)\s+(\S+)`)

// filterTOC выкидывает из строк `pg_restore --list` EXTENSION-записи и данные
// конфиг-таблиц расширений (cfg — множество "schema.name").
func filterTOC(lines []string, cfg map[string]bool) (kept []string, droppedExt int, droppedData []string) {
	kept = make([]string, 0, len(lines))
	for _, ln := range lines {
		if tocExtensionEntry.MatchString(ln) {
			droppedExt++
			continue
		}
		if m := tocDataEntry.FindStringSubmatch(ln); m != nil && cfg[m[1]+"."+m[2]] {
			droppedData = append(droppedData, m[1]+"."+m[2])
			continue
		}
		kept = append(kept, ln)
	}
	return kept, droppedExt, droppedData
}

// extensionConfigTables — конфиг-таблицы/последовательности всех расширений в
// database как множество "schema.name".
func extensionConfigTables(ctx context.Context, e Env, database string) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, pgTool("psql", e.pgMajor), "--set", "ON_ERROR_STOP=1",
		"--no-psqlrc", "--tuples-only", "--no-align", "--quiet",
		"--command", `SELECT n.nspname || '.' || c.relname
		 FROM pg_extension x, unnest(x.extconfig) AS cfg(oid)
		 JOIN pg_class c ON c.oid = cfg.oid
		 JOIN pg_namespace n ON n.oid = c.relnamespace;`)
	cmd.Env = e.pgEnv(database)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, ln := range strings.Split(string(out), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			set[ln] = true
		}
	}
	return set, nil
}
