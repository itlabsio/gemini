package store

import (
	"testing"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

func TestListAudit(t *testing.T) {
	s, ctx := testStore(t)

	inst, err := s.CreateInstance(ctx, InstanceInput{
		Role: model.InstanceSource, Name: "audittest", Host: "db.example", Port: 5432,
		SSLMode: "require", AuthType: model.AuthPlain, PlainUsername: "u", PlainPassword: "p",
	}, "tester@example.com")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	t.Cleanup(func() {
		_ = s.DeleteInstance(ctx, inst.ID)
		_, _ = s.pool.Exec(ctx, `DELETE FROM audit_log WHERE actor LIKE 'audit-%'`)
	})

	for _, e := range []AuditEntry{
		{Actor: "audit-a", Action: "instance.update", Target: inst.ID},
		{Actor: "audit-a", Action: "settings.update", Target: "app_settings", Detail: []byte(`{"note":"100%_done"}`)},
		{Actor: "audit-b", Action: "access.denied", Target: "PUT /api/instances/x", IP: "203.0.113.7"},
	} {
		if err := s.WriteAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	list := func(f AuditFilter) []model.AuditRecord {
		t.Helper()
		items, err := s.ListAudit(ctx, f)
		if err != nil {
			t.Fatalf("list %+v: %v", f, err)
		}
		var out []model.AuditRecord
		for _, it := range items { // в общей тестовой БД могут быть чужие записи
			if it.Actor == "audit-a" || it.Actor == "audit-b" {
				out = append(out, it)
			}
		}
		return out
	}

	all := list(AuditFilter{})
	if len(all) != 3 || all[0].Action != "access.denied" || all[0].IP != "203.0.113.7" {
		t.Fatalf("newest first with ip: %+v", all)
	}
	if all[2].ObjectName != "audittest" {
		t.Fatalf("instance target must resolve to its name, got %q", all[2].ObjectName)
	}
	if got := list(AuditFilter{BeforeID: all[0].ID}); len(got) != 2 {
		t.Fatalf("cursor: got %d, want 2", len(got))
	}
	if got := list(AuditFilter{Actor: "audit-b"}); len(got) != 1 {
		t.Fatalf("actor filter: got %d", len(got))
	}
	if got := list(AuditFilter{Actions: []string{"instance.update", "settings.update"}}); len(got) != 2 {
		t.Fatalf("action filter: got %d", len(got))
	}
	if got := list(AuditFilter{Query: "AUDITTEST"}); len(got) != 1 || got[0].Action != "instance.update" {
		t.Fatalf("search by object name: %+v", got)
	}
	// % и _ ищутся буквально, а не как шаблон LIKE.
	if got := list(AuditFilter{Query: "100%_"}); len(got) != 1 {
		t.Fatalf("literal search: got %d", len(got))
	}
	if got := list(AuditFilter{Query: "1%0"}); len(got) != 0 {
		t.Fatalf("%% must not act as wildcard: got %d", len(got))
	}
}
