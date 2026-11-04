package jobrunner

import (
	"strings"
	"testing"
)

func TestParseTerminated(t *testing.T) {
	// Вывод psql -At -z -0: поля и записи через NUL; пустой application_name — пустое поле.
	out := []byte("api\x00app\x00t\x00\x00admin\x00f\x00api\x00app\x00t\x00\n")
	got := parseTerminated(out)
	want := []backendSession{
		{App: "api", User: "app", Terminated: true},
		{App: "", User: "admin", Terminated: false},
		{App: "api", User: "app", Terminated: true},
	}
	if len(got) != len(want) {
		t.Fatalf("parseTerminated = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if s := parseTerminated(nil); len(s) != 0 {
		t.Errorf("empty output → %+v, want none", s)
	}
}

func TestSummarizeTerminated(t *testing.T) {
	if msg := summarizeTerminated("db", nil); msg != "" {
		t.Errorf("no sessions → %q, want empty", msg)
	}

	got := summarizeTerminated("dns-manager", []backendSession{
		{App: "psql", User: "admin", Terminated: true},
		{App: "api", User: "app", Terminated: true},
		{App: "api", User: "app", Terminated: true},
		{App: "", User: "", Terminated: true},
		{App: "repl", User: "rdsadmin", Terminated: false},
	})
	want := "restore: terminated 4 session(s) to dns-manager (api@app ×2, ?@?, psql@admin); " +
		"failed to terminate 1 (repl@rdsadmin)"
	if got != want {
		t.Errorf("summary =\n  %q\nwant\n  %q", got, want)
	}

	// Значения от клиента не ломают строку лога и обрезаются.
	long := strings.Repeat("x", 100)
	got = summarizeTerminated("db", []backendSession{{App: "evil\napp\x1b[31m", User: long, Terminated: true}})
	if strings.ContainsAny(got, "\n\x1b") {
		t.Errorf("control chars leaked into log line: %q", got)
	}
	if !strings.Contains(got, strings.Repeat("x", 64)+"…") || strings.Contains(got, strings.Repeat("x", 65)) {
		t.Errorf("long user not truncated to 64 runes: %q", got)
	}
}
