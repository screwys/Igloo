package web

import (
	"net/http"
	"time"
)

const (
	productHealthUsername            = "admin"
	feedSnapshotHealthGrace          = 15 * time.Minute
	androidSyncHealthReportGrace     = 15 * time.Minute
	androidSyncHealthReportMaxAge    = 6 * time.Hour
	productHealthStatusHealthy       = "healthy"
	productHealthStatusDegraded      = "degraded"
	productHealthStatusUnhealthy     = "unhealthy"
	productHealthReasonNoData        = "no_data"
	productHealthReasonUnavailable   = "unavailable"
	productHealthReasonCurrent       = "current"
	productHealthReasonStale         = "stale"
	productHealthReasonMissing       = "missing"
	productHealthReasonMismatch      = "generation_mismatch"
	productHealthReasonAssetFailures = "asset_failures"
)

type productHealth struct {
	Status string
	Checks map[string]map[string]any
}

// /api/health/live is a liveness probe: no auth, no DB, no product state. It is
// used by Android reachability and container process checks.
func (s *Server) registerHealthAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health/live", s.handleHealthLive)
	mux.HandleFunc("GET /api/health", s.handleHealth)
}

func (s *Server) handleHealthLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"status": "live",
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := s.productHealth(time.Now())
	statusCode := http.StatusOK
	if health.Status == productHealthStatusUnhealthy {
		statusCode = http.StatusServiceUnavailable
	}
	writeJSON(w, statusCode, health.response())
}

func (s *Server) productHealth(now time.Time) productHealth {
	checks := map[string]map[string]any{}
	status := productHealthStatusHealthy

	feedStatus, feed := s.feedSnapshotProductHealth(now)
	checks["feed_snapshot"] = feed
	status = mergeProductHealthStatus(status, feedStatus)

	syncStatus, sync := s.androidSyncProductHealth(now)
	checks["android_sync"] = sync
	status = mergeProductHealthStatus(status, syncStatus)

	return productHealth{
		Status: status,
		Checks: checks,
	}
}

func (h productHealth) response() map[string]any {
	return map[string]any{
		"ok":     h.Status == productHealthStatusHealthy,
		"status": h.Status,
		"checks": h.Checks,
	}
}

func mergeProductHealthStatus(current, next string) string {
	if current == productHealthStatusUnhealthy || next == productHealthStatusUnhealthy {
		return productHealthStatusUnhealthy
	}
	if current == productHealthStatusDegraded || next == productHealthStatusDegraded {
		return productHealthStatusDegraded
	}
	return productHealthStatusHealthy
}

func (s *Server) feedSnapshotProductHealth(now time.Time) (string, map[string]any) {
	check := map[string]any{
		"status": productHealthStatusHealthy,
		"reason": productHealthReasonCurrent,
	}
	if s.db == nil {
		check["status"] = productHealthStatusUnhealthy
		check["reason"] = productHealthReasonUnavailable
		return productHealthStatusUnhealthy, check
	}

	snapshot, err := s.db.GetFeedSnapshotHealth(productHealthUsername)
	if err != nil {
		check["status"] = productHealthStatusUnhealthy
		check["reason"] = err.Error()
		return productHealthStatusUnhealthy, check
	}

	check["snapshot_at_ms"] = snapshot.SnapshotAtMs
	check["candidate_count"] = snapshot.CandidateCount
	check["latest_candidate_fetched_at_ms"] = snapshot.LatestCandidateFetchedAtMs
	check["latest_candidate_published_at_ms"] = snapshot.LatestCandidatePublishedAtMs
	check["fresh_items_since_snapshot"] = snapshot.FreshItemsSinceSnapshot
	if snapshot.SnapshotAtMs > 0 {
		check["snapshot_age_ms"] = now.UnixMilli() - snapshot.SnapshotAtMs
	}
	if snapshot.LatestCandidateFetchedAtMs > snapshot.SnapshotAtMs {
		check["snapshot_lag_ms"] = snapshot.LatestCandidateFetchedAtMs - snapshot.SnapshotAtMs
	}

	if snapshot.CandidateCount == 0 {
		check["reason"] = productHealthReasonNoData
		return productHealthStatusHealthy, check
	}

	latestAge := time.Duration(now.UnixMilli()-snapshot.LatestCandidateFetchedAtMs) * time.Millisecond
	if snapshot.FreshItemsSinceSnapshot > 0 && latestAge >= feedSnapshotHealthGrace {
		check["status"] = productHealthStatusUnhealthy
		check["reason"] = productHealthReasonStale
		check["stale_after_ms"] = feedSnapshotHealthGrace.Milliseconds()
		return productHealthStatusUnhealthy, check
	}

	return productHealthStatusHealthy, check
}

