package web

import (
	"net/http"
	"sync"
)

// ── In-memory Android state ───────────────────────────────────────────────────

type androidLogEvent struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Tag       string `json:"tag"`
	Message   string `json:"message"`
}

type androidState struct {
	mu             sync.Mutex
	forceSyncFlag  bool
	fetchRequested bool
	eventBuffer    []androidLogEvent // maxlen 500
	cacheHealth    map[string]any
}

var android = &androidState{}

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

	// Android dashboard
	mux.HandleFunc("POST /api/logs/android", s.handleAndroidLog)
	mux.HandleFunc("POST /api/logs/android/batch", s.handleAndroidBatch)
	mux.HandleFunc("POST /api/logs/android/stats", s.handleAndroidStats)
	mux.HandleFunc("POST /api/logs/android/debug", s.handleAndroidDebugLog)
	mux.HandleFunc("POST /api/logs/android/cache-health", s.handleAndroidCacheHealth)
	mux.HandleFunc("GET /api/logs/android/status", s.handleAndroidStatus)
	mux.HandleFunc("POST /api/logs/android/force-sync", s.handleAndroidForceSync)
	mux.HandleFunc("GET /api/logs/android/force-sync/check", s.handleAndroidForceSyncCheck)
	mux.HandleFunc("POST /api/logs/android/fetch", s.handleAndroidFetch)
}
