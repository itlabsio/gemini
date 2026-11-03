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
