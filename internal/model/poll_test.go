package model

import (
	"testing"
	"time"
)

func TestPollClosureUsesCaptureTime(t *testing.T) {
	end := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
	poll := Poll{EndsAt: end.Format(time.RFC3339), CapturedAt: end.Add(-time.Hour).UnixMilli()}
	if poll.ClosedAtCapture() {
		t.Fatal("a pre-close snapshot must not become final just because the poll has since ended")
	}
	poll.CapturedAt = end.UnixMilli()
	if !poll.ClosedAtCapture() {
		t.Fatal("capture at the closing time should show closed when captured")
	}
}
