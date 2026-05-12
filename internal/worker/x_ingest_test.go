package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/model"
)

type fakeXFeedFetcher struct {
	timeline func(context.Context, string, int) ([]model.FeedItem, error)
	source   func(context.Context, string, int) ([]model.FeedItem, error)
}

func (f fakeXFeedFetcher) FetchTimeline(ctx context.Context, handle string, limit int) ([]model.FeedItem, error) {
	return f.timeline(ctx, handle, limit)
}

func (f fakeXFeedFetcher) FetchSource(ctx context.Context, rawURL string, limit int) ([]model.FeedItem, error) {
	return f.source(ctx, rawURL, limit)
}

func TestDownloadNewAuthorAvatars_QueuesProfileFallbackForPlaceholderURL(t *testing.T) {
	d := newTestWorkerDB(t)
	m := &Manager{
		db:            d,
		cfg:           testCfg(t.TempDir()),
		avatarRequest: make(chan string, 1),
	}

	m.downloadNewAuthorAvatars(context.Background(), []model.FeedItem{{
		AuthorHandle:    "UserAlpha",
		AuthorAvatarURL: "https://x.com/example_account/status/undefined",
	}})

	select {
	case got := <-m.avatarRequest:
		if got != "twitter_useralpha" {
			t.Fatalf("queued channelID = %q, want %q", got, "twitter_useralpha")
		}
	default:
		t.Fatal("expected profile fallback request")
	}
}

func TestPrimeFeedItemProfilesSeedsProfileRowsAndQueuesRecovery(t *testing.T) {
	d := newTestWorkerDB(t)
	m := &Manager{
		db:            d,
		cfg:           testCfg(t.TempDir()),
		avatarRequest: make(chan string, 4),
	}

	m.primeFeedItemProfiles(context.Background(), []model.FeedItem{{
		AuthorHandle:      "UserAlpha",
		AuthorDisplayName: "User Alpha",
	}})

	got, err := d.GetChannelProfile("twitter_useralpha")
	if err != nil {
		t.Fatalf("GetChannelProfile: %v", err)
	}
	if got == nil || got.Handle != "useralpha" || got.DisplayName != "User Alpha" || got.FetchedAt != nil {
		t.Fatalf("profile row not primed: %+v", got)
	}
	select {
	case queued := <-m.avatarRequest:
		if queued != "twitter_useralpha" {
			t.Fatalf("queued channelID = %q, want twitter_useralpha", queued)
		}
	default:
		t.Fatal("expected profile recovery request")
	}
}

func TestFeedMediaJobRowsForItemsRespectsMediaDownloadLimit(t *testing.T) {
	items := []model.FeedItem{
		{TweetID: "text_only", SourceHandle: "twitter_alice"},
		{TweetID: "media_1", SourceHandle: "twitter_alice", CanonicalURL: "https://x.com/alice/status/1", MediaJSON: `[{"url":"https://cdn.example/1.jpg","type":"photo"}]`},
		{TweetID: "media_2", SourceHandle: "twitter_alice", CanonicalURL: "https://x.com/alice/status/2", MediaJSON: `[{"url":"https://cdn.example/2.mp4","type":"video"}]`},
		{TweetID: "media_3", SourceHandle: "twitter_alice", CanonicalURL: "https://x.com/alice/status/3", MediaJSON: `[{"url":"https://cdn.example/3.jpg","type":"photo"}]`},
	}

	jobs := feedMediaJobRowsForItems(items, &db.ChannelSettings{MediaDownloadLimit: 2})
	if len(jobs) != 2 {
		t.Fatalf("jobs len = %d, want 2: %+v", len(jobs), jobs)
	}
	if jobs[0].TweetID != "media_1" || jobs[1].TweetID != "media_2" {
		t.Fatalf("job IDs = %q, %q; want media_1, media_2", jobs[0].TweetID, jobs[1].TweetID)
	}
}

func TestFeedMediaJobRowsForItemsUsesQuoteMedia(t *testing.T) {
	items := []model.FeedItem{{
		TweetID:           "quote_only",
		SourceHandle:      "twitter_alice",
		CanonicalURL:      "https://x.com/alice/status/quote",
		QuoteTweetID:      "quoted_post",
		QuoteMediaJSON:    `[{"url":"https://cdn.example/q1.jpg","type":"photo"},{"url":"https://cdn.example/q2.jpg","type":"photo"}]`,
		QuoteAuthorHandle: "bob",
	}}

	jobs := feedMediaJobRowsForItems(items, &db.ChannelSettings{MediaDownloadLimit: 20})
	if len(jobs) != 1 {
		t.Fatalf("jobs len = %d, want 1: %+v", len(jobs), jobs)
	}
	if jobs[0].TweetID != "quote_only" {
		t.Fatalf("job tweet = %q, want quote_only", jobs[0].TweetID)
	}
	if jobs[0].MediaKind != "image" {
		t.Fatalf("job media kind = %q, want image", jobs[0].MediaKind)
	}
	if jobs[0].SlideCount != 2 {
		t.Fatalf("job slide count = %d, want 2", jobs[0].SlideCount)
	}
}

