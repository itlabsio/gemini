package api

import (
	"testing"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
)

func TestMappingDumpPrefix(t *testing.T) {
	for in, want := range map[string]string{
		"pg/orders":  "pg/orders/",
		"pg/orders/": "pg/orders/",
	} {
		if got := mappingDumpPrefix(&model.DatabaseMapping{S3Prefix: in}); got != want {
			t.Errorf("mappingDumpPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRestoreCandidates(t *testing.T) {
	const prefix = "pg/orders/"
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	dump := func(name string, day int, ready, signed bool) storage.LocatedDump {
		return storage.LocatedDump{Object: storage.Object{
			Key: prefix + name, Size: int64(day), LastModified: base.AddDate(0, 0, day),
			Ready: ready, Signed: signed,
		}}
	}
	located := []storage.LocatedDump{
		dump("d1.dump", 1, true, true),
		dump("d3.dump", 3, true, false),
		dump("d2.dump", 2, true, true),
		dump("d9.dump", 9, false, false), // ещё дозаписывается
		dump("d5.dump", 5, true, true),
		{Object: storage.Object{Key: "pg/orders-old/x.dump", LastModified: base.AddDate(0, 0, 8), Ready: true}},
		{Object: storage.Object{Key: prefix + "nested/x.dump", LastModified: base.AddDate(0, 0, 7), Ready: true}},
	}

	got := restoreCandidates(located, prefix, 3)
	want := []string{"d5.dump", "d3.dump", "d2.dump"}
	if len(got) != len(want) {
		t.Fatalf("restoreCandidates = %+v, want keys %v", got, want)
	}
	for i, name := range want {
		if got[i].Key != prefix+name {
			t.Errorf("[%d] = %s, want %s", i, got[i].Key, prefix+name)
		}
	}
	if !got[0].Signed || got[1].Signed {
		t.Errorf("signed flags = %v/%v, want true/false", got[0].Signed, got[1].Signed)
	}

	if all := restoreCandidates(located, prefix, restoreCandidatesLimit); len(all) != 4 {
		t.Errorf("unlimited candidates = %d, want 4 ready direct-child dumps", len(all))
	}
	if empty := restoreCandidates(nil, prefix, 1); empty == nil || len(empty) != 0 {
		t.Errorf("no dumps → %v, want empty non-nil slice (JSON [])", empty)
	}
}
