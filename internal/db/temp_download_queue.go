package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/model"
)

// TempDownloadWork is an interactive download claimed by the durable worker.
type TempDownloadWork struct {
	URL        string
	RequestID  string
	Platform   string
	Origin     string
	RetryCount int
	LeaseOwner string
}

// TempDownloadState is the persisted state shown by the temporary watch page.
type TempDownloadState struct {
	RequestID string
	Status    string
	Error     string
}

var ErrTempDownloadSuperseded = errors.New("download request has changed")
var ErrTempDownloadInactive = errors.New("temporary download attempt is no longer active")

func (db *DB) TempDownloadState(rawURL string) (TempDownloadState, bool, error) {
	var state TempDownloadState
	err := db.reader().QueryRow(`
		SELECT request_id, CASE WHEN last_error_kind = 'cancelled' THEN
			CASE WHEN lease_owner = '' THEN 'cancelled' ELSE 'cancelling' END
			ELSE status END, last_error
		FROM temp_download_queue
		WHERE url = $1
	`, strings.TrimSpace(rawURL)).Scan(&state.RequestID, &state.Status, &state.Error)
	if err == sql.ErrNoRows {
		return TempDownloadState{}, false, nil
	}
	if err != nil {
		return TempDownloadState{}, false, err
	}
	return state, true, nil
}

// CancelTempDownloadWork revokes publication before the producer is stopped.
// The cancelled row keeps Discover from adding it again in the same generation.
func (db *DB) CancelTempDownloadWork(rawURL, requestID string) (TempDownloadState, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	requestID = strings.TrimSpace(requestID)
	state := TempDownloadState{RequestID: requestID, Status: "complete"}
	var owner string
	err := db.WithWrite(func(tx *sql.Tx) error {
		var status, errorKind string
		err := tx.QueryRow(`SELECT request_id, status, last_error_kind, lease_owner FROM temp_download_queue WHERE url = $1`, rawURL).
			Scan(&state.RequestID, &status, &errorKind, &owner)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if requestID != "" && requestID != state.RequestID {
			return ErrTempDownloadSuperseded
		}
		if status != "processing" && errorKind != "cancelled" {
			owner = ""
		}
		if _, err := tx.Exec(`
			UPDATE temp_download_queue
			SET status = 'blocked', next_attempt_at_ms = 0, last_error_kind = 'cancelled',
				last_error = '', save_intent_json = '', lease_owner = $1, lease_until_ms = 0
			WHERE url = $2
		`, owner, rawURL); err != nil {
			return err
		}
		state.Status = "cancelled"
		if owner != "" {
			state.Status = "cancelling"
		}
		return nil
	})
	return state, owner, err
}

func (db *DB) FinishCancelledTempDownloadWork(rawURL, owner string) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE temp_download_queue SET lease_owner = '', lease_until_ms = 0
			WHERE url = $1 AND status = 'blocked' AND last_error_kind = 'cancelled' AND lease_owner = $2
		`, rawURL, owner)
		return err
	})
}

func (db *DB) TempDownloadOrigin(rawURL string) (string, error) {
	var origin string
	err := db.reader().QueryRow(`SELECT origin FROM temp_download_queue WHERE url = $1`, strings.TrimSpace(rawURL)).Scan(&origin)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return origin, err
}

func (db *DB) MarkDiscoverTempVideo(videoID string, nowMs int64) error {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return nil
	}
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO discover_temp_downloads (video_id, downloaded_at_ms)
			VALUES ($1, $2)
			ON CONFLICT(video_id) DO UPDATE SET downloaded_at_ms = excluded.downloaded_at_ms
		`, videoID, nowMs)
		return err
	})
}

// EnqueueTempDownload records an interactive download before any network work
// starts. A blocked URL is explicitly retried when submitted again.
func (db *DB) EnqueueTempDownload(rawURL, platform string) (bool, error) {
	rawURL = strings.TrimSpace(rawURL)
	platform = strings.TrimSpace(platform)
	if rawURL == "" || platform == "" {
		return false, fmt.Errorf("temporary download URL and platform are required")
	}
	nowMs := time.Now().UnixMilli()
	queued := false
	err := db.WithWrite(func(tx *sql.Tx) error {
		var err error
		queued, err = enqueueTempDownloadTx(tx, rawURL, platform, nowMs)
		return err
	})
	return queued, err
}

