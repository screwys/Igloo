package download

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lrstanley/go-ytdlp"
	"github.com/screwys/igloo/internal/model"
)

// Upstream reader: cjmaxik/twspace-dl, commit 0d7f10daddaacfc9287d2d7285dc65a56914f240.
// The bundled upstream files are unchanged and retain their license.
//
//go:embed xpresence/adapter.py xpresence/upstream/*
var xPresenceFiles embed.FS

type XSpacePresenceResult struct {
	Spaces         []model.XSpacePresence `json:"spaces"`
	RefreshSeconds int                    `json:"refresh_seconds"`
	CookieIndex    int                    `json:"cookie_index"`
}

type XSpacePresenceError struct {
	Message   string `json:"error"`
	Status    int    `json:"status"`
	RetryAtMs int64  `json:"retry_at_ms"`
	Detail    string `json:"detail"`
}

func (err *XSpacePresenceError) Error() string {
	if IsTransportFailure(errors.New(err.Detail), nil) {
		return "X live discovery: network is unreachable"
	}
	if err.Status == 401 || err.Status == 403 {
		return "Login required: " + err.Message
	}
	return err.Message
}

func FetchXSpacePresence(ctx context.Context, userIDs []string, cookies []CookieSet) (*XSpacePresenceResult, error) {
	dir, err := os.MkdirTemp("", "igloo-x-presence-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.CopyFS(dir, xPresenceFiles); err != nil {
		return nil, err
	}
	files := make([]string, len(cookies))
	for index, cookie := range cookies {
		files[index] = cookie.File
		if cookie.File != "" || cookie.Browser == "" {
			continue
		}
		batch := filepath.Join(dir, "empty-urls.txt")
		if err := os.WriteFile(batch, nil, 0o600); err != nil {
			return nil, err
		}
		path := filepath.Join(dir, fmt.Sprintf("cookies-%d.txt", index))
		command := ytdlp.New().IgnoreConfig().CookiesFromBrowser(cookie.Browser).
			Cookies(path).SkipDownload().BatchFile(batch)
		result := (CommandRunner{}).RunBuilt(ctx, command.BuildCommand(ctx))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// An empty batch exits with 2 after yt-dlp saves the browser cookies.
		// Only accept that result when the absent destination became a cookie jar.
		if result.ExitCode == 0 || result.ExitCode == 2 {
			file, err := os.Open(path)
			if err == nil {
				header, readErr := bufio.NewReader(file).ReadString('\n')
				_ = file.Close()
				if readErr == nil && strings.TrimSpace(header) == "# Netscape HTTP Cookie File" {
					files[index] = path
				}
			}
		}
	}
	data, err := json.Marshal(map[string]any{"user_ids": userIDs, "cookies": files})
	if err != nil {
		return nil, err
	}
	cmd := pythonCommand(ctx, "-u", filepath.Join(dir, "xpresence", "adapter.py"))
	cmd.Stdin = bytes.NewReader(data)
	result := (CommandRunner{}).RunBuilt(ctx, cmd)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if result.Err != nil {
		var failure XSpacePresenceError
		if json.Unmarshal(result.Stdout, &failure) == nil && failure.Message != "" {
			return nil, &failure
		}
		return nil, fmt.Errorf("x live discovery: %w: %s", result.Err, RedactText(string(result.Stderr)))
	}
	var presence XSpacePresenceResult
	if err := json.Unmarshal(result.Stdout, &presence); err != nil {
		return nil, fmt.Errorf("x live discovery response: %w", err)
	}
	return &presence, nil
}
