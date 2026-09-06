package fxtwitter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchConversationAndQuotingPostsUseSinglePages(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		switch r.URL.Path {
		case "/2/conversation/100":
			_, _ = w.Write([]byte(`{"code":200,"status":{"id":"100","author":{"screen_name":"sample_author"},"replying_to":{"screen_name":"other_author","status":"90"}},"thread":[{"id":"90","author":{"screen_name":"other_author"},"text":"Parent"},{"type":"tombstone","id":"80"}],"replies":[{"id":"101","author":{"screen_name":"reply_author"},"replying_to":{"screen_name":"sample_author","status":"100"}}],"cursor":{"bottom":"unused-next-page"}}`))
		case "/2/status/100/quotes":
			_, _ = w.Write([]byte(`{"code":200,"results":[{"id":"102","author":{"screen_name":"quote_author"},"quote":{"id":"100","author":{"screen_name":"sample_author"}}}],"cursor":{"bottom":"unused-next-page"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), Timeout: time.Second}
	conversation, err := c.FetchConversation(context.Background(), "100")
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Status.ReplyToStatus != "90" || len(conversation.Thread) != 1 || conversation.Thread[0].AuthorHandle != "other_author" || len(conversation.Replies) != 1 || conversation.Replies[0].ReplyToStatus != "100" {
		t.Fatalf("conversation = %+v", conversation)
	}
	quotes, err := c.FetchQuotingPosts(context.Background(), "100")
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 1 || quotes[0].Quote == nil || quotes[0].Quote.ID != "100" {
		t.Fatalf("quotes = %+v", quotes)
	}
	if len(paths) != 2 || paths[0] != "/2/conversation/100" || paths[1] != "/2/status/100/quotes?count=20" {
		t.Fatalf("requests = %v", paths)
	}
}

func TestFetchQuotingPostsEmptyPage(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), Timeout: time.Second}
	quotes, err := c.FetchQuotingPosts(context.Background(), "100")
	if err != nil || len(quotes) != 0 {
		t.Fatalf("empty quotes = %v, %v", quotes, err)
	}
	if _, err := c.FetchConversation(context.Background(), "100"); err != ErrNotFound {
		t.Fatalf("missing conversation error = %v", err)
	}
}
