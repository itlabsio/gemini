package jobrunner

import (
	"slices"
	"testing"
)

func TestPGEnvAppName(t *testing.T) {
	const want = "PGAPPNAME=gemini-restore-run1"
	if env := (Env{appName: "gemini-restore-run1"}).pgEnv("db"); !slices.Contains(env, want) {
		t.Errorf("env has no %s", want)
	}
	if env := (Env{appName: "gemini-restore-run1"}).pgEnvAs("db", "u", "p"); !slices.Contains(env, want) {
		t.Errorf("pgEnvAs env has no %s", want)
	}
	t.Setenv("PGAPPNAME", "")
	for _, kv := range (Env{}).pgEnv("db") {
		if kv != "PGAPPNAME=" && len(kv) > 10 && kv[:10] == "PGAPPNAME=" {
			t.Errorf("unexpected %s", kv)
		}
	}
}
