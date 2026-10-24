package worker

import (
	"context"
	"log"
	"strconv"
	"time"
)

const (
	identityMediaPruneInterval = 24 * time.Hour
	identityMediaPruneKey      = "identity_media_pruned_at_ms"
)

func (m *Manager) runIdentityMediaPrune(ctx context.Context) {
	next := m.nextPeriodicRun(identityMediaPruneKey, identityMediaPruneInterval)
	for {
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		removed, err := m.db.PruneUnusedIdentityMedia(ctx)
		if removed > 0 {
			log.Printf("[identity_media] removed %d unused avatars and banners", removed)
			m.WakeUnfollowCleanup()
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[identity_media] prune failed: %v", err)
			next = time.Now().Add(time.Hour)
			continue
		}
		if err := m.db.SetSetting(identityMediaPruneKey, strconv.FormatInt(time.Now().UnixMilli(), 10)); err != nil {
			log.Printf("[identity_media] record prune: %v", err)
		}
		next = time.Now().Add(identityMediaPruneInterval)
	}
}
