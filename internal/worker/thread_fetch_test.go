package worker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/fxtwitter"
	"github.com/screwys/igloo/internal/model"
)

func TestRefreshThreadStoresRepliesAndQuotesAsContextWhenAutomaticDisabled(t *testing.T) {
	d := newTestWorkerDB(t)
	if err := d.SetSetting("x_thread_fetch_mode", "never"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertFeedItems([]model.FeedItem{{TweetID: "100", AuthorHandle: "sample_author", BodyText: "Root", ContentHash: "root", FetchedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/2/conversation/100" {
			_, _ = w.Write([]byte(`{"code":200,"status":{"id":"100","text":"Root","author":{"screen_name":"sample_author"}},"thread":[{"id":"100","text":"Root","author":{"screen_name":"sample_author"}}],"replies":[{"id":"101","text":"Reply","author":{"screen_name":"reply_author"},"replying_to":{"screen_name":"sample_author","status":"100"},"media":{"all":[{"type":"photo","url":"https://pbs.twimg.com/media/sample.jpg"}]}}]}`))
		} else if r.URL.Path == "/2/status/100/quotes" {
			_, _ = w.Write([]byte(`{"code":200,"results":[{"id":"102","text":"Quote","author":{"screen_name":"quote_author"},"quote":{"type":"tombstone","id":"100"}}]}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	m := &Manager{db: d, replyResolver: NewReplyResolver(d, &fxtwitter.Client{BaseURL: srv.URL, HTTP: srv.Client(), Timeout: time.Second})}
	n, err := m.RefreshThread(context.Background(), "100")
	if err != nil || n != 3 || requests != 2 {
		t.Fatalf("refresh = %d, %v; requests=%d", n, err, requests)
	}
	root, _ := d.GetFeedItemByTweetID("100")
	reply, _ := d.GetFeedItemByTweetID("101")
	quote, _ := d.GetFeedItemByTweetID("102")
	if root == nil || root.IsGhost || reply == nil || !reply.IsGhost || reply.ReplyToStatus != "100" || quote == nil || !quote.IsGhost || quote.QuoteTweetID != "100" || quote.ReplyToStatus != "" {
		t.Fatalf("context ownership: root=%+v reply=%+v quote=%+v", root, reply, quote)
	}
	quotes, err := d.ListThreadQuotes("100", 20)
	if err != nil || len(quotes) != 1 || quotes[0].TweetID != "102" {
		t.Fatalf("quote lookup = %+v, %v", quotes, err)
	}
	var assets, profiles int
	if err := d.QueryRow(`SELECT COUNT(*) FROM assets WHERE owner_kind='tweet' AND owner_id='101'`).Scan(&assets); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM profile_jobs WHERE channel_id IN ('twitter_reply_author','twitter_quote_author')`).Scan(&profiles); err != nil {
		t.Fatal(err)
	}
	if assets == 0 || profiles != 2 {
		t.Fatalf("convergence work: assets=%d profiles=%d", assets, profiles)
	}
}

func TestRefreshThreadResolvesStoredRepostAfterSuccessfulFetch(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprintf("mismatch=%v", mismatch), func(t *testing.T) {
			d := newTestWorkerDB(t)
			const media = `[{"type":"photo","url":"https://pbs.twimg.com/media/captured.jpg"}]`
			if _, err := d.UpsertFeedItems([]model.FeedItem{{
				TweetID: "200", CanonicalTweetID: "100", CanonicalURL: "https://x.com/sample_author/status/100",
				AuthorHandle: "sample_author", SourceHandle: "sample_reposter", RetweetedByHandle: "sample_reposter", IsRetweet: true,
				BodyText: "Captured original", MediaJSON: media, ContentHash: "captured-original", FetchedAt: time.Now(),
			}}); err != nil {
				t.Fatal(err)
			}
			before, err := d.GetThreadTree("200")
			if err != nil || len(before) != 1 || before[0].TweetID != "200" || !before[0].IsRetweet {
				t.Fatalf("initial repost thread = %+v, %v", before, err)
			}
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				switch r.URL.Path {
				case "/2/conversation/100":
					id := "100"
					if mismatch {
						id = "999"
					}
					_, _ = fmt.Fprintf(w, `{"code":200,"status":{"id":%q,"text":"Remote original","author":{"screen_name":"sample_author"}},"replies":[{"id":"101","text":"Reply","author":{"screen_name":"reply_author"},"replying_to":{"screen_name":"sample_author","status":"100"}}]}`, id)
				case "/2/status/100/quotes":
					_, _ = w.Write([]byte(`{"code":200,"results":[{"id":"102","text":"Quote","author":{"screen_name":"quote_author"}}]}`))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			m := &Manager{db: d, replyResolver: NewReplyResolver(d, &fxtwitter.Client{BaseURL: srv.URL, HTTP: srv.Client(), Timeout: time.Second})}
			n, err := m.RefreshThread(context.Background(), "200")
			original, lookupErr := d.GetFeedItemByTweetID("100")
			if lookupErr != nil {
				t.Fatal(lookupErr)
			}
			if mismatch {
				if err == nil || n != 0 || original != nil || requests != 1 {
					t.Fatalf("mismatched response changed capture: n=%d err=%v original=%+v requests=%d", n, err, original, requests)
				}
				return
			}
			if err != nil || n != 3 || requests != 2 {
				t.Fatalf("refresh=%d, %v; requests=%d", n, err, requests)
			}
			if original == nil || original.IsGhost || original.IsRetweet || original.ContentHash != "captured-original" || original.BodyText != "Captured original" || original.MediaJSON != media {
				t.Fatalf("lost original capture: %+v", original)
			}
			after, err := d.GetThreadTree("200")
			if err != nil || len(after) != 2 || after[0].TweetID != "100" || after[1].TweetID != "101" {
				t.Fatalf("refreshed repost thread = %+v, %v", after, err)
			}
			quotes, err := d.ListThreadQuotes("200", 20)
			if err != nil || len(quotes) != 1 || quotes[0].TweetID != "102" {
				t.Fatalf("repost quotes = %+v, %v", quotes, err)
			}
			wrapper, err := d.GetFeedItemByTweetID("200")
			if err != nil || wrapper == nil || !wrapper.IsRetweet || wrapper.BodyText != "Captured original" || wrapper.MediaJSON != media {
				t.Fatalf("changed repost capture: %+v, %v", wrapper, err)
			}
		})
	}
}

func TestAutomaticThreadSelectionUsesNewRootsModeAndLimit(t *testing.T) {
	d := newTestWorkerDB(t)
	if err := d.ExecRaw(`INSERT INTO channel_follows(channel_id,followed_at) VALUES ('twitter_sample_author',1)`); err != nil {
		t.Fatal(err)
	}
	m := &Manager{db: d}
	items := []model.FeedItem{
		{TweetID: "100", AuthorHandle: "sample_author", ContentHash: "known", FetchedAt: time.Now()},
		{TweetID: "101"}, {TweetID: "105", IsReply: true}, {TweetID: "106", IsRetweet: true}, {TweetID: "107", IsGhost: true}, {TweetID: "102"}, {TweetID: "103"},
	}
	if _, err := d.UpsertFeedItems(items[:1]); err != nil {
		t.Fatal(err)
	}
	if err := d.SetSetting("x_thread_auto_post_limit", "2"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		mode    string
		starred bool
		want    string
	}{
		{"never", true, "[]"}, {"starred", false, "[]"}, {"starred", true, "[103 102]"}, {"always", false, "[103 102]"},
	} {
		if err := d.SetSetting("x_thread_fetch_mode", test.mode); err != nil {
			t.Fatal(err)
		}
		if err := d.ExecRaw(`DELETE FROM channel_stars`); err != nil {
			t.Fatal(err)
		}
		if test.starred {
			if err := d.ExecRaw(`INSERT INTO channel_stars(channel_id,starred_at) VALUES ('twitter_sample_author',1)`); err != nil {
				t.Fatal(err)
			}
		}
		got, err := m.newAutomaticThreadRoots("twitter_sample_author", items)
		if err != nil || fmt.Sprint(got) != test.want {
			t.Fatalf("mode=%s starred=%v roots=%v err=%v", test.mode, test.starred, got, err)
		}
	}
}

func TestReplyResolverUsesConversationAncestorsWithoutFetchingReplies(t *testing.T) {
	d := newTestWorkerDB(t)
	if _, err := d.UpsertFeedItems([]model.FeedItem{{TweetID: "103", AuthorHandle: "leaf_author", IsReply: true, ReplyToHandle: "other_author", ContentHash: "leaf", FetchedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/2/conversation/103" {
			t.Errorf("unexpected lookup %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"status":{"id":"103","author":{"screen_name":"leaf_author"},"replying_to":{"screen_name":"other_author","status":"102"}},"thread":[{"id":"102","text":"Parent","author":{"screen_name":"other_author"},"replying_to":{"screen_name":"root_author","status":"101"}},{"id":"101","text":"Root","author":{"screen_name":"root_author"}}],"replies":[{"id":"104","text":"Unrequested reply","author":{"screen_name":"reply_author"}}]}`))
	}))
	defer srv.Close()
	r := NewReplyResolver(d, &fxtwitter.Client{BaseURL: srv.URL, HTTP: srv.Client(), Timeout: time.Second})
	if err := r.ResolveCycle(context.Background(), []model.FeedItem{{TweetID: "103", AuthorHandle: "leaf_author", IsReply: true}}); err != nil {
		t.Fatal(err)
	}
	chain, err := d.GetThreadChain("103")
	if err != nil || len(chain) != 3 || requests != 1 {
		t.Fatalf("chain=%+v err=%v requests=%d", chain, err, requests)
	}
	if extra, err := d.GetFeedItemByTweetID("104"); err != nil || extra != nil {
		t.Fatalf("unrequested reply stored=%+v err=%v", extra, err)
	}
}
