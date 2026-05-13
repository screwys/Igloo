package db

func schemaUserStateStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS channel_follows (
			user_id     TEXT    NOT NULL,
			channel_id  TEXT    NOT NULL,
			followed_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, channel_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_follows_channel ON channel_follows(channel_id)`,

		`CREATE TABLE IF NOT EXISTS channel_stars (
			user_id    TEXT    NOT NULL,
			channel_id TEXT    NOT NULL,
			starred_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, channel_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_stars_channel ON channel_stars(channel_id)`,

		`CREATE TABLE IF NOT EXISTS channel_settings (
			channel_id           TEXT PRIMARY KEY,
			media_only           INTEGER,
			include_reposts      INTEGER,
			media_download_limit INTEGER,
			max_videos           INTEGER,
			download_subtitles   INTEGER,
			updated_at           INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (channel_id) REFERENCES channels(channel_id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS settings (
			user_id TEXT NOT NULL DEFAULT '',
			key TEXT NOT NULL,
			value TEXT,
			PRIMARY KEY (user_id, key)
		)`,

		`CREATE TABLE IF NOT EXISTS watch_history (
			user_id TEXT NOT NULL,
			video_id TEXT NOT NULL,
			playback_position REAL DEFAULT 0,
			duration REAL,
			progress_updated_at_ms INTEGER,
			progress_source TEXT,
			last_watched INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, video_id)
		)`,

		`CREATE TABLE IF NOT EXISTS feed_seen (
			username TEXT NOT NULL,
			tweet_id TEXT NOT NULL,
			seen_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (username, tweet_id)
		)`,

		`CREATE TABLE IF NOT EXISTS moment_views (
			username TEXT NOT NULL,
			video_id TEXT NOT NULL,
			viewed_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (username, video_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_moment_views_user_date ON moment_views(username, viewed_at DESC)`,

		`CREATE TABLE IF NOT EXISTS feed_likes (
			username TEXT NOT NULL,
			tweet_id TEXT NOT NULL,
			source_handle TEXT,
			author_handle TEXT,
			author_display_name TEXT,
			body_text TEXT,
			link TEXT,
			canonical_x_link TEXT,
			published_at INTEGER NOT NULL DEFAULT 0,
			media_url TEXT,
			avatar_url TEXT,
			media_json TEXT,
			platform TEXT,
			quote_payload_json TEXT,
			liked_at INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (username, tweet_id)
		)`,

		`CREATE TABLE IF NOT EXISTS bookmarks (
			user_id TEXT NOT NULL DEFAULT '',
			video_id TEXT NOT NULL,
			category_id INTEGER DEFAULT 0,
			custom_title TEXT,
			account_handles TEXT,
			media_indices TEXT,
			bookmarked_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, video_id)
		)`,

		`CREATE TABLE IF NOT EXISTS bookmark_categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			name TEXT NOT NULL,
			archive_path TEXT,
			created_at INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS sync_changes (
			version INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			item_id TEXT NOT NULL,
			value TEXT,
			created_at INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS muted_accounts (
			handle TEXT PRIMARY KEY,
			muted_at INTEGER NOT NULL DEFAULT 0
		)`,
	}
}
