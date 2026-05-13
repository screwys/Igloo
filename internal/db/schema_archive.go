package db

func schemaArchiveStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id TEXT UNIQUE NOT NULL,
			source_id TEXT,
			name TEXT NOT NULL,
			url TEXT,
			platform TEXT,
			quality TEXT,
			last_checked INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			video_id TEXT UNIQUE NOT NULL,
			channel_id TEXT NOT NULL,
			title TEXT,
			description TEXT,
			duration INTEGER,
			thumbnail_path TEXT,
			file_path TEXT,
			file_size INTEGER,
			published_at INTEGER NOT NULL DEFAULT 0,
			downloaded_at INTEGER NOT NULL DEFAULT 0,
			watched INTEGER DEFAULT 0,
			is_temp INTEGER DEFAULT 0,
			is_pinned INTEGER DEFAULT 0,
			metadata_json TEXT,
			source_kind TEXT DEFAULT '',
			dearrow_title TEXT,
			dearrow_title_casual TEXT,
			dearrow_thumb_path TEXT,
			dearrow_checked_at INTEGER
		)`,

		`CREATE TABLE IF NOT EXISTS feed_items (
			tweet_id TEXT PRIMARY KEY,
			source_handle TEXT,
			author_handle TEXT NOT NULL,
			author_display_name TEXT,
			author_avatar_url TEXT,
			body_text TEXT,
			lang TEXT,
			is_retweet INTEGER DEFAULT 0,
			retweeted_by_handle TEXT,
			retweeted_by_display_name TEXT,
			quote_tweet_id TEXT,
			quote_author_handle TEXT,
			quote_author_display_name TEXT,
			quote_author_avatar_url TEXT,
			quote_body_text TEXT,
			quote_lang TEXT,
			quote_media_json TEXT,
			media_json TEXT,
			canonical_url TEXT,
			reply_to_handle TEXT,
			reply_to_status TEXT,
			is_reply INTEGER DEFAULT 0,
			is_ghost INTEGER DEFAULT 0,
			quote_published_at INTEGER NOT NULL DEFAULT 0,
			views INTEGER,
			likes INTEGER,
			retweets INTEGER,
			published_at INTEGER NOT NULL DEFAULT 0,
			fetched_at INTEGER NOT NULL DEFAULT 0,
			content_hash TEXT,
			canonical_tweet_id TEXT,
			media_status TEXT
		)`,

		`CREATE TABLE IF NOT EXISTS feed_sources (
			source_id TEXT PRIMARY KEY,
			platform TEXT NOT NULL,
			source_type TEXT NOT NULL,
			external_id TEXT NOT NULL,
			label TEXT NOT NULL,
			url TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			last_checked INTEGER,
			last_ok INTEGER,
			last_error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_feed_sources_platform ON feed_sources(platform, enabled)`,

		`CREATE TABLE IF NOT EXISTS feed_item_sources (
			tweet_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			first_seen_at INTEGER NOT NULL DEFAULT 0,
			last_seen_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (tweet_id, source_id),
			FOREIGN KEY (source_id) REFERENCES feed_sources(source_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_feed_item_sources_source ON feed_item_sources(source_id, last_seen_at DESC)`,

		`CREATE TABLE IF NOT EXISTS video_comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			video_id TEXT NOT NULL,
			comment_id TEXT NOT NULL,
			parent_id TEXT,
			author_name TEXT,
			author_id TEXT,
			author_thumbnail TEXT,
			text TEXT,
			like_count INTEGER,
			published_at INTEGER NOT NULL DEFAULT 0,
			platform TEXT DEFAULT 'youtube',
			fetched_at INTEGER NOT NULL DEFAULT 0,
			UNIQUE(video_id, comment_id)
		)`,

		`CREATE TABLE IF NOT EXISTS sponsorblock_checked (
			video_id TEXT PRIMARY KEY,
			checked_at INTEGER NOT NULL DEFAULT 0,
			video_age_at_check TEXT
		)`,

		`CREATE TABLE IF NOT EXISTS sponsorblock_segments (
			video_id TEXT NOT NULL,
			start_time REAL NOT NULL,
			end_time REAL NOT NULL,
			category TEXT NOT NULL,
			PRIMARY KEY (video_id, start_time)
		)`,

		`CREATE TABLE IF NOT EXISTS retweet_sources (
			content_hash TEXT NOT NULL,
			retweeter_handle TEXT NOT NULL,
			retweeter_display_name TEXT,
			tweet_id TEXT NOT NULL,
			published_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (content_hash, retweeter_handle)
		)`,

		`CREATE TABLE IF NOT EXISTS video_repost_sources (
			video_id TEXT NOT NULL,
			reposter_channel_id TEXT NOT NULL,
			reposter_handle TEXT NOT NULL DEFAULT '',
			reposter_display_name TEXT,
			reposted_at_ms INTEGER NOT NULL DEFAULT 0,
			first_seen_at_ms INTEGER NOT NULL DEFAULT 0,
			updated_at_ms INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (video_id, reposter_channel_id)
		)`,

		`CREATE TABLE IF NOT EXISTS channel_profiles (
			channel_id     TEXT PRIMARY KEY,
			platform       TEXT NOT NULL,
			handle         TEXT,
			display_name   TEXT,
			bio            TEXT,
			website        TEXT,
			followers      INTEGER DEFAULT 0,
			following      INTEGER DEFAULT 0,
			verified       INTEGER DEFAULT 0,
			verified_type  TEXT,
			protected      INTEGER DEFAULT 0,
			avatar_url     TEXT,
			banner_url     TEXT,
			fetched_at     INTEGER NOT NULL DEFAULT 0,
			fail_count     INTEGER DEFAULT 0,
			next_retry_at  INTEGER NOT NULL DEFAULT 0,
			tombstone      INTEGER DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS media_files (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			owner_type  TEXT    NOT NULL,
			owner_id    TEXT    NOT NULL,
			media_index INTEGER DEFAULT 0,
			file_path   TEXT    NOT NULL,
			media_type  TEXT,
			source_url  TEXT,
			file_size   INTEGER,
			created_at  INTEGER NOT NULL DEFAULT 0,
			UNIQUE(owner_type, owner_id, media_index)
		)`,
	}
}
