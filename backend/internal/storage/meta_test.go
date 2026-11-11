package storage

import (
	"maps"
	"slices"
	"testing"
)

func TestSignedDumpPrefixes(t *testing.T) {
	got := signedDumpPrefixes([]string{
		"pg/app/1.dump", "pg/app/1.dump.sha256", "pg/app/1.dump.sig", "pg/app/1.dump.extensions",
		"pg/a/b/1.dump", "pg/a/b/1.dump.sha256", "pg/a/b/1.dump.sig", // имя базы со "/"
		"pg/nosig/1.dump", "pg/nosig/1.dump.sha256", // без подписи
		"pg/partial/1.dump", "pg/partial/1.dump.sig", // не залит до конца
		"top.dump", "top.dump.sha256", "top.dump.sig", // нет инстанса
		"pg/meta.json", "pg/meta.json.sig",
	})
	want := []string{"pg/a/b", "pg/app"}
	if keys := slices.Sorted(maps.Keys(got)); !slices.Equal(keys, want) {
		t.Fatalf("got %v, want %v", keys, want)
	}
}
