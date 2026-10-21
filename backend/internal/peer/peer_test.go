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
