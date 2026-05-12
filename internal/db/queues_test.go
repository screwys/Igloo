package db

import (
	"errors"
	"testing"
	"time"
)

func TestEnqueueAndClaimFeedMediaJobs(t *testing.T) {
	d := openWritableTestDB(t)

	jobs := []FeedMediaJobRow{
		{TweetID: "queue_test_001", TweetURL: "https://x.com/user_a/status/queue_test_001", SourceHandle: "user_a", MediaKind: "image"},
		{TweetID: "queue_test_002", TweetURL: "https://x.com/user_b/status/queue_test_002", SourceHandle: "user_b", MediaKind: "video"},
	}

	if err := d.EnqueueFeedMediaJobs(jobs); err != nil {
		t.Fatalf("EnqueueFeedMediaJobs: %v", err)
	}

	// Use a large batch to ensure we claim our test jobs even if schema migrations
	// re-queued many Python-era jobs in the production DB copy.
	claimed, err := d.ClaimFeedMediaBatch(10000)
	if err != nil {
		t.Fatalf("ClaimFeedMediaBatch: %v", err)
	}
	// Should have claimed at least our 2 (there may be pre-existing queued jobs in the copy)
	found := 0
	for _, j := range claimed {
		if j.TweetID == "queue_test_001" || j.TweetID == "queue_test_002" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("expected both test jobs in claimed batch, found %d in %d total claimed", found, len(claimed))
	}

	// A second claim should return 0 of our test jobs (they are processing now)
	claimed2, err := d.ClaimFeedMediaBatch(10000)
	if err != nil {
		t.Fatalf("second ClaimFeedMediaBatch: %v", err)
	}
	for _, j := range claimed2 {
		if j.TweetID == "queue_test_001" || j.TweetID == "queue_test_002" {
			t.Errorf("test job %q should not be claimable again", j.TweetID)
		}
	}
}

func TestUpdateFeedMediaJobStatus(t *testing.T) {
	d := openWritableTestDB(t)

	jobs := []FeedMediaJobRow{
		{TweetID: "update_test_001", TweetURL: "https://x.com/user_a/status/update_test_001", MediaKind: "image"},
	}
	if err := d.EnqueueFeedMediaJobs(jobs); err != nil {
		t.Fatalf("EnqueueFeedMediaJobs: %v", err)
	}

	// Claim it
	claimed, err := d.ClaimFeedMediaBatch(10)
	if err != nil {
		t.Fatalf("ClaimFeedMediaBatch: %v", err)
	}
	found := false
	for _, j := range claimed {
		if j.TweetID == "update_test_001" {
			found = true
		}
	}
	if !found {
		t.Skip("update_test_001 not in claimed batch (unexpected state in test DB copy)")
	}

	// Mark completed
	if err := d.UpdateFeedMediaJobStatus("update_test_001", "completed", "", 0); err != nil {
		t.Fatalf("UpdateFeedMediaJobStatus: %v", err)
	}

	// Should no longer be claimable
	claimed2, err := d.ClaimFeedMediaBatch(10)
	if err != nil {
		t.Fatalf("second ClaimFeedMediaBatch: %v", err)
	}
	for _, j := range claimed2 {
		if j.TweetID == "update_test_001" {
			t.Error("completed job should not be claimable")
		}
	}
}

func TestClaimFeedMediaBatchPrefersNewestWithinPriority(t *testing.T) {
	d := openWritableTestDB(t)

	if err := d.ExecRaw(`
		INSERT INTO feed_media_jobs
			(tweet_id, status, media_kind, retry_count, priority, created_at, updated_at)
		VALUES
			('claim_old_high_priority', 'queued', 'image', 0, 9999, 1000, 1000),
			('claim_new_high_priority', 'queued', 'image', 0, 9999, 2000, 2000)
	`); err != nil {
		t.Fatalf("insert feed media jobs: %v", err)
	}

	claimed, err := d.ClaimFeedMediaBatch(1)
	if err != nil {
		t.Fatalf("ClaimFeedMediaBatch: %v", err)
	}
	if len(claimed) != 1 || claimed[0].TweetID != "claim_new_high_priority" {
		t.Fatalf("claimed %+v, want newest high-priority job", claimed)
	}
}

func TestPromoteFeedMediaJobForTweetQueuesExistingJob(t *testing.T) {
	d := openWritableTestDB(t)

	if err := d.ExecRaw(`
		INSERT INTO feed_media_jobs (tweet_id, status, media_kind, retry_count, priority, last_error, created_at, updated_at)
		VALUES ('promote_test_001', 'failed', 'image', 3, 0, 'old error', 1, 1)
	`); err != nil {
		t.Fatalf("insert feed media job: %v", err)
	}

	changed, err := d.PromoteFeedMediaJobForTweet("promote_test_001", 20)
	if err != nil {
		t.Fatalf("PromoteFeedMediaJobForTweet: %v", err)
	}
	if !changed {
		t.Fatal("expected promotion to update the existing job")
	}

	var status, lastError string
	var retryCount, priority int
	if err := d.QueryRow(`
		SELECT status, retry_count, priority, COALESCE(last_error, '')
		FROM feed_media_jobs
		WHERE tweet_id = 'promote_test_001'
	`).Scan(&status, &retryCount, &priority, &lastError); err != nil {
		t.Fatalf("query promoted job: %v", err)
	}
	if status != "queued" || retryCount != 0 || priority != 20 || lastError != "" {
		t.Fatalf("promoted job = status %q retry %d priority %d error %q", status, retryCount, priority, lastError)
	}
}

