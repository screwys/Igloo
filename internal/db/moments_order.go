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

// ReconcileMomentsOrder restores returning videos to their saved positions and appends
// first arrivals. Positions survive content deletion. Event time only orders new arrivals
// within their batch before assigning positions after the current tail.
func (db *DB) ReconcileMomentsOrder(scope string) error {
	scope = NormalizeMomentsTab(scope)
	positionColumn, ok := momentsPositionColumn(scope)
	if !ok {
		return nil
	}
	visibleCTE := db.shortsVisibleCTEForUnpositioned(scope, positionColumn)
	return db.WithWrite(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO moments_order_counters (scope, next_position)
			SELECT $1, COALESCE((SELECT MAX(order_position) FROM moments_order_history WHERE scope = $2), 0) + 1
			WHERE NOT EXISTS (SELECT 1 FROM moments_order_counters WHERE scope = $3)`, scope, scope, scope); err != nil {
			return err
		}
		rows, err := tx.Query(bind(visibleCTE+`
			SELECT v.video_id, COALESCE(history.order_position, 0)
			FROM visible v
			LEFT JOIN moments_order_history history ON history.video_id = v.video_id AND history.scope = ?
			ORDER BY v.effective_moment_at_ms ASC, v.video_id ASC`), scope)
		if err != nil {
			return err
		}
		type momentPosition struct {
			videoID  string
			position int64
		}
		var positions []momentPosition
		for rows.Next() {
			var item momentPosition
			if err := rows.Scan(&item.videoID, &item.position); err != nil {
				_ = rows.Close()
				return err
			}
			positions = append(positions, item)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(positions) == 0 {
			return nil
		}
		var next int64
		if err := tx.QueryRow(`SELECT next_position FROM moments_order_counters WHERE scope = $1`, scope).Scan(&next); err != nil {
			return err
		}
		stmt, err := tx.Prepare(bind(`UPDATE videos SET ` + positionColumn + ` = ? WHERE video_id = ? AND ` + positionColumn + ` = 0`))
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, item := range positions {
			if item.position == 0 {
				item.position = next
				if _, err := tx.Exec(`INSERT INTO moments_order_history (scope, video_id, order_position)
					VALUES ($1, $2, $3)`, scope, item.videoID, item.position); err != nil {
					return err
				}
				next++
			}
			if _, err := stmt.Exec(item.position, item.videoID); err != nil {
				return fmt.Errorf("assign %s Moments position: %w", scope, err)
			}
		}
		_, err = tx.Exec(`UPDATE moments_order_counters SET next_position = $1 WHERE scope = $2`, next, scope)
		return err
	})
}

func (db *DB) GetMomentsPosition(videoID, scope string) (int64, bool, error) {
	positionColumn, ok := momentsPositionColumn(scope)
	if !ok {
		return 0, false, nil
	}
	var position int64
	err := db.reader().QueryRow(bind(`SELECT `+positionColumn+` FROM videos WHERE video_id = ?`), videoID).Scan(&position)
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
		CROSS JOIN visible v
		JOIN videos stored ON stored.video_id = v.video_id
		  AND stored.` + positionColumn + ` <= candidate.position
		GROUP BY candidate.video_id`
	var videoID string
	var ordinal int
	err := db.reader().QueryRow(bind(query), position, position, position).Scan(&videoID, &ordinal)
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
