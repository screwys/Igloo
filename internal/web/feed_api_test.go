package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestHandleFeedMute_RequiresAuth(t *testing.T) {
	srv := newTestServer(t)

	for _, method := range []string{"POST", "DELETE"} {
		req := httptest.NewRequest(method, "/api/feed/mute/alice", nil)
		rr := httptest.NewRecorder()
		srv.mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s: got %d, want 401", method, rr.Code)
		}
	}
}

func TestHandleFeedMute_AuthedSucceeds(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("POST", "/api/feed/mute/alice", nil)
	req = attachTestAuth(req, "bob")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("post authed: got %d — %s", rr.Code, rr.Body.String())
	}
}

func TestHandleFeedMutedListKeepsHandleArrayContract(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.db.ExecRaw(`
		INSERT INTO channel_profiles (channel_id, platform, handle)
		VALUES ('tiktok_sample_account', 'tiktok', 'sample_account');
		INSERT INTO muted_channels (channel_id, muted_at)
		VALUES ('tiktok_sample_account', 1);
	`); err != nil {
		t.Fatalf("seed muted account: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/feed/muted", nil)
	req = attachTestAuth(req, "sample_user")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/feed/muted: got %d — %s", rr.Code, rr.Body.String())
	}

	var body struct {
		Muted []string `json:"muted"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Muted) != 1 || body.Muted[0] != "sample_account" {
		t.Fatalf("muted = %#v, want handle array", body.Muted)
	}
}

func TestFeedRankedEndpointRemovedForAndroidSyncContract(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/feed/ranked", nil)
	req = attachTestAuth(req, "alice")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET /api/feed/ranked status = %d, want 404", rr.Code)
	}
}

func TestAndroidSyncFeedItemBuildsCanonicalURLs(t *testing.T) {
	item := androidSyncFeedItemFromModel(model.FeedItem{
		TweetID:           "tw_1",
		SourceHandle:      "source_user",
		AuthorHandle:      "@alice",
		QuoteTweetID:      "quote_1",
		QuoteAuthorHandle: "@bob",
	})

	if item.CanonicalURL != "https://x.com/i/status/tw_1" {
		t.Fatalf("canonical_url = %q", item.CanonicalURL)
	}
	if item.QuoteCanonicalURL != "https://x.com/i/status/quote_1" {
		t.Fatalf("quote_canonical_url = %q", item.QuoteCanonicalURL)
	}
}

func TestAndroidSyncFeedItemPreservesStoredCanonicalURLs(t *testing.T) {
	item := androidSyncFeedItemFromModel(model.FeedItem{
		TweetID:           "tw_1",
		AuthorHandle:      "alice",
		CanonicalURL:      "https://example.invalid/canonical",
		QuoteTweetID:      "quote_1",
		QuoteAuthorHandle: "bob",
		QuoteCanonicalURL: "https://example.invalid/quote",
	})

	if item.CanonicalURL != "https://example.invalid/canonical" {
		t.Fatalf("canonical_url = %q", item.CanonicalURL)
	}
	if item.QuoteCanonicalURL != "https://example.invalid/quote" {
		t.Fatalf("quote_canonical_url = %q", item.QuoteCanonicalURL)
	}
}

func TestAndroidSyncFeedItemBuildsHandleIndependentCanonicalURL(t *testing.T) {
	tweetID := "sample_tweet"
	sourceHandle := "sample_source"
	placeholderAuthor := "unknown"
	item := androidSyncFeedItemFromModel(model.FeedItem{
		TweetID:      tweetID,
		SourceHandle: sourceHandle,
		AuthorHandle: placeholderAuthor,
	})

	want := "https://x.com/i/status/" + tweetID
	if item.CanonicalURL != want {
		t.Fatalf("canonical_url = %q", item.CanonicalURL)
	}
}

func TestAndroidSyncFeedItemIncludesTranslationSourceLabels(t *testing.T) {
	item := androidSyncFeedItemFromModel(model.FeedItem{
		TweetID:          "tw_translated",
		AuthorHandle:     "alice",
		BodyTranslation:  "hello",
		BodySourceLang:   "Korean",
		QuoteTweetID:     "quote_1",
		QuoteTranslation: "quoted hello",
		QuoteSourceLang:  "Japanese",
	})

	if item.BodySourceLang != "Korean" {
		t.Fatalf("body_source_lang = %q, want Korean", item.BodySourceLang)
	}
	if item.QuoteSourceLang != "Japanese" {
		t.Fatalf("quote_source_lang = %q, want Japanese", item.QuoteSourceLang)
	}
}

func TestHandleFeedBookmarked_RequiresAuth(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/feed/bookmarked", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rr.Code)
	}
}

func TestHandleFeedInteraction_BookmarkActionRejected(t *testing.T) {
	srv := newTestServer(t)

	body := strings.NewReader(`{"action":"bookmark","tweet_id":"x"}`)
	req := httptest.NewRequest("POST", "/api/feed/interaction", body)
	req.Header.Set("Content-Type", "application/json")
	req = attachTestAuth(req, "alice")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("bookmark via interaction: got %d, want 400", rr.Code)
	}
}

func TestHandleFeedInteractionQueuesAppliedFeedOrderChanges(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.db.ExecRaw(`
		INSERT INTO feed_items (
			tweet_id, channel_id, source_channel_id, body_text, published_at, fetched_at, algo_scored_at
		) VALUES
			('sample_interaction_like', 'twitter_other', 'twitter_other', 'like', 2, 2, 810),
			('sample_interaction_mute', 'twitter_sample_action', 'twitter_sample_action', 'mute', 1, 1, 820)
	`); err != nil {
		t.Fatal(err)
	}

	for _, body := range []string{
		`{"action":"like","tweet_id":"sample_interaction_like","item":{}}`,
		`{"action":"mute","item":{"source_handle":"sample_action"}}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/feed/interaction", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = attachTestAuth(req, "sample")
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("body %s status = %d response = %s", body, rec.Code, rec.Body.String())
		}
	}

	_ = mutationOwnerRevision(t, srv, "feed_like", "sample_interaction_like")
	_ = mutationOwnerRevision(t, srv, "muted_channel", "twitter_sample_action")
	for tweetID, want := range map[string]int64{
		"sample_interaction_like": 810,
		"sample_interaction_mute": 820,
	} {
		var scoredAt int64
		if err := srv.db.QueryRow(`SELECT algo_scored_at FROM feed_items WHERE tweet_id = ?`, tweetID).Scan(&scoredAt); err != nil {
			t.Fatal(err)
		}
		if scoredAt != want {
			t.Fatalf("%s response-path algo_scored_at = %d, want %d", tweetID, scoredAt, want)
		}
	}

	processQueuedFeedOrderInvalidations(t, srv)
	for _, tweetID := range []string{"sample_interaction_like", "sample_interaction_mute"} {
		var scoredAt int64
		if err := srv.db.QueryRow(`SELECT algo_scored_at FROM feed_items WHERE tweet_id = ?`, tweetID).Scan(&scoredAt); err != nil {
			t.Fatal(err)
		}
		if scoredAt != 0 {
			t.Fatalf("%s queued algo_scored_at = %d, want 0", tweetID, scoredAt)
		}
	}
}