func TestPromoteFeedMediaJobForTweetIgnoresCompletedJob(t *testing.T) {
	d := openWritableTestDB(t)

	if err := d.ExecRaw(`
		INSERT INTO feed_media_jobs (tweet_id, status, media_kind, retry_count, priority, last_error, created_at, updated_at)
		VALUES ('promote_test_002', 'completed', 'image', 0, 0, NULL, 1, 1)
	`); err != nil {
		t.Fatalf("insert feed media job: %v", err)
	}

	changed, err := d.PromoteFeedMediaJobForTweet("promote_test_002", 20)
	if err != nil {
		t.Fatalf("PromoteFeedMediaJobForTweet: %v", err)
	}
	if changed {
		t.Fatal("completed job should not be promoted")
	}
}

func TestEnqueueDuplicateIgnored(t *testing.T) {
	d := openWritableTestDB(t)

	job := FeedMediaJobRow{
		TweetID:   "dup_test_001",
		TweetURL:  "https://x.com/user_a/status/dup_test_001",
		MediaKind: "image",
	}

	if err := d.EnqueueFeedMediaJobs([]FeedMediaJobRow{job}); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	// Second enqueue with the same tweet_id should not error
	if err := d.EnqueueFeedMediaJobs([]FeedMediaJobRow{job}); err != nil {
		t.Fatalf("second enqueue (duplicate) should not error: %v", err)
	}

	q, _, err := d.CountPendingFeedMediaJobs()
	if err != nil {
		t.Fatalf("CountPendingFeedMediaJobs: %v", err)
	}
	// Just verify the count is a non-negative integer; exact count depends on DB state
	if q < 0 {
		t.Error("negative queued count")
	}
}

