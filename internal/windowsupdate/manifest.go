package windowsupdate

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const ManifestSchema = 1

var ErrUnsignedBuild = errors.New("Windows update signing key is not configured")

type Manifest struct {
	Schema            int      `json:"schema"`
	OS                string   `json:"os"`
	Arch              string   `json:"arch"`
	MinimumAppVersion string   `json:"minimum_app_version"`
	App               *Payload `json:"app,omitempty"`
	Runtime           *Payload `json:"runtime,omitempty"`
}

type Payload struct {
	Version string `json:"version"`
	Asset   string `json:"asset"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

func ParseSignedManifest(raw, signature []byte, publicKeyBase64 string) (Manifest, error) {
	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyBase64))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return Manifest{}, ErrUnsignedBuild
	}
	sigText := strings.TrimSpace(string(signature))
	sig, err := base64.StdEncoding.DecodeString(sigText)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(publicKey), raw, sig) {
		return Manifest{}, errors.New("Windows update manifest signature is invalid")
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse Windows update manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if m.Schema != ManifestSchema {
		return fmt.Errorf("unsupported Windows update manifest schema %d", m.Schema)
	}
	if m.OS != "windows" || (m.Arch != "amd64" && m.Arch != "arm64") {
		return fmt.Errorf("unsupported Windows update target %s/%s", m.OS, m.Arch)
	}
	if m.App == nil && m.Runtime == nil {
		return errors.New("Windows update manifest has no payloads")
	}
	if strings.TrimSpace(m.MinimumAppVersion) == "" {
		return errors.New("Windows update manifest minimum app version is empty")
	}
	for name, payload := range map[string]*Payload{"app": m.App, "runtime": m.Runtime} {
		if payload == nil {
			continue
		}
		if strings.TrimSpace(payload.Version) == "" {
			return fmt.Errorf("%s payload version is empty", name)
		}
		if filepath.Base(payload.Asset) != payload.Asset || !strings.HasSuffix(strings.ToLower(payload.Asset), ".zip") {
			return fmt.Errorf("%s payload asset is invalid", name)
		}
		if payload.Size <= 0 {
			return fmt.Errorf("%s payload size is invalid", name)
		}
		digest, err := hex.DecodeString(payload.SHA256)
		if err != nil || len(digest) != 32 {
			return fmt.Errorf("%s payload SHA-256 is invalid", name)
		}
	}
	return nil
}

func NewerVersion(candidate, current string) bool {
	candidate = normalizeVersion(candidate)
	current = normalizeVersion(current)
	if candidate == "" || candidate == "dev" {
		return false
	}
	if current == "" || current == "dev" {
		return true
	}
	candidateParts, candidateOK := numericVersion(candidate)
	currentParts, currentOK := numericVersion(current)
	if candidateOK && currentOK {
		for i := 0; i < len(candidateParts) || i < len(currentParts); i++ {
			var left, right int
			if i < len(candidateParts) {
				left = candidateParts[i]
			}
			if i < len(currentParts) {
				right = currentParts[i]
			}
			if left != right {
				return left > right
			}
		}
		return false
	}
	return candidate > current
}

func normalizeVersion(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "v")
}

func numericVersion(value string) ([]int, bool) {
	parts := strings.Split(value, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		var value int
		for _, r := range part {
			if r < '0' || r > '9' {
				return nil, false
			}
			value = value*10 + int(r-'0')
		}
		out = append(out, value)
	}
	return out, true
}
