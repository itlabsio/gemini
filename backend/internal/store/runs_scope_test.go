package store

import (
	"testing"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

// Журнал прогонов для не-superuser видит только базы инстансов своих проектов.
func TestRunsProjectScope(t *testing.T) {
	s, ctx := testStore(t)

	runIn := map[string]string{} // проект → id restore-прогона
	for _, p := range []string{"scope-a", "scope-b"} {
		if _, err := s.CreateProject(ctx, p, "tester", ""); err != nil {
			t.Fatalf("project %s: %v", p, err)
		}
		proj := p
		inst, err := s.CreateInstance(ctx, InstanceInput{
			Role: model.InstanceTarget, Name: "runscope-" + p, Host: "db.example", Port: 5432,
			SSLMode: "require", AuthType: model.AuthPlain, PlainUsername: "u", PlainPassword: "p",
			Project: &proj,
		}, "tester")
		if err != nil {
			t.Fatalf("instance %s: %v", p, err)
		}
		t.Cleanup(func() {
			_ = s.DeleteInstance(ctx, inst.ID)
			_, _ = s.pool.Exec(ctx, `DELETE FROM projects WHERE name = $1`, proj)
		})
		dbs, err := s.SyncDiscovered(ctx, inst.ID, []string{"app"}, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		run, err := s.CreateRun(ctx, NewRunInput{DatabaseID: dbs[0].ID, Kind: model.KindRestore, Trigger: model.TriggerWebhook})
		if err != nil {
			t.Fatalf("run %s: %v", p, err)
		}
		runIn[p] = run.ID
	}

	ids := func(runs []model.BackupRun, err error) map[string]bool {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, r := range runs {
			out[r.ID] = true
		}
		return out
	}

	got := ids(s.ListRuns(ctx, RunFilter{RestrictProjects: true, Projects: []string{"scope-a"}, Limit: 500}))
	if !got[runIn["scope-a"]] || got[runIn["scope-b"]] {
		t.Errorf("ListRuns(scope-a): own=%v foreign=%v", got[runIn["scope-a"]], got[runIn["scope-b"]])
	}
	got = ids(s.ListRuns(ctx, RunFilter{RestrictProjects: true, Kind: model.KindRestore, Limit: 500}))
	if got[runIn["scope-a"]] || got[runIn["scope-b"]] {
		t.Error("ListRuns without projects must return nothing")
	}
	got = ids(s.ListActiveRunsInProjects(ctx, []string{"scope-a"}, false))
	if !got[runIn["scope-a"]] || got[runIn["scope-b"]] {
		t.Errorf("ListActiveRunsInProjects(scope-a): own=%v foreign=%v", got[runIn["scope-a"]], got[runIn["scope-b"]])
	}
}
