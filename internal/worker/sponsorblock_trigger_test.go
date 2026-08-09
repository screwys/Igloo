package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/dearrow"
	"github.com/screwys/igloo/internal/sponsorblock"
)

// stubSBClient lets us inject canned segments / error without real network.
type stubSBClient struct {
	segments []sponsorblock.Segment
	err      error
	callsN   int
}

func (s *stubSBClient) Fetch(_ context.Context, _ string) ([]sponsorblock.Segment, error) {
	s.callsN++
	if s.err != nil {
		return nil, s.err
	}
	return s.segments, nil
}

func (s *stubSBClient) calls() int { return s.callsN }

func TestYoutubeDownloadEnrichmentFetchesDearrowAndSponsorBlock(t *testing.T) {
	realTitle := "Community"
	m, daClient := newTestManagerWithDearrow(t, dearrow.Result{Title: &realTitle}, nil)

	sbStub := &stubSBClient{segments: []sponsorblock.Segment{
		{Start: 10, End: 20, Category: "sponsor"},
	}}
	m.sponsorblockClient = sbStub

	seedVideo(t, m, "v1")

	m.triggerYoutubeEnrichFetch(t.Context(), "v1", "", "youtube")
	if daClient.calls() != 1 {
		t.Errorf("dearrow calls = %d, want 1", daClient.calls())
	}
	if sbStub.calls() != 1 {
		t.Errorf("sponsorblock calls = %d, want 1", sbStub.calls())
	}

	// DeArrow title landed.
	v, err := m.db.GetVideo("v1")
	if err != nil || v == nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if v.DearrowTitle == nil || *v.DearrowTitle != "Community" {
		t.Errorf("DearrowTitle = %v, want Community", v.DearrowTitle)
	}

	// SponsorBlock segment + checked-marker landed.
	segs, err := m.db.GetSponsorBlockSegments("v1")
	if err != nil {
		t.Fatalf("GetSponsorBlockSegments: %v", err)
	}
	if len(segs) != 1 || segs[0].Category != "sponsor" {
		t.Errorf("segments = %+v, want one 'sponsor' segment", segs)
	}
	checked, err := m.db.GetSponsorBlockChecked("v1")
	if err != nil || checked == nil {
		t.Fatalf("GetSponsorBlockChecked: %v (checked=%v)", err, checked)
	}

	m.triggerYoutubeEnrichFetch(t.Context(), "v1", "", "youtube")
	if daClient.calls() != 1 || sbStub.calls() != 1 {
		t.Fatalf("fresh component checks repeated: dearrow=%d sponsorblock=%d", daClient.calls(), sbStub.calls())
	}
}

func TestFetchSponsorBlockMarksCheckedOnError(t *testing.T) {
	m, _ := newTestManagerWithDearrow(t, dearrow.Result{}, nil)
	m.sponsorblockClient = &stubSBClient{err: errors.New("network down")}
	seedVideo(t, m, "v1")

	m.fetchSponsorBlockFor(t.Context(), "v1", 0)

	checked, err := m.db.GetSponsorBlockChecked("v1")
	if err != nil {
		t.Fatalf("GetSponsorBlockChecked: %v", err)
	}
	if checked == nil {
		t.Fatal("checked row missing after SB error; worker would re-fetch forever")
	}
}

func TestFetchSponsorBlockSkipsNilClient(t *testing.T) {
	m, _ := newTestManagerWithDearrow(t, dearrow.Result{}, nil)
	// m.sponsorblockClient deliberately left nil.
	seedVideo(t, m, "v1")

	m.fetchSponsorBlockFor(t.Context(), "v1", 0)

	checked, _ := m.db.GetSponsorBlockChecked("v1")
	if checked != nil {
		t.Error("SB-checked row written despite nil client")
	}
}

func TestVideoMetadataComponentFreshnessStopsAfterFinalOldCheck(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	published := now.Add(-72 * time.Hour).UnixMilli()
	if !videoMetadataComponentDue(0, published, now.UnixMilli()) {
		t.Fatal("unchecked old component should get one fetch")
	}
	if videoMetadataComponentDue(now.Add(-time.Hour).UnixMilli(), published, now.UnixMilli()) {
		t.Fatal("component checked after the video became old should be terminal")
	}

	recentPublished := now.Add(-30 * time.Hour).UnixMilli()
	checked := now.Add(-25 * time.Hour).UnixMilli()
	if !videoMetadataComponentDue(checked, recentPublished, now.UnixMilli()) {
		t.Fatal("young component should refresh after 24 hours")
	}
	if videoMetadataComponentDue(now.Add(-time.Hour).UnixMilli(), recentPublished, now.UnixMilli()) {
		t.Fatal("recent component check should not refresh yet")
	}
}
