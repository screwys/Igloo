package components

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestTempDownloadPageProvidesProducerCancellationControls(t *testing.T) {
	p := newTestPageProps()
	var buf bytes.Buffer
	if err := TempDownloadPage(p, "video123", "https://www.youtube.com/watch?v=video123", "", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("TempDownloadPage render failed: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		`id="temp-dl-cancel-btn"`,
		`id="temp-dl-stream-btn"`,
		`data-title-cancelled="Download cancelled"`,
		`data-status-cancelled="Cancelled."`,
		"js/temp_download.js",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected temp download cancellation controls to contain %q", want)
		}
	}
}

func TestTempDownloadPageHandlesNonJSONDownloadErrors(t *testing.T) {
	p := newTestPageProps()
	var buf bytes.Buffer
	if err := TempDownloadPage(p, "video123", "https://www.youtube.com/watch?v=video123", "", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("TempDownloadPage render failed: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "js/temp_download.js") {
		t.Fatal("temp download page must load its response handler")
	}
	source, err := os.ReadFile("../../static/js/temp_download.js")
	if err != nil {
		t.Fatalf("read temp download script: %v", err)
	}
	script := string(source)

	for _, want := range []string{
		"response.text()",
		"JSON.parse(raw)",
		"response.statusText || cfg.dataset.statusFailed",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected temp download response handling script to contain %q", want)
		}
	}
	if strings.Contains(script, "response.json()") {
		t.Fatalf("temp download page should not assume every response is JSON")
	}
}

func TestTempDownloadPagePollsQueuedDownloadWithoutReloading(t *testing.T) {
	p := newTestPageProps()
	var buf bytes.Buffer
	if err := TempDownloadPage(p, "video123", "https://www.youtube.com/watch?v=video123", "", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("TempDownloadPage render failed: %v", err)
	}
	html := buf.String()
	source, err := os.ReadFile("../../static/js/temp_download.js")
	if err != nil {
		t.Fatalf("read temp download script: %v", err)
	}
	script := string(source)

	for _, want := range []string{
		`data-video-id="video123"`,
		"/api/temp-download-status?url=",
		"js/temp_download.js",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected queued download page to contain %q", want)
		}
	}
	for _, want := range []string{"data.queued", "setTimeout(pollDownload, 2000)"} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected queued download handler to contain %q", want)
		}
	}
	if strings.Contains(script, "location.reload()") {
		t.Fatal("queued temporary downloads must not refresh the whole page while waiting")
	}
}
