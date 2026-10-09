package jobrunner

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPickPGMajor(t *testing.T) {
	avail := []int{16, 17, 18}
	cases := []struct {
		want  int
		avail []int
		out   int
	}{
		{17, avail, 17},         // точное совпадение
		{16, avail, 16},         //
		{18, avail, 18},         //
		{15, avail, 16},         // старее всех доступных → младший
		{19, avail, 18},         // новее всех → старший (упадёт позже с понятной ошибкой)
		{0, avail, 0},           // версия неизвестна → PATH
		{17, nil, 0},            // клиентов по мажорам нет → PATH
		{17, []int{16, 18}, 18}, // пропуск в наборе → ближайший сверху
	}
	for _, c := range cases {
		if got := pickPGMajor(c.want, c.avail); got != c.out {
			t.Errorf("pickPGMajor(%d, %v) = %d, want %d", c.want, c.avail, got, c.out)
		}
	}
}

func TestMajorFromVersionNum(t *testing.T) {
	for in, want := range map[int]int{170004: 17, 160010: 16, 180000: 18, 0: 0} {
		if got := majorFromVersionNum(in); got != want {
			t.Errorf("majorFromVersionNum(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestPGDumpVersionRe(t *testing.T) {
	header := `;
; Archive created at 2026-09-07 10:00:00 UTC
;     dbname: app
;     TOC Entries: 42
;     Compression: -1
;     Dump Version: 1.16-0
;     Format: CUSTOM
;     Integer: 4 bytes
;     Offset: 8 bytes
;     Dumped from database version: 17.5
;     Dumped by pg_dump version: 18.6
;
`
	m := pgDumpVersionRe.FindStringSubmatch(header)
	if m == nil || m[1] != "18" {
		t.Fatalf("FindStringSubmatch = %v, want [_ 18]", m)
	}
}

func TestTocExtensionEntry(t *testing.T) {
	drop := []string{
		"2; 3079 16385 EXTENSION - pg_trgm ",
		"3499; 0 0 COMMENT - EXTENSION pg_trgm ",
		"1234; 0 0 ACL - EXTENSION pg_trgm ",
	}
	keep := []string{
		"216; 1259 16466 TABLE public t postgres",
		"3492; 0 16466 TABLE DATA public t postgres",
		"217; 1259 16467 TABLE public extension_audit postgres", // "extension" в имени
		";     Dumped by pg_dump version: 16.15",
		"; Selected TOC Entries:",
		"",
		"4; 1259 16500 FUNCTION public f() postgres",
	}
	for _, l := range drop {
		if !tocExtensionEntry.MatchString(l) {
			t.Errorf("expected to drop: %q", l)
		}
	}
	for _, l := range keep {
		if tocExtensionEntry.MatchString(l) {
			t.Errorf("expected to keep: %q", l)
		}
	}
}

func TestAvailablePGMajors(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"postgresql16", "postgresql18", "postgresql", "not-postgres"} {
		if err := os.Mkdir(filepath.Join(dir, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := pgLibexec
	pgLibexec = dir
	t.Cleanup(func() { pgLibexec = old })

	if got := availablePGMajors(); !slices.Equal(got, []int{16, 18}) {
		t.Errorf("availablePGMajors() = %v, want [16 18]", got)
	}
}

func TestPGToolFallsBackToPATH(t *testing.T) {
	if got := pgTool("pg_dump", 0); got != "pg_dump" {
		t.Errorf("pgTool(pg_dump, 0) = %q, want pg_dump", got)
	}
	old := pgLibexec
	pgLibexec = t.TempDir()
	t.Cleanup(func() { pgLibexec = old })
	if got := pgTool("pg_restore", 17); got != "pg_restore" {
		t.Errorf("pgTool(pg_restore, 17) with no libexec = %q, want pg_restore", got)
	}
}
