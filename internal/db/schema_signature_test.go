package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestValidateCurrentSchemaAcceptsEquivalentIndexWhitespace(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	if err := EnsureSchema(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
		DROP INDEX idx_video_metadata_jobs_ready;
		CREATE INDEX idx_video_metadata_jobs_ready
			ON video_metadata_jobs(status, next_attempt_at_ms, lease_until_ms, requested_at_ms);
	`); err != nil {
		t.Fatal(err)
	}

	if err := ValidateCurrentSchema(conn); err != nil {
		t.Fatalf("ValidateCurrentSchema rejected equivalent index formatting: %v", err)
	}
}

func TestNormalizeSchemaDefinitionPreservesQuotedWhitespace(t *testing.T) {
	withTwoSpaces := normalizeSchemaDefinition(`CREATE INDEX sample ON records(value) WHERE value = 'two  spaces'`)
	withOneSpace := normalizeSchemaDefinition(`CREATE INDEX sample ON records(value) WHERE value = 'two spaces'`)
	if withTwoSpaces == withOneSpace {
		t.Fatal("normalization changed whitespace inside a quoted value")
	}
}
