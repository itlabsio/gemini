package api

import (
	"testing"

	"github.com/itlabsio/gemini/backend/internal/operator"
)

func TestOfflineToCandidatesRBAC(t *testing.T) {
	found := []operator.OfflineCandidate{
		{Prefix: "pg/app", DBName: "app", Project: "billing", ProjectKnown: true},
		{Prefix: "pg/crm", DBName: "crm", Project: "sales", ProjectKnown: true},
		{Prefix: "old/x", DBName: "x"},                                 // без meta.json
		{Prefix: "np/y", DBName: "y", Project: "", ProjectKnown: true}, // инстанс без проекта
	}
	ids := func(cs []candidate) (out []string) {
		for _, c := range cs {
			out = append(out, c.ExternalID)
		}
		return out
	}
	// Админ проекта видит только базы с проверенным проектом своего проекта.
	if got := ids(offlineToCandidates(found, "billing", false)); len(got) != 1 || got[0] != "offline:pg/app" {
		t.Fatalf("admin: got %v", got)
	}
	// Superuser — ещё и базы с неизвестным проектом, помеченные как непроверенные.
	got := offlineToCandidates(found, "billing", true)
	if len(got) != 3 {
		t.Fatalf("superuser: got %v", ids(got))
	}
	for _, c := range got {
		if c.Project != "billing" || c.ProjectUnverified != (c.ExternalID != "offline:pg/app") {
			t.Errorf("superuser: unexpected %+v", c)
		}
	}
}
