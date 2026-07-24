package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// TempDownloadWork is an interactive download claimed by the durable worker.
type TempDownloadWork struct {
	URL        string
	Platform   string
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
			INSERT INTO temp_download_queue (url, platform, added_at_ms)
			VALUES (?, ?, ?)
			ON CONFLICT(url) DO UPDATE SET
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
				ORDER BY added_at_ms, url
				LIMIT 1
			)
			RETURNING url, platform, retry_count, lease_owner
		`, owner, nowMs+lease.Milliseconds(), nowMs, nowMs, nowMs)
		if err := row.Scan(&work.URL, &work.Platform, &work.RetryCount, &work.LeaseOwner); err != nil {
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
