package api

import (
	"net/http/httptest"
	"testing"
)

func TestManagedBy(t *testing.T) {
	cases := map[string]string{"terraform": "terraform", "": "", "pulumi": "", "Terraform": ""}
	for header, want := range cases {
		r := httptest.NewRequest("POST", "/", nil)
		if header != "" {
			r.Header.Set(managedByHeader, header)
		}
		if got := managedBy(r); got != want {
			t.Errorf("managedBy(%q) = %q, want %q", header, got, want)
		}
	}
}
