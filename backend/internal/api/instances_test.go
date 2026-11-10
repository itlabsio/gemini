package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

func selfSignedPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestValidateRootCert(t *testing.T) {
	one := selfSignedPEM(t)

	if err := validateRootCert(one); err != nil {
		t.Fatalf("single cert rejected: %v", err)
	}
	if err := validateRootCert(one + selfSignedPEM(t)); err != nil {
		t.Fatalf("bundle rejected: %v", err)
	}
	if err := validateRootCert("not a pem at all"); err == nil {
		t.Fatal("garbage accepted")
	}
	if err := validateRootCert(""); err == nil {
		t.Fatal("empty accepted")
	}

	// Не-сертификатный PEM-блок.
	keyBlock := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("x")}))
	if err := validateRootCert(keyBlock); err == nil || !strings.Contains(err.Error(), "CERTIFICATE") {
		t.Fatalf("non-certificate block: got %v", err)
	}
}

func TestValidateStripsRootCertForNonVerifyModes(t *testing.T) {
	pemText := selfSignedPEM(t)
	req := instanceRequest{
		Name: "x", Host: "h", SSLMode: "require", SSLRootCert: pemText,
		AuthType: "plain", PlainUsername: "u",
	}
	in, msg := req.validate("source")
	if msg != "" {
		t.Fatalf("unexpected validation error: %s", msg)
	}
	if in.SSLRootCert != "" {
		t.Fatal("ssl_root_cert must be dropped when ssl_mode is not verify-*")
	}

	req.SSLMode = "verify-full"
	in, msg = req.validate("source")
	if msg != "" {
		t.Fatalf("verify-full rejected: %s", msg)
	}
	if in.SSLRootCert == "" {
		t.Fatal("ssl_root_cert must be kept for verify-full")
	}
}

// Где лежат креды инстанса (plain-пользователь, координаты Vault), видит только
// superuser; даже admin проекта получает лишь auth_type.
func TestRedactCredCoords(t *testing.T) {
	full := func() model.Instance {
		return model.Instance{
			Project: "p", AuthType: model.AuthVault, PlainUsername: "u",
			VaultPath: "kv/data/x", VaultRole: "r", VaultUsernameKey: "uk", VaultPasswordKey: "pk",
		}
	}
	cases := map[string]struct {
		id   auth.Identity
		keep bool
	}{
		"superuser": {auth.Identity{Superuser: true}, true},
		"admin":     {auth.Identity{Projects: map[string]auth.Role{"p": auth.RoleAdmin}}, false},
		"viewer":    {auth.Identity{Projects: map[string]auth.Role{"p": auth.RoleViewer}}, false},
	}
	for name, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r = r.WithContext(auth.WithIdentity(r.Context(), c.id))
		inst := full()
		redactCredCoords(r, &inst)
		if c.keep {
			if !reflect.DeepEqual(inst, full()) {
				t.Errorf("%s: координаты не должны прятаться: %+v", name, inst)
			}
			continue
		}
		if inst.PlainUsername != "" || inst.VaultPath != "" || inst.VaultRole != "" ||
			inst.VaultUsernameKey != "" || inst.VaultPasswordKey != "" {
			t.Errorf("%s: координаты кред утекли: %+v", name, inst)
		}
		if inst.AuthType != model.AuthVault {
			t.Errorf("%s: auth_type должен остаться виден", name)
		}
	}
}

func TestChangedInstanceFields(t *testing.T) {
	cur := &model.Instance{Name: "a", Host: "h", Port: 5432, Project: "p", ExcludedDatabases: []string{"x"}}
	same := store.InstanceInput{Name: "a", Host: "h", Port: 5432, ExcludedDatabases: []string{"x"}}
	if got := changedInstanceFields(cur, same); len(got) != 0 {
		t.Errorf("unchanged input: got %v", got)
	}
	in := same
	in.Host, in.PlainPassword, in.VaultPath = "h2", "secret", "kv/pg"
	other := "q"
	in.Project = &other
	want := []string{"host", "plain_password", "vault_path", "project"}
	if got := changedInstanceFields(cur, in); !slices.Equal(got, want) {
		t.Errorf("changedInstanceFields = %v, want %v", got, want)
	}
	if s := fmt.Sprint(changedInstanceFields(cur, in)); strings.Contains(s, "secret") {
		t.Errorf("password value leaked into audit: %s", s)
	}
}
