package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestTempDownloadPageCancelAbortsRequestAndStopsSpinner(t *testing.T) {
	p := newTestPageProps()
	var buf bytes.Buffer
	if err := TempDownloadPage(p, "video123", "https://www.youtube.com/watch?v=video123").Render(context.Background(), &buf); err != nil {
		t.Fatalf("TempDownloadPage render failed: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		"new AbortController()",
		"requestController.abort()",
		"setCancelledState()",
		"temp-dl-spinner stopped",
		"Download cancelled",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected temp download cancel script to contain %q", want)
		}
	}
}

func TestTempDownloadPageHandlesNonJSONDownloadErrors(t *testing.T) {
	p := newTestPageProps()
	var buf bytes.Buffer
	if err := TempDownloadPage(p, "video123", "https://www.youtube.com/watch?v=video123").Render(context.Background(), &buf); err != nil {
		t.Fatalf("TempDownloadPage render failed: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		"return r.text()",
		"JSON.parse(text)",
		"parse download response",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected temp download response handling script to contain %q", want)
		}
	}
	if strings.Contains(html, "return r.json();") {
		t.Fatalf("temp download page should not assume every response is JSON")
	}
}
