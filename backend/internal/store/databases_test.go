package store

import (
	"context"
	"errors"
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

// Креды restore базы: пароль шифруется и читается только DatabaseCredentials,
// пустой пароль — «не менять» (но не при первой настройке), в выборках баз
// виден лишь username.
func TestDatabaseCredentialsRoundTrip(t *testing.T) {
	s, ctx := testStore(t)

	inst, err := s.CreateInstance(ctx, InstanceInput{
		Role: model.InstanceTarget, Name: "dbcredstest", Host: "db.example", Port: 5432,
		RestoreMode: model.RestoreInPlacePerDB,
		SSLMode:     "require", AuthType: model.AuthPlain, PlainUsername: "u", PlainPassword: "p",
	}, "tester@example.com")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteInstance(ctx, inst.ID) })
	dbs, err := s.SyncDiscovered(ctx, inst.ID, []string{"app"}, 0)
	if err != nil || len(dbs) != 1 {
		t.Fatalf("sync: %v %v", dbs, err)
	}
	id := dbs[0].ID

	if _, _, err := s.DatabaseCredentials(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no creds yet: err = %v, want ErrNotFound", err)
	}
	if err := s.SetDatabaseCredentials(ctx, id, "owner", ""); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("first set without password: err = %v, want ErrPasswordRequired", err)
	}
	if err := s.SetDatabaseCredentials(ctx, id, "owner", "s3cret"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.SetDatabaseCredentials(ctx, id, "owner2", ""); err != nil {
		t.Fatalf("rename keeping password: %v", err)
	}
	user, pass, err := s.DatabaseCredentials(ctx, id)
	if err != nil || user != "owner2" || pass != "s3cret" {
		t.Fatalf("creds = %q/%q (%v), want owner2/s3cret", user, pass, err)
	}
	for _, get := range []func() (*model.Database, error){
		func() (*model.Database, error) { return s.GetDatabase(ctx, id) },
		func() (*model.Database, error) {
			return s.PatchDatabase(ctx, id, DatabasePatch{Enabled: new(bool)})
		},
	} {
		if d, err := get(); err != nil || d.RestoreUsername != "owner2" {
			t.Fatalf("restore_username = %+v (%v), want owner2", d, err)
		}
	}
	if err := s.DeleteDatabaseCredentials(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if d, _ := s.GetDatabase(ctx, id); d == nil || d.RestoreUsername != "" {
		t.Fatalf("after delete restore_username = %+v, want empty", d)
	}
}
