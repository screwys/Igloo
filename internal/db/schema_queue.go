package db

func schemaQueueStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS ingest_state (
			handle          TEXT PRIMARY KEY,
			fail_count      INTEGER DEFAULT 0,
			next_retry_at   REAL,
			last_success_at REAL,
			last_attempt_at REAL,
			last_error      TEXT,
			last_http_status INTEGER,
			avg_latency_ms  REAL,
			updated_at      INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS translation_jobs (
			tweet_id        TEXT NOT NULL,
			field           TEXT NOT NULL,
			target_lang     TEXT NOT NULL,
			source_hash     TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'queued',
			priority        INTEGER NOT NULL DEFAULT 0,
			attempts        INTEGER NOT NULL DEFAULT 0,
			next_attempt_at INTEGER NOT NULL DEFAULT 0,
			last_error_kind TEXT NOT NULL DEFAULT '',
			last_error      TEXT NOT NULL DEFAULT '',
			created_at      INTEGER NOT NULL DEFAULT 0,
			updated_at      INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (tweet_id, field, target_lang)
		)`,

		`CREATE TABLE IF NOT EXISTS download_queue (
			video_id           TEXT PRIMARY KEY,
			owner_channel_id   TEXT NOT NULL,
			title              TEXT NOT NULL DEFAULT '',
			published_at_ms    INTEGER NOT NULL DEFAULT 0,
			status             TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'blocked')),
			retry_count        INTEGER NOT NULL DEFAULT 0,
			next_attempt_at_ms INTEGER NOT NULL DEFAULT 0,
			last_error_kind    TEXT NOT NULL DEFAULT '',
			last_error         TEXT NOT NULL DEFAULT '',
			lease_owner        TEXT NOT NULL DEFAULT '',
			lease_until_ms     INTEGER NOT NULL DEFAULT 0,
			added_at_ms        INTEGER NOT NULL DEFAULT 0,
			started_at_ms      INTEGER NOT NULL DEFAULT 0
		)`,

		tempDownloadQueueTableStatement(),

		videoMetadataJobsTableStatement(),

		youtubeRecommendationsTableStatement(),

		discoverGenerationTableStatement(),

		discoverRefreshAnchorsTableStatement(),

		discoverTempDownloadsTableStatement(),

		feedOrderInvalidationQueueStatement(),
	}
}

func discoverGenerationTableStatement() string {
	return `CREATE TABLE IF NOT EXISTS discover_generation (
		id INTEGER PRIMARY KEY CHECK(id = 1),
		candidates_json TEXT NOT NULL DEFAULT '[]',
		prepared_at_ms INTEGER NOT NULL DEFAULT 0,
		expires_at_ms INTEGER NOT NULL DEFAULT 0,
		refresh_started_at_ms INTEGER NOT NULL DEFAULT 0
	)`
}

func discoverRefreshAnchorsTableStatement() string {
	return `CREATE TABLE IF NOT EXISTS discover_refresh_anchors (
		anchor_video_id TEXT PRIMARY KEY,
		FOREIGN KEY (anchor_video_id) REFERENCES videos(video_id) ON DELETE CASCADE
	) WITHOUT ROWID`
}

func discoverTempDownloadsTableStatement() string {
	return `CREATE TABLE IF NOT EXISTS discover_temp_downloads (
		video_id TEXT PRIMARY KEY,
		downloaded_at_ms INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (video_id) REFERENCES videos(video_id) ON DELETE CASCADE
	) WITHOUT ROWID`
}

func tempDownloadQueueTableStatement() string {
	return `CREATE TABLE IF NOT EXISTS temp_download_queue (
		url                TEXT PRIMARY KEY,
		platform           TEXT NOT NULL,
		origin             TEXT NOT NULL DEFAULT 'interactive' CHECK(origin IN ('interactive', 'discover')),
		status             TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'blocked')),
		retry_count        INTEGER NOT NULL DEFAULT 0,
		next_attempt_at_ms INTEGER NOT NULL DEFAULT 0,
		last_error_kind    TEXT NOT NULL DEFAULT '',
		last_error         TEXT NOT NULL DEFAULT '',
		lease_owner        TEXT NOT NULL DEFAULT '',
		lease_until_ms     INTEGER NOT NULL DEFAULT 0,
		added_at_ms        INTEGER NOT NULL DEFAULT 0,
		started_at_ms      INTEGER NOT NULL DEFAULT 0
	)`
}

func youtubeRecommendationsTableStatement() string {
	return `CREATE TABLE IF NOT EXISTS youtube_recommendations (
		anchor_video_id    TEXT PRIMARY KEY,
		candidates_json    TEXT NOT NULL DEFAULT '[]',
		status             TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'ready', 'blocked')),
		fetched_at_ms      INTEGER NOT NULL DEFAULT 0,
		expires_at_ms      INTEGER NOT NULL DEFAULT 0,
		attempts           INTEGER NOT NULL DEFAULT 0,
		next_attempt_at_ms INTEGER NOT NULL DEFAULT 0,
		last_error         TEXT NOT NULL DEFAULT '',
		lease_owner        TEXT NOT NULL DEFAULT '',
		lease_until_ms     INTEGER NOT NULL DEFAULT 0,
		requested_at_ms    INTEGER NOT NULL DEFAULT 0,
		updated_at_ms      INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (anchor_video_id) REFERENCES videos(video_id) ON DELETE CASCADE
	)`
}

func videoMetadataJobsTableStatement() string {
	return `CREATE TABLE IF NOT EXISTS video_metadata_jobs (
		video_id            TEXT PRIMARY KEY,
		status              TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'done', 'blocked')),
		checked_at_ms       INTEGER NOT NULL DEFAULT 0,
		video_age_at_check  TEXT NOT NULL DEFAULT '',
		attempts            INTEGER NOT NULL DEFAULT 0,
		next_attempt_at_ms  INTEGER NOT NULL DEFAULT 0,
		last_error          TEXT NOT NULL DEFAULT '',
		lease_owner         TEXT NOT NULL DEFAULT '',
		lease_until_ms      INTEGER NOT NULL DEFAULT 0,
		requested_at_ms     INTEGER NOT NULL DEFAULT 0,
		updated_at_ms       INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (video_id) REFERENCES videos(video_id) ON DELETE CASCADE
	)`
}

func feedOrderInvalidationQueueStatement() string {
	return `CREATE TABLE IF NOT EXISTS feed_order_invalidations (
		owner_kind TEXT NOT NULL CHECK (owner_kind IN ('tweet', 'channel')),
		owner_id TEXT NOT NULL CHECK (TRIM(owner_id) != ''),
		PRIMARY KEY (owner_kind, owner_id)
	) WITHOUT ROWID`
}
