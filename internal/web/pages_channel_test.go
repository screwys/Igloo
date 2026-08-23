package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/model"
)

func TestFeedItemToVideoKeepsTweetAssetOwnership(t *testing.T) {
	video := feedItemToVideo(model.FeedItem{TweetID: "sample_post"}, model.Channel{
		ChannelID: "twitter_sample", IsSubscribed: true, IsStarred: true,
	})
	if video.ThumbnailURL != "/api/media/thumbnail/sample_post?owner_kind=tweet" {
		t.Fatalf("thumbnail URL = %q", video.ThumbnailURL)
	}
	if video.OwnerKind != "tweet" || !video.IsSubscribed || !video.IsStarred {
		t.Fatalf("converted owner/follow state = %+v", video)
	}
}

func TestHandlePageChannelRendersProfileOnlyTikTok(t *testing.T) {
	srv := newTestServer(t)
	srv.staticV = func(path string) string { return "/static/" + path }
	if err := srv.db.UpsertChannelProfile(model.ChannelProfile{
		ChannelID:   "tiktok_riki",
		Platform:    "tiktok",
		Handle:      "riki",
		DisplayName: "Riki",
		Bio:         "profile-only account",
		BannerURL:   "https://example.test/banner.jpg",
	}); err != nil {
		t.Fatalf("UpsertChannelProfile: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/channels/tiktok_riki", nil)
	req.SetPathValue("channelID", "tiktok_riki")
	rec := httptest.NewRecorder()
	srv.handlePageChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; location=%q", rec.Code, http.StatusOK, rec.Header().Get("Location"))
	}
	html := rec.Body.String()
	for _, want := range []string{
		`profile-card--hero`,
		`data-channel-id="tiktok_riki"`,
		`@riki`,
		`profile-only account`,
		`No channel videos found.`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered page missing %q\n%s", want, html)
		}
	}
	if strings.Contains(html, `hx-delete="/api/unsubscribe/tiktok_riki`) {
		t.Fatal("profile-only unfollowed channel should not render unsubscribe action")
	}
	assertActiveNav(t, html, "/channels")
	assertInactiveNav(t, html, "/videos")
}

