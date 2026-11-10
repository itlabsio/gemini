package store

import (
	"errors"
	"slices"
	"testing"

	"github.com/itlabsio/gemini/backend/internal/model"
)

// Флаг базы на Target — только автовосстановление; предохранитель инстанса
// (restorable) выключает авто, но не restore-шаблоны: их удаление каскадом
// убило бы идущий restore (ручной запуск отсекает StartRestore).
func TestRestoreMappingSelection(t *testing.T) {
	s, ctx := testStore(t)

	inst, err := s.CreateInstance(ctx, InstanceInput{
		Role: model.InstanceTarget, Name: "restoresel", Host: "db.example", Port: 5432,
		SSLMode: "require", AuthType: model.AuthPlain, PlainUsername: "u", PlainPassword: "p",
	}, "tester@example.com")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if !inst.Restorable {
		t.Fatal("new instance must be restorable by default")
	}

	var pairingID string
	if err := s.pool.QueryRow(ctx, `INSERT INTO head_pairings (peer_role, status)
		VALUES ('source', 'revoked') RETURNING id`).Scan(&pairingID); err != nil {
		t.Fatalf("pairing: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM head_pairings WHERE id = $1`, pairingID) // каскадом — mappings
		_ = s.DeleteInstance(ctx, inst.ID)
	})

	if _, err := s.SyncDiscovered(ctx, inst.ID, []string{"auto", "manual"}, nil, 0); err != nil {
		t.Fatalf("discover: %v", err)
	}
	dbs, err := s.ListDatabases(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, d := range dbs {
		ids[d.DBName] = d.ID
		if d.Enabled {
			t.Errorf("%s: discovered database must start with auto-restore off", d.DBName)
		}
	}
	on := true
	if _, err := s.PatchDatabase(ctx, ids["auto"], DatabasePatch{Enabled: &on}); err != nil {
		t.Fatalf("enable auto: %v", err)
	}

	mapped := map[string]string{} // mapping id → имя целевой базы
	for _, name := range []string{"auto", "manual"} {
		m, err := s.UpsertMapping(ctx, MappingInput{
			HeadPairingID: pairingID, SourceDatabaseExternalID: "restoresel-" + name,
			SourceDBName: name, TargetDatabaseID: ids[name], TargetOwner: "app", S3Prefix: "src/" + name,
		}, "tester@example.com")
		if err != nil {
			t.Fatalf("mapping %s: %v", name, err)
		}
		mapped[m.ID] = name
	}

	names := func(ms []model.DatabaseMapping, err error) []string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range ms {
			if n, ok := mapped[m.ID]; ok { // в общей тестовой БД могут быть чужие строки
				out = append(out, n)
			}
		}
		slices.Sort(out)
		return out
	}

	if got := names(s.AutoRestoreMappings(ctx)); !slices.Equal(got, []string{"auto"}) {
		t.Errorf("auto-restore mappings = %v, want [auto]", got)
	}
	if got := names(s.TemplateMappings(ctx)); !slices.Equal(got, []string{"auto", "manual"}) {
		t.Errorf("template mappings = %v, want [auto manual]", got)
	}

	// Webhook ищет сопоставление без оглядки на флаги (их проверяет StartRestore).
	if m, err := s.MappingByPairingExternal(ctx, pairingID, "restoresel-manual"); err != nil || mapped[m.ID] != "manual" {
		t.Errorf("MappingByPairingExternal(manual) = %v, %v", m, err)
	}
	if _, err := s.MappingByPairingExternal(ctx, "00000000-0000-0000-0000-000000000000", "restoresel-manual"); !errors.Is(err, ErrNotFound) {
		t.Errorf("MappingByPairingExternal(other pairing) err = %v, want ErrNotFound", err)
	}

	// Предохранитель: авто нет, шаблоны остаются.
	inst, err = s.SetInstanceRestorable(ctx, inst.ID, false)
	if err != nil || inst.Restorable {
		t.Fatalf("disable restore: %v, restorable=%v", err, inst != nil && inst.Restorable)
	}
	if got := names(s.AutoRestoreMappings(ctx)); len(got) != 0 {
		t.Errorf("auto-restore mappings with restorable=false = %v, want none", got)
	}
	if got := names(s.TemplateMappings(ctx)); !slices.Equal(got, []string{"auto", "manual"}) {
		t.Errorf("template mappings with restorable=false = %v, want [auto manual]", got)
	}
	// Настройки баз при этом не трогаются — после снятия запрета всё как было.
	if inst, err = s.SetInstanceRestorable(ctx, inst.ID, true); err != nil || !inst.Restorable {
		t.Fatalf("enable restore: %v", err)
	}
	if got := names(s.AutoRestoreMappings(ctx)); !slices.Equal(got, []string{"auto"}) {
		t.Errorf("auto-restore mappings after re-enable = %v, want [auto]", got)
	}

	if _, err := s.SetInstanceRestorable(ctx, "00000000-0000-0000-0000-000000000000", false); err != ErrNotFound {
		t.Errorf("unknown instance: err = %v, want ErrNotFound", err)
	}
}
