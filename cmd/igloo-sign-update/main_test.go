package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDecodePrivateKeyAcceptsSeed(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodePrivateKey(base64.StdEncoding.EncodeToString(privateKey.Seed()))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(privateKey) {
		t.Fatal("decoded private key differs")
	}
}

func TestGenerateKeyPairWritesMatchingRawKeys(t *testing.T) {
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "private.b64")
	publicPath := filepath.Join(dir, "public.b64")
	if err := generateKeyPair(privatePath, publicPath); err != nil {
		t.Fatal(err)
	}
	privateRaw, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	publicRaw, err := os.ReadFile(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := decodePrivateKey(string(privateRaw))
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := base64.StdEncoding.DecodeString(string(publicRaw[:len(publicRaw)-1]))
	if err != nil {
		t.Fatal(err)
	}
	if !privateKey.Public().(ed25519.PublicKey).Equal(ed25519.PublicKey(publicKey)) {
		t.Fatal("generated public key does not match private seed")
	}
	info, err := os.Stat(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode = %v", info.Mode().Perm())
	}
}

func TestGenerateKeyPairDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "private.b64")
	publicPath := filepath.Join(dir, "public.b64")
	if err := os.WriteFile(privatePath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := generateKeyPair(privatePath, publicPath); err == nil {
		t.Fatal("existing private key was overwritten")
	}
}
