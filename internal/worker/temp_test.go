package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/screwys/igloo/internal/download"
)

func TestTempDownloadCancellationRetriesEvenWhenOutputMentionsCookies(t *testing.T) {
	result := TempDownloadResult{
		Message: "yt-dlp: context canceled: provided YouTube account cookies are no longer valid",
		Cause:   fmt.Errorf("yt-dlp: %w", context.Canceled),
	}

	classification := classifyTempDownloadFailure(result, 1)
	if classification.Kind != download.ErrorKindCanceled {
		t.Fatalf("failure kind = %q, want %q", classification.Kind, download.ErrorKindCanceled)
	}
	if classification.Permanent {
		t.Fatal("server shutdown cancellation must not permanently block durable work")
	}
	if classification.RetryDelay <= 0 {
		t.Fatal("server shutdown cancellation must be retried")
	}
}
