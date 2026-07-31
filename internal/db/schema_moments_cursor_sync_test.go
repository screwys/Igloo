package db

import (
	"path/filepath"
	"testing"
)

func TestOpenRepairsExistingAndroidMomentsCursorHeadsOnce(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "igloo.db")
	store, err := OpenPath(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ExecRaw(`
		INSERT INTO moments_cursors (
			scope, video_id, position_ms, sort_at_ms, updated_at_ms
		) VALUES
			('all', 'sample_all', 10, 100, 1000),
			('following', 'sample_following', 20, 200, 2000),
			('stories', 'sample_story', 30, 300, 3000);
		DELETE FROM android_sync_heads
		WHERE owner_kind = 'moments_cursor' AND owner_id = 'following';
		DELETE FROM schema_migrations
		WHERE name = '20260731_repair_android_moments_cursor_heads';
	`); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	var allRevision, clockBefore int64
	if err := store.QueryRow(`
		SELECT revision FROM android_sync_heads
		WHERE owner_kind = 'moments_cursor' AND owner_id = 'all'
	`).Scan(&allRevision); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.QueryRow(`SELECT revision FROM android_sync_clock WHERE id = 1`).Scan(&clockBefore); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPath(path, root)
	if err != nil {
		t.Fatalf("OpenPath with missing cursor head: %v", err)
	}
	var repairedAllRevision, followingRevision, clockAfter int64
	if err := store.QueryRow(`
		SELECT revision FROM android_sync_heads
		WHERE owner_kind = 'moments_cursor' AND owner_id = 'all'
	`).Scan(&repairedAllRevision); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.QueryRow(`
		SELECT revision FROM android_sync_heads
		WHERE owner_kind = 'moments_cursor' AND owner_id = 'following'
	`).Scan(&followingRevision); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.QueryRow(`SELECT revision FROM android_sync_clock WHERE id = 1`).Scan(&clockAfter); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if repairedAllRevision != allRevision {
		_ = store.Close()
		t.Fatalf("existing all cursor head revision = %d, want %d", repairedAllRevision, allRevision)
	}
	if followingRevision != clockBefore+1 || clockAfter != clockBefore+1 {
		_ = store.Close()
		t.Fatalf("repaired following/head clock = %d/%d, want %d", followingRevision, clockAfter, clockBefore+1)
	}
	var storiesHeads int
	if err := store.QueryRow(`
		SELECT COUNT(*) FROM android_sync_heads
		WHERE owner_kind = 'moments_cursor' AND owner_id = 'stories'
	`).Scan(&storiesHeads); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if storiesHeads != 0 {
		_ = store.Close()
		t.Fatalf("Android-local stories cursor heads = %d, want 0", storiesHeads)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPath(path, root)
	if err != nil {
		t.Fatalf("second OpenPath after cursor repair: %v", err)
	}
	defer func() { _ = store.Close() }()
	var clockAfterReopen, followingAfterReopen int64
	if err := store.QueryRow(`SELECT revision FROM android_sync_clock WHERE id = 1`).Scan(&clockAfterReopen); err != nil {
		t.Fatal(err)
	}
	if err := store.QueryRow(`
		SELECT revision FROM android_sync_heads
		WHERE owner_kind = 'moments_cursor' AND owner_id = 'following'
	`).Scan(&followingAfterReopen); err != nil {
		t.Fatal(err)
	}
	if clockAfterReopen != clockAfter || followingAfterReopen != followingRevision {
		t.Fatalf(
			"second repair changed clock/head from %d/%d to %d/%d",
			clockAfter, followingRevision, clockAfterReopen, followingAfterReopen,
		)
	}
}
