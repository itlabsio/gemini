package storage

import "testing"

func TestLabels(t *testing.T) {
	b := JobBucket{ID: BuiltInID, Name: "helm", Endpoint: "storage.yandexcloud.net/", Bucket: "dumps"}
	if got, want := b.String(), "s3://dumps (storage.yandexcloud.net)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := b.Object("db/x.dump"), "s3://dumps/db/x.dump (storage.yandexcloud.net)"; got != want {
		t.Errorf("Object() = %q, want %q", got, want)
	}
}

func TestExtensionsDigest(t *testing.T) {
	if got := ExtensionsDigest(nil, false); got != NoExtensionsDigest {
		t.Errorf("missing sidecar digest = %q, want %q", got, NoExtensionsDigest)
	}
	empty := ExtensionsDigest([]byte{}, true)
	if empty == NoExtensionsDigest {
		t.Error("empty sidecar must differ from a missing one")
	}
	if a, b := ExtensionsDigest([]byte("pg_trgm\n"), true), ExtensionsDigest([]byte("pg_trgm\ndblink\n"), true); a == b {
		t.Error("different lists share a digest")
	}
}

func TestParseExtensions(t *testing.T) {
	got := ParseExtensions([]byte("pg_trgm\n\n  uuid-ossp \nbad;name\npostgis\n"))
	want := []string{"pg_trgm", "uuid-ossp", "postgis"}
	if len(got) != len(want) {
		t.Fatalf("ParseExtensions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParseExtensions = %v, want %v", got, want)
		}
	}
}
