package web

import (
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
