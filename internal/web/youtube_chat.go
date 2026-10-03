package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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
	err := s.workers.StreamYouTubeChat(r.Context(), videoID, func(record json.RawMessage) error {
		if len(record) == 0 {
			if time.Since(lastHeartbeat) < 10*time.Second {
				return nil
			}
			lastHeartbeat = time.Now()
			return send("ping", []byte(`{}`))
		}
		count++
		return send("chat", record)
	})
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
