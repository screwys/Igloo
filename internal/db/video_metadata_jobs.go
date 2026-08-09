package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/screwys/igloo/internal/model"
)

const (
	VideoMetadataYoungAge = 48 * time.Hour
	VideoMetadataRefresh  = 24 * time.Hour
)

type VideoMetadataJob struct {
	VideoID       string
	PublishedAtMs int64
	LeaseOwner    string
	LeaseUntilMs  int64
	Attempts      int
}

type VideoMetadataRefreshResult struct {
	Comments     []CommentInput
	ViewCount    *int64
	LikeCount    *int64
	CommentCount *int64
}

func (db *DB) QueueVideoMetadataJob(videoID string, nowMs int64) error {
	if videoID == "" {
		return fmt.Errorf("video id is empty")
	}
	if nowMs == 0 {
		nowMs = time.Now().UnixMilli()
	}
	return db.WithWrite(func(tx *sql.Tx) error {
		var ownerKind string
		if err := tx.QueryRow(`SELECT owner_kind FROM videos WHERE video_id = ?`, videoID).Scan(&ownerKind); err != nil {
			return err
		}
		if ownerKind != "youtube_video" {
			return fmt.Errorf("video %s is not a YouTube video", videoID)
		}
		_, err := tx.Exec(`
			INSERT INTO video_metadata_jobs (
				video_id, status, next_attempt_at_ms, requested_at_ms, updated_at_ms
			) VALUES (?, 'pending', 0, ?, ?)
			ON CONFLICT(video_id) DO UPDATE SET
				status = CASE WHEN video_metadata_jobs.status = 'processing' THEN 'processing' ELSE 'pending' END,
				attempts = CASE WHEN video_metadata_jobs.status = 'processing' THEN video_metadata_jobs.attempts ELSE 0 END,
				next_attempt_at_ms = CASE WHEN video_metadata_jobs.status = 'processing' THEN video_metadata_jobs.next_attempt_at_ms ELSE 0 END,
				last_error = CASE WHEN video_metadata_jobs.status = 'processing' THEN video_metadata_jobs.last_error ELSE '' END,
				requested_at_ms = excluded.requested_at_ms,
				updated_at_ms = excluded.updated_at_ms
		`, videoID, nowMs, nowMs)
		return err
	})
}

func (db *DB) ClaimVideoMetadataJob(opts LeaseOptions) (VideoMetadataJob, bool, error) {
	opts = normalizeLeaseOptions(opts, "pending", "processing")
	var job VideoMetadataJob
	err := db.WithWrite(func(tx *sql.Tx) error {
		ids, err := claimLeasedIDs(tx, "video_metadata_jobs", "video_id", `
			SELECT vmj.video_id
			FROM video_metadata_jobs vmj
			JOIN videos v ON v.video_id = vmj.video_id
			WHERE v.owner_kind = 'youtube_video'
			  AND vmj.next_attempt_at_ms <= ?
			  AND (
				(vmj.status = ? AND (vmj.lease_until_ms = 0 OR vmj.lease_until_ms <= ?))
				OR (vmj.status = ? AND vmj.lease_until_ms <= ?)
			  )
			ORDER BY vmj.requested_at_ms DESC, vmj.video_id
			LIMIT ?
		`, []any{opts.NowMs, opts.StatusFrom, opts.NowMs, opts.StatusTo, opts.NowMs, 1}, opts)
		if err != nil || len(ids) == 0 {
			return err
		}
		return tx.QueryRow(`
			SELECT vmj.video_id, v.published_at, vmj.lease_owner,
			       vmj.lease_until_ms, vmj.attempts
			FROM video_metadata_jobs vmj
			JOIN videos v ON v.video_id = vmj.video_id
			WHERE vmj.video_id = ?
		`, ids[0]).Scan(
			&job.VideoID, &job.PublishedAtMs, &job.LeaseOwner,
			&job.LeaseUntilMs, &job.Attempts,
		)
	})
	if err != nil {
		return VideoMetadataJob{}, false, err
	}
	return job, job.VideoID != "", nil
}

