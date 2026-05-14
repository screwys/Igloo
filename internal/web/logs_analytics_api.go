package web

import (
	"log/slog"
	"net/http"

	"github.com/screwys/igloo/internal/db"
)

// ── Analytics ─────────────────────────────────────────────────────────────────

const analyticsEventsMaxBodyBytes int64 = 256 << 10

func (s *Server) handleAnalyticsEvents(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Events []db.AnalyticsEvent `json:"events"`
	}
	if err := decodeLimitedJSON(w, r, analyticsEventsMaxBodyBytes, &body); err != nil {
		if requestBodyTooLarge(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"success": false, "error": requestBodyTooLargeMessage})
			return
		}
		writeJSON(w, 400, map[string]any{"success": false, "error": "invalid JSON"})
		return
	}
	added, err := s.db.AddAnalyticsEvents(body.Events)
	if err != nil {
		slog.Error("AddAnalyticsEvents", "err", err)
		writeJSON(w, 500, map[string]any{"success": false, "error": "db error"})
		return
	}
	writeJSON(w, 200, map[string]any{"success": true, "added": added})
}

func (s *Server) handleAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	rollups, err := s.db.GetAnalyticsRollups(200)
	if err != nil {
		slog.Error("GetAnalyticsRollups", "err", err)
		writeJSON(w, 500, map[string]any{"error": "db error"})
		return
	}
	events, err := s.db.GetAnalyticsRecentEvents(50)
	if err != nil {
		slog.Error("GetAnalyticsRecentEvents", "err", err)
		writeJSON(w, 500, map[string]any{"error": "db error"})
		return
	}
	if rollups == nil {
		rollups = []db.AnalyticsRollup{}
	}
	if events == nil {
		events = []db.AnalyticsEvent{}
	}
	writeJSON(w, 200, map[string]any{
		"rollups":       rollups,
		"recent_events": events,
	})
}
