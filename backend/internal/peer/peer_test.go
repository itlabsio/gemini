package peer

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestArtifactSignatureCoversExtensions(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const key, sum, ext = "inst/db/20261021T000000Z.dump", "abc123", "sha256:deadbeef"
	sig := SignArtifact(priv, key, sum, ext)

	if !VerifyArtifact(pub, key, sum, ext, sig) {
		t.Fatal("valid v2 signature rejected")
	}
	// Подмена любого поля манифеста, включая список расширений, ломает подпись.
	for name, args := range map[string][3]string{
		"extensions": {key, sum, "sha256:other"},
		"no sidecar": {key, sum, "none"},
		"checksum":   {key, "abc124", ext},
		"key":        {"inst/db/other.dump", sum, ext},
	} {
		if VerifyArtifact(pub, args[0], args[1], args[2], sig) {
			t.Errorf("%s: tampered manifest accepted", name)
		}
	}
	// v2-подпись не проходит как v1 и наоборот.
	if VerifyArtifactV1(pub, key, sum, sig) {
		t.Error("v2 signature accepted as v1")
	}
	if VerifyArtifact(pub, key, sum, ext, signV1(priv, key, sum)) {
		t.Error("v1 signature accepted as v2")
	}
	if !VerifyArtifactV1(pub, key, sum, signV1(priv, key, sum)) {
		t.Error("valid v1 signature rejected")
	}
}

func signV1(priv ed25519.PrivateKey, key, sum string) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, artifactMessageV1(key, sum)))
}

func TestInstanceMetaSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const key = "pg-main/meta.json"
	body := []byte(`{"instance":"pg-main","project":"billing"}`)
	sig := SignInstanceMeta(priv, key, body)
	if !VerifyInstanceMeta(pub, key, body, sig) {
		t.Fatal("valid meta signature rejected")
	}
	if VerifyInstanceMeta(pub, "pg-other/meta.json", body, sig) {
		t.Error("meta moved to another instance folder accepted")
	}
	if VerifyInstanceMeta(pub, key, []byte(`{"instance":"pg-main","project":"other"}`), sig) {
		t.Error("tampered meta accepted")
	}
	// Подписи дампа и meta.json не взаимозаменяемы.
	if VerifyArtifactV1(pub, key, string(body), sig) || VerifyArtifact(pub, key, string(body), "none", sig) {
		t.Error("meta signature accepted as dump signature")
	}
}
