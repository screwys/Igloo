package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/model"
	"github.com/screwys/igloo/internal/settings"
)

const YouTubeRecommendationTTL = 24 * time.Hour

type YouTubeRecommendationJob struct {
	AnchorVideoID string
	AnchorTitle   string
	ChannelID     string
	ChannelName   string
	ChannelHandle string
	ChannelURL    string
	LeaseOwner    string
	LeaseUntilMs  int64
	Attempts      int
}

func (db *DB) QueueYouTubeRecommendations(videoID string, nowMs int64) error {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return fmt.Errorf("recommendation anchor is empty")
	}
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	return db.WithWrite(func(tx *sql.Tx) error {
		return queueYouTubeRecommendationsTx(tx, videoID, nowMs)
	})
}

func queueYouTubeRecommendationsTx(tx *sql.Tx, videoID string, nowMs int64) error {
	var ownerKind string
	if err := tx.QueryRow(`SELECT owner_kind FROM videos WHERE video_id = ?`, videoID).Scan(&ownerKind); err != nil {
		return err
	}
	if ownerKind != "youtube_video" {
		return nil
	}
	_, err := tx.Exec(`
		INSERT INTO youtube_recommendations (
			anchor_video_id, status, requested_at_ms, updated_at_ms
		) VALUES (?, 'pending', ?, ?)
		ON CONFLICT(anchor_video_id) DO UPDATE SET
			status = CASE
				WHEN youtube_recommendations.status = 'processing' THEN 'processing'
				WHEN youtube_recommendations.expires_at_ms > ?
				 AND EXISTS (
				   SELECT 1 FROM json_each(youtube_recommendations.candidates_json) candidate
				   WHERE json_extract(candidate.value, '$.source') = 'related'
				 ) THEN youtube_recommendations.status
				ELSE 'pending' END,
			attempts = CASE WHEN youtube_recommendations.expires_at_ms > ? AND EXISTS (SELECT 1 FROM json_each(youtube_recommendations.candidates_json) c WHERE json_extract(c.value, '$.source') = 'related') THEN youtube_recommendations.attempts ELSE 0 END,
			next_attempt_at_ms = CASE WHEN youtube_recommendations.expires_at_ms > ? AND EXISTS (SELECT 1 FROM json_each(youtube_recommendations.candidates_json) c WHERE json_extract(c.value, '$.source') = 'related') THEN youtube_recommendations.next_attempt_at_ms ELSE 0 END,
			last_error = CASE WHEN youtube_recommendations.expires_at_ms > ? AND EXISTS (SELECT 1 FROM json_each(youtube_recommendations.candidates_json) c WHERE json_extract(c.value, '$.source') = 'related') THEN youtube_recommendations.last_error ELSE '' END,
			requested_at_ms = excluded.requested_at_ms,
			updated_at_ms = excluded.updated_at_ms
	`, videoID, nowMs, nowMs, nowMs, nowMs, nowMs, nowMs)
	return err
}

func (db *DB) QueueFollowedYouTubeChannelRecommendations(nowMs int64) (int, error) {
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	queued := 0
	err := db.WithWrite(func(tx *sql.Tx) error {
		rows, err := tx.Query(`
			WITH ranked AS (
				SELECT v.video_id,
				       ROW_NUMBER() OVER (
				         PARTITION BY v.channel_id
				         ORDER BY v.published_at DESC, v.downloaded_at DESC, v.video_id DESC
				       ) AS channel_position
				FROM videos v
				JOIN channel_follows followed ON followed.channel_id = v.channel_id
				WHERE v.owner_kind = 'youtube_video'
				  AND COALESCE(v.is_temp, 0) = 0
				  AND ` + readyVideoMediaExistsSQL("v") + `
			)
			SELECT video_id FROM ranked
			WHERE channel_position = 1
			ORDER BY video_id
		`)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, id := range ids {
			if err := queueYouTubeRecommendationsTx(tx, id, nowMs); err != nil {
				return err
			}
			queued++
		}
		return nil
	})
	return queued, err
}

