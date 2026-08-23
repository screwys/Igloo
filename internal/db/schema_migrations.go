package db

import (
	"database/sql"
	"fmt"
	"time"
)

type schemaMigration struct {
	name  string
	apply func(*sql.Tx) error
}

func schemaMigrationStatements() []string {
	return []string{schemaMigrationLedgerStatement()}
}

func schemaMigrationLedgerStatement() string {
	return `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at_ms INTEGER NOT NULL
	) WITHOUT ROWID`
}

var schemaMigrations = []schemaMigration{
	{
		name:  "20260823_add_discover_temp_provenance",
		apply: addDiscoverTempProvenance,
	},
	{
		name:  "20260823_add_youtube_discover",
		apply: addYouTubeDiscover,
	},
	{
		name:  "20260814_index_android_sync_peer_triggers",
		apply: installAndroidSyncPeerTriggers,
	},
	{
		name:  "20260813_add_feed_related_anchor_indexes",
		apply: addFeedRelatedAnchorIndexes,
	},
	{
		name:  "20260809_add_video_metadata_jobs",
		apply: addVideoMetadataJobs,
	},
	{
		name:  "20260809_hide_x_profile_history_from_feed",
		apply: hideXProfileHistoryFromFeed,
	},
	{
		name:  "20260809_retire_feed_items_pin",
		apply: retireFeedItemsPin,
	},
	{
		name:  "20260802_install_android_sync_peer_triggers",
		apply: installAndroidSyncPeerTriggers,
	},
	{
		name:  "20260801_add_feed_order_invalidation_queue",
		apply: addFeedOrderInvalidationQueue,
	},
	{
		name:  "20260801_add_youtube_member_only_channel_setting",
		apply: addYouTubeMemberOnlyChannelSetting,
	},
	{
		name:  "20260731_repair_android_moments_cursor_heads",
		apply: repairAndroidMomentsCursorHeads,
	},
	{
		name:  "20260727_add_video_fetch_history",
		apply: addVideoFetchHistory,
	},
	{
		name:  "20260727_collapse_fetched_introduced_sources",
		apply: collapseFetchedIntroducedSources,
	},
	{
		name:  "20260724_add_temp_download_queue",
		apply: addTempDownloadQueue,
	},
	{
		name:  "20260718_add_videos_is_temp",
		apply: addVideosIsTempColumn,
	},
}

func addDiscoverTempProvenance(tx *sql.Tx) error {
	if _, err := tx.Exec(youtubeRecommendationsTableStatement()); err != nil {
		return err
	}
	if _, err := tx.Exec(discoverTempDownloadsTableStatement()); err != nil {
		return err
	}
	_, err := tx.Exec(`
		INSERT OR IGNORE INTO discover_temp_downloads (video_id, downloaded_at_ms)
		SELECT DISTINCT v.video_id, v.downloaded_at
		FROM videos v
		JOIN youtube_recommendations rec
		JOIN json_each(rec.candidates_json) candidate
		  ON json_extract(candidate.value, '$.video_id') = v.video_id
		WHERE COALESCE(v.is_temp, 0) = 1
	`)
	return err
}