func TestHandlePageYouTubeChannelIncludesReadyTemporaryVideos(t *testing.T) {
	srv := newTestServer(t)
	srv.staticV = func(path string) string { return "/static/" + path }
	const videoID = "sample_ready_discover"
	storeReadyMediaAsset(t, srv, "youtube", "youtube_video", videoID, "video_stream", 0,
		filepath.Join("media", "youtube", videoID+".mp4"), "video/mp4", []byte("fake-mp4"))
	if err := srv.db.ExecRaw(`UPDATE videos SET title = 'Ready Discover Video', is_temp = 1 WHERE video_id = ?`, videoID); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/channels/youtube_asset_fixture", nil)
	req.SetPathValue("channelID", "youtube_asset_fixture")
	rec := httptest.NewRecorder()
	srv.handlePageChannel(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Ready Discover Video") {
		t.Fatalf("ready temporary video missing from channel page: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlePageChannelSeparatesAuthoredMomentsAndRepostsWithoutFullPageSwap(t *testing.T) {
	srv := newTestServer(t)
	srv.staticV = func(path string) string { return "/static/" + path }
	if err := srv.db.ExecRaw(`
		INSERT INTO channels (channel_id, source_id, name, platform) VALUES
			('tiktok_sample_profile', 'sample_profile', 'Sample Profile', 'tiktok'),
			('tiktok_sample_author', 'sample_author', 'Sample Author', 'tiktok');
		INSERT INTO videos (video_id, channel_id, owner_kind, title, published_at) VALUES
			('sample_authored', 'tiktok_sample_profile', 'tiktok_video', 'Authored moment', 100),
			('sample_reposted', 'tiktok_sample_author', 'tiktok_video', 'Reposted moment', 200)
	`); err != nil {
		t.Fatalf("seed channel content: %v", err)
	}
	if err := srv.db.UpsertChannelProfile(model.ChannelProfile{
		ChannelID: "tiktok_sample_profile", Platform: "tiktok",
		Handle: "sample_profile", DisplayName: "Sample Profile",
	}); err != nil {
		t.Fatalf("UpsertChannelProfile: %v", err)
	}
	if _, err := srv.db.UpsertVideoRepostSources([]model.VideoRepostSource{{
		VideoID: "sample_reposted", ReposterChannelID: "tiktok_sample_profile",
		ReposterHandle: "sample_profile", RepostedAtMs: 300,
	}}); err != nil {
		t.Fatalf("UpsertVideoRepostSources: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/channels/tiktok_sample_profile?section=reposts", nil)
	req.SetPathValue("channelID", "tiktok_sample_profile")
	rec := httptest.NewRecorder()
	srv.handlePageChannel(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reposts status = %d", rec.Code)
	}
	html := rec.Body.String()
	if !strings.Contains(html, "Reposted moment") || strings.Contains(html, "Authored moment") {
		t.Fatalf("reposts section rendered the wrong owner set:\n%s", html)
	}
	if !strings.Contains(html, `aria-selected="1">Reposts`) {
		t.Fatalf("reposts tab is not active:\n%s", html)
	}

	req = httptest.NewRequest(http.MethodGet, "/channels/tiktok_sample_profile", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("channelID", "tiktok_sample_profile")
	rec = httptest.NewRecorder()
	srv.handlePageChannel(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("moments partial status = %d", rec.Code)
	}
	html = rec.Body.String()
	if !strings.Contains(html, `id="channel-video-section"`) ||
		!strings.Contains(html, "Authored moment") || strings.Contains(html, "Reposted moment") {
		t.Fatalf("moments partial rendered the wrong section:\n%s", html)
	}
	if strings.Contains(html, `profile-card--hero`) || strings.Contains(html, `id="shorts-layout"`) {
		t.Fatalf("section swap should not replace the full page:\n%s", html)
	}
}

func TestHandlePageChannelRendersTwitterChannelWithChannelsNavActive(t *testing.T) {
	srv := newTestServer(t)
	srv.staticV = func(path string) string { return "/static/" + path }
	if err := srv.db.UpsertChannelProfile(model.ChannelProfile{
		ChannelID:   "twitter_alice",
		Platform:    "twitter",
		Handle:      "alice",
		DisplayName: "Alice",
	}); err != nil {
		t.Fatalf("UpsertChannelProfile: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/channels/twitter_alice", nil)
	req.SetPathValue("channelID", "twitter_alice")
	rec := httptest.NewRecorder()
	srv.handlePageChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; location=%q", rec.Code, http.StatusOK, rec.Header().Get("Location"))
	}
	html := rec.Body.String()
	assertActiveNav(t, html, "/channels")
	assertInactiveNav(t, html, "/feed")
}

func TestHandlePageTwitterProfileShowsPostsAndOriginalMedia(t *testing.T) {
	srv := newTestServer(t)
	srv.staticV = func(path string) string { return "/static/" + path }
	if err := srv.db.UpsertChannelProfile(model.ChannelProfile{
		ChannelID: "twitter_sample_artist", Platform: "twitter", Handle: "sample_artist", DisplayName: "Sample Artist",
	}); err != nil {
		t.Fatalf("UpsertChannelProfile: %v", err)
	}
	now := time.UnixMilli(1_700_000_000_000)
	if _, err := srv.db.UpsertFeedItems([]model.FeedItem{
		{TweetID: "first_original", SourceHandle: "sample_artist", AuthorHandle: "sample_artist", AuthorDisplayName: "Sample Artist", BodyText: "First original", MediaJSON: `[{"url":"https://cdn.example/first.jpg","type":"photo"}]`, PublishedAt: &now, FetchedAt: now, ContentHash: "first"},
		{TweetID: "original_media", SourceHandle: "sample_artist", AuthorHandle: "sample_artist", AuthorDisplayName: "Sample Artist", BodyText: "Original media", MediaJSON: `[{"url":"https://cdn.example/original.jpg","type":"photo"}]`, PublishedAt: &now, FetchedAt: now, ContentHash: "original"},
		{TweetID: "reposted_media", SourceHandle: "sample_artist", AuthorHandle: "other_artist", AuthorDisplayName: "Other Artist", BodyText: "Reposted media", MediaJSON: `[{"url":"https://cdn.example/repost.jpg","type":"photo"}]`, IsRetweet: true, PublishedAt: &now, FetchedAt: now, ContentHash: "repost"},
	}); err != nil {
		t.Fatalf("UpsertFeedItems: %v", err)
	}
	storeReadyMediaAsset(t, srv, "twitter", "tweet", "first_original", "post_media", 0, "media/first.jpg", "image/jpeg", testJPEGBytes())
	storeReadyMediaAsset(t, srv, "twitter", "tweet", "original_media", "post_media", 0, "media/original.jpg", "image/jpeg", testJPEGBytes())

	req := httptest.NewRequest(http.MethodGet, "/channels/twitter_sample_artist", nil)
	req.SetPathValue("channelID", "twitter_sample_artist")
	rec := httptest.NewRecorder()
	srv.handlePageChannel(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("posts status = %d", rec.Code)
	}
	html := rec.Body.String()
	for _, want := range []string{
		`class="x-profile-content"`, `class="x-profile-tabs"`, `>Posts</a>`, `?tab=media`, `First original`, `Original media`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("posts page missing %q\n%s", want, html)
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/channels/twitter_sample_artist?tab=media", nil)
	req.SetPathValue("channelID", "twitter_sample_artist")
	rec = httptest.NewRecorder()
	srv.handlePageChannel(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("media status = %d", rec.Code)
	}
	html = rec.Body.String()
	for _, want := range []string{
		`class="x-profile-content"`, `class="x-profile-media-grid"`, `data-x-profile-media`, `data-feed-thread-href="/thread/first_original?return=%2Fchannels%2Ftwitter_sample_artist%3Ftab%3Dmedia"`, `data-feed-thread-href="/thread/original_media?return=%2Fchannels%2Ftwitter_sample_artist%3Ftab%3Dmedia"`, `data-x-profile-media-slide`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("media page missing %q\n%s", want, html)
		}
	}
	if strings.Contains(html, `<a class="x-profile-media-tile"`) {
		t.Fatalf("media tiles should open the overlay instead of navigating to a thread\n%s", html)
	}
	if strings.Contains(html, "reposted_media") || strings.Contains(html, "Reposted media") {
		t.Fatalf("media page should exclude reposts\n%s", html)
	}
	if strings.Contains(html, `x-profile-media-more`) || strings.Contains(html, `>Show more<`) {
		t.Fatalf("media page should not expose manual pagination\n%s", html)
	}
}

func TestHandlePageTwitterChannelFeedPaginatesPastFirstChunk(t *testing.T) {
	srv := newTestServer(t)
	srv.staticV = func(path string) string { return "/static/" + path }
	if err := srv.db.UpsertChannelProfile(model.ChannelProfile{
		ChannelID:   "twitter_sample_author",
		Platform:    "twitter",
		Handle:      "sample_author",
		DisplayName: "Sample Author",
	}); err != nil {
		t.Fatalf("UpsertChannelProfile: %v", err)
	}

	base := time.UnixMilli(1700000000000)
	items := make([]model.FeedItem, 45)
	for i := range items {
		publishedAt := base.Add(time.Duration(i) * time.Minute)
		items[i] = model.FeedItem{
			TweetID:           fmt.Sprintf("sample_post_%02d", i+1),
			AuthorHandle:      "sample_author",
			AuthorDisplayName: "Sample Author",
			BodyText:          "post body",
			PublishedAt:       &publishedAt,
		}
	}
	if _, err := srv.db.UpsertFeedItems(items); err != nil {
		t.Fatalf("UpsertFeedItems: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/channels/twitter_sample_author", nil)
	req.SetPathValue("channelID", "twitter_sample_author")
	rec := httptest.NewRecorder()
	srv.handlePageChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	html := rec.Body.String()
	if got := strings.Count(html, `data-tweet-id=`); got != 40 {
		t.Fatalf("initial page tweet count = %d, want 40\n%s", got, html)
	}
	if !strings.Contains(html, `sample_post_45`) || strings.Contains(html, `sample_post_05`) {
		t.Fatalf("initial page should contain newest chunk only\n%s", html)
	}
	if !strings.Contains(html, `hx-get="/channels/twitter_sample_author?offset=40"`) {
		t.Fatalf("initial page missing next-page sentinel\n%s", html)
	}

	req = httptest.NewRequest(http.MethodGet, "/channels/twitter_sample_author?offset=40", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("channelID", "twitter_sample_author")
	rec = httptest.NewRecorder()
	srv.handlePageChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("partial status = %d, want %d", rec.Code, http.StatusOK)
	}
	html = rec.Body.String()
	if got := strings.Count(html, `data-tweet-id=`); got != 5 {
		t.Fatalf("partial page tweet count = %d, want 5\n%s", got, html)
	}
	for _, want := range []string{`sample_post_05`, `sample_post_01`} {
		if !strings.Contains(html, want) {
			t.Fatalf("partial page missing %q\n%s", want, html)
		}
	}
	if strings.Contains(html, `profile-card--hero`) || strings.Contains(html, `feed-scroll-sentinel`) {
		t.Fatalf("final partial should contain only remaining feed rows\n%s", html)
	}
}

func TestHandlePageTwitterChannelFeedDoesNotRepeatThreadAcrossPages(t *testing.T) {
	srv := newTestServer(t)
	srv.staticV = func(path string) string { return "/static/" + path }
	if err := srv.db.UpsertChannelProfile(model.ChannelProfile{
		ChannelID:   "twitter_sample_author",
		Platform:    "twitter",
		Handle:      "sample_author",
		DisplayName: "Sample Author",
	}); err != nil {
		t.Fatalf("UpsertChannelProfile: %v", err)
	}

	base := time.UnixMilli(1700000000000)
	items := make([]model.FeedItem, 0, 42)
	for i := 0; i < 39; i++ {
		publishedAt := base.Add(time.Duration(300-i) * time.Minute)
		items = append(items, model.FeedItem{
			TweetID:           fmt.Sprintf("sample_filler_%02d", i+1),
			AuthorHandle:      "sample_author",
			AuthorDisplayName: "Sample Author",
			BodyText:          "filler post",
			PublishedAt:       &publishedAt,
			FetchedAt:         publishedAt,
			ContentHash:       fmt.Sprintf("sample_filler_hash_%02d", i+1),
			CanonicalTweetID:  fmt.Sprintf("sample_filler_%02d", i+1),
		})
	}
	rootAt := base.Add(100 * time.Minute)
	leafAt := base.Add(101 * time.Minute)
	oldAt := base.Add(90 * time.Minute)
	items = append(items,
		model.FeedItem{
			TweetID:           "sample_thread_leaf",
			AuthorHandle:      "sample_author",
			AuthorDisplayName: "Sample Author",
			BodyText:          "thread leaf body",
			IsReply:           true,
			ReplyToHandle:     "sample_author",
			ReplyToStatus:     "sample_thread_root",
			PublishedAt:       &leafAt,
			FetchedAt:         leafAt,
			ContentHash:       "sample_thread_leaf_hash",
			CanonicalTweetID:  "sample_thread_leaf",
		},
		model.FeedItem{
			TweetID:           "sample_thread_root",
			AuthorHandle:      "sample_author",
			AuthorDisplayName: "Sample Author",
			BodyText:          "thread root body",
			PublishedAt:       &rootAt,
			FetchedAt:         rootAt,
			ContentHash:       "sample_thread_root_hash",
			CanonicalTweetID:  "sample_thread_root",
		},
		model.FeedItem{
			TweetID:           "sample_old_post",
			AuthorHandle:      "sample_author",
			AuthorDisplayName: "Sample Author",
			BodyText:          "old post body",
			PublishedAt:       &oldAt,
			FetchedAt:         oldAt,
			ContentHash:       "sample_old_post_hash",
			CanonicalTweetID:  "sample_old_post",
		},
	)
	if _, err := srv.db.UpsertFeedItems(items); err != nil {
		t.Fatalf("UpsertFeedItems: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/channels/twitter_sample_author", nil)
	req.SetPathValue("channelID", "twitter_sample_author")
	rec := httptest.NewRecorder()
	srv.handlePageChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	html := rec.Body.String()
	if !strings.Contains(html, `thread root body`) || !strings.Contains(html, `thread leaf body`) {
		t.Fatalf("initial page should render the thread preview\n%s", html)
	}
	if strings.Contains(html, `sample_old_post`) {
		t.Fatalf("initial page should not include the next representative\n%s", html)
	}
	if !strings.Contains(html, `hx-get="/channels/twitter_sample_author?offset=40"`) {
		t.Fatalf("initial page missing grouped next-page sentinel\n%s", html)
	}

	req = httptest.NewRequest(http.MethodGet, "/channels/twitter_sample_author?offset=40", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("channelID", "twitter_sample_author")
	rec = httptest.NewRecorder()
	srv.handlePageChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("partial status = %d, want %d", rec.Code, http.StatusOK)
	}
	html = rec.Body.String()
	if !strings.Contains(html, `sample_old_post`) || !strings.Contains(html, `old post body`) {
		t.Fatalf("partial page should continue after the grouped thread\n%s", html)
	}
	if strings.Contains(html, `sample_thread_root`) || strings.Contains(html, `sample_thread_leaf`) || strings.Contains(html, `thread root body`) {
		t.Fatalf("partial page repeated a thread already rendered on the first page\n%s", html)
	}
}

func TestHandlePageShortsStartsAtOldestMoment(t *testing.T) {
	srv := newTestServer(t)
	srv.staticV = func(path string) string { return "/static/" + path }
	if err := srv.db.ExecRaw(
		`INSERT INTO channels (channel_id, name, platform) VALUES (?, ?, ?)`,
		"tiktok_demo", "Demo", "tiktok",
	); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.ExecRaw(
		`INSERT INTO channel_follows (channel_id, followed_at) VALUES (?, 1)`,
		"tiktok_demo",
	); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		videoID := "short_00" + string(rune('0'+i))
		if err := srv.db.ExecRaw(
			`INSERT INTO videos (video_id, channel_id, owner_kind, title, duration, published_at)
			 VALUES (?, ?, 'tiktok_video', ?, 0, ?)`,
			videoID, "tiktok_demo", "Short 00"+string(rune('0'+i)), i,
		); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/shorts?tab=following", nil)
	rec := httptest.NewRecorder()
	srv.handlePageShorts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	html := rec.Body.String()
	oldest := strings.Index(html, `data-video-id="short_001"`)
	newest := strings.Index(html, `data-video-id="short_003"`)
	if newest < 0 || oldest < 0 {
		t.Fatalf("rendered page missing seeded shorts\n%s", html)
	}
	if oldest > newest {
		t.Fatalf("shorts order starts newest first; oldest index %d newest index %d\n%s", oldest, newest, html)
	}
}

func TestActiveNavForPathMapsRouteFamilies(t *testing.T) {
	tests := map[string]string{
		"/channels":              "channels",
		"/channels/tiktok_alice": "channels",
		"/videos":                "videos",
		"/player/video-1":        "videos",
		"/feed":                  "feed",
		"/shorts":                "shorts",
		"/bookmarks":             "bookmarks",
	}
	for path, want := range tests {
		if got := activeNavForPath(path); got != want {
			t.Fatalf("activeNavForPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func assertActiveNav(t *testing.T, html, href string) {
	t.Helper()
	want := `href="` + href + `" class="nav-item active"`
	if !strings.Contains(html, want) {
		t.Fatalf("expected active nav %s\n%s", href, html)
	}
}

func assertInactiveNav(t *testing.T, html, href string) {
	t.Helper()
	bad := `href="` + href + `" class="nav-item active"`
	if strings.Contains(html, bad) {
		t.Fatalf("expected inactive nav %s\n%s", href, html)
	}
}
