package web

import (
	"net/http"
)

// ── Route registration ────────────────────────────────────────────────────────

func (s *Server) registerLogsAPIRoutes(mux *http.ServeMux) {
	// Server logs
	mux.HandleFunc("GET /api/logs/server/read", s.handleLogsServer)
	mux.HandleFunc("GET /api/logs/summary", s.handleLogsSummary)
	mux.HandleFunc("POST /api/logs/cleanup", s.handleLogsCleanup)
	mux.HandleFunc("GET /api/logs", s.handleLogsMerged)

	// Analytics
	mux.HandleFunc("POST /api/analytics/events", s.handleAnalyticsEvents)
	mux.HandleFunc("GET /api/analytics/summary", s.handleAnalyticsSummary)

	mux.HandleFunc("GET /api/logs/android/status", s.handleAndroidStatus)
}
