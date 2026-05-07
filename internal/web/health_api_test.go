package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/model"
)

func TestHealthLiveHandlerShape(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health/live", nil)
	s.handleHealthLive(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := decodeHealthBody(t, rec)
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
	ts, ok := body["server_time_ms"].(float64)
	if !ok {
		t.Fatalf("expected server_time_ms numeric, got %T", body["server_time_ms"])
	}
	if ts <= 0 {
		t.Errorf("server_time_ms should be positive, got %v", ts)
	}
}

func TestHealthReportsStaleFeedSnapshot(t *testing.T) {
	srv := newTestServer(t)
	now := time.Now().UnixMilli()
	staleAt := now - int64((2 * time.Hour).Milliseconds())
	freshAt := now - int64((45 * time.Minute).Milliseconds())

	insertFeedItemAt(t, srv, "old_ranked", "old_author", staleAt, 1)
	if err := srv.db.ReplaceFeedRankSnapshot("admin", []db.SnapshotRow{
		{TweetID: "old_ranked", RankPosition: 1, FinalScore: 1},
	}); err != nil {
		t.Fatalf("replace snapshot: %v", err)
	}
	if err := srv.db.ExecRaw(`UPDATE feed_rank_snapshot SET computed_at = ?`, staleAt); err != nil {
		t.Fatalf("age snapshot: %v", err)
	}
	insertFeedItemAt(t, srv, "fresh_unranked", "fresh_author", freshAt, 2)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health", nil)
	srv.handleHealth(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeHealthBody(t, rec)
	if body["status"] != "unhealthy" {
		t.Fatalf("status = %v body=%#v", body["status"], body)
	}
	feedCheck := healthCheckBody(t, body, "feed_snapshot")
	if feedCheck["status"] != "unhealthy" {
		t.Fatalf("feed snapshot check = %#v", feedCheck)
	}
	if feedCheck["fresh_items_since_snapshot"].(float64) < 1 {
		t.Fatalf("fresh_items_since_snapshot missing: %#v", feedCheck)
	}
}

func TestHealthReportsAndroidGenerationWithoutFreshHealth(t *testing.T) {
	srv := newTestServer(t)
	now := time.Now().UnixMilli()
	old := now - int64((90 * time.Minute).Milliseconds())

	if err := srv.db.StoreAndroidSyncGeneration(model.AndroidSyncGeneration{
		GenerationID:  "android-sync-current",
		CreatedAtMs:   old,
		Status:        "ready",
		SourceVersion: "current-source",
		Retention: map[string]int{
			"feed_days": 7, "youtube_days": 7, "moments_days": 7, "story_hours": 48,
		},
		ContentCounts: map[string]int{},
		AssetCounts:   map[string]int{},
	}, nil, nil); err != nil {
		t.Fatalf("store generation: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health", nil)
	srv.handleHealth(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeHealthBody(t, rec)
	syncCheck := healthCheckBody(t, body, "android_sync")
	if syncCheck["status"] != "unhealthy" {
		t.Fatalf("android sync check = %#v", syncCheck)
	}
	if syncCheck["latest_generation_id"] != "android-sync-current" {
		t.Fatalf("latest_generation_id = %v", syncCheck["latest_generation_id"])
	}
}

func TestServerStatusIncludesProductHealth(t *testing.T) {
	srv := newTestServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/server/status", nil)
	srv.handleServerStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("server status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeHealthBody(t, rec)
	if _, ok := body["health"].(map[string]any); !ok {
		t.Fatalf("server status should include product health, got %#v", body)
	}
}

func decodeHealthBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func healthCheckBody(t *testing.T, body map[string]any, name string) map[string]any {
	t.Helper()
	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks missing: %#v", body)
	}
	check, ok := checks[name].(map[string]any)
	if !ok {
		t.Fatalf("%s check missing: %#v", name, checks)
	}
	return check
}
