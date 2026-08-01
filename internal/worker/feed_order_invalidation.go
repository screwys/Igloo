package worker

import (
	"context"
	"fmt"
	"log"
	"time"
)

const (
	feedOrderTweetQueueBatch   = 64
	feedOrderChannelQueueBatch = 4
	feedOrderRetryDelay        = time.Second
	feedOrderPollPeriod        = time.Minute
)

// WakeFeedOrderInvalidation hints that a canonical state transaction committed.
// The database queue remains the owner of what work needs to run.
func (m *Manager) WakeFeedOrderInvalidation() {
	if m == nil || m.feedOrderKick == nil {
		return
	}
	select {
	case m.feedOrderKick <- struct{}{}:
	default:
	}
}

// ProcessFeedOrderInvalidations applies one bounded durable batch.
func (m *Manager) ProcessFeedOrderInvalidations(ctx context.Context) (bool, error) {
	if m == nil || m.db == nil {
		return false, fmt.Errorf("feed-order invalidation database is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	processed, err := m.db.DrainFeedOrderInvalidations(
		ctx,
		feedOrderTweetQueueBatch,
		feedOrderChannelQueueBatch,
	)
	if err != nil || processed == 0 {
		return false, err
	}
	return true, nil
}

func (m *Manager) runFeedOrderInvalidationLoop(ctx context.Context) {
	log.Printf("[feed_order_invalidation] worker started")
	poll := time.NewTicker(feedOrderPollPeriod)
	defer poll.Stop()
	var retryTimer *time.Timer
	var retry <-chan time.Time
	defer func() {
		if retryTimer != nil {
			retryTimer.Stop()
		}
	}()

	run := true
	startup := true
	pendingScoring := false
	for {
		if run {
			if err := ctx.Err(); err != nil {
				return
			}
			processed, err := m.ProcessFeedOrderInvalidations(ctx)
			if err != nil {
				log.Printf("[feed_order_invalidation] apply failed: %v", err)
				if retryTimer == nil {
					retryTimer = time.NewTimer(feedOrderRetryDelay)
					retry = retryTimer.C
				}
				run = false
			} else if processed {
				pendingScoring = true
				continue
			} else {
				if startup {
					if m.feedOrderReady != nil {
						close(m.feedOrderReady)
					}
					startup = false
					pendingScoring = false
				} else if pendingScoring {
					m.kickFeedScoringAfterAction()
					pendingScoring = false
				}
				run = false
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-m.feedOrderKick:
			run = true
		case <-poll.C:
			run = true
		case <-retry:
			retry = nil
			retryTimer = nil
			run = true
		}
	}
}
