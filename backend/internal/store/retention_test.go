package store

import (
	"testing"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

func TestPruneRunsKeepsLatestAndActive(t *testing.T) {
	s, ctx := testStore(t)

	inst, err := s.CreateInstance(ctx, InstanceInput{
		Role: model.InstanceSource, Name: "prunetest", Host: "db.example", Port: 5432,
		SSLMode: "require", AuthType: model.AuthPlain, PlainUsername: "u", PlainPassword: "p",
	}, "tester@example.com")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteInstance(ctx, inst.ID) })
	dbs, err := s.SyncDiscovered(ctx, inst.ID, []string{"app"}, 0)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	dbID := dbs[0].ID

	// Прогон с возрастом ageDays; finish=false — остаётся активным.
	mk := func(kind model.RunKind, ageDays int, finish bool) string {
		t.Helper()
		run, err := s.CreateRun(ctx, NewRunInput{DatabaseID: dbID, Kind: kind, Trigger: model.TriggerManual})
		if err != nil {
			t.Fatalf("create run: %v", err)
		}
		if finish {
			if _, err := s.UpdateRun(ctx, run.ID, RunUpdate{Status: model.StatusSucceeded, MarkFinished: true}); err != nil {
				t.Fatalf("finish run: %v", err)
			}
		}
		if _, err := s.pool.Exec(ctx, `UPDATE backup_runs SET created_at = NOW() - make_interval(days => $2) WHERE id = $1`,
			run.ID, ageDays); err != nil {
			t.Fatalf("age run: %v", err)
		}
		return run.ID
	}
	// Оба старых dump'а удаляются: есть более новый (активный) dump. Единственный
	// restore старше срока, но он последний своего вида — остаётся.
	oldDump := mk(model.KindDump, 40, true)
	prevDump := mk(model.KindDump, 30, true)
	lastRestore := mk(model.KindRestore, 50, true)
	freshActive := mk(model.KindDump, 0, false)

	n, err := s.PruneRuns(ctx, time.Now().AddDate(0, 0, -14))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 2 {
		t.Fatalf("pruned %d runs, want 2 (both old dumps: a newer dump exists)", n)
	}
	for id, want := range map[string]bool{oldDump: false, prevDump: false, lastRestore: true, freshActive: true} {
		_, err := s.GetRun(ctx, id)
		if got := err == nil; got != want {
			t.Fatalf("run %s exists=%v, want %v", id, got, want)
		}
	}
}

func TestPruneAudit(t *testing.T) {
	s, ctx := testStore(t)
	if err := s.WriteAudit(ctx, "tester", "prune.old", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAudit(ctx, "tester", "prune.fresh", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE audit_log SET created_at = NOW() - interval '400 days' WHERE action = 'prune.old'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(ctx, `DELETE FROM audit_log WHERE action LIKE 'prune.%'`) })

	if _, err := s.PruneAudit(ctx, time.Now().AddDate(0, 0, -365)); err != nil {
		t.Fatalf("prune: %v", err)
	}
	var left []string
	rows, err := s.pool.Query(ctx, `SELECT action FROM audit_log WHERE action LIKE 'prune.%'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a string
		_ = rows.Scan(&a)
		left = append(left, a)
	}
	if len(left) != 1 || left[0] != "prune.fresh" {
		t.Fatalf("left %v, want [prune.fresh]", left)
	}
}