func addYouTubeDiscover(tx *sql.Tx) error {
	if _, err := tx.Exec(youtubeRecommendationsTableStatement()); err != nil {
		return err
	}
	if _, err := tx.Exec(tempDownloadQueueTableStatement()); err != nil {
		return err
	}
	hasOrigin, err := schemaColumnExists(tx, "temp_download_queue", "origin")
	if err != nil {
		return err
	}
	if !hasOrigin {
		if _, err := tx.Exec(`ALTER TABLE temp_download_queue ADD COLUMN origin TEXT NOT NULL DEFAULT 'interactive' CHECK(origin IN ('interactive', 'discover'))`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_temp_download_queue_ready`); err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_temp_download_queue_ready ON temp_download_queue(origin, status, next_attempt_at_ms, lease_until_ms, added_at_ms)`,
		`CREATE INDEX IF NOT EXISTS idx_youtube_recommendations_ready ON youtube_recommendations(status, next_attempt_at_ms, lease_until_ms, requested_at_ms)`,
		`CREATE INDEX IF NOT EXISTS idx_youtube_recommendations_fetched ON youtube_recommendations(fetched_at_ms DESC, anchor_video_id)`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func addFeedRelatedAnchorIndexes(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feed_seen_at ON feed_seen(seen_at, tweet_id)`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_feed_rank_snapshot_history_tweet ON feed_rank_snapshot_history(tweet_id, computed_at)`)
	return err
}

func addVideoMetadataJobs(tx *sql.Tx) error {
	if _, err := tx.Exec(videoMetadataJobsTableStatement()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_video_metadata_jobs_ready`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_video_metadata_jobs_ready ON video_metadata_jobs(status, next_attempt_at_ms, lease_until_ms, requested_at_ms)`); err != nil {
		return err
	}
	nowMs := time.Now().UnixMilli()
	_, err := tx.Exec(`
		INSERT OR IGNORE INTO video_metadata_jobs (
			video_id, status, next_attempt_at_ms, requested_at_ms, updated_at_ms
		)
		SELECT video_id, 'pending', 0, ?, ?
		FROM videos
		WHERE owner_kind = 'youtube_video'
		  AND downloaded_at >= ?
	`, nowMs, nowMs, nowMs-int64(48*time.Hour/time.Millisecond))
	return err
}

func hideXProfileHistoryFromFeed(tx *sql.Tx) error {
	_, err := tx.Exec(`
		WITH ranked AS (
			SELECT fi.tweet_id,
			       ROW_NUMBER() OVER (
			         PARTITION BY fi.source_channel_id, fi.fetched_at
			         ORDER BY fi.published_at DESC, fi.tweet_id DESC
			       ) AS batch_position,
			       COUNT(*) OVER (
			         PARTITION BY fi.source_channel_id, fi.fetched_at
			       ) AS batch_size,
			       COALESCE(
			         NULLIF(cs.media_download_limit, 0),
			         NULLIF(CAST((SELECT value FROM settings WHERE key = 'media_download_limit_default') AS INTEGER), 0),
			         20
			       ) AS feed_limit
			FROM feed_items fi
			LEFT JOIN channel_settings cs ON cs.channel_id = fi.source_channel_id
			WHERE fi.source_channel_id LIKE 'twitter_%'
			  AND fi.fetched_at >= 1786177380000
		)
		INSERT INTO feed_seen (tweet_id, seen_at)
		SELECT tweet_id, ?
		FROM ranked
		WHERE batch_size > feed_limit AND batch_position > feed_limit
		ON CONFLICT(tweet_id) DO NOTHING
	`, time.Now().UnixMilli())
	return err
}

func retireFeedItemsPin(tx *sql.Tx) error {
	if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_feed_items_pinned_author`); err != nil {
		return err
	}
	hasColumn, err := schemaColumnExists(tx, "feed_items", "is_pinned")
	if err != nil {
		return err
	}
	if !hasColumn {
		return nil
	}
	_, err = tx.Exec(`ALTER TABLE feed_items DROP COLUMN is_pinned`)
	return err
}

func installAndroidSyncPeerTriggers(tx *sql.Tx) error {
	return ensureAndroidSyncHeadTriggers(tx)
}

func addFeedOrderInvalidationQueue(tx *sql.Tx) error {
	_, err := tx.Exec(feedOrderInvalidationQueueStatement())
	return err
}

func addYouTubeMemberOnlyChannelSetting(tx *sql.Tx) error {
	hasColumn, err := schemaColumnExists(tx, "channel_settings", "include_member_only")
	if err != nil {
		return err
	}
	if !hasColumn {
		if _, err := tx.Exec(`ALTER TABLE channel_settings ADD COLUMN include_member_only INTEGER`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DROP TRIGGER IF EXISTS android_sync_head_channel_settings_update`); err != nil {
		return err
	}
	return ensureAndroidSyncHeadTriggers(tx)
}

func addVideoFetchHistory(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS video_fetch_history (
		video_id TEXT PRIMARY KEY,
		fetched_at_ms INTEGER NOT NULL DEFAULT 0
	) WITHOUT ROWID`); err != nil {
		return err
	}
	_, err := tx.Exec(`
		INSERT OR IGNORE INTO video_fetch_history (video_id, fetched_at_ms)
		SELECT video_id, downloaded_at
		FROM videos
		WHERE downloaded_at > 0
	`)
	return err
}

func collapseFetchedIntroducedSources(tx *sql.Tx) error {
	return collapseFetchedIntroducedSourcesTx(tx, "")
}

func addTempDownloadQueue(tx *sql.Tx) error {
	_, err := tx.Exec(tempDownloadQueueTableStatement())
	if err != nil {
		return err
	}
	_, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_temp_download_queue_ready ON temp_download_queue(origin, status, next_attempt_at_ms, lease_until_ms, added_at_ms)`)
	return err
}

// ApplySchemaMigrations advances an existing database through the ordered,
// named transitions required by the current schema. Each migration and its
// ledger entry commit together, so a failed upgrade can be retried safely.
func ApplySchemaMigrations(conn *sql.DB) error {
	if _, err := conn.Exec(schemaMigrationLedgerStatement()); err != nil {
		return fmt.Errorf("create schema migration ledger: %w", err)
	}
	for index := len(schemaMigrations) - 1; index >= 0; index-- {
		if _, err := runSchemaMigrationOnce(conn, schemaMigrations[index]); err != nil {
			return err
		}
	}
	return nil
}

func runSchemaMigrationOnce(conn *sql.DB, migration schemaMigration) (bool, error) {
	tx, err := conn.Begin()
	if err != nil {
		return false, fmt.Errorf("begin schema migration %s: %w", migration.name, err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name = ?)`, migration.name).Scan(&exists); err != nil {
		return false, fmt.Errorf("check schema migration %s: %w", migration.name, err)
	}
	if exists {
		return false, tx.Commit()
	}

	if err := migration.apply(tx); err != nil {
		return false, fmt.Errorf("run schema migration %s: %w", migration.name, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (name, applied_at_ms) VALUES (?, ?)`,
		migration.name,
		time.Now().UnixMilli(),
	); err != nil {
		return false, fmt.Errorf("record schema migration %s: %w", migration.name, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit schema migration %s: %w", migration.name, err)
	}
	return true, nil
}

func addVideosIsTempColumn(tx *sql.Tx) error {
	hasColumn, err := schemaColumnExists(tx, "videos", "is_temp")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}
	if _, err := tx.Exec(`ALTER TABLE videos ADD COLUMN is_temp INTEGER DEFAULT 0`); err != nil {
		return err
	}
	return nil
}

type schemaColumnQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func schemaColumnExists(conn schemaColumnQuerier, table, column string) (bool, error) {
	rows, err := conn.Query(`PRAGMA table_xinfo(` + quoteSchemaIdentifier(table) + `)`)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid, notNull, primaryKey, hidden int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey, &hidden); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}
