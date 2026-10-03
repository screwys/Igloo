package download

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	ytdlp "github.com/lrstanley/go-ytdlp"
)

// DownloadYouTubeChat writes the replay timeline using the same extractor as playback.
func (y *YtDlpWrapper) DownloadYouTubeChat(ctx context.Context, rawURL string, opts Opts, path string) (bool, error) {
	file, err := os.Create(path)
	if err != nil {
		return false, err
	}
	count := 0
	err = y.StreamYouTubeChat(ctx, rawURL, opts, func(record json.RawMessage) error {
		if len(record) == 0 {
			return nil
		}
		if _, err := file.Write(append(record, '\n')); err != nil {
			return err
		}
		count++
		return nil
	})
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil || count == 0 {
		_ = os.Remove(path)
		return false, err
	}
	return true, nil
}

// StreamYouTubeChat reads the JSON lines written by yt-dlp's chat downloader.
// The caller owns the connection; closing it stops extraction and removes files.
func (y *YtDlpWrapper) StreamYouTubeChat(ctx context.Context, rawURL string, opts Opts, emit func(json.RawMessage) error) error {
	var lastErr error
	for _, auth := range opts.cookieAttempts("youtube") {
		emitted := false
		lastErr = y.streamYouTubeChat(ctx, rawURL, opts.withCookieSet(auth), func(record json.RawMessage) error {
			if len(record) > 0 {
				emitted = true
			}
			return emit(record)
		})
		if lastErr == nil || emitted || !shouldTryNextCookieAttempt(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func (y *YtDlpWrapper) streamYouTubeChat(ctx context.Context, rawURL string, opts Opts, emit func(json.RawMessage) error) error {
	dir, err := os.MkdirTemp("", "igloo-chat-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := applyCookieAuth(ytdlp.New().Output(filepath.Join(dir, "chat")).
		NoPlaylist().SkipDownload().IgnoreNoFormatsError().WriteSubs().SubLangs("live_chat").
		NoPart().NoProgress(), opts)
	done := make(chan CommandResult, 1)
	go func() { done <- (CommandRunner{}).RunBuilt(ctx, cmd.BuildCommand(ctx, rawURL)) }()
	// Wait for the process before removing its output, including on disconnect.
	defer func() {
		cancel()
		if done != nil {
			<-done
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var file *os.File
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	var offset int64
	var pending []byte
	count := 0
	read := func() error {
		if file == nil {
			file, err = os.Open(filepath.Join(dir, "chat.live_chat.json"))
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
		}
		buffer := make([]byte, 64*1024)
		for {
			n, readErr := file.ReadAt(buffer, offset)
			offset += int64(n)
			pending = append(pending, buffer[:n]...)
			for {
				end := bytes.IndexByte(pending, '\n')
				if end < 0 {
					break
				}
				line := bytes.TrimSpace(pending[:end])
				if len(line) > 0 {
					if !json.Valid(line) {
						return fmt.Errorf("invalid yt-dlp chat record")
					}
					if err := emit(line); err != nil {
						return err
					}
					count++
				}
				pending = pending[end+1:]
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-done:
			done = nil
			err := read()
			if err == nil && result.Err != nil {
				err = fmt.Errorf("yt-dlp chat: %w: %s", result.Err, RedactText(string(result.Stderr)))
			}
			if err == nil && count == 0 && containsAuthSignal(string(result.Stderr)) {
				err = fmt.Errorf("yt-dlp chat: %s", RedactText(string(result.Stderr)))
			}
			y.recordYtDlpOperationWithCounts(ctx, "youtube.chat", rawURL, start, err, opts, count, 0, 0)
			return err
		case <-ticker.C:
			if err := emit(nil); err != nil {
				return err
			}
			if err := read(); err != nil {
				return err
			}
		}
	}
}
