package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQuickDownloadQueuesDurableWork(t *testing.T) {
	srv := newTestServer(t)
	const rawURL = "https://www.youtube.com/watch?v=sample_video"
	req := httptest.NewRequest(http.MethodPost, "/api/quick-download", strings.NewReader(`{"url":"`+rawURL+`"}`))
	req = req.WithContext(contextWithUser(req, "admin", "admin"))
	rec := httptest.NewRecorder()

	srv.handleQuickDownload(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	var platform, status string
	if err := srv.db.QueryRow(`SELECT platform, status FROM temp_download_queue WHERE url = ?`, rawURL).Scan(&platform, &status); err != nil {
		t.Fatal(err)
	}
	if platform != "youtube" || status != "pending" {
		t.Fatalf("queued work = platform=%q status=%q", platform, status)
	}
}

func TestTempDownloadStatusReportsQueueState(t *testing.T) {
	srv := newTestServer(t)
	const rawURL = "https://www.youtube.com/watch?v=sample_video"
	if _, err := srv.db.EnqueueTempDownload(rawURL, "youtube"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/temp-download-status?url="+rawURL, nil)
	req = req.WithContext(contextWithUser(req, "admin", "admin"))
	rec := httptest.NewRecorder()

	srv.handleTempDownloadStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response struct {
		Success  bool   `json:"success"`
		Complete bool   `json:"complete"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Complete || response.Status != "pending" {
		t.Fatalf("response = %+v", response)
	}
}
