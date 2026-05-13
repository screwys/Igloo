package db

func schemaLegacyMigrationStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at_ms INTEGER NOT NULL
		)`,
	}
}