func TestFetchOneChannelUsesXFeedFetcherAndQueuesMedia(t *testing.T) {
	d := newTestWorkerDB(t)
	m := &Manager{
		db:            d,
		cfg:           testCfg(t.TempDir()),
		downloader:    testDownloader(),
		avatarRequest: make(chan string, 1),
		xFeedFetcher: fakeXFeedFetcher{
			timeline: func(_ context.Context, handle string, limit int) ([]model.FeedItem, error) {
				if handle != "sample_user" {
					t.Fatalf("handle = %q, want sample_user", handle)
				}
				if limit != 100 {
					t.Fatalf("limit = %d, want 100", limit)
				}
				return []model.FeedItem{{
					TweetID:          "1000000000000000100",
					SourceHandle:     "sample_user",
					AuthorHandle:     "sample_user",
					BodyText:         "post with media",
					MediaJSON:        `[{"url":"https://pbs.twimg.com/media/sample.jpg","type":"photo"}]`,
					CanonicalURL:     "https://x.com/sample_user/status/1000000000000000100",
					ContentHash:      "hash",
					CanonicalTweetID: "1000000000000000100",
				}}, nil
			},
			source: func(context.Context, string, int) ([]model.FeedItem, error) {
				t.Fatal("source fetch should not be called")
				return nil, nil
			},
		},
	}

	n, err := m.FetchOneChannel(context.Background(), "twitter_sample_user")
	if err != nil {
		t.Fatalf("FetchOneChannel: %v", err)
	}
	if n != 1 {
		t.Fatalf("upserted = %d, want 1", n)
	}
	got, err := d.GetFeedItemByTweetID("1000000000000000100")
	if err != nil {
		t.Fatalf("GetFeedItemByTweetID: %v", err)
	}
	if got == nil || got.BodyText != "post with media" {
		t.Fatalf("feed item = %+v", got)
	}
	queued, processing, err := d.CountPendingFeedMediaJobs()
	if err != nil {
		t.Fatalf("CountPendingFeedMediaJobs: %v", err)
	}
	if queued+processing != 1 {
		t.Fatalf("media jobs = queued %d processing %d, want 1 total", queued, processing)
	}
}

func TestFetchOneChannelRecordsFailureBackoff(t *testing.T) {
	d := newTestWorkerDB(t)
	m := &Manager{
		db:            d,
		cfg:           testCfg(t.TempDir()),
		downloader:    testDownloader(),
		avatarRequest: make(chan string, 1),
		xFeedFetcher: fakeXFeedFetcher{
			timeline: func(context.Context, string, int) ([]model.FeedItem, error) {
				return nil, errors.New("HTTP 429: Too Many Requests")
			},
			source: func(context.Context, string, int) ([]model.FeedItem, error) {
				t.Fatal("source fetch should not be called")
				return nil, nil
			},
		},
	}

	if _, err := m.FetchOneChannel(context.Background(), "twitter_sample_user"); err == nil {
		t.Fatal("expected fetch error")
	}
	state, err := d.GetIngestState("twitter_sample_user")
	if err != nil {
		t.Fatalf("GetIngestState: %v", err)
	}
	if state.FailCount != 1 || !strings.Contains(state.LastError, "Too Many Requests") {
		t.Fatalf("state = %+v", state)
	}
	if state.NextRetryAt <= float64(time.Now().Unix()) {
		t.Fatalf("next_retry_at = %f, want future", state.NextRetryAt)
	}
}

func TestFetchOneFeedSourceRecordsAttribution(t *testing.T) {
	d := newTestWorkerDB(t)
	if err := d.UpsertFeedSource(model.FeedSource{
		SourceID:   "twitter_list_sample",
		Platform:   "twitter",
		SourceType: "list",
		ExternalID: "sample",
		Label:      "Sample list",
		URL:        "https://x.com/i/lists/123",
		Enabled:    true,
	}); err != nil {
		t.Fatalf("UpsertFeedSource: %v", err)
	}
	m := &Manager{
		db:            d,
		cfg:           testCfg(t.TempDir()),
		downloader:    testDownloader(),
		avatarRequest: make(chan string, 1),
		xFeedFetcher: fakeXFeedFetcher{
			timeline: func(context.Context, string, int) ([]model.FeedItem, error) {
				t.Fatal("timeline fetch should not be called")
				return nil, nil
			},
			source: func(_ context.Context, rawURL string, limit int) ([]model.FeedItem, error) {
				if rawURL != "https://x.com/i/lists/123" {
					t.Fatalf("rawURL = %q", rawURL)
				}
				if limit != 100 {
					t.Fatalf("limit = %d, want 100", limit)
				}
				return []model.FeedItem{{
					TweetID:          "1000000000000000200",
					SourceHandle:     "source_author",
					AuthorHandle:     "source_author",
					BodyText:         "list post",
					CanonicalURL:     "https://x.com/source_author/status/1000000000000000200",
					ContentHash:      "hash2",
					CanonicalTweetID: "1000000000000000200",
				}}, nil
			},
		},
	}

	n, err := m.FetchOneFeedSource(context.Background(), "twitter_list_sample")
	if err != nil {
		t.Fatalf("FetchOneFeedSource: %v", err)
	}
	if n != 1 {
		t.Fatalf("upserted = %d, want 1", n)
	}
	items, err := d.ListFeedItemsBySourceID("twitter_list_sample", 10)
	if err != nil {
		t.Fatalf("ListFeedItemsBySourceID: %v", err)
	}
	if len(items) != 1 || items[0].TweetID != "1000000000000000200" {
		t.Fatalf("source items = %+v", items)
	}
}
