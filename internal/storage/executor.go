package storage

import (
	"context"
	"fmt"
)

type MediaLane string

const (
	MediaLaneState           MediaLane = "state_ssd"
	MediaLaneBulkInteractive MediaLane = "bulk_interactive"
	MediaLaneBulkForeground  MediaLane = "bulk_foreground"
	MediaLaneBulkRegular     MediaLane = "bulk_regular"
	MediaLaneBulkBackground  MediaLane = "bulk_background"
)

const mediaStateConcurrency = 2

// MediaExecutor is the single admission owner for file-producing work. Bulk
// lanes describe scheduling intent, but they share one physical writer because
// they target the same media disk. Small state assets converge independently.
type MediaExecutor struct {
	state chan struct{}
	bulk  chan struct{}
}

func NewMediaExecutor() *MediaExecutor {
	return &MediaExecutor{
		state: make(chan struct{}, mediaStateConcurrency),
		bulk:  make(chan struct{}, 1),
	}
}

func (e *MediaExecutor) Run(ctx context.Context, lane MediaLane, work func() error) error {
	if e == nil {
		return work()
	}
	switch lane {
	case MediaLaneState:
		return e.run(ctx, e.state, work)
	case MediaLaneBulkInteractive, MediaLaneBulkForeground, MediaLaneBulkRegular, MediaLaneBulkBackground:
		return e.run(ctx, e.bulk, work)
	default:
		return fmt.Errorf("unknown media lane %q", lane)
	}
}

func (e *MediaExecutor) run(ctx context.Context, slot chan struct{}, work func() error) error {
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
		if err := ctx.Err(); err != nil {
			return err
		}
		return work()
	case <-ctx.Done():
		return ctx.Err()
	}
}
