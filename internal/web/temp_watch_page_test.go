package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTempWatchWaitsForQueuedJobAfterMediaIsStored(t *testing.T) {
	srv := newTestServer(t)
	const videoID = "sample_temp_video"
	storeReadyMediaAsset(t, srv, "youtube", "youtube_video", videoID, "video_stream", 0,
		filepath.Join("media", "youtube", videoID+".mp4"), "video/mp4", []byte("fake-mp4"))

	url := "https://www.youtube.com/watch?v=" + videoID
	if _, err := srv.db.EnqueueTempDownload(url, "youtube"); err != nil {
		t.Fatal(err)
	}

	req := attachTestAuth(httptest.NewRequest(http.MethodGet, "/temp/watch?v="+videoID+"&queued=1", nil), "viewer")
	rec := httptest.NewRecorder()
	srv.handlePageTempWatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; location=%q", rec.Code, http.StatusOK, rec.Header().Get("Location"))
	}
	if !strings.Contains(rec.Body.String(), `data-download-status="pending"`) {
		t.Fatalf("page must retain queued ownership until comments and metadata are complete: %s", rec.Body.String())
	}
}
