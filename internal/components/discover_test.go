package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/model"
)

func TestDiscoveryCardUsesTemporaryWatchUntilMediaIsReady(t *testing.T) {
	publishedAt := time.Now().Add(-48 * time.Hour).Truncate(time.Millisecond)
	video := model.DiscoveryVideo{
		VideoID: "sample_discover", Title: "Sample Discover", ChannelID: "youtube_UCsample_discover",
		ChannelName: "Sample Creator", AvatarURL: "https://unavatar.io/youtube/sample_creator",
		ThumbnailURL: "https://i.ytimg.com/vi/sample_discover/mqdefault.jpg", Source: "related", PublishedAt: &publishedAt,
	}
	var buf bytes.Buffer
	if err := DiscoveryCard(newTestPageProps(), video).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, expected := range []string{
		`href="/temp/watch?v=sample_discover"`,
		`data-profile-channel-id="youtube_UCsample_discover"`,
		`class="video-date"`,
		`data-video-date="` + publishedAtStr(&publishedAt) + `"`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("missing %q in %s", expected, html)
		}
	}
	video.Ready = true
	buf.Reset()
	if err := DiscoveryCard(newTestPageProps(), video).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `href="/player/sample_discover"`) {
		t.Fatalf("ready card did not use player: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `class="discover-ready-badge"`) || !strings.Contains(buf.String(), `Ready`) {
		t.Fatalf("ready card did not render ready badge: %s", buf.String())
	}
}

func TestPlayerDiscoveryRailPollsOnlyWhileEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := PlayerDiscoveryRail(newTestPageProps(), "sample_anchor", nil, nil, true).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `hx-get="/api/videos/sample_anchor/recommendations"`) {
		t.Fatalf("pending rail does not poll: %s", buf.String())
	}
	if strings.Contains(buf.String(), "every") {
		t.Fatalf("pending rail schedules overlapping polling: %s", buf.String())
	}
}

func TestDiscoverGridRefreshesWhenAChannelIsFollowed(t *testing.T) {
	var buf bytes.Buffer
	videos := []model.DiscoveryVideo{{VideoID: "sample_visible", ChannelID: "youtube_UCvisible"}}
	if err := DiscoverGrid(newTestPageProps(), videos, false).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-get="/api/discover/cards"`) || !strings.Contains(html, `hx-trigger="followChanged from:body"`) {
		t.Fatalf("populated Discover grid does not refresh after follow: %s", html)
	}
}
