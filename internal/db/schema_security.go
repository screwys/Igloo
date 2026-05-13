package db

func schemaSecurityStateStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS auth_sessions (
			session_id         TEXT PRIMARY KEY,
			username           TEXT NOT NULL,
			created_at_ms      INTEGER NOT NULL,
			last_active_at_ms  INTEGER NOT NULL,
			revoked            INTEGER NOT NULL DEFAULT 0,
			revoke_reason      TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions(username, revoked)`,

		`CREATE TABLE IF NOT EXISTS auth_refresh_tokens (
			token_id           TEXT PRIMARY KEY,
			session_id         TEXT NOT NULL,
			issued_at_ms       INTEGER NOT NULL,
			expires_at_ms      INTEGER NOT NULL,
			consumed_at_ms     INTEGER,
			FOREIGN KEY (session_id) REFERENCES auth_sessions(session_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_refresh_session ON auth_refresh_tokens(session_id)`,
	}
}
