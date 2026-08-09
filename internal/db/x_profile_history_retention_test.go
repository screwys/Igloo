package db

import (
	"strings"
	"testing"
)

func TestXProfileHistoryRetentionPlanUsesThreadIndexes(t *testing.T) {
	d := openFreshTestDB(t)
	rows, err := d.conn.Query("EXPLAIN QUERY PLAN "+xProfileHistoryRetentionItemsQuery, 0, 0, "twitter_sample_profile")
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
	if strings.Contains(plan, "SCAN reply") || strings.Contains(plan, "SCAN quote") {
		t.Fatalf("profile history retention scans feed_items for thread references: %s", plan)
	}
	if !strings.Contains(plan, "idx_feed_items_reply_parent") || !strings.Contains(plan, "idx_feed_items_quote") {
		t.Fatalf("profile history retention plan = %s", plan)
	}
}

func TestPruneXProfileHistoryKeepsSavedAndSharedRows(t *testing.T) {
	d := openWritableTestDB(t)
	if err := d.RecordAndroidFeedRetention(0, 1); err != nil {
		t.Fatal(err)
	}
	if err := d.ExecRaw(`
		INSERT INTO feed_sources (source_id, platform, source_type, external_id, label, url, enabled)
		VALUES ('twitter_list_sample', 'twitter', 'list', 'sample', 'Sample list', '', 1);

		INSERT INTO feed_items (tweet_id, source_channel_id, channel_id, body_text, published_at, fetched_at, content_hash)
		VALUES
			('newest', 'twitter_sample_profile', 'twitter_sample_profile', 'newest', 5000, 5000, 'newest'),
			('old_pruned', 'twitter_sample_profile', 'twitter_sample_profile', 'old', 4000, 4000, 'old'),
			('saved_old', 'twitter_sample_profile', 'twitter_sample_profile', 'saved', 3000, 3000, 'saved'),
			('shared_old', 'twitter_sample_profile', 'twitter_sample_profile', 'shared', 2000, 2000, 'shared'),
			('thread_parent', 'twitter_sample_profile', 'twitter_sample_profile', 'parent', 1000, 1000, 'parent'),
			('thread_child', 'twitter_other_profile', 'twitter_other_profile', 'child', 6000, 6000, 'child');

		UPDATE feed_items SET quote_tweet_id = 'thread_parent' WHERE tweet_id = 'thread_child';
		INSERT INTO bookmarks (video_id, bookmarked_at) VALUES ('saved_old', 1);
		INSERT INTO feed_item_sources (tweet_id, source_id, first_seen_at, last_seen_at)
		VALUES ('shared_old', 'twitter_list_sample', 1, 1);
		INSERT INTO feed_seen (tweet_id, seen_at) VALUES ('old_pruned', 1);
		INSERT INTO feed_rank_snapshot (tweet_id, rank_position, base_score, decay_factor, freshness_bonus, jitter, final_score, computed_at)
		VALUES ('old_pruned', 1, 1, 1, 0, 0, 1, 1);
	`); err != nil {
		t.Fatal(err)
	}

	result, err := d.PruneXProfileHistory("twitter_sample_profile", 1, 10_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if result.PrunedItems != 1 {
		t.Fatalf("pruned items = %d, want 1", result.PrunedItems)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_items WHERE tweet_id = 'old_pruned'`); got != 0 {
		t.Fatalf("ordinary old row remained: %d", got)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_items WHERE tweet_id IN ('newest','saved_old','shared_old','thread_parent')`); got != 4 {
		t.Fatalf("retained profile rows = %d, want 4", got)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_rank_snapshot WHERE tweet_id = 'old_pruned'`); got != 0 {
		t.Fatalf("rank snapshot row remained: %d", got)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_seen WHERE tweet_id = 'old_pruned'`); got != 1 {
		t.Fatalf("seen state should survive history pruning, got %d", got)
	}
}

func TestMarkXProfileHistorySeenDoesNotExpandAcrossThread(t *testing.T) {
	d := openWritableTestDB(t)
	if err := d.ExecRaw(`
		INSERT INTO feed_items (tweet_id, source_channel_id, channel_id, body_text, reply_to_status, published_at, fetched_at, content_hash)
		VALUES
			('parent', 'twitter_sample_profile', 'twitter_sample_profile', 'parent', '', 1000, 1000, 'parent'),
			('history_reply', 'twitter_sample_profile', 'twitter_sample_profile', 'reply', 'parent', 2000, 2000, 'reply');
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.MarkXProfileHistorySeen([]string{"history_reply"}, 3000); err != nil {
		t.Fatal(err)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_seen WHERE tweet_id = 'history_reply'`); got != 1 {
		t.Fatalf("history reply seen rows = %d, want 1", got)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_seen WHERE tweet_id = 'parent'`); got != 0 {
		t.Fatalf("parent should remain unseen, got %d", got)
	}
}

func TestHideXProfileHistoryFromFeedRepairsOversizedFetchBatches(t *testing.T) {
	d := openWritableTestDB(t)
	if err := d.ExecRaw(`
		INSERT INTO channels (channel_id, source_id, name, platform)
		VALUES ('twitter_sample_profile', '@sample_profile', 'Sample profile', 'twitter');
		INSERT INTO channel_settings (channel_id, media_download_limit)
		VALUES ('twitter_sample_profile', 2);
		INSERT INTO feed_items (tweet_id, source_channel_id, channel_id, body_text, published_at, fetched_at, content_hash)
		VALUES
			('batch_newest', 'twitter_sample_profile', 'twitter_sample_profile', 'newest', 4000, 1786177381000, 'newest'),
			('batch_second', 'twitter_sample_profile', 'twitter_sample_profile', 'second', 3000, 1786177381000, 'second'),
			('batch_history_a', 'twitter_sample_profile', 'twitter_sample_profile', 'history a', 2000, 1786177381000, 'history-a'),
			('batch_history_b', 'twitter_sample_profile', 'twitter_sample_profile', 'history b', 1000, 1786177381000, 'history-b');
	`); err != nil {
		t.Fatal(err)
	}
	tx, err := d.conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := hideXProfileHistoryFromFeed(tx); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_seen WHERE tweet_id IN ('batch_newest','batch_second')`); got != 0 {
		t.Fatalf("feed-window rows marked seen: %d", got)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM feed_seen WHERE tweet_id IN ('batch_history_a','batch_history_b')`); got != 2 {
		t.Fatalf("history rows marked seen = %d, want 2", got)
	}
}