func enqueueTempDownloadTx(tx *sql.Tx, rawURL, platform string, nowMs int64) (bool, error) {
	res, err := tx.Exec(`
				INSERT INTO temp_download_queue (url, request_id, platform, origin, added_at_ms)
				VALUES ($1, replace(gen_random_uuid()::text, '-', ''), $2, 'interactive', $3)
				ON CONFLICT(url) DO UPDATE SET
					origin = 'interactive',
				request_id = CASE WHEN temp_download_queue.status = 'blocked' THEN excluded.request_id ELSE temp_download_queue.request_id END,
				status = CASE WHEN temp_download_queue.status = 'blocked' THEN 'pending' ELSE temp_download_queue.status END,
				retry_count = CASE WHEN temp_download_queue.status = 'blocked' THEN 0 ELSE temp_download_queue.retry_count END,
				next_attempt_at_ms = CASE WHEN temp_download_queue.status = 'blocked' THEN 0 ELSE temp_download_queue.next_attempt_at_ms END,
				last_error_kind = CASE WHEN temp_download_queue.status = 'blocked' THEN '' ELSE temp_download_queue.last_error_kind END,
				last_error = CASE WHEN temp_download_queue.status = 'blocked' THEN '' ELSE temp_download_queue.last_error END,
				lease_owner = CASE WHEN temp_download_queue.status = 'blocked' THEN '' ELSE temp_download_queue.lease_owner END,
				lease_until_ms = CASE WHEN temp_download_queue.status = 'blocked' THEN 0 ELSE temp_download_queue.lease_until_ms END,
				started_at_ms = CASE WHEN temp_download_queue.status = 'blocked' THEN 0 ELSE temp_download_queue.started_at_ms END
		`, rawURL, platform, nowMs)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	return changed > 0, err
}

// EnqueueDiscoverTempDownloads maintains a bounded global warm set. Ready media
// and active work reserve slots; blocked attempts do not satisfy the target.
func (db *DB) EnqueueDiscoverTempDownloads(candidates []model.DiscoveryVideo, target int) (int, error) {
	if target <= 0 || len(candidates) == 0 {
		return 0, nil
	}
	if target > 50 {
		target = 50
	}
	nowMs := time.Now().UnixMilli()
	added := 0
	err := db.WithWrite(func(tx *sql.Tx) error {
		seen := make(map[string]struct{})
		warm := 0
		for _, candidate := range candidates {
			videoID := strings.TrimSpace(candidate.VideoID)
			if videoID == "" {
				continue
			}
			if _, duplicate := seen[videoID]; duplicate {
				continue
			}
			seen[videoID] = struct{}{}
			var ready bool
			if err := tx.QueryRow(bind(`
				SELECT EXISTS(SELECT 1 FROM videos v WHERE v.video_id = ? AND `+readyVideoMediaExistsSQL("v")+`)
			`), videoID).Scan(&ready); err != nil {
				return err
			}
			url := "https://www.youtube.com/watch?v=" + videoID
			var queued bool
			if err := tx.QueryRow(`
				SELECT EXISTS(SELECT 1 FROM temp_download_queue WHERE url = $1 AND status IN ('pending', 'processing'))
			`, url).Scan(&queued); err != nil {
				return err
			}
			if ready || queued {
				warm++
			}
		}
		if warm >= target {
			return nil
		}
		seen = make(map[string]struct{})
		for _, candidate := range candidates {
			videoID := strings.TrimSpace(candidate.VideoID)
			if videoID == "" {
				continue
			}
			if _, duplicate := seen[videoID]; duplicate {
				continue
			}
			seen[videoID] = struct{}{}
			url := "https://www.youtube.com/watch?v=" + videoID
			var occupied bool
			if err := tx.QueryRow(bind(`
				SELECT EXISTS(SELECT 1 FROM videos v WHERE v.video_id = ? AND `+readyVideoMediaExistsSQL("v")+`)
				    OR EXISTS(SELECT 1 FROM temp_download_queue WHERE url = ?)
			`), videoID, url).Scan(&occupied); err != nil {
				return err
			}
			if occupied {
				continue
			}
			if _, err := tx.Exec(`
				INSERT INTO temp_download_queue (url, request_id, platform, origin, added_at_ms)
				VALUES ($1, replace(gen_random_uuid()::text, '-', ''), 'youtube', 'discover', $2)
			`, url, nowMs); err != nil {
				return err
			}
			added++
			warm++
			if warm >= target {
				break
			}
		}
		return nil
	})
	return added, err
}

