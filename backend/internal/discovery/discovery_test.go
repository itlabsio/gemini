package discovery

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestFilter(t *testing.T) {
	cases := []struct {
		name     string
		in       []string
		excluded []string
		want     []string
	}{
		{
			name: "postgres dropped even without instance list",
			in:   []string{"postgres", "app", "billing"},
			want: []string{"app", "billing"},
		},
		{
			name:     "instance exclusions on top of builtin",
			in:       []string{"postgres", "app", "audit", "scratch"},
			excluded: []string{"audit", " scratch ", ""},
			want:     []string{"app"},
		},
		{
			name: "recreate leftovers dropped, look-alikes kept",
			in:   []string{"drivers_heatmap", "drivers_heatmap_old_20261012t181143z", "app_old", "app_old_2026"},
			want: []string{"drivers_heatmap", "app_old", "app_old_2026"},
		},
		{
			name: "nothing to drop",
			in:   []string{"app", "billing"},
			want: []string{"app", "billing"},
		},
		{
			name: "empty input",
			in:   nil,
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Filter(tc.in, tc.excluded)
			if !slices.Equal(got, tc.want) {
				t.Errorf("Filter(%v, %v) = %v, want %v", tc.in, tc.excluded, got, tc.want)
			}
		})
	}
}

func TestConnectHint(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string // подстрока; "" — подсказки нет
	}{
		{"server missing db", &pgconn.PgError{Code: "3D000", Message: `database "x" does not exist`}, "discovery_db"},
		{"pgbouncer missing db", &pgconn.PgError{Code: "08P01", Message: "no such database: x"}, "discovery_db"},
		{"pgbouncer login retry", &pgconn.PgError{Code: "08P01",
			Message: "server login has been failing, cached error: connect failed (server_login_retry)"}, "PgBouncer"},
		{"auth failed", &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}, ""},
		{"not a pg error", errors.New("dial tcp: i/o timeout"), ""},
	}
	for _, c := range cases {
		got := connectHint(c.err, "x")
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: connectHint = %q, want containing %q", c.name, got, c.want)
		}
	}
}
