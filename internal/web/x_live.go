package web

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/components"
	"github.com/screwys/igloo/internal/download"
	"github.com/screwys/igloo/internal/model"
)

func (s *Server) registerXLiveRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/x/lives", s.handleXLives)
	mux.HandleFunc("POST /api/x/stream", s.handleXLiveStream)
	mux.HandleFunc("GET /api/x/lives/{broadcastID}/chat", s.handleXLiveChat)
}

func (s *Server) handleXLives(w http.ResponseWriter, r *http.Request) {
	lives, err := s.db.ListXBroadcasts()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "live_list", "Could not load live streams")
		return
	}
	if r.URL.Query().Get("fmt") == "html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = components.FeedLiveRows(s.pageProps(w, r), lives).Render(r.Context(), w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lives": lives})
}

func (s *Server) handleXLiveStream(w http.ResponseWriter, r *http.Request) {
	if !s.platformEnabled("twitter") {
		writeJSONError(w, http.StatusNotFound, "platform_disabled", "X is not enabled")
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		if requestBodyTooLarge(err) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "invalid_stream_request", requestBodyTooLargeMessage)
			return
		}
		writeJSONError(w, http.StatusBadRequest, "invalid_stream_request", "X broadcast URL required")
		return
	}
	body.URL = model.NormalizeXBroadcastURL(body.URL)
	if body.URL == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_stream_request", "X broadcast URL required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	live, info, err := s.workers.ResolveXPlayback(ctx, body.URL)
	if err != nil {
		if download.ClassifyFailure(err, nil, 0).Kind == download.ErrorKindAuth {
			writeJSONError(w, http.StatusBadGateway, "x_auth_required", "X requires sign-in. Add X cookies in Preferences.")
			return
		}
		writeJSONError(w, http.StatusBadGateway, "live_stream", "Could not load the live stream")
		return
	}
	if len(info.Formats) == 0 {
		message := "Live stream ended"
		if live.LiveStatus == "is_upcoming" {
			message = "Live stream has not started"
		}
		writeJSONError(w, http.StatusGone, "live_ended", message)
		return
	}
	client := *download.NewHTTPDownloader().Client
	client.Timeout = 0
	session := &youtubeStreamSession{
		id: rand.Text(), videoID: info.ID, info: info, client: &client,
		resources: make(map[string]youtubeStreamResource), resourceIDs: make(map[string]string), lastUsed: time.Now(),
	}
	if err := session.prepare(ctx); err != nil {
		writeJSONError(w, http.StatusBadGateway, "live_prepare", "Could not prepare the live stream")
		return
	}
	s.storeYouTubeStream(session)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true, "manifest_url": "/api/streams/" + session.id + "/manifest",
		"manifest_type": session.manifestType, "session_id": session.id, "live": live,
	})
}

func (s *Server) handleXLiveChat(w http.ResponseWriter, r *http.Request) {
	if !s.platformEnabled("twitter") {
		http.NotFound(w, r)
		return
	}
	broadcastID := r.PathValue("broadcastID")
	if model.NormalizeXBroadcastURL("https://x.com/i/broadcasts/"+url.PathEscape(broadcastID)) == "" {
		http.Error(w, "X broadcast required", http.StatusBadRequest)
		return
	}
	broadcast, err := s.db.GetXBroadcast(broadcastID)
	if err != nil {
		http.Error(w, "Could not load chat", http.StatusInternalServerError)
		return
	}
	live := broadcast == nil || broadcast.LiveStatus == "is_live"
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
	if err := send("start", []byte(`{}`)); err != nil {
		return
	}
	if broadcast != nil && strings.Contains(broadcast.URL, "/i/spaces/") {
		_ = send("unavailable", []byte(`{}`))
		return
	}
	err = download.StreamXBroadcastChat(r.Context(), broadcastID, live, func(record json.RawMessage) error {
		if len(record) == 0 {
			return send("ping", []byte(`{}`))
		}
		var event struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(record, &event); err != nil {
			return err
		}
		if event.Type == "ready" {
			return send("ready", []byte(`{}`))
		}
		return send("chat", record)
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