// ResetDiscoverTempDownloadQueue clears the previous generation's bounded
// attempts. A currently running download is allowed to finish.
func (db *DB) ResetDiscoverTempDownloadQueue() error {
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM temp_download_queue WHERE (origin = 'discover' OR (platform = 'youtube' AND last_error_kind = 'cancelled')) AND status != 'processing' AND lease_owner = ''`)
		return err
	})
}

// ClaimTempDownloadWork leases the oldest due user-submitted download.
func (db *DB) ClaimTempDownloadWork(owner string, nowMs int64, lease time.Duration) (TempDownloadWork, bool, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return TempDownloadWork{}, false, fmt.Errorf("temporary download lease owner is required")
	}
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	if lease <= 0 {
		lease = 3 * time.Hour
	}
	var work TempDownloadWork
	claimed := false
	err := db.WithWrite(func(tx *sql.Tx) error {
		row := tx.QueryRow(`
			UPDATE temp_download_queue
			SET status = 'processing', lease_owner = $1, lease_until_ms = $2, started_at_ms = CASE WHEN started_at_ms = 0 THEN $3 ELSE started_at_ms END,
				request_id = CASE WHEN request_id = '' THEN replace(gen_random_uuid()::text, '-', '') ELSE request_id END
			WHERE url = (
				SELECT url FROM temp_download_queue
				WHERE (status = 'pending' AND next_attempt_at_ms <= $4)
				   OR (status = 'processing' AND lease_until_ms <= $5)
					ORDER BY CASE WHEN origin = 'interactive' THEN 0 ELSE 1 END, added_at_ms, url
				LIMIT 1
			)
				RETURNING url, request_id, platform, origin, retry_count, lease_owner
			`, owner, nowMs+lease.Milliseconds(), nowMs, nowMs, nowMs)
		if err := row.Scan(&work.URL, &work.RequestID, &work.Platform, &work.Origin, &work.RetryCount, &work.LeaseOwner); err != nil {
			if err == sql.ErrNoRows {
				return nil
			}
			return err
		}
		claimed = true
		return nil
	})
	return work, claimed, err
}

func (db *DB) CompleteTempDownloadWork(rawURL, owner string) error {
	return db.updateTempDownloadLease(rawURL, owner, `DELETE FROM temp_download_queue WHERE url = ? AND status = 'processing' AND lease_owner = ?`)
}

func (db *DB) RetryTempDownloadWork(rawURL, owner, errorKind, message string, delay time.Duration) error {
	if delay <= 0 {
		delay = time.Minute
	}
	next := time.Now().Add(delay).UnixMilli()
	return db.updateTempDownloadLease(rawURL, owner, `
		UPDATE temp_download_queue
		SET status = 'pending', retry_count = retry_count + 1, next_attempt_at_ms = ?,
			last_error_kind = ?, last_error = ?, lease_owner = '', lease_until_ms = 0
		WHERE url = ? AND status = 'processing' AND lease_owner = ?
	`, next, trimJobError(errorKind), trimJobError(message))
}

func (db *DB) BlockTempDownloadWork(rawURL, owner, errorKind, message string) error {
	return db.updateTempDownloadLease(rawURL, owner, `
		UPDATE temp_download_queue
		SET status = 'blocked', next_attempt_at_ms = 0, last_error_kind = ?, last_error = ?, lease_owner = '', lease_until_ms = 0
		WHERE url = ? AND status = 'processing' AND lease_owner = ?
	`, trimJobError(errorKind), trimJobError(message))
}

func (db *DB) updateTempDownloadLease(rawURL, owner, query string, args ...any) error {
	rawURL = strings.TrimSpace(rawURL)
	owner = strings.TrimSpace(owner)
	return db.WithWrite(func(tx *sql.Tx) error {
		args = append(args, rawURL, owner)
		res, err := tx.Exec(bind(query), args...)
		if err != nil {
			return err
		}
		return requireQueueLeaseUpdate(res, "temp_download_queue", rawURL, owner)
	})
}

// ResetTempDownloadWork makes interrupted interactive downloads claimable as
// soon as the local server comes back up. Their stable yt-dlp output names
// preserve the partial file for the resumed attempt.
func (db *DB) ResetTempDownloadWork() error {
	return db.WithWrite(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE temp_download_queue SET lease_owner = '', lease_until_ms = 0 WHERE status = 'blocked' AND last_error_kind = 'cancelled'`); err != nil {
			return err
		}
		_, err := tx.Exec(`
			UPDATE temp_download_queue
			SET status = 'pending', lease_owner = '', lease_until_ms = 0
			WHERE status = 'processing'
		`)
		return err
	})
}
