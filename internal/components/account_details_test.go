package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestAccountDetailsPreserveUnknownLocationAccuracy(t *testing.T) {
	for _, tc := range []struct {
		details string
		warning bool
	}{
		{`{"source":"Web","user_id":"123","username_changes":0}`, false},
		{`{"location_accurate":true}`, false},
		{`{"location_accurate":false}`, true},
	} {
		var buf bytes.Buffer
		if err := accountDetailsBadge(newTestPageProps(), "United States", tc.details, "Sample <name>", "sample").Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		html := buf.String()
		if strings.Contains(html, "VPN or proxy") != tc.warning {
			t.Fatalf("warning mismatch: %s", html)
		}
		for _, value := range []string{"data-account-details-content", "Sample &lt;name&gt;", "🇺🇸"} {
			if !strings.Contains(html, value) {
				t.Fatalf("missing %s: %s", value, html)
			}
		}
		if strings.Contains(tc.details, "username_changes") && !strings.Contains(html, "<dd>0</dd>") {
			t.Fatalf("zero changes omitted: %s", html)
		}
	}
}

func TestFeedActionsReplyLikeBookmarkShareOpenOrder(t *testing.T) {
	var buf bytes.Buffer
	if err := feedActions(newTestPageProps(), model.FeedItem{TweetID: "123", AuthorHandle: "sample", CanonicalURL: "https://x.com/sample/status/123"}).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	previous := -1
	for _, marker := range []string{`data-feed-thread-open`, `data-feed-action="heart"`, `data-feed-action="bookmark"`, `data-feed-action="share"`, `data-feed-action="open"`} {
		at := strings.Index(html, marker)
		if at <= previous {
			t.Fatalf("missing or misplaced %s: %s", marker, html)
		}
		previous = at
	}
}
