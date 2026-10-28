package auth

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseGroups(t *testing.T) {
	got := parseGroups([]string{
		"/p1/admin",
		"/p2/developer",
		"/p2/readonly",    // ниже уже имеющейся — не понижает
		"/p3/team/viewer", // вложенность: роль среди остальных сегментов
		"/p4",             // без роли
		"/p5/unknown",     // нераспознанная роль
		"p6/admin",        // без ведущего "/" (fullPath выключен)
		"/",
		"",
	})
	want := map[string]Role{"p1": RoleAdmin, "p2": RoleOperator, "p3": RoleViewer}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseGroups = %v, want %v", got, want)
	}
}

func TestIdentityCan(t *testing.T) {
	user := Identity{Projects: map[string]Role{"a": RoleOperator}}
	cases := []struct {
		id      Identity
		project string
		min     Role
		want    bool
	}{
		{user, "a", RoleViewer, true},
		{user, "a", RoleOperator, true},
		{user, "a", RoleAdmin, false},
		{user, "b", RoleViewer, false},
		{user, "", RoleViewer, false}, // инстанс без проекта
		{Identity{Superuser: true}, "", RoleAdmin, true},
		{Identity{Superuser: true}, "zzz", RoleAdmin, true},
	}
	for i, c := range cases {
		if got := c.id.Can(c.project, c.min); got != c.want {
			t.Errorf("case %d: Can(%q,%s) = %v, want %v", i, c.project, c.min, got, c.want)
		}
	}
}

func TestRequireSuperuserAndAnyProject(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	run := func(h http.HandlerFunc, id Identity) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(WithIdentity(req.Context(), id))
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec.Code
	}
	admin := Identity{Projects: map[string]Role{"a": RoleAdmin}}
	viewer := Identity{Projects: map[string]Role{"a": RoleViewer}}
	su := Identity{Superuser: true}

	if run(RequireSuperuser(ok), admin) != http.StatusForbidden {
		t.Error("project admin must not pass RequireSuperuser")
	}
	if run(RequireSuperuser(ok), su) != http.StatusOK {
		t.Error("superuser must pass RequireSuperuser")
	}
	if run(RequireAnyProject(RoleAdmin, ok), viewer) != http.StatusForbidden {
		t.Error("viewer must not pass RequireAnyProject(admin)")
	}
	if run(RequireAnyProject(RoleViewer, ok), viewer) != http.StatusOK {
		t.Error("viewer must pass RequireAnyProject(viewer)")
	}
}
