package jobrunner

import (
	"strings"
	"testing"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
)

func TestRestoreOrderPreferredFirst(t *testing.T) {
	e := Env{
		S3Buckets:  []storage.JobBucket{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		S3BucketID: "b",
	}
	got := e.restoreOrder()
	want := []string{"b", "a", "c"}
	for i, b := range got {
		if b.ID != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}

	e.S3BucketID = "missing"
	if got := e.restoreOrder(); len(got) != 3 || got[0].ID != "a" {
		t.Fatalf("unknown preferred id must keep original order: %v", got)
	}
}

func TestParseExtensionsEnv(t *testing.T) {
	got := parseExtensionsEnv(" pg_trgm,,uuid-ossp, bad\"name ,postgis")
	want := []string{"pg_trgm", "uuid-ossp", "postgis"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("parseExtensionsEnv = %v, want %v", got, want)
	}
	if got := parseExtensionsEnv(""); len(got) != 0 {
		t.Fatalf("empty EXTENSIONS → %v, want none", got)
	}
}

// pg_restore в in_place_per_db идёт под пользователем базы, остальные режимы —
// под restorer'ом; RESTORE_PG* дочерним процессам не достаются.
func TestRestoreEnvUser(t *testing.T) {
	t.Setenv("RESTORE_PGPASSWORD", "db-secret")
	e := Env{PGUser: "gemini", PGPassword: "inst-secret", RestorePGUser: "digital_sign", RestorePGPassword: "db-secret"}

	lookup := func(env []string, key string) string {
		v := ""
		for _, kv := range env {
			if k, val, _ := strings.Cut(kv, "="); k == key {
				v = val // последнее значение побеждает, как в exec
			}
		}
		return v
	}
	for _, tc := range []struct{ mode, user, pass string }{
		{"in_place", "gemini", "inst-secret"},
		{"recreate", "gemini", "inst-secret"},
		{"in_place_per_db", "digital_sign", "db-secret"},
	} {
		e.RestoreMode = tc.mode
		env := e.restoreEnv("app")
		if got := lookup(env, "PGUSER"); got != tc.user {
			t.Errorf("%s: PGUSER = %q, want %q", tc.mode, got, tc.user)
		}
		if got := lookup(env, "PGPASSWORD"); got != tc.pass {
			t.Errorf("%s: PGPASSWORD = %q, want %q", tc.mode, got, tc.pass)
		}
		if got := lookup(env, "RESTORE_PGPASSWORD"); got != "" {
			t.Errorf("%s: RESTORE_PGPASSWORD leaked to child env", tc.mode)
		}
	}
}

func TestMissingExtensions(t *testing.T) {
	got := missingExtensions([]string{"pg_trgm", "postgis", "uuid-ossp"}, []string{"plpgsql", "pg_trgm", "uuid-ossp"})
	if len(got) != 1 || got[0] != "postgis" {
		t.Fatalf("missing = %v, want [postgis]", got)
	}
	if got := missingExtensions(nil, []string{"plpgsql"}); len(got) != 0 {
		t.Fatalf("missing = %v, want none", got)
	}
}
