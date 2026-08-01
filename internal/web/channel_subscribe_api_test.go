package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestChannelStarCommitsBeforeQueuedFeedWindowInvalidation(t *testing.T) {
	srv := newTestServer(t)
	const channelID = "twitter_sample_star"
	if err := srv.db.AddChannel(model.Channel{
		ChannelID: channelID,
		SourceID:  "sample_star",
		Name:      "Sample Star",
		Platform:  "twitter",
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.ExecRaw(`
		INSERT INTO feed_items (
			tweet_id, channel_id, source_channel_id, body_text, published_at, fetched_at, algo_scored_at
		) VALUES ('sample_star_item', ?, ?, 'body', 1, 1, 789)
	`, channelID, channelID); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/channels/"+channelID+"/star", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !srv.db.IsChannelStarred(channelID) {
		t.Fatal("channel star was not committed before the response")
	}
	_ = mutationOwnerRevision(t, srv, "channel_star", channelID)
	var scoredAt int64
	if err := srv.db.QueryRow(`
		SELECT algo_scored_at FROM feed_items WHERE tweet_id = 'sample_star_item'
	`).Scan(&scoredAt); err != nil {
		t.Fatal(err)
	}
	if scoredAt != 789 {
		t.Fatalf("response-path algo_scored_at = %d, want 789", scoredAt)
	}

	processQueuedFeedOrderInvalidations(t, srv)
	if err := srv.db.QueryRow(`
		SELECT algo_scored_at FROM feed_items WHERE tweet_id = 'sample_star_item'
	`).Scan(&scoredAt); err != nil {
		t.Fatal(err)
	}
	if scoredAt != 0 {
		t.Fatalf("queued algo_scored_at = %d, want 0", scoredAt)
	}
}

func TestChannelSubscribeRouteFollowsExistingTempChannel(t *testing.T) {
	srv := newTestServer(t)
	const channelID = "youtube_UCtempchannel"
	if err := srv.db.AddChannel(model.Channel{
		ChannelID: channelID,
		SourceID:  "UCtempchannel",
		Name:      "Temp Channel",
		URL:       "https://www.youtube.com/channel/UCtempchannel",
		Platform:  "youtube",
	}); err != nil {
		t.Fatalf("AddChannel: %v", err)
	}
	if err := srv.db.ExecRaw(`
		INSERT INTO feed_items (
			tweet_id, channel_id, source_channel_id, body_text, published_at, fetched_at, algo_scored_at
		) VALUES ('sample_follow_item', ?, ?, 'body', 1, 1, 901)
	`, channelID, channelID); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/channels/"+channelID+"/subscribe", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !srv.db.IsChannelFollowed(channelID) {
		t.Fatal("expected existing temp channel to gain a follow row")
	}
	_ = mutationOwnerRevision(t, srv, "channel_follow", channelID)
	var scoredAt int64
	if err := srv.db.QueryRow(`
		SELECT algo_scored_at FROM feed_items WHERE tweet_id = 'sample_follow_item'
	`).Scan(&scoredAt); err != nil {
		t.Fatal(err)
	}
	if scoredAt != 901 {
		t.Fatalf("response-path algo_scored_at = %d, want 901", scoredAt)
	}
	processQueuedFeedOrderInvalidations(t, srv)
	if err := srv.db.QueryRow(`
		SELECT algo_scored_at FROM feed_items WHERE tweet_id = 'sample_follow_item'
	`).Scan(&scoredAt); err != nil {
		t.Fatal(err)
	}
	if scoredAt != 0 {
		t.Fatalf("queued algo_scored_at = %d, want 0", scoredAt)
	}
	var body struct {
		Success    bool   `json:"success"`
		ChannelID  string `json:"channel_id"`
		Subscribed bool   `json:"subscribed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v; body = %s", err, rec.Body.String())
	}
	if !body.Success || !body.Subscribed || body.ChannelID != channelID {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestChannelSubscribeRouteRejectsUnknownChannel(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/channels/youtube_UCmissing/subscribe", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if srv.db.IsChannelFollowed("youtube_UCmissing") {
		t.Fatal("unknown channel should not create a follow row")
	}
}

func TestChannelSubscribeRouteCanonicalizesBareYouTubeID(t *testing.T) {
	srv := newTestServer(t)
	const rawID = "UCtempchannel"
	const channelID = "youtube_" + rawID
	if err := srv.db.AddChannel(model.Channel{
		ChannelID: channelID,
		SourceID:  rawID,
		Name:      "Temp Channel",
		URL:       "https://www.youtube.com/channel/" + rawID,
		Platform:  "youtube",
	}); err != nil {
		t.Fatalf("AddChannel: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/channels/"+rawID+"/subscribe", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !srv.db.IsChannelFollowed(channelID) {
		t.Fatal("expected canonical channel to gain a follow row")
	}
	if srv.db.IsChannelFollowed(rawID) {
		t.Fatal("bare YouTube id should not gain a follow row")
	}
}
