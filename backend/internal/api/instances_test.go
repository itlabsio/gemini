package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
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
