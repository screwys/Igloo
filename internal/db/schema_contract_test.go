package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenRejectsExistingSchemaDrift(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "igloo.db")
	store, err := OpenPath(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`ALTER TABLE channel_profiles ADD COLUMN retired_retry_state INTEGER NOT NULL DEFAULT 0`); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPath(path, root)
	if store != nil {
		_ = store.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "database schema does not match the current contract") {
		t.Fatalf("OpenPath schema drift error = %v", err)
	}
}

func TestOpenMigratesKnownSchemaChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "igloo.db")
	store, err := OpenPath(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ExecRaw(`INSERT INTO videos (video_id, channel_id, owner_kind) VALUES ('sample_video', 'sample_channel', 'youtube_video')`); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`ALTER TABLE videos DROP COLUMN is_temp`); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	if _, err := conn.Exec(`DROP TABLE schema_migrations`); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPath(path, root)
	if err != nil {
		t.Fatalf("OpenPath legacy schema: %v", err)
	}

	var isTemp int
	if err := store.QueryRow(`SELECT is_temp FROM videos WHERE video_id = 'sample_video'`).Scan(&isTemp); err != nil {
		t.Fatalf("read migrated column: %v", err)
	}
	if isTemp != 0 {
		t.Fatalf("migrated is_temp = %d, want 0", isTemp)
	}
	var migrations int
	if err := store.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = '20260718_add_videos_is_temp'`).Scan(&migrations); err != nil {
		t.Fatalf("read migration ledger: %v", err)
	}
	if migrations != 1 {
		t.Fatalf("migration ledger entries = %d, want 1", migrations)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPath(path, root)
	if err != nil {
		t.Fatalf("OpenPath migrated schema: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = '20260718_add_videos_is_temp'`).Scan(&migrations); err != nil {
		t.Fatalf("read migration ledger after reopen: %v", err)
	}
	if migrations != 1 {
		t.Fatalf("migration ledger entries after reopen = %d, want 1", migrations)
	}
}

func TestOpenMigratesYouTubeMemberOnlyChannelSettingAndSyncTrigger(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "igloo.db")
	store, err := OpenPath(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ExecRaw(`
		INSERT INTO channel_settings (channel_id, max_videos)
		VALUES ('sample_channel', 25)
	`); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DELETE FROM schema_migrations WHERE name = '20260801_add_youtube_member_only_channel_setting'`,
		`DROP TRIGGER android_sync_head_channel_settings_update`,
		`ALTER TABLE channel_settings DROP COLUMN include_member_only`,
		`CREATE TRIGGER android_sync_head_channel_settings_update
		 AFTER UPDATE OF max_videos ON channel_settings
		 BEGIN SELECT 1; END`,
	} {
		if _, err := conn.Exec(statement); err != nil {
			_ = conn.Close()
			t.Fatalf("prepare legacy channel settings schema: %v", err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPath(path, root)
	if err != nil {
		t.Fatalf("OpenPath legacy channel settings schema: %v", err)
	}
	defer func() { _ = store.Close() }()

	var triggerSQL string
	if err := store.QueryRow(`
		SELECT sql FROM sqlite_schema
		WHERE type = 'trigger' AND name = 'android_sync_head_channel_settings_update'
	`).Scan(&triggerSQL); err != nil {
		t.Fatalf("read migrated channel settings trigger: %v", err)
	}
	if !strings.Contains(triggerSQL, "include_member_only") {
		t.Fatalf("migrated channel settings trigger = %q, want include_member_only", triggerSQL)
	}

	var beforeRevision int64
	if err := store.QueryRow(`
		SELECT revision FROM android_sync_heads
		WHERE owner_kind = 'channel_setting' AND owner_id = 'sample_channel'
	`).Scan(&beforeRevision); err != nil {
		t.Fatalf("read channel settings revision before update: %v", err)
	}
	if err := store.ExecRaw(`
		UPDATE channel_settings SET include_member_only = 1
		WHERE channel_id = 'sample_channel'
	`); err != nil {
		t.Fatalf("update migrated member-only setting: %v", err)
	}
	var afterRevision int64
	if err := store.QueryRow(`
		SELECT revision FROM android_sync_heads
		WHERE owner_kind = 'channel_setting' AND owner_id = 'sample_channel'
	`).Scan(&afterRevision); err != nil {
		t.Fatalf("read channel settings revision after update: %v", err)
	}
	if afterRevision <= beforeRevision {
		t.Fatalf("channel settings revision = %d, want greater than %d", afterRevision, beforeRevision)
	}
}

func TestOpenMigratesFeedOrderInvalidationQueue(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "igloo.db")
	store, err := OpenPath(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
		DROP TABLE feed_order_invalidations;
		DELETE FROM schema_migrations
		WHERE name = '20260801_add_feed_order_invalidation_queue';
	`); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPath(path, root)
	if err != nil {
		t.Fatalf("OpenPath schema without feed-order queue: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ExecRaw(`
		INSERT INTO feed_items (tweet_id, body_text)
		VALUES ('sample_item', 'body')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MutateLike(LikeMutation{
		TweetID: "sample_item", Action: "set", UpdatedAtMs: 10,
	}); err != nil {
		t.Fatal(err)
	}
	var queued int
	if err := store.QueryRow(`
		SELECT COUNT(*) FROM feed_order_invalidations
		WHERE owner_kind = 'tweet' AND owner_id = 'sample_item'
	`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("migrated feed-order invalidations = %d, want 1", queued)
	}
}

func TestOpenInstallsAndroidSyncPeerTriggersBeforeSchemaValidation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "igloo.db")
	store, err := OpenPath(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
		DROP TRIGGER android_sync_head_bookmarks_peers_delete;
		DELETE FROM schema_migrations
		WHERE name = '20260802_install_android_sync_peer_triggers';
	`); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPath(path, root)
	if err != nil {
		t.Fatalf("OpenPath schema without Android sync peer trigger: %v", err)
	}
	defer func() { _ = store.Close() }()
	var installed int
	if err := store.QueryRow(`
		SELECT COUNT(*) FROM sqlite_schema
		WHERE type = 'trigger' AND name = 'android_sync_head_bookmarks_peers_delete'
	`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if installed != 1 {
		t.Fatalf("installed Android sync peer triggers = %d, want 1", installed)
	}
}

func TestOpenBackfillsVideoFetchHistory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "igloo.db")
	store, err := OpenPath(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ExecRaw(`
		INSERT INTO videos (video_id, channel_id, owner_kind, downloaded_at)
		VALUES ('sample_fetched', 'tiktok_sample', 'tiktok_video', 1234)
	`); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
		DROP TABLE video_fetch_history;
		DELETE FROM schema_migrations WHERE name = '20260727_add_video_fetch_history'
	`); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPath(path, root)
	if err != nil {
		t.Fatalf("OpenPath legacy schema: %v", err)
	}
	defer func() { _ = store.Close() }()
	var fetchedAtMs int64
	if err := store.QueryRow(`
		SELECT fetched_at_ms FROM video_fetch_history WHERE video_id = 'sample_fetched'
	`).Scan(&fetchedAtMs); err != nil {
		t.Fatalf("read fetch history: %v", err)
	}
	if fetchedAtMs != 1234 {
		t.Fatalf("fetched_at_ms = %d, want 1234", fetchedAtMs)
	}
}

