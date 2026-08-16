package components

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestFeedPageDoesNotRenderFeedSourceRail(t *testing.T) {
	p := newTestPageProps()
	p.ActiveNav = "feed"
	var buf bytes.Buffer
	err := FeedPage(p, nil, false, "", true, true, nil, "anchor").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("FeedPage render: %v", err)
	}
	if strings.Contains(buf.String(), `class="feed-source-rail"`) {
		t.Fatal("source rail should not render inside the feed page")
	}
}

func TestFeedPagePrioritizesOnlyInitialPostMedia(t *testing.T) {
	items := make([]model.FeedItem, 4)
	for i := range items {
		id := string(rune('a' + i))
		items[i] = model.FeedItem{
			TweetID:        "tweet_" + id,
			AuthorHandle:   "author_" + id,
			Media:          []model.MediaRef{{Type: "photo"}},
			MediaSlideURLs: []string{"/api/media/slide/tweet_" + id + "/0?owner_kind=tweet"},
		}
	}

	p := newTestPageProps()
	var buf bytes.Buffer
	if err := FeedPage(p, items, false, "", true, true, nil, "anchor").Render(context.Background(), &buf); err != nil {
		t.Fatalf("FeedPage render: %v", err)
	}
	html := buf.String()
	for _, id := range []string{"a", "b", "c"} {
		if strings.Contains(html, `src="/api/media/slide/tweet_`+id+`/0?owner_kind=tweet" loading="lazy"`) {
			t.Fatalf("initial post %s media was deferred; html=%s", id, html)
		}
	}
	if !strings.Contains(html, `src="/api/media/slide/tweet_d/0?owner_kind=tweet" loading="lazy"`) {
		t.Fatalf("fourth post media was not deferred; html=%s", html)
	}
}

func TestFeedKeyboardNavigationShortcuts(t *testing.T) {
	srcBytes, err := os.ReadFile("../../static/js/src/feed/index.js")
	if err != nil {
		t.Fatalf("read feed source: %v", err)
	}
	src := string(srcBytes)
	for _, want := range []string{
		"function scrollFeedCardBy(delta)",
		"function visibleFeedEntries()",
		"scope.querySelectorAll('[data-feed-item]')",
		"if (feedEntryVisible(entries[i])) visible.push(entries[i])",
		"if (event.key === 'j' || event.key === 'J')",
		"if (event.key === 'k' || event.key === 'K')",
		"next.scrollIntoView({ behavior: 'smooth', block: 'center' })",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("feed keyboard navigation missing %q", want)
		}
	}
}

func TestQuoteOverlayBookmarkUsesQuotedPostAccount(t *testing.T) {
	srcBytes, err := os.ReadFile("../../static/js/src/feed/index.js")
	if err != nil {
		t.Fatalf("read feed source: %v", err)
	}
	src := string(srcBytes)
	if !strings.Contains(src, "qCard2.getAttribute('data-quote-author-handle')") {
		t.Fatal("quote overlay bookmark should read the account from the quoted post")
	}
}
