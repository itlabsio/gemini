package store

import (
	"testing"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

func TestRecentRunsByInstance(t *testing.T) {
	s, ctx := testStore(t)

	inst, err := s.CreateInstance(ctx, InstanceInput{
		Role: model.InstanceSource, Name: "recentruns", Host: "db.example", Port: 5432,
		SSLMode: "require", AuthType: model.AuthPlain, PlainUsername: "u", PlainPassword: "p",
	}, "tester@example.com")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteInstance(ctx, inst.ID) })

	dbs, err := s.SyncDiscovered(ctx, inst.ID, []string{"app", "billing"}, 0)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	byName := map[string]string{}
	for _, d := range dbs {
		byName[d.DBName] = d.ID
	}

	// app: 4 завершённых прогона, billing: 1.
	statuses := []model.RunStatus{model.StatusSucceeded, model.StatusFailed, model.StatusSucceeded, model.StatusFailed}
	for _, st := range statuses {
		run, err := s.CreateRun(ctx, NewRunInput{DatabaseID: byName["app"], Kind: model.KindDump, Trigger: model.TriggerManual})
		if err != nil {
			t.Fatalf("create run: %v", err)
		}
		if _, err := s.UpdateRun(ctx, run.ID, RunUpdate{Status: st, MarkFinished: true}); err != nil {
			t.Fatalf("finish run: %v", err)
		}
	}
	if _, err := s.CreateRun(ctx, NewRunInput{DatabaseID: byName["billing"], Kind: model.KindDump, Trigger: model.TriggerManual}); err != nil {
		t.Fatalf("create run: %v", err)
	}

	runs, err := s.RecentRunsByInstance(ctx, inst.ID, 3)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	var app []model.RunStatus
	billing := 0
	for _, r := range runs {
		switch r.DatabaseID {
		case byName["app"]:
			app = append(app, r.Status)
		case byName["billing"]:
			billing++
		default:
			t.Fatalf("run of foreign database %s", r.DatabaseID)
		}
	}
	// Срез по perDB и порядок от новых к старым.
	want := []model.RunStatus{model.StatusFailed, model.StatusSucceeded, model.StatusFailed}
	if len(app) != len(want) {
		t.Fatalf("app runs = %v, want %v", app, want)
	}
	for i := range want {
		if app[i] != want[i] {
			t.Fatalf("app runs = %v, want %v", app, want)
		}
	}
	if billing != 1 {
		t.Fatalf("billing runs = %d, want 1", billing)
	}
}
