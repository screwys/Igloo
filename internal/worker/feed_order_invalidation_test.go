package worker

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/db"
)

func TestFeedOrderInvalidationResumesCommittedWorkAfterWorkerStarts(t *testing.T) {
	database := newTestWorkerDB(t)
	if err := database.ExecRaw(`
		INSERT INTO feed_items (
			tweet_id, channel_id, source_channel_id, body_text, published_at, fetched_at, algo_scored_at
		) VALUES
			('sample_direct', 'twitter_other', 'twitter_other', 'direct', 2, 2, 101),
			('sample_channel', 'twitter_sample', 'twitter_sample', 'channel', 1, 1, 202)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.MutateLike(db.LikeMutation{
		TweetID: "sample_direct", Action: "set", UpdatedAtMs: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.MutateMute("twitter_sample", "set", 11); err != nil {
		t.Fatal(err)
	}
	var queued int
	if err := database.QueryRow(`SELECT COUNT(*) FROM feed_order_invalidations`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 2 {
		t.Fatalf("persisted invalidations = %d, want 2", queued)
	}

	m := NewManager(database, testCfg(t.TempDir()))
	writeHeld := make(chan struct{})
	releaseWrite := make(chan struct{})
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- database.WithWrite(func(_ *sql.Tx) error {
			close(writeHeld)
			<-releaseWrite
			return nil
		})
	}()
	<-writeHeld

	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		m.runFeedOrderInvalidationLoop(ctx)
		close(workerDone)
	}()

	select {
	case <-m.feedScoringKick:
		t.Fatal("scoring was kicked before the persisted invalidation committed")
	default:
	}
	close(releaseWrite)
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}

	select {
	case <-m.feedOrderReady:
	case <-time.After(2 * time.Second):
		t.Fatal("startup readiness did not wait for the persisted invalidation")
	}
	select {
	case <-m.feedScoringKick:
		t.Fatal("startup invalidation scheduled a redundant scoring pass")
	default:
	}
	for _, tweetID := range []string{"sample_direct", "sample_channel"} {
		var scoredAt int64
		if err := database.QueryRow(`SELECT algo_scored_at FROM feed_items WHERE tweet_id = ?`, tweetID).Scan(&scoredAt); err != nil {
			t.Fatal(err)
		}
		if scoredAt != 0 {
			t.Fatalf("%s algo_scored_at = %d, want 0", tweetID, scoredAt)
		}
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM feed_order_invalidations`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("persisted invalidations after drain = %d, want 0", queued)
	}
	if err := database.UpdateAlgoInterest(map[string]float64{"sample_direct": 404}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.MutateLike(db.LikeMutation{
		TweetID: "sample_direct", Action: "clear", UpdatedAtMs: 20,
	}); err != nil {
		t.Fatal(err)
	}
	m.WakeFeedOrderInvalidation()
	select {
	case <-m.feedScoringPriorityKick:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime invalidation did not request prompt scoring")
	}
	var scoredAt int64
	if err := database.QueryRow(`
		SELECT algo_scored_at FROM feed_items WHERE tweet_id = 'sample_direct'
	`).Scan(&scoredAt); err != nil {
		t.Fatal(err)
	}
	if scoredAt != 0 {
		t.Fatalf("prompt scoring signal preceded score invalidation: %d", scoredAt)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM feed_order_invalidations`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("prompt scoring signal preceded queue drain: %d", queued)
	}

	cancel()
	select {
	case <-workerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("feed-order invalidation worker did not stop")
	}
}

func TestFeedOrderInvalidationCoalescesAndDrainsBoundedBatches(t *testing.T) {
	database := newTestWorkerDB(t)
	ownerCount := feedOrderTweetQueueBatch + 1
	for i := 0; i < ownerCount; i++ {
		tweetID := fmt.Sprintf("sample_%03d", i)
		if err := database.ExecRaw(`
			INSERT INTO feed_items (tweet_id, body_text, algo_scored_at)
			VALUES (?, 'body', 303)
		`, tweetID); err != nil {
			t.Fatal(err)
		}
		if _, err := database.MutateLike(db.LikeMutation{
			TweetID: tweetID, Action: "set", UpdatedAtMs: int64(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.MutateLike(db.LikeMutation{
		TweetID: "sample_000", Action: "clear", UpdatedAtMs: 10_000,
	}); err != nil {
		t.Fatal(err)
	}

	var queued int
	if err := database.QueryRow(`SELECT COUNT(*) FROM feed_order_invalidations`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != ownerCount {
		t.Fatalf("coalesced invalidations = %d, want %d", queued, ownerCount)
	}

	m := NewManager(database, testCfg(t.TempDir()))
	processed, err := m.ProcessFeedOrderInvalidations(context.Background())
	if err != nil || !processed {
		t.Fatalf("first ProcessFeedOrderInvalidations = (%t, %v)", processed, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM feed_order_invalidations`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("invalidations after bounded batch = %d, want 1", queued)
	}
	var invalidated int
	if err := database.QueryRow(`SELECT COUNT(*) FROM feed_items WHERE algo_scored_at = 0`).Scan(&invalidated); err != nil {
		t.Fatal(err)
	}
	if invalidated != feedOrderTweetQueueBatch {
		t.Fatalf("invalidated items after first batch = %d, want %d", invalidated, feedOrderTweetQueueBatch)
	}

	processed, err = m.ProcessFeedOrderInvalidations(context.Background())
	if err != nil || !processed {
		t.Fatalf("second ProcessFeedOrderInvalidations = (%t, %v)", processed, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM feed_order_invalidations`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("invalidations after second batch = %d, want 0", queued)
	}
}
