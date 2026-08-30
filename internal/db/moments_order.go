package db

import (
	"database/sql"
	"fmt"
)

func momentsPositionColumn(scope string) (string, bool) {
	switch NormalizeMomentsTab(scope) {
	case "all":
		return "moments_all_position", true
	case "following":
		return "moments_following_position", true
	default:
		return "", false
	}
}

// ReconcileMomentsOrder removes rows that are no longer eligible and appends every newly eligible
// video as one server-owned batch. Existing positions never change; the batch uses effective event
// time only to mix its members before assigning final positions after the current tail.
func (db *DB) ReconcileMomentsOrder(scope string) error {
	scope = NormalizeMomentsTab(scope)
	positionColumn, ok := momentsPositionColumn(scope)
	if !ok {
		return nil
	}
	visibleCTE := db.shortsVisibleCTE(scope)
	return db.WithWrite(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO moments_order_counters (scope, next_position)
			SELECT ?, COALESCE(MAX(`+positionColumn+`), 0) + 1 FROM videos WHERE 1
			ON CONFLICT(scope) DO NOTHING`, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(visibleCTE + `
			UPDATE videos SET ` + positionColumn + ` = 0
			WHERE ` + positionColumn + ` > 0
			  AND video_id NOT IN (SELECT video_id FROM visible)`); err != nil {
			return err
		}
		rows, err := tx.Query(visibleCTE + `
			SELECT v.video_id
			FROM visible v
			JOIN videos stored ON stored.video_id = v.video_id
			WHERE stored.` + positionColumn + ` = 0
			ORDER BY v.effective_moment_at_ms ASC, v.video_id ASC`)
		if err != nil {
			return err
		}
		var videoIDs []string
		for rows.Next() {
			var videoID string
			if err := rows.Scan(&videoID); err != nil {
				_ = rows.Close()
				return err
			}
			videoIDs = append(videoIDs, videoID)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(videoIDs) == 0 {
			return nil
		}
		var next int64
		if err := tx.QueryRow(`SELECT next_position FROM moments_order_counters WHERE scope = ?`, scope).Scan(&next); err != nil {
			return err
		}
		stmt, err := tx.Prepare(`UPDATE videos SET ` + positionColumn + ` = ? WHERE video_id = ? AND ` + positionColumn + ` = 0`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, videoID := range videoIDs {
			if _, err := stmt.Exec(next, videoID); err != nil {
				return fmt.Errorf("assign %s Moments position: %w", scope, err)
			}
			next++
		}
		_, err = tx.Exec(`UPDATE moments_order_counters SET next_position = ? WHERE scope = ?`, next, scope)
		return err
	})
}

func (db *DB) ReconcileAllMomentsOrders() error {
	if err := db.ReconcileMomentsOrder("following"); err != nil {
		return err
	}
	return db.ReconcileMomentsOrder("all")
}
