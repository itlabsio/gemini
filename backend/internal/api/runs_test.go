package api

import (
	"testing"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

func TestRunCursorRoundTrip(t *testing.T) {
	run := model.BackupRun{
		ID:        "0b6f1c1e-5a3d-4f7e-9c2a-1d2e3f4a5b6c",
		CreatedAt: time.Date(2026, 10, 30, 12, 0, 0, 123456000, time.UTC),
	}
	c, ok := parseRunCursor(runCursor(run))
	if !ok || c.ID != run.ID || !c.CreatedAt.Equal(run.CreatedAt) {
		t.Fatalf("round trip: %+v %v", c, ok)
	}
	for _, bad := range []string{"", "123", "abc_" + run.ID, "0_" + run.ID, "123_not-a-uuid", "123_" + run.ID + "'"} {
		if _, ok := parseRunCursor(bad); ok {
			t.Errorf("cursor %q must be rejected", bad)
		}
	}
}
