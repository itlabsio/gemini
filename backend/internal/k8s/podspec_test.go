package k8s

import "testing"

func TestValidStorageSize(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{
		{"20Gi", true},
		{"1Gi", true},
		{"1024Mi", true},
		{"2G", true},
		{"20", false},    // 20 байт — забыли единицу
		{"512Mi", false}, // меньше 1Gi
		{"20GB", false},  // не k8s-величина
		{"", false},
	} {
		if err := ValidStorageSize(tc.in); (err == nil) != tc.ok {
			t.Errorf("ValidStorageSize(%q) = %v, want ok=%v", tc.in, err, tc.ok)
		}
	}
}