func (s *Server) androidSyncProductHealth(now time.Time) (string, map[string]any) {
	check := map[string]any{
		"status": productHealthStatusHealthy,
		"reason": productHealthReasonCurrent,
	}
	if s.db == nil {
		check["status"] = productHealthStatusUnhealthy
		check["reason"] = productHealthReasonUnavailable
		return productHealthStatusUnhealthy, check
	}

	gen, err := s.db.GetLatestAndroidSyncGeneration()
	if err != nil {
		check["status"] = productHealthStatusUnhealthy
		check["reason"] = err.Error()
		return productHealthStatusUnhealthy, check
	}
	health, err := s.db.GetLatestAndroidSyncHealthReport()
	if err != nil {
		check["status"] = productHealthStatusUnhealthy
		check["reason"] = err.Error()
		return productHealthStatusUnhealthy, check
	}

	if gen == nil {
		check["reason"] = productHealthReasonNoData
		return productHealthStatusHealthy, check
	}
	check["latest_generation_id"] = gen.GenerationID
	check["latest_generation_created_at_ms"] = gen.CreatedAtMs
	check["latest_generation_age_ms"] = now.UnixMilli() - gen.CreatedAtMs

	if health == nil {
		check["latest_health_reported_at_ms"] = int64(0)
		if generationOldEnoughForHealth(now, gen.CreatedAtMs) {
			check["status"] = productHealthStatusUnhealthy
			check["reason"] = productHealthReasonMissing
			return productHealthStatusUnhealthy, check
		}
		check["status"] = productHealthStatusDegraded
		check["reason"] = productHealthReasonMissing
		return productHealthStatusDegraded, check
	}

	check["latest_health_generation_id"] = health.GenerationID
	check["latest_health_reported_at_ms"] = health.ReportedAtMs
	check["health_report_age_ms"] = now.UnixMilli() - health.ReportedAtMs
	check["total_assets"] = health.TotalAssets
	check["verified_assets"] = health.VerifiedAssets
	check["pending_assets"] = health.PendingAssets
	check["failed_assets"] = health.FailedAssets
	check["missing_assets"] = health.MissingAssets

	if health.GenerationID != gen.GenerationID && generationOldEnoughForHealth(now, gen.CreatedAtMs) {
		check["status"] = productHealthStatusUnhealthy
		check["reason"] = productHealthReasonMismatch
		return productHealthStatusUnhealthy, check
	}
	if time.Duration(now.UnixMilli()-health.ReportedAtMs)*time.Millisecond > androidSyncHealthReportMaxAge {
		check["status"] = productHealthStatusUnhealthy
		check["reason"] = productHealthReasonStale
		return productHealthStatusUnhealthy, check
	}
	if health.FailedAssets > 0 || health.MissingAssets > 0 {
		check["status"] = productHealthStatusDegraded
		check["reason"] = productHealthReasonAssetFailures
		return productHealthStatusDegraded, check
	}

	return productHealthStatusHealthy, check
}

func generationOldEnoughForHealth(now time.Time, createdAtMs int64) bool {
	return time.Duration(now.UnixMilli()-createdAtMs)*time.Millisecond >= androidSyncHealthReportGrace
}