func (db *DB) ClaimYouTubeRecommendationJob(opts LeaseOptions) (YouTubeRecommendationJob, bool, error) {
	opts = normalizeLeaseOptions(opts, "pending", "processing")
	var job YouTubeRecommendationJob
	err := db.WithWrite(func(tx *sql.Tx) error {
		ids, err := claimLeasedIDs(tx, "youtube_recommendations", "anchor_video_id", `
			SELECT rec.anchor_video_id
			FROM youtube_recommendations rec
			JOIN videos v ON v.video_id = rec.anchor_video_id
			WHERE v.owner_kind = 'youtube_video'
			  AND rec.next_attempt_at_ms <= ?
			  AND ((rec.status = ? AND (rec.lease_until_ms = 0 OR rec.lease_until_ms <= ?))
			    OR (rec.status = ? AND rec.lease_until_ms <= ?))
			ORDER BY rec.requested_at_ms DESC, rec.anchor_video_id
			LIMIT ?
		`, []any{opts.NowMs, opts.StatusFrom, opts.NowMs, opts.StatusTo, opts.NowMs, 1}, opts)
		if err != nil || len(ids) == 0 {
			return err
		}
		return tx.QueryRow(`
			SELECT rec.anchor_video_id, COALESCE(v.title, ''), v.channel_id,
			       COALESCE(NULLIF(cp.display_name, ''), NULLIF(c.name, ''), ''),
			       COALESCE(cp.handle, ''), COALESCE(c.url, ''),
			       rec.lease_owner, rec.lease_until_ms, rec.attempts
			FROM youtube_recommendations rec
			JOIN videos v ON v.video_id = rec.anchor_video_id
			LEFT JOIN channels c ON c.channel_id = v.channel_id
			LEFT JOIN channel_profiles cp ON cp.channel_id = v.channel_id
			WHERE rec.anchor_video_id = ?
		`, ids[0]).Scan(&job.AnchorVideoID, &job.AnchorTitle, &job.ChannelID, &job.ChannelName,
			&job.ChannelHandle, &job.ChannelURL, &job.LeaseOwner, &job.LeaseUntilMs, &job.Attempts)
	})
	if err != nil {
		return YouTubeRecommendationJob{}, false, err
	}
	return job, job.AnchorVideoID != "", nil
}

func (db *DB) CompleteYouTubeRecommendationJob(job YouTubeRecommendationJob, candidates []model.DiscoveryVideo, nowMs int64) error {
	if job.AnchorVideoID == "" || job.LeaseOwner == "" {
		return fmt.Errorf("complete recommendations: missing anchor or lease")
	}
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	payload, err := json.Marshal(candidates)
	if err != nil {
		return err
	}
	return db.WithWrite(func(tx *sql.Tx) error {
		res, err := tx.Exec(`
			UPDATE youtube_recommendations
			SET candidates_json = ?, status = 'ready', fetched_at_ms = ?, expires_at_ms = ?,
			    attempts = 0, next_attempt_at_ms = 0, last_error = '',
			    lease_owner = '', lease_until_ms = 0, updated_at_ms = ?
			WHERE anchor_video_id = ? AND status = 'processing'
			  AND lease_owner = ? AND lease_until_ms = ?
		`, string(payload), nowMs, nowMs+YouTubeRecommendationTTL.Milliseconds(), nowMs,
			job.AnchorVideoID, job.LeaseOwner, job.LeaseUntilMs)
		if err != nil {
			return err
		}
		return requireQueueLeaseUpdate(res, "youtube_recommendations", job.AnchorVideoID, job.LeaseOwner)
	})
}

