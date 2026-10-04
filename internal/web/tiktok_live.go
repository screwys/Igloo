package web

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/components"
	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/download"
)

func (s *Server) registerTikTokLiveRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tiktok/lives", s.handleTikTokLives)
	mux.HandleFunc("POST /api/tiktok/lives/{channelID}/stream", s.handleTikTokLiveStream)
	mux.HandleFunc("GET /api/tiktok/lives/{channelID}/chat", s.handleTikTokLiveChat)
	mux.HandleFunc("GET /api/streams/{sessionID}/manifest", s.handleYouTubeStreamManifest)
	mux.HandleFunc("GET /api/streams/{sessionID}/media/{resourceID}/{rest...}", s.handleYouTubeStreamMedia)
	mux.HandleFunc("GET /api/streams/{sessionID}/live/{file}", s.handleLiveRemuxFile)
	mux.HandleFunc("GET /api/videos/{videoID}/saved-state", s.handleYouTubeSavedState)
	mux.HandleFunc("DELETE /api/streams/{sessionID}", s.handleStreamRelease)
}

func (s *Server) handleTikTokLives(w http.ResponseWriter, r *http.Request) {
	lives, err := s.db.ListTikTokLives()
	if err != nil {
		writeJSONError(w, 500, "live_list", "Could not load live streams")
		return
	}
	writeJSON(w, 200, map[string]any{"lives": lives})
}

func (s *Server) handleTikTokLiveStream(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	live, info, err := s.workers.ResolveTikTokLive(ctx, r.PathValue("channelID"))
	if err != nil {
		writeJSONError(w, 502, "live_stream", "Could not load the live stream")
		return
	}
	if live == nil {
		writeJSONError(w, 410, "live_ended", "Live stream ended")
		return
	}
	metadata, err := json.Marshal(info.Metadata)
	if err != nil {
		writeJSONError(w, 500, "live_metadata", "Could not prepare the live stream")
		return
	}
	if err := s.db.ObserveStreamVideo(db.CompletedVideo{VideoID: "tiktok_live_" + live.RoomID, ChannelID: live.ChannelID,
		OwnerKind: "tiktok_video", Title: live.Title, MetadataJSON: string(metadata), PublishedAtMs: live.ObservedAtMs}); err != nil {
		writeJSONError(w, 500, "live_metadata", "Could not prepare the live stream")
		return
	}
	client := *download.NewHTTPDownloader().Client
	bookmarked, categoryID, err := s.db.IsBookmarked("tiktok_live_" + live.RoomID)
	if err != nil {
		writeJSONError(w, 500, "live_metadata", "Could not read saved video state")
		return
	}
	client.Timeout = 0
	session := &youtubeStreamSession{id: rand.Text(), videoID: info.ID, info: info, client: &client, resources: make(map[string]youtubeStreamResource), resourceIDs: make(map[string]string), lastUsed: time.Now()}
	manifestURL := "/api/streams/" + session.id + "/manifest"
	hasManifest := false
	for _, format := range info.Formats {
		hasManifest = hasManifest || format.ManifestURL != ""
	}
	if !hasManifest {
		if err := s.startLiveRemux(ctx, session); err != nil {
			writeJSONError(w, 502, "live_prepare", "Could not prepare the live stream")
			return
		}
		manifestURL = "/api/streams/" + session.id + "/live/index.m3u8"
	} else if err := session.prepare(ctx); err != nil {
		writeJSONError(w, 502, "live_prepare", "Could not prepare the live stream")
		return
	}
	s.storeYouTubeStream(session)
	writeJSON(w, 200, map[string]any{"success": true, "video_id": "tiktok_live_" + live.RoomID, "manifest_url": manifestURL, "manifest_type": session.manifestType, "session_id": session.id, "live": live, "bookmarked": bookmarked, "bookmark_category_id": categoryID})
}

func (s *Server) startLiveRemux(ctx context.Context, session *youtubeStreamSession) error {
	if len(session.info.Formats) == 0 {
		return fmt.Errorf("no live formats")
	}
	directory, err := os.MkdirTemp("", "igloo-live-")
	if err != nil {
		return err
	}
	cancel, done := s.workers.StartLiveRemux(session.info.Formats[0].URL, directory)
	ready := false
	defer func() {
		if !ready {
			cancel()
			<-done
			_ = os.RemoveAll(directory)
		}
	}()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for !ready {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			if err == nil {
				err = fmt.Errorf("live stream ended")
			}
			return err
		case <-ticker.C:
			if info, err := os.Stat(filepath.Join(directory, "index.m3u8")); err == nil && info.Size() > 0 {
				ready = true
			}
		}
	}
	session.liveDirectory = directory
	session.liveCancel = cancel
	session.manifestType = "hls"
	session.textTracks = []components.StreamTextTrack{}
	go func() {
		process := done
		defer func() { cancel(); <-process; _ = os.RemoveAll(directory) }()
		check := time.NewTicker(15 * time.Second)
		defer check.Stop()
		for {
			select {
			case err := <-done:
				if errors.Is(err, context.Canceled) {
					return
				}
				done = nil
			case <-check.C:
				session.mu.Lock()
				idle := time.Since(session.lastUsed) > time.Minute
				session.mu.Unlock()
				if idle {
					return
				}
			}
		}
	}()
	return nil
}

func (s *Server) handleStreamRelease(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	s.youtubeStreamsMu.Lock()
	session := s.youtubeStreams[id]
	delete(s.youtubeStreams, id)
	s.youtubeStreamsMu.Unlock()
	if session != nil && session.liveCancel != nil {
		session.liveCancel()
	}
	w.WriteHeader(http.StatusNoContent)
}

var liveRemuxSegment = regexp.MustCompile(`^segment-[0-9]{9}\.ts$`)

func (s *Server) handleLiveRemuxFile(w http.ResponseWriter, r *http.Request) {
	session := s.youtubeStream(r.PathValue("sessionID"))
	name := r.PathValue("file")
	if session == nil || session.liveDirectory == "" || (name != "index.m3u8" && !liveRemuxSegment.MatchString(name)) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if name == "index.m3u8" {
		data, err := os.ReadFile(filepath.Join(session.liveDirectory, name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if liveRemuxSegment.MatchString(line) {
				lines[i] = "/api/streams/" + session.id + "/live/" + line
			}
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte(strings.Join(lines, "\n")))
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeFile(w, r, filepath.Join(session.liveDirectory, name))
}

func (s *Server) handleTikTokLiveChat(w http.ResponseWriter, r *http.Request) {
	channel, err := s.db.GetChannel(r.PathValue("channelID"))
	if err != nil || channel == nil || channel.Platform != "tiktok" {
		http.NotFound(w, r)
		return
	}
	if !s.cfg.PlatformEnabled("tiktok") {
		http.NotFound(w, r)
		return
	}
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		http.Error(w, "Chat streaming unavailable", 500)
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
	if err := send("start", []byte(`{}`)); err != nil {
		return
	}
	handle := channel.Handle
	if handle == "" {
		handle = strings.TrimPrefix(channel.ChannelID, "tiktok_")
	}
	err = download.StreamTikTokLiveChat(r.Context(), handle, func(line json.RawMessage) error {
		if len(line) == 0 {
			return send("ping", []byte(`{}`))
		}
		return send("chat", line)
	})
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		_ = send("failed", []byte(`{}`))
	} else {
		_ = send("end", []byte(`{}`))
	}
}
