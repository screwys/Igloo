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
