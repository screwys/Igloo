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
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS temp_download_queue (
		url TEXT PRIMARY KEY,
		platform TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'blocked')),
		retry_count INTEGER NOT NULL DEFAULT 0,
		next_attempt_at_ms INTEGER NOT NULL DEFAULT 0,
		last_error_kind TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		lease_owner TEXT NOT NULL DEFAULT '',
		lease_until_ms INTEGER NOT NULL DEFAULT 0,
		added_at_ms INTEGER NOT NULL DEFAULT 0,
		started_at_ms INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_temp_download_queue_ready ON temp_download_queue(status, next_attempt_at_ms, lease_until_ms, added_at_ms)`)
	return err
}

// ApplySchemaMigrations advances an existing database through the ordered,
// named transitions required by the current schema. Each migration and its
// ledger entry commit together, so a failed upgrade can be retried safely.
func ApplySchemaMigrations(conn *sql.DB) error {
	if _, err := conn.Exec(schemaMigrationLedgerStatement()); err != nil {
		return fmt.Errorf("create schema migration ledger: %w", err)
	}
	for _, migration := range schemaMigrations {
		if _, err := runSchemaMigrationOnce(conn, migration); err != nil {
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
