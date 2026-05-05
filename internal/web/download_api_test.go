package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/worker"
)

func TestWriteQuickDownloadJSONWithKeepaliveKeepsResponseParseable(t *testing.T) {
	rec := httptest.NewRecorder()
	results := make(chan worker.TempDownloadResult, 1)
	go func() {
		time.Sleep(5 * time.Millisecond)
		results <- worker.TempDownloadResult{Success: true, VideoID: "video123", Message: "done"}
	}()

	writeQuickDownloadJSONWithKeepalive(rec, results, time.Millisecond)

	body := rec.Body.String()
	if !strings.HasPrefix(body, "\n") {
		t.Fatalf("expected keepalive whitespace before final JSON, got %q", body)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("response should remain valid JSON with leading keepalive whitespace: %v, body=%q", err, body)
	}
	if payload["video_id"] != "video123" {
		t.Fatalf("video_id = %v, body=%q", payload["video_id"], body)
	}
}
