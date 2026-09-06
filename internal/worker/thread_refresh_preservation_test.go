package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/fxtwitter"
	"github.com/screwys/igloo/internal/model"
)

func TestRefreshThreadPreservesCapturedContentAndUpdatesPoll(t *testing.T) {
	d := newTestWorkerDB(t)
	now := time.Now().UTC()
	original := model.FeedItem{
		TweetID: "100", SourceHandle: "source_author", AuthorHandle: "sample_author",
		BodyText: "Captured body", ArticleTitle: "Captured article", ContentHash: "captured_hash",
		MediaJSON:    `[{"type":"photo","url":"https://pbs.twimg.com/media/captured.jpg"}]`,
		QuoteTweetID: "200", QuoteAuthorHandle: "quote_author", QuoteBodyText: "Captured quote body", QuoteArticleTitle: "Captured quote article",
		QuoteMediaJSON: `[{"type":"photo","url":"https://pbs.twimg.com/media/quoted.jpg"}]`,
		PublishedAt:    &now, FetchedAt: now,
	}
	if _, err := d.UpsertFeedItems([]model.FeedItem{original}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2/conversation/100" {
			_, _ = w.Write([]byte(`{"code":200,"status":{"id":"100","author":{"screen_name":"sample_author"},"article":{"title":"Different article","content":{"blocks":[{"text":"Different body"}]}},"media":{"all":[{"type":"photo","url":"https://pbs.twimg.com/media/replacement.jpg"}]},"poll":{"choices":[{"label":"First","count":5,"percentage":100}],"total_votes":5,"ends_at":"2026-09-07T12:00:00Z"},"community_note":{"text":"New note"},"quote":{"id":"200","author":{"screen_name":"quote_author"},"article":{"title":"Different quote article","content":{"blocks":[{"text":"Different quote body"}]}},"media":{"all":[{"type":"photo","url":"https://pbs.twimg.com/media/quote-replacement.jpg"}]}}},"thread":[],"replies":[]}`))
		} else {
			_, _ = w.Write([]byte(`{"code":200,"results":[]}`))
		}
	}))
	defer srv.Close()
	m := &Manager{db: d, replyResolver: NewReplyResolver(d, &fxtwitter.Client{BaseURL: srv.URL, HTTP: srv.Client(), Timeout: time.Second})}
	if _, err := m.RefreshThread(context.Background(), "100"); err != nil {
		t.Fatal(err)
	}
	stored, err := d.GetFeedItemByTweetID("100")
	if err != nil {
		t.Fatal(err)
	}
	if stored.IsGhost || stored.ContentHash != original.ContentHash || stored.BodyText != original.BodyText || stored.ArticleTitle != original.ArticleTitle || stored.MediaJSON != original.MediaJSON || stored.QuoteBodyText != original.QuoteBodyText || stored.QuoteArticleTitle != original.QuoteArticleTitle || stored.QuoteMediaJSON != original.QuoteMediaJSON || stored.SourceChannelID != "twitter_source_author" {
		t.Fatalf("thread refresh changed captured content identity: %+v", stored)
	}
	if poll := model.ParsePoll(stored.PollJSON); poll == nil || poll.TotalVotes != 5 || stored.CommunityNote != "New note" {
		t.Fatalf("new context not retained: %+v", stored)
	}
	for owner, want := range map[string]string{"100": "https://pbs.twimg.com/media/captured.jpg", "200": "https://pbs.twimg.com/media/quoted.jpg"} {
		var source string
		if err := d.QueryRow(`SELECT desired.source_url FROM assets a JOIN media_objects desired ON desired.object_id = a.desired_object_id WHERE a.owner_kind='tweet' AND a.owner_id=? AND a.asset_kind='post_media' AND a.media_index=0`, owner).Scan(&source); err != nil {
			t.Fatal(err)
		}
		if source != want {
			t.Fatalf("pending asset for %s changed to %q, want %q", owner, source, want)
		}
	}
}
