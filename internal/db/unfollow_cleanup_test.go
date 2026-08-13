package db

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestUnfollowXContentPlanUsesIdentityAndThreadIndexes(t *testing.T) {
	d := openFreshTestDB(t)
	rows, err := d.conn.Query("EXPLAIN QUERY PLAN "+unreferencedXContentIDsQuery(),
		"twitter_sample_profile", "twitter_sample_profile", "twitter_sample_profile")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	plan := strings.Join(details, "\n")
	for _, scan := range []string{"SCAN fi", "SCAN current", "SCAN peer"} {
		if strings.Contains(plan, scan) {
			t.Fatalf("unfollow retention scans feed_items through %q: %s", scan, plan)
		}
	}
	for _, index := range []string{
		"idx_feed_items_source_channel",
		"idx_feed_items_channel",
		"idx_feed_items_reposter_channel",
		"idx_feed_items_canonical_tweet",
		"idx_feed_items_quote",
		"idx_feed_items_reply_parent",
		"idx_feed_items_content_hash",
	} {
		if !strings.Contains(plan, index) {
			t.Fatalf("unfollow retention plan does not use %s: %s", index, plan)
		}
	}
}

func TestUnfollowCollectsOnlyUnreferencedXContent(t *testing.T) {
	d := openWritableTestDB(t)
	const (
		dropped = "twitter_sample_dropped"
		kept    = "twitter_sample_kept"
	)
	for _, profile := range []model.ChannelProfile{
		{ChannelID: dropped, Platform: "twitter", Handle: "sample_dropped", DisplayName: "Dropped", Bio: "Stored bio"},
		{ChannelID: kept, Platform: "twitter", Handle: "sample_kept", DisplayName: "Kept"},
	} {
		if err := d.UpsertChannelProfile(profile); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.ExecRaw(`
		INSERT INTO channel_follows (channel_id, followed_at)
		VALUES ('twitter_sample_dropped', 1), ('twitter_sample_kept', 1);
		INSERT INTO feed_sources (source_id, platform, source_type, external_id, label, url, enabled)
		VALUES ('twitter_sample_list', 'twitter', 'list', 'sample', 'Sample list', '', 1);
		INSERT INTO feed_items (
			tweet_id, source_channel_id, channel_id, body_text, quote_tweet_id,
			quote_channel_id, published_at, fetched_at, content_hash
		) VALUES
			('sample_orphan', 'twitter_sample_dropped', 'twitter_sample_dropped', 'orphan', '', '', 10, 10, 'orphan'),
			('sample_shared_target', 'twitter_sample_dropped', 'twitter_sample_dropped', 'shared target', '', '', 20, 20, 'shared-target'),
			('sample_shared_reference', 'twitter_sample_kept', 'twitter_sample_kept', 'shared reference', 'sample_shared_target', 'twitter_sample_dropped', 30, 30, 'shared-reference'),
			('sample_bookmarked', 'twitter_sample_dropped', 'twitter_sample_dropped', 'bookmarked', '', '', 40, 40, 'bookmarked'),
			('sample_liked', 'twitter_sample_dropped', 'twitter_sample_dropped', 'liked', '', '', 50, 50, 'liked'),
			('sample_listed', 'twitter_sample_dropped', 'twitter_sample_dropped', 'listed', '', '', 60, 60, 'listed');
		INSERT INTO bookmarks (video_id, bookmarked_at) VALUES ('sample_bookmarked', 1);
		INSERT INTO feed_likes (tweet_id, liked_at) VALUES ('sample_liked', 1);
		INSERT INTO feed_item_sources (tweet_id, source_id, first_seen_at, last_seen_at)
		VALUES ('sample_listed', 'twitter_sample_list', 1, 1)
	`); err != nil {
		t.Fatal(err)
	}
	for _, asset := range []Asset{
		{AssetID: "sample-orphan-media", AssetKind: "post_media", OwnerKind: "tweet", OwnerID: "sample_orphan", FilePath: "media/twitter/sample-orphan.jpg"},
		{AssetID: "sample-avatar", AssetKind: "avatar", OwnerKind: "channel", OwnerID: dropped, FilePath: "thumbnails/avatars/sample-dropped.jpg"},
		{AssetID: "sample-banner", AssetKind: "banner", OwnerKind: "channel", OwnerID: dropped, FilePath: "thumbnails/banners/sample-dropped.jpg"},
	} {
		publishAssetMetadataForTest(t, d, asset, 1)
	}
	if err := d.RequestProfileJob(dropped, 90); err != nil {
		t.Fatal(err)
	}

	if _, err := d.MutateFollow(dropped, "clear", 100); err != nil {
		t.Fatal(err)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_items WHERE tweet_id = 'sample_orphan'`); got != 0 {
		t.Fatalf("orphaned post remained: %d", got)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM assets WHERE asset_id = 'sample-orphan-media'`); got != 0 {
		t.Fatalf("orphaned post media remained: %d", got)
	}
	requireAndroidSyncHead(t, d, "feed", "sample_orphan")
	requireAndroidSyncHead(t, d, "asset", "sample-orphan-media")
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_items WHERE tweet_id IN ('sample_shared_target','sample_shared_reference','sample_bookmarked','sample_liked','sample_listed')`); got != 5 {
		t.Fatalf("rooted posts remaining = %d, want 5", got)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM channel_profiles WHERE channel_id = ? AND bio = 'Stored bio'`, dropped); got != 1 {
		t.Fatalf("stored profile metadata changed: %d", got)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM assets WHERE owner_kind = 'channel' AND owner_id = ?`, dropped); got != 2 {
		t.Fatalf("stored profile assets remaining = %d, want 2", got)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM profile_jobs WHERE channel_id = ? AND requested_revision > completed_revision`, dropped); got != 0 {
		t.Fatalf("pending profile work remaining = %d", got)
	}
	if err := d.WithWrite(func(tx *sql.Tx) error {
		return observeProfileTx(tx, profileObservation{
			channelID: dropped, platform: "twitter", handle: "sample_dropped", observedAt: 101,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM profile_jobs WHERE channel_id = ? AND requested_revision > completed_revision`, dropped); got != 1 {
		t.Fatalf("later identity observation pending work = %d, want 1", got)
	}

	if _, err := d.MutateFollow(kept, "clear", 200); err != nil {
		t.Fatal(err)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_items WHERE tweet_id IN ('sample_shared_target','sample_shared_reference')`); got != 0 {
		t.Fatalf("content retained after its last followed reference was removed: %d", got)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_items WHERE tweet_id IN ('sample_bookmarked','sample_liked','sample_listed')`); got != 3 {
		t.Fatalf("durable content remaining = %d, want 3", got)
	}
}

func TestUnfollowCollectsOnlyUnreferencedShortsContent(t *testing.T) {
	for _, platform := range []string{"tiktok", "instagram"} {
		t.Run(platform, func(t *testing.T) {
			d := openWritableTestDB(t)
			first := platform + "_sample_first"
			second := platform + "_sample_second"
			ownerKind := platform + "_video"
			if platform == "instagram" {
				ownerKind = "instagram_reel"
			}
			seedVideoDesireChannels(t, d, first, second)
			if err := d.UpsertChannelProfile(model.ChannelProfile{
				ChannelID: first, Platform: platform, Handle: "sample_first", DisplayName: "First", Bio: "Stored bio",
			}); err != nil {
				t.Fatal(err)
			}
			if err := d.ExecRaw(`
				INSERT INTO videos (video_id, channel_id, owner_kind, title, published_at) VALUES
					('sample_orphan_video', ?, ?, 'Orphan', 1),
					('sample_shared_video', ?, ?, 'Shared', 2),
					('sample_bookmarked_video', ?, ?, 'Bookmarked', 3);
				INSERT INTO bookmarks (video_id, bookmarked_at) VALUES ('sample_bookmarked_video', 1)
			`, first, ownerKind, first, ownerKind, first, ownerKind); err != nil {
				t.Fatal(err)
			}
			for _, snapshot := range []VideoDesireSnapshot{
				{SourceChannelID: first, Component: "direct", Items: []VideoDesire{
					{VideoID: "sample_orphan_video", OwnerChannelID: first, SourcePosition: 0, Lane: DownloadLaneCurrent},
					{VideoID: "sample_shared_video", OwnerChannelID: first, SourcePosition: 1, Lane: DownloadLaneCurrent},
					{VideoID: "sample_bookmarked_video", OwnerChannelID: first, SourcePosition: 2, Lane: DownloadLaneCurrent},
				}},
				{SourceChannelID: second, Component: "direct", Items: []VideoDesire{
					{VideoID: "sample_shared_video", OwnerChannelID: first, SourcePosition: 0, Lane: DownloadLaneCurrent},
				}},
			} {
				if _, err := d.ReconcileVideoDesires(snapshot); err != nil {
					t.Fatal(err)
				}
			}
			for _, asset := range []Asset{
				{AssetID: platform + "-orphan-stream", AssetKind: "video_stream", OwnerKind: ownerKind, OwnerID: "sample_orphan_video", FilePath: "media/" + platform + "/sample-orphan.mp4", ContentType: "video/mp4"},
				{AssetID: platform + "-avatar", AssetKind: "avatar", OwnerKind: "channel", OwnerID: first, FilePath: "thumbnails/avatars/" + platform + "-sample-first.jpg"},
				{AssetID: platform + "-banner", AssetKind: "banner", OwnerKind: "channel", OwnerID: first, FilePath: "thumbnails/banners/" + platform + "-sample-first.jpg"},
			} {
				publishAssetMetadataForTest(t, d, asset, 1)
			}
			if err := d.RequestProfileJob(first, 90); err != nil {
				t.Fatal(err)
			}

			if _, err := d.MutateFollow(first, "clear", 100); err != nil {
				t.Fatal(err)
			}
			if got := testRowCount(t, d, `SELECT COUNT(*) FROM videos WHERE video_id = 'sample_orphan_video'`); got != 0 {
				t.Fatalf("orphaned video remained: %d", got)
			}
			requireAndroidSyncHead(t, d, "video", "sample_orphan_video")
			requireAndroidSyncHead(t, d, "asset", platform+"-orphan-stream")
			if got := testRowCount(t, d, `SELECT COUNT(*) FROM videos WHERE video_id IN ('sample_shared_video','sample_bookmarked_video')`); got != 2 {
				t.Fatalf("rooted videos remaining = %d, want 2", got)
			}
			if got := testRowCount(t, d, `SELECT COUNT(*) FROM video_desires WHERE source_channel_id = ?`, first); got != 0 {
				t.Fatalf("unfollowed source desires remaining = %d", got)
			}
			if got := testRowCount(t, d, `SELECT COUNT(*) FROM channel_profiles WHERE channel_id = ? AND bio = 'Stored bio'`, first); got != 1 {
				t.Fatalf("stored profile metadata changed: %d", got)
			}
			if got := testRowCount(t, d, `SELECT COUNT(*) FROM assets WHERE owner_kind = 'channel' AND owner_id = ?`, first); got != 2 {
				t.Fatalf("stored profile assets remaining = %d, want 2", got)
			}
			if got := testRowCount(t, d, `SELECT COUNT(*) FROM profile_jobs WHERE channel_id = ? AND requested_revision > completed_revision`, first); got != 0 {
				t.Fatalf("pending profile work remaining = %d", got)
			}

			if _, err := d.MutateFollow(second, "clear", 200); err != nil {
				t.Fatal(err)
			}
			if got := testRowCount(t, d, `SELECT COUNT(*) FROM videos WHERE video_id = 'sample_shared_video'`); got != 0 {
				t.Fatalf("shared video remained after its last followed source was removed: %d", got)
			}
			if got := testRowCount(t, d, `SELECT COUNT(*) FROM videos WHERE video_id = 'sample_bookmarked_video'`); got != 1 {
				t.Fatalf("bookmarked video remaining = %d, want 1", got)
			}
		})
	}
}

func TestVideoRetentionCollectsContentFromAlreadyUnfollowedShortsSources(t *testing.T) {
	d := openWritableTestDB(t)
	const source = "tiktok_sample_unfollowed"
	seedVideoDesireChannels(t, d, source)
	if err := d.ExecRaw(`
		INSERT INTO videos (video_id, channel_id, owner_kind, title, published_at)
		VALUES ('sample_stale_video', ?, 'tiktok_video', 'Stale', 1)
	`, source); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReconcileVideoDesires(VideoDesireSnapshot{
		SourceChannelID: source,
		Component:       "direct",
		Items: []VideoDesire{{
			VideoID: "sample_stale_video", OwnerChannelID: source,
			SourcePosition: 0, Lane: DownloadLaneCurrent,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.ExecRaw(`DELETE FROM channel_follows WHERE channel_id = ?`, source); err != nil {
		t.Fatal(err)
	}

	if _, err := d.MaintainVideoRetention(100); err != nil {
		t.Fatal(err)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM video_desires WHERE source_channel_id = ?`, source); got != 0 {
		t.Fatalf("stale unfollowed desires remaining = %d", got)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM videos WHERE video_id = 'sample_stale_video'`); got != 0 {
		t.Fatalf("stale unreferenced video remained: %d", got)
	}
}
