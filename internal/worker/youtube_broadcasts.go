package worker

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/db"
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
			for _, broadcast := range broadcasts {
				if broadcast.LiveStatus == "was_live" {
					if err := m.QueueYouTubeReplayChat(broadcast.VideoID); err != nil {
						log.Printf("[youtube-chat] queue %s: %v", broadcast.VideoID, err)
					}
				}
			}
		}
		window.Complete = true
		return window, nil
	}
}

func (m *Manager) nextYouTubeBroadcastCheck(attempts map[string]time.Time) (*model.Channel, time.Time, error) {
	if !m.db.BoolSetting("youtube_broadcasts_enabled") {
		return nil, time.Time{}, nil
	}
	broadcasts, err := m.db.ListYouTubeBroadcasts(db.YouTubeBroadcastQuery{States: []string{"is_upcoming"}, Limit: -1})
	if err != nil {
		return nil, time.Time{}, err
	}
	var channelID string
	var readyAt time.Time
	for _, broadcast := range broadcasts {
		if broadcast.StartsAtMs <= 0 {
			continue
		}
		due := time.UnixMilli(broadcast.StartsAtMs)
		observed := time.UnixMilli(broadcast.ObservedAtMs)
		if !observed.Before(due) {
			due = observed.Add(2 * time.Minute)
		}
		if retry := attempts[broadcast.ChannelID].Add(2 * time.Minute); retry.After(due) {
			due = retry
		}
		if channelID == "" || due.Before(readyAt) {
			channelID, readyAt = broadcast.ChannelID, due
		}
	}
	if channelID == "" {
		return nil, time.Time{}, nil
	}
	channel, err := m.db.GetChannelByID(channelID)
	return &channel, readyAt, err
}

func (m *Manager) processYouTubeBroadcastCheck(ctx context.Context, channel model.Channel) {
	if !m.db.IsChannelFollowed(channel.ChannelID) || m.downloader == nil || m.downloader.YtDlp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryChannelCheckTimeout)
	defer cancel()
	includeMemberOnly := m.db.BoolSetting("youtube_include_member_only")
	if settings, err := m.db.GetChannelSettings(channel.ChannelID); err == nil && settings != nil {
		includeMemberOnly = settings.IncludeMemberOnly
	}
	_, err := m.checkYouTubeBroadcasts(ctx, channel, m.getChannelMaxVideos(channel), includeMemberOnly)
	m.ReportExternalResult(err)
	m.recordDownloadPlatformBackoff("youtube", download.ClassifyFailure(err, nil, 0), err)
	if err != nil {
		log.Printf("[youtube-live] scheduled check failed for %s: %v", channel.Name, err)
	} else {
		log.Printf("[youtube-live] checked scheduled broadcasts for %s", channel.Name)
	}
}
