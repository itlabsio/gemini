package jobrunner

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
func extensionFreeTOCList(ctx context.Context, e Env, dumpPath string) (string, error) {
	out, err := exec.CommandContext(ctx, pgTool("pg_restore", e.pgMajor), "--list", dumpPath).Output()
	if err != nil {
		return "", fmt.Errorf("pg_restore --list: %w", err)
	}
	lines := strings.Split(string(out), "\n")
	kept := make([]string, 0, len(lines))
	dropped := 0
	for _, ln := range lines {
		if tocExtensionEntry.MatchString(ln) {
			dropped++
			continue
		}
		kept = append(kept, ln)
	}
	if dropped == 0 {
		return "", nil
	}
	path := filepath.Join(e.WorkDir, "restore.toc")
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600); err != nil {
		return "", err
	}
	log.Printf("restore: TOC filter dropped %d EXTENSION entr(y/ies) (archive from pg_dump without --exclude-extension)", dropped)
	return path, nil
}
