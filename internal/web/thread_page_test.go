package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/model"
)

func TestThreadResolvesCapturedQuote(t *testing.T) {
	ts := newTestServer(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := ts.db.UpsertFeedItems([]model.FeedItem{
		{TweetID: "sample_quoting", AuthorHandle: "sample_writer", BodyText: "quoting body", QuoteTweetID: "sample_quoted", QuoteAuthorHandle: "sample_author", QuoteBodyText: "captured quote body", QuoteLang: "en", QuotePublishedAt: &now, QuoteMediaJSON: `[{"type":"photo","url":"https://example.test/photo.jpg"}]`, PublishedAt: &now, FetchedAt: now},
		{TweetID: "sample_reply", AuthorHandle: "sample_reader", BodyText: "reply body", ReplyToStatus: "sample_quoted", IsReply: true, PublishedAt: &now, FetchedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/thread/sample_quoted", "/thread/sample_quoted?fmt=partial", "/thread/sample_reply"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ts.mux.ServeHTTP(rec, attachTestAuth(httptest.NewRequest(http.MethodGet, path, nil), "test_user"))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			for _, want := range []string{"captured quote body", "reply body", `data-thread-depth="1"`} {
				if !strings.Contains(rec.Body.String(), want) {
					t.Errorf("missing %q", want)
				}
			}
		})
	}
	rec := httptest.NewRecorder()
	ts.mux.ServeHTTP(rec, attachTestAuth(httptest.NewRequest(http.MethodGet, "/api/thread/sample_quoted", nil), "test_user"))
	var response struct {
		RootID string           `json:"root_id"`
		Thread []map[string]any `json:"thread"`
		Quotes []map[string]any `json:"quotes"`
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("API status = %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.RootID != "sample_quoted" || len(response.Thread) != 2 || len(response.Quotes) != 1 {
		t.Fatalf("unexpected thread: %+v", response)
	}
	quote := response.Thread[0]
	if quote["body_text"] != "captured quote body" || quote["author_handle"] != "sample_author" {
		t.Fatalf("unexpected quote: %+v", quote)
	}
	item, err := ts.db.GetThreadChain("sample_quoted")
	if err != nil || len(item) != 1 || len(item[0].Media) != 1 || item[0].PublishedAt == nil || !item[0].PublishedAt.Equal(now) {
		t.Fatalf("quote lost media or timestamp: %+v, %v", item, err)
	}
	if _, err := ts.db.UpsertFeedItems([]model.FeedItem{{TweetID: "sample_quoted", AuthorHandle: "sample_author", BodyText: "full original body", PublishedAt: &now, FetchedAt: now}}); err != nil {
		t.Fatal(err)
	}
	items, err := ts.db.GetThreadTree("sample_quoted")
	if err != nil || len(items) != 2 || items[0].BodyText != "full original body" {
		t.Fatalf("standalone post should win: %+v, %v", items, err)
	}
}

func TestHandlePageThreadRendersAllReplyBranches(t *testing.T) {
	ts := newTestServer(t)
	rootAt := time.Unix(100, 0).UTC()
	leafAt := time.Unix(110, 0).UTC()
	siblingAt := time.Unix(120, 0).UTC()
	if _, err := ts.db.UpsertFeedItems([]model.FeedItem{
		{TweetID: "sample_root", AuthorHandle: "sample_root_author", BodyText: "root body", PublishedAt: &rootAt, FetchedAt: rootAt, ContentHash: "sample_thread_root"},
		{TweetID: "sample_leaf", AuthorHandle: "sample_author", BodyText: "leaf body", IsReply: true, ReplyToHandle: "sample_root_author", ReplyToStatus: "sample_root", PublishedAt: &leafAt, FetchedAt: leafAt, ContentHash: "sample_thread_leaf"},
		{TweetID: "sample_sibling", AuthorHandle: "sample_author_b", BodyText: "sibling body", IsReply: true, ReplyToHandle: "sample_root_author", ReplyToStatus: "sample_root", PublishedAt: &siblingAt, FetchedAt: siblingAt, ContentHash: "sample_thread_sibling"},
		{TweetID: "sample_quote", AuthorHandle: "sample_quoting_author", BodyText: "quoting body", QuoteTweetID: "sample_leaf", IsGhost: true, PublishedAt: &siblingAt, FetchedAt: siblingAt, ContentHash: "sample_thread_quote"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/thread/sample_leaf", nil)
	req = attachTestAuth(req, "test_user")
	rec := httptest.NewRecorder()
	ts.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`root body`, `leaf body`, `data-thread-back-link`, `href="/feed"`, `data-thread-reply`, `data-thread-depth="1"`, `data-thread-id="sample_leaf"`, `data-thread-fetch-status`, `data-thread-quotes`, `quoting body`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in body: %s", want, body)
		}
	}
	if !strings.Contains(body, `sibling body`) {
		t.Fatalf("thread route should include sibling reply branches: %s", body)
	}
}

func TestHandlePageThreadRendersPartialThreadRoute(t *testing.T) {
	ts := newTestServer(t)
	now := time.Now().UTC()
	if _, err := ts.db.UpsertFeedItems([]model.FeedItem{
		{TweetID: "sample_root", AuthorHandle: "sample_root_author", BodyText: "root body", PublishedAt: &now, FetchedAt: now, ContentHash: "sample_thread_root"},
		{TweetID: "sample_leaf", AuthorHandle: "sample_author", BodyText: "leaf body", IsReply: true, ReplyToHandle: "sample_root_author", ReplyToStatus: "sample_root", PublishedAt: &now, FetchedAt: now, ContentHash: "sample_thread_leaf"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/thread/sample_leaf?fmt=partial", nil)
	req = attachTestAuth(req, "test_user")
	rec := httptest.NewRecorder()
	ts.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`root body`, `leaf body`, `data-thread-route`, `id="thread-feed-list"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in body: %s", want, body)
		}
	}
	if strings.Contains(body, `<html`) || strings.Contains(body, `id="feed-list"`) {
		t.Fatalf("partial rendered full page or duplicate feed-list: %s", body)
	}
}

func TestHandlePageThreadReturnsNotFoundForMissingTweet(t *testing.T) {
	ts := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/thread/missing_tweet", nil)
	req = attachTestAuth(req, "test_user")
	rec := httptest.NewRecorder()
	ts.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
