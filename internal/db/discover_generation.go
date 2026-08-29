package db

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/model"
	"github.com/screwys/igloo/internal/settings"
)

// BootstrapPreparedDiscoverGeneration preserves the pre-generation Discover
// cache during an upgrade. It never replaces a page once one has been stored.
func (db *DB) BootstrapPreparedDiscoverGeneration(nowMs int64, limit int) (bool, error) {
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	var existing int
	if err := db.reader().QueryRow(`SELECT COALESCE(json_array_length(candidates_json), 0) FROM discover_generation WHERE id = 1`).Scan(&existing); err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if existing > 0 {
		return false, nil
	}
	videos, err := db.ListYouTubeDiscoverVideos(limit)
	if err != nil || len(videos) == 0 {
		return false, err
	}
	payload, err := json.Marshal(videos)
	if err != nil {
		return false, err
	}
	stored := false
	err = db.WithWrite(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO discover_generation (id) VALUES (1)`); err != nil {
			return err
		}
		res, err := tx.Exec(`
			UPDATE discover_generation
			SET candidates_json = ?, prepared_at_ms = ?
			WHERE id = 1 AND COALESCE(json_array_length(candidates_json), 0) = 0`, string(payload), nowMs)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		stored = n > 0
		return err
	})
	return stored, err
}

// BeginDiscoverRefresh starts one page-generation refresh when the published
// page is due. The published JSON remains untouched until every anchor has
// finished, so readers always see one complete generation.
func (db *DB) BeginDiscoverRefresh(nowMs int64) (bool, int, error) {
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	started := false
	anchors := 0
	err := db.WithWrite(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO discover_generation (id) VALUES (1)`); err != nil {
			return err
		}
		var expiresAt, refreshStarted int64
		if err := tx.QueryRow(`SELECT expires_at_ms, refresh_started_at_ms FROM discover_generation WHERE id = 1`).Scan(&expiresAt, &refreshStarted); err != nil {
			return err
		}
		if refreshStarted > 0 || expiresAt > nowMs {
			return nil
		}
		if _, err := tx.Exec(`DELETE FROM discover_refresh_anchors`); err != nil {
			return err
		}
		ids, err := randomFollowedYouTubeAnchorIDsTx(tx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := tx.Exec(`INSERT INTO discover_refresh_anchors (anchor_video_id) VALUES (?)`, id); err != nil {
				return err
			}
			if err := queueYouTubeRecommendationsTx(tx, id, nowMs); err != nil {
				return err
			}
			if _, err := tx.Exec(`
				UPDATE youtube_recommendations
				SET status = CASE WHEN status = 'processing' THEN status ELSE 'pending' END,
				    attempts = CASE WHEN status = 'processing' THEN attempts ELSE 0 END,
				    next_attempt_at_ms = CASE WHEN status = 'processing' THEN next_attempt_at_ms ELSE 0 END,
				    last_error = CASE WHEN status = 'processing' THEN last_error ELSE '' END,
				    requested_at_ms = ?, updated_at_ms = ?
				WHERE anchor_video_id = ?`, nowMs, nowMs, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE discover_generation SET refresh_started_at_ms = ? WHERE id = 1`, nowMs); err != nil {
			return err
		}
		started, anchors = true, len(ids)
		return nil
	})
	return started, anchors, err
}

// PublishDiscoverGeneration atomically replaces the prepared page after all
// refresh anchors have reached a terminal state. It returns the old warmed
// video IDs that are no longer part of the page.
func (db *DB) PublishDiscoverGeneration(nowMs int64, limit int) (bool, []string, error) {
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	if limit <= 0 {
		limit = 80
	}
	maxDurationMinutes := settings.ClampDiscoverMaxDurationMinutes(db.IntSetting("discover_max_duration_minutes"))
	resetHours := settings.ClampDiscoverResetHours(db.IntSetting("discover_reset_hours"))
	var published bool
	var retired []string
	err := db.WithWrite(func(tx *sql.Tx) error {
		var refreshStarted int64
		var previousJSON, historyJSON string
		if err := tx.QueryRow(`SELECT refresh_started_at_ms, candidates_json, history_video_ids_json FROM discover_generation WHERE id = 1`).Scan(&refreshStarted, &previousJSON, &historyJSON); err != nil {
			return err
		}
		if refreshStarted == 0 {
			return nil
		}
		var unfinished int
		if err := tx.QueryRow(`
			SELECT COUNT(*) FROM discover_refresh_anchors refresh
			JOIN youtube_recommendations rec ON rec.anchor_video_id = refresh.anchor_video_id
			WHERE rec.status IN ('pending', 'processing')`).Scan(&unfinished); err != nil || unfinished > 0 {
			return err
		}
		followed, err := followedChannelSetTx(tx)
		if err != nil {
			return err
		}
		rows, err := tx.Query(`
			SELECT rec.candidates_json
			FROM discover_refresh_anchors refresh
			JOIN youtube_recommendations rec ON rec.anchor_video_id = refresh.anchor_video_id
			WHERE rec.status = 'ready'
			ORDER BY rec.fetched_at_ms DESC, rec.anchor_video_id`)
		if err != nil {
			return err
		}
		var batches [][]model.DiscoveryVideo
		for rows.Next() {
			var payload string
			if err := rows.Scan(&payload); err != nil {
				_ = rows.Close()
				return err
			}
			var batch []model.DiscoveryVideo
			if json.Unmarshal([]byte(payload), &batch) != nil {
				continue
			}
			batch = filterDiscoverDurationWithLimit(batch, maxDurationMinutes)
			batch = excludeFollowedDiscoverCandidates(relatedDiscoverCandidates(batch), followed)
			if len(batch) > 0 {
				batches = append(batches, batch)
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		var previous []model.DiscoveryVideo
		_ = json.Unmarshal([]byte(previousJSON), &previous)
		var history [][]string
		_ = json.Unmarshal([]byte(historyJSON), &history)
		oldIDs := make(map[string]struct{}, len(previous)+len(history)*limit)
		previousIDsSet := make(map[string]struct{}, len(previous))
		for _, video := range previous {
			oldIDs[video.VideoID] = struct{}{}
			previousIDsSet[video.VideoID] = struct{}{}
		}
		for _, generation := range history {
			for _, videoID := range generation {
				oldIDs[videoID] = struct{}{}
			}
		}
		freshBatches := batches[:0]
		for _, batch := range batches {
			freshBatch := batch[:0]
			for _, candidate := range batch {
				if _, repeated := oldIDs[candidate.VideoID]; !repeated {
					freshBatch = append(freshBatch, candidate)
				}
			}
			if len(freshBatch) > 0 {
				freshBatches = append(freshBatches, freshBatch)
			}
		}
		fresh := interleaveDiscoverBatches(freshBatches, limit, 3, 3)
		payload, err := json.Marshal(fresh)
		if err != nil {
			return err
		}
		history = nextDiscoverGenerationHistory(previous, history)
		historyPayload, err := json.Marshal(history)
		if err != nil {
			return err
		}
		expiresAt := nowMs + int64(resetHours)*time.Hour.Milliseconds()
		if _, err := tx.Exec(`UPDATE discover_generation SET candidates_json = ?, history_video_ids_json = ?, prepared_at_ms = ?, expires_at_ms = ?, refresh_started_at_ms = 0 WHERE id = 1`, string(payload), string(historyPayload), nowMs, expiresAt); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM discover_refresh_anchors`); err != nil {
			return err
		}
		newIDs := make(map[string]struct{}, len(fresh))
		for _, video := range fresh {
			newIDs[video.VideoID] = struct{}{}
		}
		for id := range previousIDsSet {
			if _, kept := newIDs[id]; !kept {
				retired = append(retired, id)
			}
		}
		published = true
		return nil
	})
	return published, retired, err
}

