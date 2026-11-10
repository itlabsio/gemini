package jobrunner

import (
	"slices"
	"testing"
)

func TestRollbackSQL(t *testing.T) {
	old := `app_old_20260101t000000z`
	cases := []struct {
		exists, created bool
		want            []string
	}{
		{true, true, []string{`DROP DATABASE "app";`, `ALTER DATABASE "app_old_20260101t000000z" RENAME TO "app";`}},
		{true, false, []string{`ALTER DATABASE "app_old_20260101t000000z" RENAME TO "app";`}},
		{false, true, []string{`DROP DATABASE "app";`}},
		{false, false, nil},
	}
	for _, c := range cases {
		if got := rollbackSQL("app", old, c.exists, c.created); !slices.Equal(got, c.want) {
			t.Errorf("rollbackSQL(exists=%v, created=%v) = %q, want %q", c.exists, c.created, got, c.want)
		}
	}
}
