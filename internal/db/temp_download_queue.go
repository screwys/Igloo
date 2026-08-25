package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/model"
)

// TempDownloadWork is an interactive download claimed by the durable worker.
type TempDownloadWork struct {
	URL        string
	Platform   string
	Origin     string
	RetryCount int
	LeaseOwner string
}

// TempDownloadState is the persisted state shown by the temporary watch page.
type TempDownloadState struct {
	Status string
	Error  string
}

func (db *DB) TempDownloadState(rawURL string) (TempDownloadState, bool, error) {
	var state TempDownloadState
	err := db.reader().QueryRow(`
		SELECT status, last_error
		FROM temp_download_queue
		WHERE url = ?
	`, strings.TrimSpace(rawURL)).Scan(&state.Status, &state.Error)
	if err == sql.ErrNoRows {
		return TempDownloadState{}, false, nil
	}
	if err != nil {
		return TempDownloadState{}, false, err
	}
	return state, true, nil
}

func (db *DB) TempDownloadOrigin(rawURL string) (string, error) {
	var origin string
	err := db.reader().QueryRow(`SELECT origin FROM temp_download_queue WHERE url = ?`, strings.TrimSpace(rawURL)).Scan(&origin)
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
			VALUES (?, ?)
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
		res, err := tx.Exec(`
				INSERT INTO temp_download_queue (url, platform, origin, added_at_ms)
				VALUES (?, ?, 'interactive', ?)
				ON CONFLICT(url) DO UPDATE SET
					origin = 'interactive',
				status = CASE WHEN temp_download_queue.status = 'blocked' THEN 'pending' ELSE temp_download_queue.status END,
				retry_count = CASE WHEN temp_download_queue.status = 'blocked' THEN 0 ELSE temp_download_queue.retry_count END,
				next_attempt_at_ms = CASE WHEN temp_download_queue.status = 'blocked' THEN 0 ELSE temp_download_queue.next_attempt_at_ms END,
				last_error_kind = CASE WHEN temp_download_queue.status = 'blocked' THEN '' ELSE temp_download_queue.last_error_kind END,
				last_error = CASE WHEN temp_download_queue.status = 'blocked' THEN '' ELSE temp_download_queue.last_error END
		`, rawURL, platform, nowMs)
		if err != nil {
			return err
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return err
		}
		queued = changed > 0
		return nil
	})
	return queued, err
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
			if err := tx.QueryRow(`
				SELECT EXISTS(SELECT 1 FROM videos v WHERE v.video_id = ? AND `+readyVideoMediaExistsSQL("v")+`)
			`, videoID).Scan(&ready); err != nil {
				return err
			}
			url := "https://www.youtube.com/watch?v=" + videoID
			var queued bool
			if err := tx.QueryRow(`
				SELECT EXISTS(SELECT 1 FROM temp_download_queue WHERE url = ? AND status IN ('pending', 'processing'))
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
			if err := tx.QueryRow(`
				SELECT EXISTS(SELECT 1 FROM videos v WHERE v.video_id = ? AND `+readyVideoMediaExistsSQL("v")+`)
				    OR EXISTS(SELECT 1 FROM temp_download_queue WHERE url = ?)
			`, videoID, url).Scan(&occupied); err != nil {
				return err
			}
			if occupied {
				continue
			}
			if _, err := tx.Exec(`
				INSERT INTO temp_download_queue (url, platform, origin, added_at_ms)
				VALUES (?, 'youtube', 'discover', ?)
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
		_, err := tx.Exec(`DELETE FROM temp_download_queue WHERE origin = 'discover' AND status != 'processing'`)
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
			SET status = 'processing', lease_owner = ?, lease_until_ms = ?, started_at_ms = CASE WHEN started_at_ms = 0 THEN ? ELSE started_at_ms END
			WHERE url = (
				SELECT url FROM temp_download_queue
				WHERE (status = 'pending' AND next_attempt_at_ms <= ?)
				   OR (status = 'processing' AND lease_until_ms <= ?)
					ORDER BY CASE WHEN origin = 'interactive' THEN 0 ELSE 1 END, added_at_ms, url
				LIMIT 1
			)
				RETURNING url, platform, origin, retry_count, lease_owner
			`, owner, nowMs+lease.Milliseconds(), nowMs, nowMs, nowMs)
		if err := row.Scan(&work.URL, &work.Platform, &work.Origin, &work.RetryCount, &work.LeaseOwner); err != nil {
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
		res, err := tx.Exec(query, args...)
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
		_, err := tx.Exec(`
			UPDATE temp_download_queue
			SET status = 'pending', lease_owner = '', lease_until_ms = 0
			WHERE status = 'processing'
		`)
		return err
	})
}
