package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"modernc.org/sqlite"
)

func execIdempotentAddColumn(conn *sql.DB, stmt string) error {
	if _, err := conn.Exec(stmt); err != nil {
		if isSQLiteDuplicateColumnError(err) {
			return nil
		}
		return fmt.Errorf("schema add column %q: %w", stmt, err)
	}
	return nil
}

func execIdempotentIndex(conn *sql.DB, stmt string) error {
	if _, err := conn.Exec(stmt); err != nil {
		if isSQLiteIndexExistsError(err) {
			return nil
		}
		return fmt.Errorf("schema create index %q: %w", stmt, err)
	}
	return nil
}

func isSQLiteDuplicateColumnError(err error) bool {
	return sqliteErrorContains(err, "duplicate column name:")
}

func isSQLiteIndexExistsError(err error) bool {
	return sqliteErrorContains(err, "index ", " already exists")
}

func sqliteErrorContains(err error, fragments ...string) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code() != 1 {
		return false
	}
	msg := strings.ToLower(sqliteErr.Error())
	for _, fragment := range fragments {
		if !strings.Contains(msg, fragment) {
			return false
		}
	}
	return true
}