func (db *DB) CompleteVideoMetadataJob(job VideoMetadataJob, result VideoMetadataRefreshResult, nowMs int64) error {
	if job.VideoID == "" || job.LeaseOwner == "" {
		return fmt.Errorf("complete video metadata job: missing video or lease owner")
	}
	if nowMs == 0 {
		nowMs = time.Now().UnixMilli()
	}
	ageLabel := "old"
	nextStatus := "done"
	var nextAttemptMs int64
	if job.PublishedAtMs > 0 && nowMs-job.PublishedAtMs < VideoMetadataYoungAge.Milliseconds() {
		ageLabel = "young"
		nextStatus = "pending"
		nextAttemptMs = nowMs + VideoMetadataRefresh.Milliseconds()
	}

	return db.WithWrite(func(tx *sql.Tx) error {
		var metadataJSON string
		if err := tx.QueryRow(`SELECT COALESCE(metadata_json, '') FROM videos WHERE video_id = ?`, job.VideoID).Scan(&metadataJSON); err != nil {
			return err
		}
		metadata := map[string]any{}
		if metadataJSON != "" {
			_ = json.Unmarshal([]byte(metadataJSON), &metadata)
		}
		if metadata == nil {
			metadata = map[string]any{}
		}
		if result.ViewCount != nil {
			metadata["view_count"] = *result.ViewCount
			metadata["view_count_label"] = model.CompactCountLabel(*result.ViewCount)
		}
		if result.LikeCount != nil {
			metadata["like_count"] = *result.LikeCount
			metadata["like_count_label"] = model.CompactCountLabel(*result.LikeCount)
		}
		if result.CommentCount != nil {
			metadata["comment_count"] = *result.CommentCount
		}
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE videos SET metadata_json = ? WHERE video_id = ?`, string(encoded), job.VideoID); err != nil {
			return err
		}
		commentsAreAuthoritative := len(result.Comments) > 0 || (result.CommentCount != nil && *result.CommentCount == 0)
		if commentsAreAuthoritative {
			if err := replaceVideoCommentsTx(tx, job.VideoID, result.Comments, nowMs); err != nil {
				return err
			}
		}
		res, err := tx.Exec(`
			UPDATE video_metadata_jobs
			SET status = ?, checked_at_ms = ?, video_age_at_check = ?,
			    attempts = 0, next_attempt_at_ms = ?, last_error = '',
			    lease_owner = '', lease_until_ms = 0, updated_at_ms = ?
			WHERE video_id = ? AND status = 'processing'
			  AND lease_owner = ? AND lease_until_ms = ?
		`, nextStatus, nowMs, ageLabel, nextAttemptMs, nowMs,
			job.VideoID, job.LeaseOwner, job.LeaseUntilMs)
		if err != nil {
			return err
		}
		return requireQueueLeaseUpdate(res, "video_metadata_jobs", job.VideoID, job.LeaseOwner)
	})
}

func replaceVideoCommentsTx(tx *sql.Tx, videoID string, comments []CommentInput, nowMs int64) error {
	if _, err := tx.Exec(`DELETE FROM video_comments WHERE video_id = ?`, videoID); err != nil {
		return err
	}
	if len(comments) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`
		INSERT INTO video_comments (
			video_id, comment_id, parent_id, author_name, author_id,
			text, like_count, published_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, comment := range comments {
		publishedAtMs := int64(0)
		if comment.Timestamp > 0 {
			publishedAtMs = comment.Timestamp * 1000
		}
		if _, err := stmt.Exec(
			videoID, comment.CommentID, nilIfEmpty(comment.ParentID),
			comment.Author, comment.AuthorID, comment.Text, comment.LikeCount, publishedAtMs,
		); err != nil {
			return err
		}
		if err := declareYouTubeCommentAvatarTx(tx, comment, nowMs); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) RetryVideoMetadataJob(job VideoMetadataJob, message string, delay time.Duration, nowMs int64) error {
	return db.updateVideoMetadataJobLease(job, `
		UPDATE video_metadata_jobs
		SET status = 'pending', attempts = attempts + 1,
		    next_attempt_at_ms = ?, last_error = ?,
		    lease_owner = '', lease_until_ms = 0, updated_at_ms = ?
		WHERE video_id = ? AND status = 'processing'
		  AND lease_owner = ? AND lease_until_ms = ?
	`, nowMs+delay.Milliseconds(), message, nowMs)
}

func (db *DB) BlockVideoMetadataJob(job VideoMetadataJob, message string, nowMs int64) error {
	return db.updateVideoMetadataJobLease(job, `
		UPDATE video_metadata_jobs
		SET status = 'blocked', attempts = attempts + 1,
		    next_attempt_at_ms = 0, last_error = ?,
		    lease_owner = '', lease_until_ms = 0, updated_at_ms = ?
		WHERE video_id = ? AND status = 'processing'
		  AND lease_owner = ? AND lease_until_ms = ?
	`, message, nowMs)
}

func (db *DB) ReleaseVideoMetadataJob(job VideoMetadataJob, nowMs int64) error {
	return db.updateVideoMetadataJobLease(job, `
		UPDATE video_metadata_jobs
		SET status = 'pending', lease_owner = '', lease_until_ms = 0, updated_at_ms = ?
		WHERE video_id = ? AND status = 'processing'
		  AND lease_owner = ? AND lease_until_ms = ?
	`, nowMs)
}

func (db *DB) updateVideoMetadataJobLease(job VideoMetadataJob, query string, args ...any) error {
	args = append(args, job.VideoID, job.LeaseOwner, job.LeaseUntilMs)
	return db.WithWrite(func(tx *sql.Tx) error {
		res, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}
		return requireQueueLeaseUpdate(res, "video_metadata_jobs", job.VideoID, job.LeaseOwner)
	})
}

func (db *DB) NextVideoMetadataJobDelay(nowMs int64) (time.Duration, error) {
	var due sql.NullInt64
	err := db.conn.QueryRow(`
		SELECT MIN(CASE
			WHEN status = 'processing' THEN lease_until_ms
			ELSE next_attempt_at_ms
		END)
		FROM video_metadata_jobs
		WHERE status IN ('pending', 'processing')
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
