package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	generate := flag.Bool("generate", false, "generate a new Ed25519 update-signing key pair")
	privateOutput := flag.String("private-output", "", "private seed output file for --generate")
	publicOutput := flag.String("public-output", "", "public key output file for --generate")
	input := flag.String("input", "", "manifest file to sign")
	output := flag.String("output", "", "signature output file")
	publicKey := flag.String("public-key", "", "expected base64 Ed25519 public key")
	flag.Parse()
	if *generate {
		if err := generateKeyPair(*privateOutput, *publicOutput); err != nil {
			fatal(err.Error())
		}
		return
	}
	if *input == "" || *output == "" {
		fatal("--input and --output are required")
	}
	privateKey, err := decodePrivateKey(os.Getenv("WINDOWS_UPDATE_SIGNING_KEY_BASE64"))
	if err != nil {
		fatal(err.Error())
	}
	derivedPublic := privateKey.Public().(ed25519.PublicKey)
	if strings.TrimSpace(*publicKey) != base64.StdEncoding.EncodeToString(derivedPublic) {
		fatal("configured Windows update public key does not match the signing key")
	}
	data, err := os.ReadFile(*input)
	if err != nil {
		fatal(err.Error())
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, data)) + "\n"
	if err := os.WriteFile(*output, []byte(signature), 0o600); err != nil {
		fatal(err.Error())
	}
}

func generateKeyPair(privateOutput, publicOutput string) error {
	if privateOutput == "" || publicOutput == "" {
		return fmt.Errorf("--private-output and --public-output are required with --generate")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	privateData := []byte(base64.StdEncoding.EncodeToString(privateKey.Seed()) + "\n")
	publicData := []byte(base64.StdEncoding.EncodeToString(publicKey) + "\n")
	if err := writeExclusive(privateOutput, privateData, 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	if err := writeExclusive(publicOutput, publicData, 0o644); err != nil {
		_ = os.Remove(privateOutput)
		return fmt.Errorf("write public key: %w", err)
	}
	return nil
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func decodePrivateKey(encoded string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, err
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf("Windows update signing key must contain a 32-byte seed or 64-byte private key")
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "igloo-sign-update:", message)
	os.Exit(1)
}
