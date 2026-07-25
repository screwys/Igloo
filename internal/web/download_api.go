package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/screwys/igloo/internal/components"
	"github.com/screwys/igloo/internal/subscribe"
)

func (s *Server) registerDownloadAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/quick-download", s.handleQuickDownload)
	mux.HandleFunc("GET /api/temp-download-status", s.handleTempDownloadStatus)
	mux.HandleFunc("POST /api/cancel-download", s.handleCancelDownload)
	mux.HandleFunc("POST /api/stop", s.handleStop)
	mux.HandleFunc("POST /api/resume", s.handleResume)
	mux.HandleFunc("GET /api/stop-play-btn", s.handleStopPlayBtn)
}

func (s *Server) handleQuickDownload(w http.ResponseWriter, r *http.Request) {
	isHTMX := r.Header.Get("HX-Request") != ""
	user := userFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		if isHTMX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, "Quick download is restricted to admins")
			return
		}
		writeJSONError(w, http.StatusForbidden, "forbidden", "Quick download is restricted to admins")
		return
	}

	var rawURL string
	if isHTMX && r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		rawURL = strings.TrimSpace(r.FormValue("url"))
	} else {
		var body struct {
			URL         string `json:"url"`
			SaveChannel bool   `json:"save_channel"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			if requestBodyTooLarge(err) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": requestBodyTooLargeMessage})
				return
			}
			writeJSON(w, 400, map[string]any{"error": "url required"})
			return
		}
		rawURL = body.URL
	}

	platform := subscribe.DetectPlatform(rawURL, "")
	if rawURL == "" || subscribe.ValidateInput(rawURL, platform) != nil {
		if isHTMX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(422)
			_, _ = fmt.Fprint(w, `Enter a supported YouTube, TikTok, or X URL`)
			return
		}
		writeJSON(w, 400, map[string]any{"error": "supported YouTube, TikTok, or X URL required"})
		return
	}
	if !s.platformEnabled(platform) {
		msg := fmt.Sprintf("%s is not enabled on this Igloo server", platformChoiceLabel(platform))
		if isHTMX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(422)
			_, _ = fmt.Fprint(w, template.HTMLEscapeString(msg))
			return
		}
		writeJSON(w, 422, map[string]any{"error": msg, "platform": platform})
		return
	}

	_, err := s.workers.EnqueueTempDownload(rawURL)
	if err != nil {
		msg := fmt.Sprintf("Queue download: %v", err)
		if isHTMX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, template.HTMLEscapeString(msg))
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": msg})
		return
	}
	if isHTMX {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<span data-download-success="true">Queued for download</span>`)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"success": true, "queued": true})
}

func (s *Server) handleTempDownloadStatus(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		writeJSONError(w, http.StatusForbidden, "forbidden", "Quick download is restricted to admins")
		return
	}
	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	platform := subscribe.DetectPlatform(rawURL, "")
	if rawURL == "" || subscribe.ValidateInput(rawURL, platform) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "supported YouTube, TikTok, or X URL required"})
		return
	}
	state, found, err := s.db.TempDownloadState(rawURL)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "temporary_download_status", "Could not read download status")
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "complete": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"complete": false,
		"status":   state.Status,
		"error":    state.Error,
	})
}

func (s *Server) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		VideoID string `json:"video_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil && requestBodyTooLarge(err) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": requestBodyTooLargeMessage})
		return
	}
	// Stub: full cancellation requires tracking active download contexts
	writeJSON(w, 200, map[string]any{"success": true, "video_id": body.VideoID})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	s.workers.SetStopRequested(true)
	s.workers.SetIngestPaused(true)
	if r.Header.Get("HX-Request") != "" {
		_ = components.StopPlayButton(s.pageProps(w, r), true).Render(r.Context(), w)
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "stopped": true})
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	s.workers.SetStopRequested(false)
	s.workers.SetIngestPaused(false)
	s.workers.KickMediaWork()
	s.workers.KickIngest()
	if r.Header.Get("HX-Request") != "" {
		_ = components.StopPlayButton(s.pageProps(w, r), false).Render(r.Context(), w)
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "resumed": true})
}

func (s *Server) handleStopPlayBtn(w http.ResponseWriter, r *http.Request) {
	_ = components.StopPlayButton(s.pageProps(w, r), s.workers.IsStopRequested()).Render(r.Context(), w)
}
