package download

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/model"
)

//go:embed tiktok_live.py
var tiktokLiveAdapter string

type TikTokLiveInfo struct {
	ChannelID    string        `json:"channel_id"`
	RoomID       string        `json:"room_id"`
	Handle       string        `json:"handle"`
	Title        string        `json:"title"`
	ThumbnailURL string        `json:"thumbnail_url"`
	ViewerCount  int64         `json:"viewer_count"`
	Playback     *PlaybackInfo `json:"playback"`
}

func tiktokLiveCommand(ctx context.Context, mode string, args ...string) *exec.Cmd {
	argv := append([]string{"-u", "-c", tiktokLiveAdapter, mode}, args...)
	return pythonCommand(ctx, argv...)
}

func FetchTikTokLive(ctx context.Context, handle, roomID string) (*TikTokLiveInfo, error) {
	handle = model.NormalizeTikTokHandle(handle)
	result := (CommandRunner{}).RunBuilt(ctx, tiktokLiveCommand(ctx, "resolve", handle, roomID))
	if result.Err != nil {
		return nil, fmt.Errorf("TikTok live: %w: %s", result.Err, strings.TrimSpace(RedactText(string(result.Stderr))))
	}
	return decodeTikTokLiveInfo(result.Stdout)
}

func DownloadTikTokLive(ctx context.Context, handle, expectedVideoID string, opts Opts) (CompletedDownload, error) {
	info, err := FetchTikTokLive(ctx, handle, strings.TrimPrefix(expectedVideoID, "tiktok_live_"))
	if err != nil {
		return CompletedDownload{}, err
	}
	if info == nil {
		return CompletedDownload{}, fmt.Errorf("TikTok live is offline")
	}
	videoID := "tiktok_live_" + info.RoomID
	if expectedVideoID != "" && expectedVideoID != videoID {
		return CompletedDownload{}, fmt.Errorf("TikTok broadcast has changed")
	}
	format := info.Playback.Formats[0]
	for _, candidate := range info.Playback.Formats {
		if candidate.VideoCodec != "none" {
			format = candidate
			break
		}
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return CompletedDownload{}, err
	}
	tmpDir, err := os.MkdirTemp(opts.OutputDir, ".tiktok-live-*")
	if err != nil {
		return CompletedDownload{}, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	filename := sanitizeDownloadID(opts.ID) + ".mp4"
	tmpPath := filepath.Join(tmpDir, filename)
	started := time.Now()
	result := (CommandRunner{}).Run(ctx, "ffmpeg", []string{
		"-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", format.URL, "-c", "copy", "-movflags", "+faststart", tmpPath,
	}, CommandOptions{})
	if result.Err != nil {
		return CompletedDownload{}, fmt.Errorf("TikTok live capture: %w: %s", result.Err, strings.TrimSpace(RedactText(string(result.Stderr))))
	}
	if err := ctx.Err(); err != nil {
		return CompletedDownload{}, err
	}
	outputPath := filepath.Join(opts.OutputDir, filename)
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return CompletedDownload{}, err
	}
	return CompletedDownload{
		MediaPaths: []string{outputPath},
		Metadata: map[string]any{
			"id": videoID, "channel_id": info.ChannelID, "channel": info.Playback.Channel,
			"channel_url": info.Playback.ChannelURL, "title": info.Title, "description": info.Playback.Description,
			"webpage_url": "https://www.tiktok.com/@" + info.Handle + "/live", "live_status": "was_live",
			"duration": int(time.Since(started).Seconds()), "timestamp": float64(started.Unix()),
		},
	}, nil
}

func decodeTikTokLiveInfo(data []byte) (*TikTokLiveInfo, error) {
	var info *TikTokLiveInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("TikTok live information: %w", err)
	}
	if info != nil && info.Playback != nil {
		info.Playback.Metadata = map[string]any{
			"live_status": "is_live",
			"webpage_url": "https://www.tiktok.com/@" + info.Handle + "/live",
		}
	}
	return info, nil
}

func FetchTikTokLives(ctx context.Context, rooms map[string]string, emit func(string, *TikTokLiveInfo, error) error) error {
	normalized := make(map[string]string, len(rooms))
	for handle, roomID := range rooms {
		normalized[model.NormalizeTikTokHandle(handle)] = roomID
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	cmd := tiktokLiveCommand(ctx, "batch")
	cmd.Stdin = bytes.NewReader(data)
	return streamTikTokLiveOutput(ctx, cmd, func(line json.RawMessage) error {
		var observation struct {
			Handle string          `json:"handle"`
			Info   json.RawMessage `json:"info"`
			Error  string          `json:"error"`
		}
		if err := json.Unmarshal(line, &observation); err != nil {
			return err
		}
		if observation.Error != "" {
			return emit(observation.Handle, nil, fmt.Errorf("TikTok live: %s", observation.Error))
		}
		info, err := decodeTikTokLiveInfo(observation.Info)
		return emit(observation.Handle, info, err)
	})
}

func StreamTikTokLiveChat(ctx context.Context, handle string, emit func(json.RawMessage) error) error {
	return streamTikTokLiveOutput(ctx, tiktokLiveCommand(ctx, "chat", model.NormalizeTikTokHandle(handle)), func(line json.RawMessage) error {
		var event struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return err
		}
		if event.Type == "ping" {
			return emit(nil)
		}
		return emit(line)
	})
}

func streamTikTokLiveOutput(ctx context.Context, cmd *exec.Cmd, emit func(json.RawMessage) error) error {
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
		return fmt.Errorf("TikTok live: %w: %s", err, strings.TrimSpace(RedactText(stderr.String())))
	}
	return nil
}
