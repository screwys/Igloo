package worker

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/screwys/igloo/internal/download"
	"github.com/screwys/igloo/internal/model"
)

func (m *Manager) runTikTokLivesLoop(ctx context.Context) {
	for ctx.Err() == nil {
		if m.cfg.PlatformEnabled("tiktok") && m.externalWorkAllowed(time.Now()) {
			rooms, err := m.db.FollowedTikTokLiveSources()
			failed := 0
			if err == nil && len(rooms) > 0 {
				err = download.FetchTikTokLives(ctx, rooms, func(handle string, info *download.TikTokLiveInfo, fetchErr error) error {
					if fetchErr == nil {
						channelID := model.TikTokChannelIDFromHandle(handle)
						if !m.db.IsChannelFollowed(channelID) {
							return nil
						}
						var live *model.TikTokLive
						if info != nil {
							live = &model.TikTokLive{ChannelID: channelID, RoomID: info.RoomID, Handle: handle, Title: info.Title, ViewerCount: info.ViewerCount, ObservedAtMs: time.Now().UnixMilli()}
						}
						return m.db.ObserveTikTokLive(channelID, live)
					}
					failed++
					return nil
				})
			}
			if failed > 0 && ctx.Err() == nil {
				log.Printf("[tiktok-live] %d channel checks failed", failed)
			}
			if err != nil && ctx.Err() == nil {
				log.Printf("[tiktok-live] refresh: %v", err)
			}
		}
		timer := time.NewTimer(time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// StartLiveRemux keeps FLV lives playable through the same HLS clients.
func (m *Manager) StartLiveRemux(sourceURL, directory string) (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithCancel(m.ctx)
	done := make(chan error, 1)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		result := (download.CommandRunner{}).Run(ctx, "ffmpeg", []string{
			"-nostdin", "-hide_banner", "-loglevel", "error", "-i", sourceURL, "-c", "copy", "-f", "hls",
			"-hls_time", "2", "-hls_list_size", "8", "-hls_flags", "temp_file+delete_segments",
			"-hls_segment_filename", filepath.Join(directory, "segment-%09d.ts"), filepath.Join(directory, "index.m3u8"),
		}, download.CommandOptions{})
		done <- result.Err
		close(done)
	}()
	return cancel, done
}

func (m *Manager) ResolveTikTokLive(ctx context.Context, channelID string) (*model.TikTokLive, *download.PlaybackInfo, error) {
	channel, err := m.db.GetChannel(channelID)
	if err != nil {
		return nil, nil, err
	}
	if channel == nil || channel.Platform != "tiktok" {
		return nil, nil, fmt.Errorf("TikTok channel not found")
	}
	if !m.cfg.PlatformEnabled("tiktok") {
		return nil, nil, fmt.Errorf("TikTok is not enabled")
	}
	cached, err := m.db.GetTikTokLive(channelID)
	if err != nil {
		return nil, nil, err
	}
	roomID := ""
	if cached != nil {
		roomID = cached.RoomID
	}
	info, err := download.FetchTikTokLive(ctx, tiktokHandleForChannel(*channel), roomID)
	if err != nil {
		return nil, nil, err
	}
	if info == nil {
		if err := m.db.ObserveTikTokLive(channelID, nil); err != nil {
			return nil, nil, err
		}
		return nil, nil, nil
	}
	live := &model.TikTokLive{ChannelID: channelID, RoomID: info.RoomID, Handle: info.Handle, DisplayName: channel.DisplayName, Title: info.Title,
		AvatarURL: "/api/media/avatar/" + channelID, ViewerCount: info.ViewerCount, ObservedAtMs: time.Now().UnixMilli()}
	if live.DisplayName == "" {
		live.DisplayName = channel.Name
	}
	if err := m.db.ObserveTikTokLive(channelID, live); err != nil {
		return nil, nil, err
	}
	return live, info.Playback, nil
}
