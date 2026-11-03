package db

import (
	"fmt"
	"strings"
	"testing"
)

// Миграции применяются по имени в лексикографическом порядке: номера должны
// идти подряд с 001, трёхзначными и без дублей — иначе две ветки с одинаковым
// номером молча применятся в случайном относительном порядке.
func TestMigrationNumbering(t *testing.T) {
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no embedded migrations")
	}
	for i, n := range names {
		want := fmt.Sprintf("%03d_", i+1)
		if !strings.HasPrefix(n, want) {
			t.Fatalf("migration #%d is %q, want prefix %q (gap or duplicate number)", i+1, n, want)
		}
		body, err := migrationsFS.ReadFile("migrations/" + n)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(body)) == "" {
			t.Errorf("%s is empty", n)
		}
	}
}
