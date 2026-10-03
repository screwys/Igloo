package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/download"
	"github.com/screwys/igloo/internal/model"
)

func (m *Manager) checkYouTubeBroadcasts(ctx context.Context, channel model.Channel, maxVideos int, includeMemberOnly bool) (download.SourceWindow, error) {
	window := download.SourceWindow{Component: download.SourceComponentReplays}
	checkReplays := m.db.BoolSetting("youtube_check_replays")
	if !checkReplays && !m.db.BoolSetting("youtube_broadcasts_enabled") {
		window.Complete = true
		return window, nil
	}
	if !m.externalWorkAllowed(time.Now()) {
		return window, fmt.Errorf("YouTube checking is paused")
	}
	channelURL := channel.URL
	if channelURL == "" && strings.HasPrefix(channel.ChannelID, "youtube_UC") {
		channelURL = "https://www.youtube.com/channel/" + strings.TrimPrefix(channel.ChannelID, "youtube_")
	}
	cookies, browser := m.cookiesFor("youtube")
	limit := max(24, maxVideos)
	if checkReplays && maxVideos <= 0 {
		limit = -1
	}
	for {
		broadcasts, err := m.downloader.YtDlp.FetchYouTubeBroadcasts(ctx, channelURL, channel.ChannelID, limit, download.Opts{
			Cookies: cookies, CookiesFromBrowser: browser,
		})
		if err != nil {
			return window, err
		}
		window.Refs = nil
		if checkReplays {
			for _, broadcast := range broadcasts {
				if broadcast.LiveStatus != "was_live" || !includeMemberOnly && broadcast.Availability == "subscriber_only" {
					continue
				}
				window.Refs = append(window.Refs, download.VideoRef{VideoID: broadcast.VideoID, Title: broadcast.Title,
					URL: "https://www.youtube.com/watch?v=" + broadcast.VideoID, PublishedAtMs: broadcast.PublishedAtMs})
			}
		}
		// Upcoming and ongoing broadcasts must not hide the channel's recent replays.
		if checkReplays && maxVideos > 0 && len(window.Refs) < maxVideos && len(broadcasts) >= limit {
			limit *= 2
			continue
		}
		if ctx.Err() != nil {
			return window, ctx.Err()
		}
		if m.db.IsChannelFollowed(channel.ChannelID) {
			if err := m.db.ReplaceYouTubeBroadcasts(channel.ChannelID, broadcasts, time.Now().UnixMilli()); err != nil {
				return window, err
			}
		}
		window.Complete = true
		return window, nil
	}
}
