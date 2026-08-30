package windowsupdate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestParseSignedManifest(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema":1,"os":"windows","arch":"amd64","minimum_app_version":"3.4.0","app":{"version":"3.4.0","asset":"igloo-app-windows-amd64.zip","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":123}}`)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, raw))

	manifest, err := ParseSignedManifest(raw, []byte(signature), base64.StdEncoding.EncodeToString(publicKey))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.App == nil || manifest.App.Version != "3.4.0" {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestParseSignedManifestRejectsTampering(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema":1}`)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, raw))
	if _, err := ParseSignedManifest(append(raw, ' '), []byte(signature), base64.StdEncoding.EncodeToString(publicKey)); err == nil {
		t.Fatal("tampered manifest was accepted")
	}
}

func TestNewerVersion(t *testing.T) {
	tests := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"3.4.1", "3.4.0", true},
		{"3.4.0", "3.4.0", false},
		{"3.3.9", "3.4.0", false},
		{"18", "17", true},
		{"3.4.0", "dev", true},
	}
	for _, test := range tests {
		if got := NewerVersion(test.candidate, test.current); got != test.want {
			t.Fatalf("NewerVersion(%q, %q) = %v, want %v", test.candidate, test.current, got, test.want)
		}
	}
}
