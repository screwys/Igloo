package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/model"
)

func TestWithSnapshotExportHonorsCanceledContext(t *testing.T) {
	d := openWritableTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dst := filepath.Join(t.TempDir(), "snapshot.pgdump")

	err := d.WithSnapshotExport(ctx, dst, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithSnapshotExport error = %v, want context canceled", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("canceled WithSnapshotExport created destination: %v", err)
	}
}

func markDBTestStateRoot(t *testing.T, stateRoot string) {
	t.Helper()
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, ".igloo-state-root"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	t.Parallel()
	stateRoot := openReadOnlyFixtureDB(t)
	d, err := OpenReadOnlyAtStateRoot(stateRoot)
	if err != nil {
		t.Fatalf("open read-only fixture: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func openReadOnlyFixtureDB(t *testing.T) string {
	t.Helper()
	stateRoot := t.TempDir()
	markDBTestStateRoot(t, stateRoot)
	d, err := OpenAtStateRoot(stateRoot)
	if err != nil {
		t.Fatalf("open writable: %v", err)
	}
	seedReadOnlyFixtureDB(t, d)
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Errorf("close fixture db: %v", err)
		}
	})
	return stateRoot
}

// openWritableTestDB opens a fresh database for synthetic fixtures.
func openWritableTestDB(t *testing.T) *DB {
	t.Helper()
	t.Parallel()
	stateRoot := t.TempDir()
	markDBTestStateRoot(t, stateRoot)
	d, err := OpenAtStateRoot(stateRoot)
	if err != nil {
		t.Fatalf("open writable: %v", err)
	}
	t.Cleanup(func() {
		_ = d.Close()
	})
	return d
}

// openFreshTestDB opens a database in a new state root.
func openFreshTestDB(t *testing.T) *DB {
	t.Helper()
	t.Parallel()
	tmpDir := t.TempDir()
	markDBTestStateRoot(t, tmpDir)
	d, err := OpenAtStateRoot(tmpDir)
	if err != nil {
		t.Fatalf("Open fresh DB: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func seedTestChannel(t *testing.T, d *DB, channelID string) {
	t.Helper()
	if err := d.ExecRaw(`
		INSERT INTO channels (channel_id, source_id, name, url, platform, created_at)
		VALUES ($1, $2, 'Fixture Channel', '', 'youtube', 1)
		ON CONFLICT DO NOTHING
	`, channelID, channelID); err != nil {
		t.Fatalf("seed channel %s: %v", channelID, err)
	}
}

func seedTestFollowedChannel(t *testing.T, d *DB, channelID string) {
	t.Helper()
	seedTestChannel(t, d, channelID)
	if err := d.ExecRaw(`
		INSERT INTO channel_follows (channel_id, followed_at)
		VALUES ($1, 1)
		ON CONFLICT DO NOTHING
	`, channelID); err != nil {
		t.Fatalf("seed channel follow %s: %v", channelID, err)
	}
}

func seedTestVideo(t *testing.T, d *DB, videoID, channelID string) {
	t.Helper()
	seedTestChannel(t, d, channelID)
	if err := d.ExecRaw(`
		INSERT INTO videos (video_id, channel_id, owner_kind, title, duration, published_at)
		VALUES ($1, $2, 'youtube_video', 'Fixture Video', 120, 1)
		ON CONFLICT DO NOTHING
	`, videoID, channelID); err != nil {
		t.Fatalf("seed video %s: %v", videoID, err)
	}
	asset := normalizeAsset(Asset{
		AssetID: "fixture-stream:" + videoID, AssetKind: "video_stream",
		OwnerKind: "youtube_video", OwnerID: videoID,
		FilePath: "media/youtube/fixture.mp4", ContentType: "video/mp4",
		SizeBytes: 1, FileMtimeNs: 1, State: AssetStateReady,
	}, 1)
	if err := d.WithWrite(func(tx *sql.Tx) error { return upsertAssetTx(tx, asset) }); err != nil {
		t.Fatalf("seed video asset %s: %v", videoID, err)
	}
}

func seedReadOnlyFixtureDB(t *testing.T, d *DB) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	const (
		channelID = "youtube_fixture_channel"
		videoID   = "youtube_fixture_video"
		tweetID   = "twitter_fixture_tweet"
	)
	seedTestFollowedChannel(t, d, channelID)
	seedTestVideo(t, d, videoID, channelID)
	if err := d.ExecRaw(`
		INSERT INTO video_comments (
			video_id, comment_id, author_name, author_id, text, like_count, published_at
		) VALUES ($1, 'fixture_comment', 'Fixture Commenter', 'fixture_author', 'Fixture comment text', 1, 1)
		ON CONFLICT DO NOTHING
	`, videoID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	if _, err := d.UpsertFeedItems([]model.FeedItem{{
		TweetID:        tweetID,
		AuthorHandle:   "fixture_author",
		BodyText:       "fixture feed item",
		PublishedAt:    &now,
		FetchedAt:      now,
		ContentHash:    "fixture_feed_hash",
		CanonicalURL:   "https://x.com/fixture_author/status/" + tweetID,
		MediaJSON:      `[]`,
		QuoteMediaJSON: `[]`,
	}}); err != nil {
		t.Fatalf("seed feed item: %v", err)
	}
	if err := d.ExecRaw(`
		INSERT INTO feed_likes (tweet_id, liked_at)
		VALUES ($1, 1)
		ON CONFLICT DO NOTHING
	`, tweetID); err != nil {
		t.Fatalf("seed feed like: %v", err)
	}
}

func TestOpen(t *testing.T) {
	d := openWritableTestDB(t)

	tables := []string{"channels", "videos", "feed_items", "settings"}
	for _, table := range tables {
		var name string
		err := d.conn.QueryRow(
			"SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_name=$1", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestOpenReadOnly(t *testing.T) {
	dataDir := openReadOnlyFixtureDB(t)

	d, err := OpenReadOnlyAtStateRoot(dataDir)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer func() {
		_ = d.Close()
	}()

	var count int
	err = d.conn.QueryRow("SELECT count(*) FROM channels").Scan(&count)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if count == 0 {
		t.Log("warning: channels table is empty")
	}
}
