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
