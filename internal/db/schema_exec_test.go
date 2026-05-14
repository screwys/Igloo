package db

import (
	"database/sql"
	"strings"
	"testing"
)

func TestExecIdempotentAddColumnIgnoresOnlyDuplicateColumn(t *testing.T) {
	conn := openBareSchemaTestDB(t)
	if _, err := conn.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatalf("create test table: %v", err)
	}

	if err := execIdempotentAddColumn(conn, `ALTER TABLE items ADD COLUMN title TEXT`); err != nil {
		t.Fatalf("duplicate column should be ignored: %v", err)
	}

	err := execIdempotentAddColumn(conn, `ALTER TABLE missing_items ADD COLUMN title TEXT`)
	if err == nil {
		t.Fatal("missing table add-column error was ignored")
	}
	if !strings.Contains(err.Error(), "schema add column") {
		t.Fatalf("error = %v, want schema add-column context", err)
	}
}

func TestExecIdempotentIndexIgnoresOnlyExistingIndex(t *testing.T) {
	conn := openBareSchemaTestDB(t)
	if _, err := conn.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatalf("create test table: %v", err)
	}
	if _, err := conn.Exec(`CREATE INDEX idx_items_title ON items(title)`); err != nil {
		t.Fatalf("create initial index: %v", err)
	}

	if err := execIdempotentIndex(conn, `CREATE INDEX idx_items_title ON items(title)`); err != nil {
		t.Fatalf("existing index should be ignored: %v", err)
	}

	err := execIdempotentIndex(conn, `CREATE INDEX idx_items_missing ON items(missing_column)`)
	if err == nil {
		t.Fatal("missing column index error was ignored")
	}
	if !strings.Contains(err.Error(), "schema create index") {
		t.Fatalf("error = %v, want schema create-index context", err)
	}
}

func openBareSchemaTestDB(t *testing.T) *sql.DB {
	t.Helper()

	conn, err := sql.Open("sqlite", "file:"+t.TempDir()+"/schema.db")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
