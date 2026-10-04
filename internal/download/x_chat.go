package download

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/screwys/igloo/internal/toolenv"
)

// Extractor source: github.com/badlogic/twitter-broadcast-chat, commit
// 7a562fd8f8c96a025b2f275f213d267a99ed2c08. Its README declares the MIT license.
//
//go:embed xchat/adapter.ts xchat/import-map.json xchat/upstream/*.ts xchat/upstream/README.md
var xChatFiles embed.FS

// StreamXBroadcastChat reads public broadcast history and live comments through
// the bundled upstream reader. Set live to false to finish after history. A ready
// record marks loaded history, and a nil record is a connection heartbeat.
func StreamXBroadcastChat(ctx context.Context, broadcastID string, live bool, emit func(json.RawMessage) error) error {
	dir, err := os.MkdirTemp("", "igloo-x-chat-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.CopyFS(dir, xChatFiles); err != nil {
		return err
	}
	toolenv.ApplyCommonToolPaths()
	mode := "history"
	if live {
		mode = "live"
	}
	cmd := exec.CommandContext(ctx, "deno", "run", "--quiet", "--no-prompt", "--no-config", "--no-lock", "--allow-net",
		"--import-map="+filepath.Join(dir, "xchat", "import-map.json"), filepath.Join(dir, "xchat", "adapter.ts"), broadcastID, mode)
	configureCommandCancellation(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := append(json.RawMessage(nil), scanner.Bytes()...)
		var event struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			_ = cmd.Cancel()
			_ = cmd.Wait()
			return err
		}
		if event.Type == "ping" {
			line = nil
		}
		if err := emit(line); err != nil {
			_ = cmd.Cancel()
			_ = cmd.Wait()
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Cancel()
		_ = cmd.Wait()
		return err
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("x broadcast chat: %w: %s", err, strings.TrimSpace(RedactText(stderr.String())))
	}
	return nil
}
