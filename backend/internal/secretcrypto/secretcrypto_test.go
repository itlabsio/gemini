package secretcrypto

import (
	"bytes"
	"strings"
	"testing"
)

const (
	key1 = "0123456789abcdef0123456789abcdef"
	key2 = "fedcba9876543210fedcba9876543210"
)

func mustBox(t *testing.T, key string) *Box {
	t.Helper()
	b, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNewKeyLength(t *testing.T) {
	for _, k := range []string{"", "short", key1 + "x"} {
		if _, err := New(k); err == nil {
			t.Errorf("New(%d bytes) accepted, want error", len(k))
		}
	}
}

func TestRoundTrip(t *testing.T) {
	b := mustBox(t, key1)
	for _, pt := range []string{"p@ss", "пароль с юникодом", strings.Repeat("x", 4096)} {
		ct, err := b.Encrypt(pt)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(ct, []byte(pt)) {
			t.Fatalf("ciphertext contains plaintext %q", pt)
		}
		got, err := b.Decrypt(ct)
		if err != nil || got != pt {
			t.Fatalf("Decrypt = %q, %v; want %q", got, err, pt)
		}
	}
}

func TestEmpty(t *testing.T) {
	b := mustBox(t, key1)
	ct, err := b.Encrypt("")
	if err != nil || ct != nil {
		t.Fatalf("Encrypt(\"\") = %v, %v; want nil, nil", ct, err)
	}
	if pt, err := b.Decrypt(nil); err != nil || pt != "" {
		t.Fatalf("Decrypt(nil) = %q, %v", pt, err)
	}
}

// Один и тот же пароль даёт разные шифротексты (случайный nonce): по БД нельзя
// понять, что у двух инстансов одинаковые пароли.
func TestNonceIsRandom(t *testing.T) {
	b := mustBox(t, key1)
	a, _ := b.Encrypt("same")
	c, _ := b.Encrypt("same")
	if bytes.Equal(a, c) {
		t.Fatal("two encryptions of the same plaintext are identical")
	}
}

func TestWrongKey(t *testing.T) {
	ct, _ := mustBox(t, key1).Encrypt("secret")
	if _, err := mustBox(t, key2).Decrypt(ct); err == nil {
		t.Fatal("decrypt with another key succeeded")
	}
}

func TestTampered(t *testing.T) {
	b := mustBox(t, key1)
	ct, _ := b.Encrypt("secret")
	for i := range ct {
		bad := bytes.Clone(ct)
		bad[i] ^= 0x01
		if _, err := b.Decrypt(bad); err == nil {
			t.Fatalf("flipped byte %d: decrypt succeeded", i)
		}
	}
	if _, err := b.Decrypt(ct[:5]); err == nil {
		t.Fatal("truncated ciphertext accepted")
	}
}
