package worker

import (
	"context"
	"log"
	"time"
)

func (m *Manager) WakeUnfollowCleanup() {
	if m == nil || m.unfollowCleanupKick == nil {
		return
	}
	select {
	case m.unfollowCleanupKick <- struct{}{}:
	default:
	}
}

func (m *Manager) runUnfollowCleanup(ctx context.Context) {
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		processed, err := m.db.ProcessUnfollowCleanup(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("[unfollow_cleanup] cleanup failed: %v", err)
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-m.unfollowCleanupKick:
		case <-poll.C:
		}
	}
}
