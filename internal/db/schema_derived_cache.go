package db

func schemaDerivedCacheStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS android_sync_generations (
			generation_id              TEXT PRIMARY KEY,
			created_at_ms              INTEGER NOT NULL,
			status                     TEXT NOT NULL,
			source_version             TEXT NOT NULL,
			retention_json             TEXT NOT NULL,
			item_count                 INTEGER NOT NULL DEFAULT 0,
			asset_count                INTEGER NOT NULL DEFAULT 0,
			ready_asset_count          INTEGER NOT NULL DEFAULT 0,
			server_missing_asset_count INTEGER NOT NULL DEFAULT 0,
			total_bytes                INTEGER NOT NULL DEFAULT 0,
			content_counts_json        TEXT NOT NULL DEFAULT '{}',
			asset_counts_json          TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_android_sync_generations_latest ON android_sync_generations(status, created_at_ms DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_android_sync_generations_source ON android_sync_generations(source_version)`,

		`CREATE TABLE IF NOT EXISTS android_sync_items (
			generation_id TEXT NOT NULL,
			seq           INTEGER NOT NULL,
			item_kind     TEXT NOT NULL,
			item_id       TEXT NOT NULL,
			payload_json  TEXT NOT NULL,
			PRIMARY KEY (generation_id, seq),
			FOREIGN KEY (generation_id) REFERENCES android_sync_generations(generation_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_android_sync_items_page ON android_sync_items(generation_id, seq)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_android_sync_items_identity ON android_sync_items(generation_id, item_kind, item_id)`,

		`CREATE TABLE IF NOT EXISTS android_sync_assets (
			generation_id        TEXT NOT NULL,
			seq                  INTEGER NOT NULL,
			asset_id             TEXT NOT NULL,
			asset_kind           TEXT NOT NULL,
			owner_id             TEXT NOT NULL,
			owner_kind           TEXT NOT NULL,
			bucket               TEXT NOT NULL,
			server_url           TEXT NOT NULL,
			content_type         TEXT NOT NULL DEFAULT '',
			size_bytes           INTEGER NOT NULL DEFAULT 0,
			sha256               TEXT NOT NULL DEFAULT '',
			state                TEXT NOT NULL DEFAULT 'ready',
			required_reason      TEXT NOT NULL DEFAULT '',
			is_auto              INTEGER,
			audio_language       TEXT NOT NULL DEFAULT '',
			effective_recency_ms INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (generation_id, seq),
			FOREIGN KEY (generation_id) REFERENCES android_sync_generations(generation_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_android_sync_assets_page ON android_sync_assets(generation_id, seq)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_android_sync_assets_identity ON android_sync_assets(generation_id, asset_id, asset_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_android_sync_assets_lookup ON android_sync_assets(asset_id, asset_kind, generation_id)`,

		`CREATE TABLE IF NOT EXISTS translations (
			tweet_id        TEXT NOT NULL,
			field           TEXT NOT NULL,
			source_lang     TEXT NOT NULL,
			target_lang     TEXT NOT NULL,
			translated_text TEXT NOT NULL,
			translated_at   INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (tweet_id, field, target_lang)
		)`,

		`CREATE TABLE IF NOT EXISTS feed_share_account_affinity (
			username         TEXT NOT NULL,
			handle           TEXT NOT NULL,
			score            REAL DEFAULT 0,
			last_event_at_ms INTEGER,
			event_count      INTEGER DEFAULT 0,
			PRIMARY KEY (username, handle)
		)`,

		`CREATE TABLE IF NOT EXISTS feed_share_token_affinity (
			username         TEXT NOT NULL,
			token            TEXT NOT NULL,
			score            REAL DEFAULT 0,
			last_event_at_ms INTEGER,
			event_count      INTEGER DEFAULT 0,
			PRIMARY KEY (username, token)
		)`,

		`CREATE TABLE IF NOT EXISTS feed_rank_snapshot (
			username        TEXT    NOT NULL,
			tweet_id        TEXT    NOT NULL,
			rank_position   INTEGER NOT NULL,
			base_score      REAL    NOT NULL,
			decay_factor    REAL    NOT NULL,
			freshness_bonus REAL    NOT NULL,
			jitter          REAL    NOT NULL,
			diversity_demoted_by REAL NOT NULL DEFAULT 0,
			final_score     REAL    NOT NULL,
			computed_at     INTEGER NOT NULL,
			PRIMARY KEY (username, tweet_id)
		)`,
	}
}
