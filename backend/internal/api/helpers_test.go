package api

import "testing"

func TestKeyUnderPrefix(t *testing.T) {
	cases := []struct {
		key, prefix string
		want        bool
	}{
		{"inst/db/x.dump", "inst/db", true},
		{"inst/db/x.dump", "inst/db/", true},
		{"inst/db/sub/x.dump", "inst/db", false}, // дамп вложенной базы "db/sub"
		{"inst/db/../other/x.dump", "inst/db", false},
		{"inst/dbx/x.dump", "inst/db", false},
		{"inst/db/x.dump", "", false},
		{"", "inst/db", false},
	}
	for _, c := range cases {
		if got := keyUnderPrefix(c.key, c.prefix); got != c.want {
			t.Errorf("keyUnderPrefix(%q, %q) = %v, want %v", c.key, c.prefix, got, c.want)
		}
	}
}

func TestValidCron(t *testing.T) {
	ok := []string{"0 0 * * *", "40 0 * * *", "*/15 * * * *", "0 3 * * 1-5", " 0 23 * * * ", "@daily"}
	bad := []string{"0 40 * * *", "60 0 * * *", "0 0 32 * *", "0 0 * 13 *", "0 0 * *", "0 0 * * * *", "CRON_TZ=UTC 0 0 * * *", "TZ=UTC 0 0 * * *", "foo"}
	for _, e := range ok {
		if err := validCron(e); err != nil {
			t.Errorf("validCron(%q) = %v, want nil", e, err)
		}
	}
	for _, e := range bad {
		if err := validCron(e); err == nil {
			t.Errorf("validCron(%q) = nil, want error", e)
		}
	}
}
