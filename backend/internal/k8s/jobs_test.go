package k8s

import "testing"

func TestPickErrorLines(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "pg_dump connection error",
			raw: "starting dump db=postgres\n" +
				"pg_dump: error: connection to server at \"c-xxx.ro.mdb.yandexcloud.net\" (10.247.27.69), port 6432 failed: " +
				"ERROR:  odyssey: c179d4d3a45c9: route for 'postgres.dr-dumper' is not found\n",
			want: "pg_dump: error: connection to server at \"c-xxx.ro.mdb.yandexcloud.net\" (10.247.27.69), port 6432 failed: " +
				"ERROR:  odyssey: c179d4d3a45c9: route for 'postgres.dr-dumper' is not found",
		},
		{
			name: "real dump failure — picks the pg_dump line, drops log noise",
			raw: "2026/09/06 16:46:53 dump: pg_dump -Fc postgres -> /work/db.dump\n" +
				"pg_dump: error: connection to server at \"c-xxx.ro.mdb.yandexcloud.net\" (10.247.27.69), port 6432 failed: " +
				"ERROR:  odyssey: c179d4d3a45c9: route for 'postgres.dr-dumper' is not found\n" +
				"2026/09/06 16:46:53 dump: pg_dump: exit status 1\n",
			want: "pg_dump: error: connection to server at \"c-xxx.ro.mdb.yandexcloud.net\" (10.247.27.69), port 6432 failed: " +
				"ERROR:  odyssey: c179d4d3a45c9: route for 'postgres.dr-dumper' is not found",
		},
		{
			name: "no markers falls back to last line",
			raw:  "line one\nline two\nline three\n",
			want: "line three",
		},
		{
			name: "keeps only last five hits",
			raw: "pg_dump: error: a\npg_dump: error: b\npg_dump: error: c\n" +
				"pg_dump: error: d\npg_dump: error: e\npg_dump: error: f\n",
			want: "pg_dump: error: b\npg_dump: error: c\npg_dump: error: d\npg_dump: error: e\npg_dump: error: f",
		},
		{
			name: "verbose progress lines are not errors",
			raw: "pg_dump: dumping contents of table \"public.users\"\n" +
				"pg_dump: last built-in OID is 16383\n" +
				"done\n",
			want: "done",
		},
		{
			name: "empty input",
			raw:  "",
			want: "",
		},
		{
			name: "trailing CR stripped",
			raw:  "ok\r\npg_restore: error: could not execute query\r\n",
			want: "pg_restore: error: could not execute query",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickErrorLines(tc.raw); got != tc.want {
				t.Errorf("pickErrorLines()\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestLooksLikeError(t *testing.T) {
	yes := []string{
		"pg_dump: error: connection failed",
		"FATAL: password authentication failed",
		"panic: runtime error",
		"could not connect to server",
		`time=... level=error msg="upload failed"`,
	}
	no := []string{
		"starting dump",
		"pg_dump: dumping contents of table public.users",
		"uploaded 42 MB to s3",
	}
	for _, s := range yes {
		if !looksLikeError(s) {
			t.Errorf("looksLikeError(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeError(s) {
			t.Errorf("looksLikeError(%q) = true, want false", s)
		}
	}
}
