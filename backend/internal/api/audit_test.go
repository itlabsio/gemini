package api

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	for _, c := range []struct{ realIP, remote, want string }{
		{"203.0.113.7", "10.0.0.5:51234", "203.0.113.7"}, // от ingress / frontend
		{"", "10.0.0.5:51234", "10.0.0.5"},               // без заголовка — адрес соединения
		{"not-an-ip", "10.0.0.5:51234", "10.0.0.5"},      // мусор в заголовке не пишем
		{"2001:db8::1", "[2001:db8::2]:443", "2001:db8::1"},
	} {
		r := httptest.NewRequest("GET", "/api/me", nil)
		r.RemoteAddr = c.remote
		if c.realIP != "" {
			r.Header.Set("X-Real-IP", c.realIP)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("X-Real-IP=%q remote=%q: got %q, want %q", c.realIP, c.remote, got, c.want)
		}
	}
}
