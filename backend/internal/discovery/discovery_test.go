package discovery

import (
	"slices"
	"testing"
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
