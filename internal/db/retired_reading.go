package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func (db *DB) cleanupRetiredReadingFeature() error {
	return db.runStartupMigrationOnce(
		"cleanup_retired_reading_feature",
		db.cleanupRetiredReadingFeatureNow,
		db.warnRetiredReadingFeatureReappeared,
	)
}

func (db *DB) cleanupRetiredReadingFeatureNow() error {
	if err := db.WithWrite(func(tx *sql.Tx) error {
		stmts := []string{
			`DROP INDEX IF EXISTS idx_reading_cache_cat_pub`,
			`DROP TABLE IF EXISTS reading_preferences`,
			`DROP TABLE IF EXISTS saved_articles`,
			`DROP TABLE IF EXISTS reading_articles_cache`,
			`DELETE FROM settings WHERE key = 'reading_download_dir'`,
			`UPDATE settings SET value = 'feed' WHERE key = 'starting_page' AND value = 'reading'`,
		}
		for _, stmt := range stmts {
			if _, err := tx.Exec(stmt); err != nil {
				return err
			}
		}

		return scrubRetiredReadingShortcuts(tx)
	}); err != nil {
		return err
	}

	if dir := retiredReadingArticlesDir(db.dataDir); dir != "" {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove articles directory: %w", err)
		}
	}
	return nil
}

func (db *DB) warnRetiredReadingFeatureReappeared() error {
	var count int
	if err := db.conn.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
		  AND name IN ('reading_preferences', 'saved_articles', 'reading_articles_cache')
	`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		log.Printf("schema migration cleanup_retired_reading_feature already applied, but retired reading tables exist; leaving them for investigation")
	}
	return nil
}

func retiredReadingArticlesDir(dataDir string) string {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return ""
	}
	return filepath.Join(dataDir, "articles")
}

func scrubRetiredReadingShortcuts(tx *sql.Tx) error {
	type update struct {
		userID string
		value  string
	}

	rows, err := tx.Query(`SELECT user_id, value FROM settings WHERE key = 'shortcuts'`)
	if err != nil {
		return err
	}

	var updates []update
	for rows.Next() {
		var userID, raw string
		if err := rows.Scan(&userID, &raw); err != nil {
			return err
		}

		var shortcuts map[string]any
		if err := json.Unmarshal([]byte(raw), &shortcuts); err != nil {
			continue
		}

		changed := false
		for key := range shortcuts {
			if strings.HasPrefix(key, "reading.") {
				delete(shortcuts, key)
				changed = true
			}
		}
		if !changed {
			continue
		}

		next, err := json.Marshal(shortcuts)
		if err != nil {
			return err
		}
		updates = append(updates, update{userID: userID, value: string(next)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, upd := range updates {
		if _, err := tx.Exec(`UPDATE settings SET value = ? WHERE user_id = ? AND key = 'shortcuts'`, upd.value, upd.userID); err != nil {
			return err
		}
	}
	return nil
}