func TestHandleFeedLikePublishesItsStateOwners(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.db.ExecRaw(`
		INSERT INTO feed_items (tweet_id, body_text, algo_scored_at)
		VALUES ('sample_like_once', 'body', 456)
	`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/feed/like/sample_like_once", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req = attachTestAuth(req, "alice")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d - %s", rr.Code, rr.Body.String())
	}

	_ = mutationOwnerRevision(t, srv, "feed_like", "sample_like_once")
	_ = mutationOwnerRevision(t, srv, "feed_seen", "sample_like_once")
	var liked, seen int
	if err := srv.db.QueryRow(`SELECT COUNT(*) FROM feed_likes WHERE tweet_id = 'sample_like_once'`).Scan(&liked); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.QueryRow(`SELECT COUNT(*) FROM feed_seen WHERE tweet_id = 'sample_like_once'`).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if liked != 1 || seen != 1 {
		t.Fatalf("like state = liked %d seen %d", liked, seen)
	}
	var scoredAt int64
	if err := srv.db.QueryRow(`
		SELECT algo_scored_at FROM feed_items WHERE tweet_id = 'sample_like_once'
	`).Scan(&scoredAt); err != nil {
		t.Fatal(err)
	}
	if scoredAt != 456 {
		t.Fatalf("response-path algo_scored_at = %d, want 456", scoredAt)
	}
	processQueuedFeedOrderInvalidations(t, srv)
	if err := srv.db.QueryRow(`
		SELECT algo_scored_at FROM feed_items WHERE tweet_id = 'sample_like_once'
	`).Scan(&scoredAt); err != nil {
		t.Fatal(err)
	}
	if scoredAt != 0 {
		t.Fatalf("queued algo_scored_at = %d, want 0", scoredAt)
	}
}

func TestHandleFeedSeenBatchDoesNotInvalidateFeedRanking(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.db.ExecRaw(`INSERT INTO feed_items
		(tweet_id, source_channel_id, channel_id, body_text, published_at, algo_interest, algo_scored_at)
		VALUES
		  ('tw_seen_ranked_a', 'twitter_sample_seen', 'twitter_sample_seen', 'body', 1000, 1.0, 12345),
		  ('tw_seen_ranked_b', 'twitter_sample_seen', 'twitter_sample_seen', 'body', 1001, 1.0, 12346)`); err != nil {
		t.Fatal(err)
	}

	seenReq := httptest.NewRequest("POST", "/api/feed/seen", strings.NewReader(`{"tweet_ids":["tw_seen_ranked_a","tw_seen_ranked_b"]}`))
	seenReq.Header.Set("Content-Type", "application/json")
	seenReq = attachTestAuth(seenReq, "alice")
	seenRec := httptest.NewRecorder()
	srv.mux.ServeHTTP(seenRec, seenReq)
	if seenRec.Code != http.StatusOK {
		t.Fatalf("seen status: got %d - %s", seenRec.Code, seenRec.Body.String())
	}

	var seenCount int
	if err := srv.db.QueryRow(`SELECT COUNT(*) FROM feed_seen WHERE tweet_id IN ('tw_seen_ranked_a', 'tw_seen_ranked_b')`).Scan(&seenCount); err != nil {
		t.Fatal(err)
	}
	if seenCount != 2 {
		t.Fatalf("seen rows = %d, want 2", seenCount)
	}
	var scoredAt int64
	if err := srv.db.QueryRow(`SELECT algo_scored_at FROM feed_items WHERE tweet_id='tw_seen_ranked_a'`).Scan(&scoredAt); err != nil {
		t.Fatal(err)
	}
	if scoredAt != 12345 {
		t.Fatalf("algo_scored_at after seen = %d, want unchanged 12345", scoredAt)
	}
}
