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

// ReconcileMomentsOrder appends every newly eligible video as one server-owned batch. Assigned
// positions survive temporary invisibility; the batch uses effective event time only to mix its
// members before assigning final positions after the current tail.
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

func (db *DB) GetMomentsPosition(videoID, scope string) (int64, bool, error) {
	positionColumn, ok := momentsPositionColumn(scope)
	if !ok {
		return 0, false, nil
	}
	var position int64
	err := db.reader().QueryRow(`SELECT `+positionColumn+` FROM videos WHERE video_id = ?`, videoID).Scan(&position)
	if err == sql.ErrNoRows || position <= 0 {
		return 0, false, nil
	}
	return position, err == nil, err
}

func (db *DB) GetNearestShortsPositionTarget(position int64, scope string) (string, int, bool, error) {
	positionColumn, ok := momentsPositionColumn(scope)
	if !ok || position <= 0 {
		return "", 0, false, nil
	}
	query := db.shortsVisibleCTE(scope) + `,
		candidate AS (
			SELECT v.video_id, stored.` + positionColumn + ` AS position
			FROM visible v
			JOIN videos stored ON stored.video_id = v.video_id
			WHERE stored.` + positionColumn + ` > 0
			ORDER BY stored.` + positionColumn + ` >= ? DESC,
			         CASE WHEN stored.` + positionColumn + ` >= ? THEN stored.` + positionColumn + ` END ASC,
			         CASE WHEN stored.` + positionColumn + ` < ? THEN stored.` + positionColumn + ` END DESC
			LIMIT 1
		)
		SELECT candidate.video_id, COUNT(*)
		FROM candidate
		JOIN visible v
		JOIN videos stored ON stored.video_id = v.video_id
		  AND stored.` + positionColumn + ` <= candidate.position
		GROUP BY candidate.video_id`
	var videoID string
	var ordinal int
	err := db.reader().QueryRow(query, position, position, position).Scan(&videoID, &ordinal)
	if err == sql.ErrNoRows {
		return "", 0, false, nil
	}
	return videoID, ordinal, err == nil && ordinal > 0, err
}

func (db *DB) ReconcileAllMomentsOrders() error {
	if err := db.ReconcileMomentsOrder("following"); err != nil {
		return err
	}
	return db.ReconcileMomentsOrder("all")
}
