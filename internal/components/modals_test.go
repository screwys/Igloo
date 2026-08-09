package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestFeedDashboardUnfollowButtonRemovesRowAndFormatsConfirm(t *testing.T) {
	p := newTestPageProps()
	d := FeedDashboardData{
		Sources: []FeedSourceEntry{
			{
				Handle:    "sample_handle",
				Status:    "failing",
				ItemCount: 3,
			},
		},
	}

	var buf bytes.Buffer
	if err := FeedDashboard(p, d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("FeedDashboard render: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		`class="feed-unfollow-link"`,
		`hx-delete="/api/unsubscribe/twitter_sample_handle"`,
		`hx-target="closest tr"`,
		`hx-swap="outerHTML"`,
		`@sample_handle`,
		`class="glass-btn feed-remove-failing"`,
		`hx-delete="/api/feed/failing-sources?filter="`,
		`Unfollow all failing feed sources?`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("FeedDashboard missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "%1$s") {
		t.Fatalf("FeedDashboard confirm prompt still contains raw placeholder:\n%s", html)
	}
}

func TestFeedDashboardDisablesRemoveFailingWithoutRedSources(t *testing.T) {
	var buf bytes.Buffer
	d := FeedDashboardData{Sources: []FeedSourceEntry{{Handle: "sample_cooling", Status: "cooling"}}}
	if err := FeedDashboard(newTestPageProps(), d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("FeedDashboard render: %v", err)
	}
	html := buf.String()
	start := strings.Index(html, `class="glass-btn feed-remove-failing"`)
	if start < 0 {
		t.Fatalf("remove failing button missing:\n%s", html)
	}
	end := strings.Index(html[start:], `</button>`)
	if end < 0 || !strings.Contains(html[start:start+end], `disabled`) {
		t.Fatalf("remove failing button should be disabled without red sources:\n%s", html)
	}
}

func TestMutedAccountsListUsesSidebarPlatformBadge(t *testing.T) {
	var buf bytes.Buffer
	if err := MutedAccountsList(newTestPageProps(), []model.MutedAccount{
		{
			ChannelID: "tiktok_sample_account",
			Handle:    "sample_account",
			Platform:  "tiktok",
		},
	}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("MutedAccountsList render: %v", err)
	}

	html := buf.String()
	for _, want := range []string{
		`@sample_account`,
		`channel-platform-label`,
		`muted-account-platform-label`,
		`plat-tiktok`,
		`TikTok`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("MutedAccountsList missing %q:\n%s", want, html)
		}
	}
}
