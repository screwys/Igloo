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

func TestDiscoverTempDownloadsUseBackgroundMediaLane(t *testing.T) {
	if got := tempDownloadLane("discover"); got != download.MediaLaneBulkBackground {
		t.Fatalf("discover lane = %q", got)
	}
	if got := tempDownloadLane("interactive"); got != download.MediaLaneBulkInteractive {
		t.Fatalf("interactive lane = %q", got)
	}
	if tempDownloadSeedsRecommendations("discover") {
		t.Fatal("discover prefetch must not recursively seed recommendations")
	}
	if !tempDownloadSeedsRecommendations("interactive") {
		t.Fatal("interactive temporary downloads should seed recommendations")
	}
}
