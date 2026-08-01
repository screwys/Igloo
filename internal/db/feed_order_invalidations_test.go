package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestFeedOrderInvalidationQueueUsesCanonicalMutationOwners(t *testing.T) {
	d := openWritableTestDB(t)
	if err := d.WithWrite(func(tx *sql.Tx) error {
		if _, err := claimMutationClockTx(tx, "like", "sample_claim", "set", 10); err != nil {
			return err
		}
		if _, err := advanceMutationClockTx(tx, "bookmark", "sample_advance", "set", 20); err != nil {
			return err
		}
		if err := advanceMutationClocksTx(tx, "follow", "set", `
			SELECT 'sample_bulk_a' AS item_key, 30 AS updated_at_ms
			UNION ALL
			SELECT 'sample_bulk_b' AS item_key, 31 AS updated_at_ms
		`); err != nil {
			return err
		}
		if _, err := mutateChannelSettingsTx(tx, "sample_settings", map[string]any{
			"media_only": true,
		}, 40, true); err != nil {
			return err
		}
		_, err := advanceMutationClockTx(tx, "progress", "sample_progress", "set", 50)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"tweet/sample_claim":      true,
		"tweet/sample_advance":    true,
		"channel/sample_bulk_a":   true,
		"channel/sample_bulk_b":   true,
		"channel/sample_settings": true,
	}
	rows, err := d.conn.Query(`
		SELECT owner_kind, owner_id
		FROM feed_order_invalidations
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	got := make(map[string]bool)
	for rows.Next() {
		var kind, id string
		if err := rows.Scan(&kind, &id); err != nil {
			t.Fatal(err)
		}
		got[kind+"/"+id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("queued owners = %v, want %v", got, want)
	}
	for owner := range want {
		if !got[owner] {
			t.Fatalf("queued owners = %v, missing %s", got, owner)
		}
	}
	if got["tweet/sample_progress"] {
		t.Fatal("progress mutation entered the feed-order queue")
	}
}

func TestStaleAndRolledBackMutationsDoNotQueueFeedOrderWork(t *testing.T) {
	d := openWritableTestDB(t)
	if err := d.WithWrite(func(tx *sql.Tx) error {
		_, err := claimMutationClockTx(tx, "like", "sample_item", "set", 100)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.ExecRaw(`DELETE FROM feed_order_invalidations`); err != nil {
		t.Fatal(err)
	}

	if err := d.WithWrite(func(tx *sql.Tx) error {
		applied, err := claimMutationClockTx(tx, "like", "sample_item", "set", 100)
		if err != nil {
			return err
		}
		if applied {
			t.Fatal("idempotent mutation was applied")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.WithWrite(func(tx *sql.Tx) error {
		_, err := claimMutationClockTx(tx, "like", "sample_item", "clear", 99)
		if !IsStaleMutation(err) {
			t.Fatalf("older mutation error = %v, want stale mutation", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	rollbackErr := errors.New("rollback mutation")
	err := d.WithWrite(func(tx *sql.Tx) error {
		if _, err := claimMutationClockTx(tx, "star", "sample_channel", "set", 200); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("rolled back mutation error = %v", err)
	}

	var queued int
	if err := d.QueryRow(`SELECT COUNT(*) FROM feed_order_invalidations`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("queued owners after ignored mutations = %d, want 0", queued)
	}
	var rolledBackClock int
	if err := d.QueryRow(`
		SELECT COUNT(*) FROM mutation_clocks
		WHERE kind = 'star' AND item_key = 'sample_channel'
	`).Scan(&rolledBackClock); err != nil {
		t.Fatal(err)
	}
	if rolledBackClock != 0 {
		t.Fatalf("rolled back mutation clocks = %d, want 0", rolledBackClock)
	}
}

func TestFeedOrderInvalidationDrainCapsNormalOwnerLanes(t *testing.T) {
	d := openWritableTestDB(t)
	if err := d.ExecRaw(`
		WITH RECURSIVE ids(value) AS (
			VALUES(0)
			UNION ALL SELECT value + 1 FROM ids WHERE value < 64
		)
		INSERT INTO feed_order_invalidations (owner_kind, owner_id)
		SELECT 'tweet', printf('sample_tweet_%03d', value) FROM ids
	`); err != nil {
		t.Fatal(err)
	}
	if err := d.ExecRaw(`
		WITH RECURSIVE ids(value) AS (
			VALUES(0)
			UNION ALL SELECT value + 1 FROM ids WHERE value < 4
		)
		INSERT INTO feed_order_invalidations (owner_kind, owner_id)
		SELECT 'channel', printf('sample_channel_%03d', value) FROM ids
	`); err != nil {
		t.Fatal(err)
	}

	processed, err := d.DrainFeedOrderInvalidations(context.Background(), 64, 4)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 68 {
		t.Fatalf("processed owners = %d, want 68", processed)
	}
	for kind, want := range map[string]int{"tweet": 1, "channel": 1} {
		var remaining int
		if err := d.QueryRow(`
			SELECT COUNT(*) FROM feed_order_invalidations WHERE owner_kind = ?
		`, kind).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining != want {
			t.Fatalf("remaining %s owners = %d, want %d", kind, remaining, want)
		}
	}
}

func TestFeedOrderInvalidationOverflowResetsAndClearsAtomically(t *testing.T) {
	d := openWritableTestDB(t)
	if err := d.ExecRaw(`
		INSERT INTO feed_items (tweet_id, body_text, algo_scored_at)
		VALUES ('sample_scored', 'body', 303);
		WITH RECURSIVE ids(value) AS (
			VALUES(0)
			UNION ALL SELECT value + 1 FROM ids WHERE value < 1024
		)
		INSERT INTO feed_order_invalidations (owner_kind, owner_id)
		SELECT 'tweet', printf('sample_owner_%04d', value) FROM ids;
		CREATE TRIGGER block_feed_order_queue_delete
		BEFORE DELETE ON feed_order_invalidations
		BEGIN
			SELECT RAISE(ABORT, 'blocked queue delete');
		END;
	`); err != nil {
		t.Fatal(err)
	}

	processed, err := d.DrainFeedOrderInvalidations(context.Background(), 64, 4)
	if err == nil || processed != 0 {
		t.Fatalf("blocked overflow drain = (%d, %v), want rollback error", processed, err)
	}
	assertFeedOrderOverflowState(t, d, 303, 1025)

	if err := d.ExecRaw(`DROP TRIGGER block_feed_order_queue_delete`); err != nil {
		t.Fatal(err)
	}
	processed, err = d.DrainFeedOrderInvalidations(context.Background(), 64, 4)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1025 {
		t.Fatalf("overflow processed owners = %d, want 1025", processed)
	}
	assertFeedOrderOverflowState(t, d, 0, 0)
}

func assertFeedOrderOverflowState(t *testing.T, d *DB, wantScore int64, wantQueued int) {
	t.Helper()
	var score int64
	if err := d.QueryRow(`
		SELECT algo_scored_at FROM feed_items WHERE tweet_id = 'sample_scored'
	`).Scan(&score); err != nil {
		t.Fatal(err)
	}
	if score != wantScore {
		t.Fatalf("algo_scored_at = %d, want %d", score, wantScore)
	}
	var queued int
	if err := d.QueryRow(`SELECT COUNT(*) FROM feed_order_invalidations`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != wantQueued {
		t.Fatalf("queued owners = %d, want %d", queued, wantQueued)
	}
}