func nextDiscoverGenerationHistory(previous []model.DiscoveryVideo, history [][]string) [][]string {
	previousIDs := make([]string, 0, len(previous))
	for _, video := range previous {
		if strings.TrimSpace(video.VideoID) != "" {
			previousIDs = append(previousIDs, video.VideoID)
		}
	}
	if len(previousIDs) > 0 {
		history = append([][]string{previousIDs}, history...)
	}
	// The current page plus these two prior ID sets cover the last three
	// prepared updates without allowing the exclusion cache to grow forever.
	if len(history) > 2 {
		history = history[:2]
	}
	return history
}

func (db *DB) ListPreparedDiscoverVideos(limit int) ([]model.DiscoveryVideo, error) {
	videos, err := db.listPreparedDiscoverCandidates()
	if err != nil {
		return nil, err
	}
	followed, err := db.followedChannelSet()
	if err != nil {
		return nil, err
	}
	visible := videos[:0]
	for _, video := range videos {
		if _, excluded := followed[video.ChannelID]; excluded {
			continue
		}
		visible = append(visible, video)
		if limit > 0 && len(visible) == limit {
			break
		}
	}
	return visible, db.projectDiscoveryMedia(visible)
}

func (db *DB) listPreparedDiscoverCandidates() ([]model.DiscoveryVideo, error) {
	var payload string
	err := db.reader().QueryRow(`SELECT candidates_json FROM discover_generation WHERE id = 1`).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var videos []model.DiscoveryVideo
	if err := json.Unmarshal([]byte(payload), &videos); err != nil {
		return nil, err
	}
	return videos, nil
}

