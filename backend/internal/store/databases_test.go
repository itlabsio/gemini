package store

import (
	"context"
	"slices"
	"testing"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

func dbNames(dbs []model.Database) []string {
	out := make([]string, len(dbs))
	for i, d := range dbs {
		out[i] = d.DBName
	}
	slices.Sort(out)
	return out
}

func listNames(t *testing.T, s *Store, ctx context.Context, id string) []string {
	t.Helper()
	dbs, err := s.ListDatabases(ctx, id)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return dbNames(dbs)
}

func TestListDatabasesHidesExcluded(t *testing.T) {
	s, ctx := testStore(t)

	inst, err := s.CreateInstance(ctx, InstanceInput{
		Role: model.InstanceSource, Name: "hidetest", Host: "db.example", Port: 5432,
		SSLMode: "require", AuthType: model.AuthPlain, PlainUsername: "u", PlainPassword: "p",
	}, "tester@example.com")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteInstance(ctx, inst.ID) })

	// Все базы уже в таблице (первый discovery до настройки исключений).
	for _, name := range []string{"postgres", "app", "billing", "audit"} {
		if _, err := s.SyncDiscovered(ctx, inst.ID, []string{name}, 0); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	// postgres скрыта всегда, даже без списка исключений.
	got := listNames(t, s, ctx, inst.ID)
	if !slices.Equal(got, []string{"app", "audit", "billing"}) {
		t.Fatalf("до исключений ожидали app/audit/billing (postgres скрыта), получили %v", got)
	}

	// audit в исключения инстанса — исчезает из списка без re-discovery.
	if _, err := s.UpdateInstance(ctx, inst.ID, InstanceInput{
		Role: model.InstanceSource, Name: "hidetest", Host: "db.example", Port: 5432,
		SSLMode: "require", AuthType: model.AuthPlain, PlainUsername: "u", PlainPassword: "",
		ExcludedDatabases: []string{"audit"},
	}); err != nil {
		t.Fatalf("update excluded: %v", err)
	}
	got = listNames(t, s, ctx, inst.ID)
	if !slices.Equal(got, []string{"app", "billing"}) {
		t.Fatalf("после исключения audit ожидали app/billing, получили %v", got)
	}
	t.Logf("список без re-discovery: %v (postgres и audit скрыты)", got)
}
