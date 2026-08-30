package windowsupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const (
	defaultRepository = "screwys/Igloo"
	manifestAssetName = "igloo-windows-update.json"
	maxMetadataBytes  = 1 << 20
)

type githubRelease struct {
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type Available struct {
	Manifest   Manifest
	AppURL     string
	RuntimeURL string
}

type GitHubSource struct {
	Repository      string
	PublicKeyBase64 string
	Client          *http.Client
	APIBaseURL      string
	TargetOS        string
	TargetArch      string
}

func (s GitHubSource) Latest(ctx context.Context, channel, etag string) (Available, string, bool, error) {
	repository := strings.TrimSpace(s.Repository)
	if repository == "" {
		repository = defaultRepository
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	apiBaseURL := strings.TrimRight(strings.TrimSpace(s.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = "https://api.github.com"
	}
	if !strings.HasPrefix(apiBaseURL, "https://") {
		return Available{}, etag, false, errors.New("GitHub API URL is not HTTPS")
	}
	releasesURL := apiBaseURL + "/repos/" + repository + "/releases?per_page=20"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return Available{}, etag, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "Igloo-Windows-Updater")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Available{}, etag, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotModified {
		return Available{}, etag, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Available{}, etag, false, fmt.Errorf("GitHub releases: %s", resp.Status)
	}
	var releases []githubRelease
	if err := decodeLimitedJSON(resp.Body, maxMetadataBytes, &releases); err != nil {
		return Available{}, etag, false, err
	}
	newETag := resp.Header.Get("ETag")
	for _, release := range releases {
		if release.Draft || (release.Prerelease && channel != "latest") {
			continue
		}
		assets := make(map[string]githubAsset, len(release.Assets))
		for _, asset := range release.Assets {
			assets[asset.Name] = asset
		}
		manifestAsset, ok := assets[manifestAssetName]
		if !ok {
			continue
		}
		signatureAsset, ok := assets[manifestAssetName+".sig"]
		if !ok {
			continue
		}
		raw, err := fetchLimited(ctx, client, manifestAsset.BrowserDownloadURL, maxMetadataBytes)
		if err != nil {
			return Available{}, newETag, false, err
		}
		signature, err := fetchLimited(ctx, client, signatureAsset.BrowserDownloadURL, 4096)
		if err != nil {
			return Available{}, newETag, false, err
		}
		manifest, err := ParseSignedManifest(raw, signature, s.PublicKeyBase64)
		if err != nil {
			return Available{}, newETag, false, err
		}
		targetOS, targetArch := s.TargetOS, s.TargetArch
		if targetOS == "" {
			targetOS = runtime.GOOS
		}
		if targetArch == "" {
			targetArch = runtime.GOARCH
		}
		if manifest.OS != targetOS || manifest.Arch != targetArch {
			continue
		}
		available := Available{Manifest: manifest}
		if manifest.App != nil {
			asset, ok := assets[manifest.App.Asset]
			if !ok || asset.Size != manifest.App.Size {
				return Available{}, newETag, false, errors.New("Windows app update asset is missing or has the wrong size")
			}
			available.AppURL = asset.BrowserDownloadURL
		}
		if manifest.Runtime != nil {
			asset, ok := assets[manifest.Runtime.Asset]
			if !ok || asset.Size != manifest.Runtime.Size {
				return Available{}, newETag, false, errors.New("Windows runtime update asset is missing or has the wrong size")
			}
			available.RuntimeURL = asset.BrowserDownloadURL
		}
		return available, newETag, false, nil
	}
	return Available{}, newETag, false, nil
}

func fetchLimited(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, errors.New("Windows update URL is not HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Igloo-Windows-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download update metadata: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("Windows update metadata is too large")
	}
	return data, nil
}

func decodeLimitedJSON(reader io.Reader, limit int64, target any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, limit+1))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode GitHub releases: %w", err)
	}
	return nil
}
