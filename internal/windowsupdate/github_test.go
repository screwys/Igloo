package windowsupdate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubSourceReadsSignedReleaseAssets(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Schema:            ManifestSchema,
		OS:                "windows",
		Arch:              "amd64",
		MinimumAppVersion: "3.4.0",
		App: &Payload{
			Version: "3.4.0",
			Asset:   "igloo-app-windows-amd64.zip",
			SHA256:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Size:    123,
		},
	}
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, rawManifest))

	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/sample/Igloo/releases":
			w.Header().Set("ETag", `"sample-etag"`)
			_, _ = fmt.Fprintf(w, `[{"assets":[
                  {"name":"%s","browser_download_url":"%s/manifest","size":%d},
                  {"name":"%s.sig","browser_download_url":"%s/signature","size":%d},
                  {"name":"%s","browser_download_url":"%s/app","size":123}
                ]}]`, manifestAssetName, server.URL, len(rawManifest), manifestAssetName, server.URL, len(signature), manifest.App.Asset, server.URL)
		case "/manifest":
			_, _ = w.Write(rawManifest)
		case "/signature":
			_, _ = w.Write([]byte(signature))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := GitHubSource{
		Repository:      "sample/Igloo",
		PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		Client:          server.Client(),
		APIBaseURL:      server.URL,
		TargetOS:        "windows",
		TargetArch:      "amd64",
	}
	available, etag, unchanged, err := source.Latest(t.Context(), "stable", "")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged || etag != `"sample-etag"` || available.Manifest.App == nil || available.AppURL != server.URL+"/app" {
		t.Fatalf("available=%+v etag=%q unchanged=%v", available, etag, unchanged)
	}
}

func TestGitHubSourceReadsNightlyAndRuntimeFeedsByTag(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Schema:            ManifestSchema,
		OS:                "windows",
		Arch:              "amd64",
		MinimumAppVersion: "3.5.0",
		App:               &Payload{Version: "3.5.0.20", Asset: "igloo-app-windows-amd64.zip", SHA256: strings.Repeat("a", 64), Size: 10},
		Runtime:           &Payload{Version: "2026.09.02.1", Asset: "igloo-runtime-windows-amd64.zip", SHA256: strings.Repeat("b", 64), Size: 20},
	}
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, rawManifest))

	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/sample/Igloo/releases/tags/windows-nightly", "/repos/sample/Igloo/releases/tags/windows-runtime":
			requestedManifest := manifestAssetName
			if strings.HasSuffix(r.URL.Path, "/windows-runtime") {
				requestedManifest = runtimeManifestAssetName
			}
			_, _ = fmt.Fprintf(w, `{"prerelease":true,"assets":[
                  {"name":"%s","browser_download_url":"%s/manifest","size":%d},
                  {"name":"%s.sig","browser_download_url":"%s/signature","size":%d},
                  {"name":"%s","browser_download_url":"%s/app","size":10},
                  {"name":"%s","browser_download_url":"%s/runtime","size":20}
                ]}`, requestedManifest, server.URL, len(rawManifest), requestedManifest, server.URL, len(signature), manifest.App.Asset, server.URL, manifest.Runtime.Asset, server.URL)
		case "/manifest":
			_, _ = w.Write(rawManifest)
		case "/signature":
			_, _ = w.Write([]byte(signature))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := GitHubSource{
		Repository:      "sample/Igloo",
		PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
		Client:          server.Client(),
		APIBaseURL:      server.URL,
		TargetOS:        "windows",
		TargetArch:      "amd64",
	}
	nightly, _, _, err := source.Latest(t.Context(), "nightly", "")
	if err != nil || nightly.Manifest.App == nil || nightly.Manifest.Runtime != nil {
		t.Fatalf("nightly=%+v err=%v", nightly, err)
	}
	runtimeAvailable, _, _, err := source.Latest(t.Context(), "runtime", "")
	if err != nil || runtimeAvailable.Manifest.Runtime == nil || runtimeAvailable.Manifest.App != nil {
		t.Fatalf("runtime=%+v err=%v", runtimeAvailable, err)
	}
}