func (db *DB) GetPreparedDiscoverVideo(videoID string) (*model.DiscoveryVideo, error) {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return nil, nil
	}
	videos, err := db.listPreparedDiscoverCandidates()
	if err != nil {
		return nil, err
	}
	for i := range videos {
		if videos[i].VideoID == videoID {
			return &videos[i], nil
		}
	}
	return nil, nil
}

func (db *DB) RescheduleDiscoverRefresh(force bool) error {
	intervalMs := int64(settings.ClampDiscoverResetHours(db.IntSetting("discover_reset_hours"))) * time.Hour.Milliseconds()
	return db.WithWrite(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO discover_generation (id) VALUES (1)`); err != nil {
			return err
		}
		if force {
			_, err := tx.Exec(`UPDATE discover_generation SET expires_at_ms = 0 WHERE id = 1 AND refresh_started_at_ms = 0`)
			return err
		}
		_, err := tx.Exec(`
			UPDATE discover_generation
			SET expires_at_ms = CASE WHEN prepared_at_ms > 0 THEN prepared_at_ms + ? ELSE 0 END
			WHERE id = 1 AND refresh_started_at_ms = 0`, intervalMs)
		return err
	})
}

func (db *DB) RetireDiscoverDownloads(videoIDs []string) error {
	if len(videoIDs) == 0 {
		return nil
	}
	return db.WithWrite(func(tx *sql.Tx) error {
		for _, id := range videoIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, err := tx.Exec(`DELETE FROM discover_temp_downloads WHERE video_id = ?`, id); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE videos SET downloaded_at = 1 WHERE video_id = ? AND COALESCE(is_temp, 0) = 1 AND COALESCE(is_pinned, 0) = 0`, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func followedChannelSetTx(tx *sql.Tx) (map[string]struct{}, error) {
	rows, err := tx.Query(`SELECT channel_id FROM channel_follows`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

func filterDiscoverDurationWithLimit(candidates []model.DiscoveryVideo, maxMinutes int) []model.DiscoveryVideo {
	if maxMinutes <= 0 {
		return candidates
	}
	out := candidates[:0]
	for _, candidate := range candidates {
		if candidate.Duration <= 0 || candidate.Duration <= maxMinutes*60 {
			out = append(out, candidate)
		}
	}
	return out
}
