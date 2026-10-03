package web

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"time"
)

var youtubeChatVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func (s *Server) handleYouTubeChat(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	videoID := r.PathValue("videoID")
	if !youtubeChatVideoID.MatchString(videoID) {
		http.Error(w, "Invalid video", http.StatusBadRequest)
		return
	}
	if !s.boolSetting("youtube_broadcasts_enabled") || !s.cfg.PlatformEnabled("youtube") {
		http.NotFound(w, r)
		return
	}
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		http.Error(w, "Chat streaming unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func(event string, data []byte) error {
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		return controller.Flush()
	}
	if err := send("start", []byte(fmt.Sprintf(`{"started_at_ms":%d}`, time.Now().UnixMilli()))); err != nil {
		return
	}
	count := 0
	lastHeartbeat := time.Now()
	emit := func(record json.RawMessage) error {
		if len(record) == 0 {
			if time.Since(lastHeartbeat) < 10*time.Second {
				return nil
			}
			lastHeartbeat = time.Now()
			return send("ping", []byte(`{}`))
		}
		count++
		return send("chat", record)
	}
	var err error
	owner, ok := s.videoAssetOwner(videoID)
	if ok {
		files := s.canonicalAssets(owner, "live_chat")
		if len(files) > 0 {
			err = readYouTubeChatFile(files[0].path, emit)
		} else {
			if video, videoErr := s.db.GetVideo(videoID); videoErr == nil && video != nil && video.LiveStatus == "was_live" {
				if queueErr := s.workers.QueueYouTubeReplayChat(videoID); queueErr != nil {
					slog.Warn("queue replay chat", "err", queueErr)
				}
			}
			err = s.workers.StreamYouTubeChat(r.Context(), videoID, emit)
		}
	} else {
		err = s.workers.StreamYouTubeChat(r.Context(), videoID, emit)
	}
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		slog.Warn("YouTube chat fetch", "err", err)
		_ = send("failed", []byte(`{}`))
	} else if count == 0 {
		_ = send("unavailable", []byte(`{}`))
	} else {
		_ = send("end", []byte(`{}`))
	}
}

func readYouTubeChatFile(path string, emit func(json.RawMessage) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if record := bytes.TrimSpace(line); len(record) > 0 {
			if emitErr := emit(record); emitErr != nil {
				return emitErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