func (db *DB) GetYouTubeRecommendations(anchorVideoID string, limit int) ([]model.DiscoveryVideo, bool, error) {
	var payload, status string
	var expiresAt int64
	err := db.reader().QueryRow(`
		SELECT candidates_json, status, expires_at_ms
		FROM youtube_recommendations WHERE anchor_video_id = ?
	`, strings.TrimSpace(anchorVideoID)).Scan(&payload, &status, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var candidates []model.DiscoveryVideo
	if err := json.Unmarshal([]byte(payload), &candidates); err != nil {
		return nil, false, err
	}
	candidates = db.filterDiscoverDuration(candidates)
	hasRelated := false
	for _, candidate := range candidates {
		if candidate.Source == "related" {
			hasRelated = true
			break
		}
	}
	followedChannels, err := db.followedChannelSet()
	if err != nil {
		return nil, false, err
	}
	candidates = excludeFollowedDiscoverCandidates(candidates, followedChannels)
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	if err := db.markDiscoveryReady(candidates); err != nil {
		return nil, false, err
	}
	fresh := status == "ready" && expiresAt > time.Now().UnixMilli() && hasRelated
	return candidates, fresh, nil
}

func (db *DB) ListYouTubeDiscoverVideos(limit int) ([]model.DiscoveryVideo, error) {
	if limit <= 0 {
		limit = 80
	}
	followedChannels, err := db.followedChannelSet()
	if err != nil {
		return nil, err
	}
	rows, err := db.reader().Query(`
		SELECT candidates_json
		FROM youtube_recommendations
		WHERE status = 'ready' AND expires_at_ms > ? AND candidates_json != '[]'
		ORDER BY fetched_at_ms DESC, anchor_video_id
	`, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var batches [][]model.DiscoveryVideo
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var batch []model.DiscoveryVideo
		if err := json.Unmarshal([]byte(payload), &batch); err != nil {
			continue
		}
		batch = db.filterDiscoverDuration(batch)
		batch = relatedDiscoverCandidates(batch)
		batch = excludeFollowedDiscoverCandidates(batch, followedChannels)
		if len(batch) > 0 {
			batches = append(batches, batch)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := interleaveDiscoverBatches(batches, limit, 6, 2)
	return out, db.markDiscoveryReady(out)
}

func (db *DB) followedChannelSet() (map[string]struct{}, error) {
	rows, err := db.reader().Query(`SELECT channel_id FROM channel_follows`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	followed := make(map[string]struct{})
	for rows.Next() {
		var channelID string
		if err := rows.Scan(&channelID); err != nil {
			return nil, err
		}
		followed[channelID] = struct{}{}
	}
	return followed, rows.Err()
}

func excludeFollowedDiscoverCandidates(candidates []model.DiscoveryVideo, followed map[string]struct{}) []model.DiscoveryVideo {
	out := candidates[:0]
	for _, candidate := range candidates {
		if _, exists := followed[candidate.ChannelID]; exists {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func relatedDiscoverCandidates(candidates []model.DiscoveryVideo) []model.DiscoveryVideo {
	out := candidates[:0]
	for _, candidate := range candidates {
		if candidate.Source == "related" {
			out = append(out, candidate)
		}
	}
	return out
}

func (db *DB) filterDiscoverDuration(candidates []model.DiscoveryVideo) []model.DiscoveryVideo {
	maxMinutes := settings.ClampDiscoverMaxDurationMinutes(db.IntSetting("discover_max_duration_minutes"))
	if maxMinutes <= 0 {
		return candidates
	}
	maxSeconds := maxMinutes * 60
	out := candidates[:0]
	for _, candidate := range candidates {
		if candidate.Duration > 0 && candidate.Duration > maxSeconds {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func interleaveDiscoverBatches(batches [][]model.DiscoveryVideo, limit, perBatch, perChannel int) []model.DiscoveryVideo {
	if limit <= 0 {
		return nil
	}
	seenVideos := make(map[string]struct{})
	channelCounts := make(map[string]int)
	batchCounts := make([]int, len(batches))
	var out []model.DiscoveryVideo
	for position := 0; len(out) < limit; position++ {
		advanced := false
		for batchIndex, batch := range batches {
			if position >= len(batch) || (perBatch > 0 && batchCounts[batchIndex] >= perBatch) {
				continue
			}
			advanced = true
			candidate := batch[position]
			if strings.TrimSpace(candidate.VideoID) == "" {
				continue
			}
			if _, duplicate := seenVideos[candidate.VideoID]; duplicate {
				continue
			}
			if perChannel > 0 && candidate.ChannelID != "" && channelCounts[candidate.ChannelID] >= perChannel {
				continue
			}
			seenVideos[candidate.VideoID] = struct{}{}
			channelCounts[candidate.ChannelID]++
			batchCounts[batchIndex]++
			out = append(out, candidate)
			if len(out) >= limit {
				break
			}
		}
		if !advanced {
			break
		}
	}
	return out
}

func (db *DB) markDiscoveryReady(candidates []model.DiscoveryVideo) error {
	if len(candidates) == 0 {
		return nil
	}
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.VideoID)
	}
	rows, err := db.reader().Query(`
		SELECT v.video_id FROM videos v
		WHERE v.video_id IN (`+placeholders(len(ids))+`) AND `+readyVideoMediaExistsSQL("v"), stringsToAny(ids)...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	ready := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ready[id] = true
	}
	for i := range candidates {
		candidates[i].Ready = ready[candidates[i].VideoID]
	}
	return rows.Err()
}

func (db *DB) RetryYouTubeRecommendationJob(job YouTubeRecommendationJob, message string, delay time.Duration, nowMs int64) error {
	return db.updateYouTubeRecommendationLease(job, `
		UPDATE youtube_recommendations
		SET status = 'pending', attempts = attempts + 1, next_attempt_at_ms = ?,
		    last_error = ?, lease_owner = '', lease_until_ms = 0, updated_at_ms = ?
		WHERE anchor_video_id = ? AND status = 'processing' AND lease_owner = ? AND lease_until_ms = ?
	`, nowMs+delay.Milliseconds(), trimJobError(message), nowMs)
}

func (db *DB) BlockYouTubeRecommendationJob(job YouTubeRecommendationJob, message string, nowMs int64) error {
	return db.updateYouTubeRecommendationLease(job, `
		UPDATE youtube_recommendations
		SET status = 'blocked', attempts = attempts + 1, next_attempt_at_ms = 0,
		    last_error = ?, lease_owner = '', lease_until_ms = 0, updated_at_ms = ?
		WHERE anchor_video_id = ? AND status = 'processing' AND lease_owner = ? AND lease_until_ms = ?
	`, trimJobError(message), nowMs)
}

func (db *DB) ReleaseYouTubeRecommendationJob(job YouTubeRecommendationJob, nowMs int64) error {
	return db.updateYouTubeRecommendationLease(job, `
		UPDATE youtube_recommendations
		SET status = 'pending', lease_owner = '', lease_until_ms = 0, updated_at_ms = ?
		WHERE anchor_video_id = ? AND status = 'processing' AND lease_owner = ? AND lease_until_ms = ?
	`, nowMs)
}

func (db *DB) updateYouTubeRecommendationLease(job YouTubeRecommendationJob, query string, args ...any) error {
	args = append(args, job.AnchorVideoID, job.LeaseOwner, job.LeaseUntilMs)
	return db.WithWrite(func(tx *sql.Tx) error {
		res, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}
		return requireQueueLeaseUpdate(res, "youtube_recommendations", job.AnchorVideoID, job.LeaseOwner)
	})
}

func (db *DB) NextYouTubeRecommendationDelay(nowMs int64) (time.Duration, error) {
	var due sql.NullInt64
	err := db.reader().QueryRow(`
		SELECT MIN(CASE WHEN status = 'processing' THEN lease_until_ms ELSE next_attempt_at_ms END)
		FROM youtube_recommendations WHERE status IN ('pending', 'processing')
	`).Scan(&due)
	if err != nil {
		return 0, err
	}
	if !due.Valid {
		return time.Hour, nil
	}
	delay := time.Duration(due.Int64-nowMs) * time.Millisecond
	if delay < 0 {
		return 0, nil
	}
	return delay, nil
}

func (db *DB) ResetYouTubeRecommendationWork() error {
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE youtube_recommendations
			SET status = 'pending', lease_owner = '', lease_until_ms = 0
			WHERE status = 'processing'
		`)
		return err
	})
}