func TestOpenCollapsesFetchedIntroducedSources(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "igloo.db")
	store, err := OpenPath(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ExecRaw(`
		INSERT INTO channels (channel_id, source_id, name, platform, created_at) VALUES
			('tiktok_sample_author', 'sample_author', 'Sample Author', 'tiktok', 1),
			('tiktok_sample_first', 'sample_first', 'Sample First', 'tiktok', 1),
			('tiktok_sample_later', 'sample_later', 'Sample Later', 'tiktok', 1);
		INSERT INTO videos (
			video_id, channel_id, owner_kind, published_at, downloaded_at
		) VALUES (
			'sample_fetched_repost', 'tiktok_sample_author', 'tiktok_video', 100, 500
		);
		INSERT INTO video_fetch_history (video_id, fetched_at_ms)
		VALUES ('sample_fetched_repost', 500);
		INSERT INTO video_desires (
			source_channel_id, source_component, video_id, source_position, lane
		) VALUES
			('tiktok_sample_first', 'reposts', 'sample_fetched_repost', 3, 'backfill'),
			('tiktok_sample_later', 'reposts', 'sample_fetched_repost', 1, 'backfill');
		INSERT INTO video_repost_sources (
			video_id, reposter_channel_id, first_seen_at_ms, updated_at_ms
		) VALUES
			('sample_fetched_repost', 'tiktok_sample_first', 200, 200),
			('sample_fetched_repost', 'tiktok_sample_later', 300, 300);
		DELETE FROM schema_migrations
		WHERE name = '20260727_collapse_fetched_introduced_sources'
	`); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPath(path, root)
	if err != nil {
		t.Fatalf("OpenPath legacy source windows: %v", err)
	}
	defer func() { _ = store.Close() }()
	for table, want := range map[string]string{
		"video_desires":        "tiktok_sample_first",
		"video_repost_sources": "tiktok_sample_first",
	} {
		var got string
		column := "source_channel_id"
		if table == "video_repost_sources" {
			column = "reposter_channel_id"
		}
		if err := store.QueryRow(`
			SELECT ` + column + `
			FROM ` + table + `
			WHERE video_id = 'sample_fetched_repost'
		`).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s source = %q, want %q", table, got, want)
		}
	}
}

func TestEnsureSchemaCanRunTwice(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := EnsureSchema(conn); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSchema(conn); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}