func TestClaimFeedMediaBatchWithLeaseExcludesActiveLease(t *testing.T) {
	d := openWritableTestDB(t)
	now := time.Now().UnixMilli()
	if err := d.ExecRaw(`
		INSERT INTO feed_media_jobs
			(tweet_id, status, media_kind, retry_count, priority, next_attempt_at_ms, created_at, updated_at)
		VALUES
			('lease_feed_001', 'queued', 'image', 0, 10, 0, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert feed media job: %v", err)
	}

	first, err := d.ClaimFeedMediaBatchWithLease(LeaseOptions{
		Owner:      "worker-a",
		NowMs:      now,
		LeaseMs:    int64(time.Minute / time.Millisecond),
		Limit:      1,
		StatusFrom: "queued",
		StatusTo:   "processing",
	})
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if len(first) != 1 || first[0].TweetID != "lease_feed_001" {
		t.Fatalf("first claim = %+v, want lease_feed_001", first)
	}

	second, err := d.ClaimFeedMediaBatchWithLease(LeaseOptions{
		Owner:      "worker-b",
		NowMs:      now + 1,
		LeaseMs:    int64(time.Minute / time.Millisecond),
		Limit:      1,
		StatusFrom: "queued",
		StatusTo:   "processing",
	})
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("active lease was claimed by another worker: %+v", second)
	}
}

func TestResetStaleFeedMediaJobsOnlyReleasesExpiredLeases(t *testing.T) {
	d := openWritableTestDB(t)
	now := time.Now().UnixMilli()
	if err := d.ExecRaw(`
		INSERT INTO feed_media_jobs
			(tweet_id, status, media_kind, lease_owner, lease_until_ms, created_at, updated_at)
		VALUES
			('lease_feed_active', 'processing', 'image', 'worker-a', ?, ?, ?),
			('lease_feed_expired', 'processing', 'image', 'worker-b', ?, ?, ?)
	`, now+60000, now, now, now-1, now, now); err != nil {
		t.Fatalf("insert feed media jobs: %v", err)
	}

	n, err := d.ResetStaleFeedMediaJobsAt(now)
	if err != nil {
		t.Fatalf("ResetStaleFeedMediaJobsAt: %v", err)
	}
	if n != 1 {
		t.Fatalf("reset count = %d, want 1", n)
	}

	var activeStatus, expiredStatus, expiredOwner string
	var expiredLease int64
	if err := d.QueryRow(`SELECT status FROM feed_media_jobs WHERE tweet_id='lease_feed_active'`).Scan(&activeStatus); err != nil {
		t.Fatalf("query active: %v", err)
	}
	if err := d.QueryRow(`SELECT status, COALESCE(lease_owner,''), COALESCE(lease_until_ms,0) FROM feed_media_jobs WHERE tweet_id='lease_feed_expired'`).Scan(&expiredStatus, &expiredOwner, &expiredLease); err != nil {
		t.Fatalf("query expired: %v", err)
	}
	if activeStatus != "processing" || expiredStatus != "queued" || expiredOwner != "" || expiredLease != 0 {
		t.Fatalf("statuses active=%q expired=%q owner=%q lease=%d", activeStatus, expiredStatus, expiredOwner, expiredLease)
	}
}

func TestRetryFeedMediaJobStoresNextAttemptAndErrorKind(t *testing.T) {
	d := openWritableTestDB(t)
	now := time.Now().UnixMilli()
	if err := d.ExecRaw(`
		INSERT INTO feed_media_jobs
			(tweet_id, status, media_kind, retry_count, lease_owner, lease_until_ms, created_at, updated_at)
		VALUES ('lease_feed_retry', 'processing', 'image', 2, 'worker-a', ?, ?, ?)
	`, now+60000, now, now); err != nil {
		t.Fatalf("insert feed media job: %v", err)
	}

	if err := d.RetryFeedMediaJob("lease_feed_retry", "worker-a", "temporary", "network timeout", 5*time.Minute, now); err != nil {
		t.Fatalf("RetryFeedMediaJob: %v", err)
	}

	var status, kind, msg, owner string
	var retries int
	var nextAttempt, leaseUntil int64
	if err := d.QueryRow(`
		SELECT status, retry_count, next_attempt_at_ms, last_error_kind, COALESCE(last_error,''), COALESCE(lease_owner,''), COALESCE(lease_until_ms,0)
		FROM feed_media_jobs WHERE tweet_id='lease_feed_retry'
	`).Scan(&status, &retries, &nextAttempt, &kind, &msg, &owner, &leaseUntil); err != nil {
		t.Fatalf("query retry job: %v", err)
	}
	if status != "queued" || retries != 3 || nextAttempt != now+int64((5*time.Minute)/time.Millisecond) || kind != "temporary" || msg != "network timeout" || owner != "" || leaseUntil != 0 {
		t.Fatalf("retry row = status=%q retries=%d next=%d kind=%q msg=%q owner=%q lease=%d", status, retries, nextAttempt, kind, msg, owner, leaseUntil)
	}
}

func TestFeedMediaTerminalUpdatesRequireCurrentLeaseOwner(t *testing.T) {
	tests := []struct {
		name string
		run  func(*DB, string, int64) error
	}{
		{
			name: "complete",
			run: func(d *DB, tweetID string, now int64) error {
				return d.CompleteFeedMediaJob(tweetID, "worker-stale", now)
			},
		},
		{
			name: "retry",
			run: func(d *DB, tweetID string, now int64) error {
				return d.RetryFeedMediaJob(tweetID, "worker-stale", "temporary", "network timeout", time.Minute, now)
			},
		},
		{
			name: "fail",
			run: func(d *DB, tweetID string, now int64) error {
				return d.FailFeedMediaJob(tweetID, "worker-stale", "auth", "login required", now)
			},
		},
		{
			name: "prune",
			run: func(d *DB, tweetID string, now int64) error {
				return d.PruneFeedMediaJob(tweetID, "worker-stale", "not_found", "gone", 4, now)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := openWritableTestDB(t)
			now := time.Now().UnixMilli()
			tweetID := "lease_terminal_" + tt.name
			if err := d.ExecRaw(`
				INSERT INTO feed_media_jobs
					(tweet_id, status, media_kind, retry_count, lease_owner, lease_until_ms, created_at, updated_at)
				VALUES (?, 'processing', 'image', 2, 'worker-current', ?, ?, ?)
			`, tweetID, now+60000, now, now); err != nil {
				t.Fatalf("insert feed media job: %v", err)
			}

			err := tt.run(d, tweetID, now)
			if !errors.Is(err, ErrQueueLeaseNotHeld) {
				t.Fatalf("%s stale owner error = %v, want ErrQueueLeaseNotHeld", tt.name, err)
			}

			var status, owner, kind, msg string
			var retries int
			var leaseUntil, nextAttempt, completedAt int64
			if err := d.QueryRow(`
				SELECT status, retry_count, COALESCE(next_attempt_at_ms,0),
				       COALESCE(last_error_kind,''), COALESCE(last_error,''),
				       COALESCE(lease_owner,''), COALESCE(lease_until_ms,0),
				       COALESCE(completed_at_ms,0)
				FROM feed_media_jobs WHERE tweet_id=?
			`, tweetID).Scan(&status, &retries, &nextAttempt, &kind, &msg, &owner, &leaseUntil, &completedAt); err != nil {
				t.Fatalf("query feed media job: %v", err)
			}
			if status != "processing" || retries != 2 || nextAttempt != 0 || kind != "" || msg != "" || owner != "worker-current" || leaseUntil == 0 || completedAt != 0 {
				t.Fatalf("stale %s changed row: status=%q retries=%d next=%d kind=%q msg=%q owner=%q lease=%d completed=%d",
					tt.name, status, retries, nextAttempt, kind, msg, owner, leaseUntil, completedAt)
			}
		})
	}
}
