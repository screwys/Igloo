package worker

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/download"
	"github.com/screwys/igloo/internal/model"
)

func (m *Manager) runXBroadcastsLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if m.cfg.PlatformEnabled("twitter") && m.externalWorkAllowed(time.Now()) {
			m.refreshXBroadcasts(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) refreshXBroadcasts(ctx context.Context) {
	sources, err := m.db.XBroadcastSources(true)
	if err != nil {
		log.Printf("[x-live] list saved broadcasts: %v", err)
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	failed := 0
	for _, rawURL := range sources {
		if checkCtx.Err() != nil || !m.externalWorkAllowed(time.Now()) {
			break
		}
		if _, _, err := m.ResolveXPlayback(checkCtx, rawURL); err != nil {
			failed++
			if ctx.Err() == nil {
				if err := m.db.RecordXBroadcastCheck(rawURL, time.Now().UnixMilli()); err != nil {
					log.Printf("[x-live] record broadcast check: %v", err)
				}
			}
		}
	}
	if failed > 0 && ctx.Err() == nil {
		log.Printf("[x-live] %d broadcast checks failed", failed)
	}
}

func (m *Manager) ResolveXPlayback(ctx context.Context, rawURL string) (*model.XBroadcast, *download.PlaybackInfo, error) {
	if m == nil || m.downloader == nil {
		return nil, nil, fmt.Errorf("x playback worker unavailable")
	}
	if !m.cfg.PlatformEnabled("twitter") {
		return nil, nil, fmt.Errorf("x is not enabled")
	}
	rawURL = model.NormalizeXBroadcastURL(rawURL)
	if rawURL == "" {
		return nil, nil, fmt.Errorf("x broadcast URL required")
	}
	file, browser := m.cookiesFor("twitter")
	info, err := m.downloader.YtDlp.FetchBroadcastInfo(ctx, rawURL, download.Opts{
		Cookies: file, CookiesFromBrowser: browser, CookieAlternates: m.cookieSetsFor("twitter"),
	})
	if err != nil {
		return nil, nil, download.WithOperationContext(err, "yt-dlp", "")
	}
	handle := info.UploaderID
	if uploaderURL, err := url.Parse(info.UploaderURL); err == nil {
		if host := strings.ToLower(uploaderURL.Hostname()); host == "x.com" || host == "twitter.com" || host == "www.x.com" || host == "www.twitter.com" {
			handle = strings.Trim(uploaderURL.Path, "/")
		}
	}
	channelID := model.TwitterChannelIDFromHandle(handle)
	if channelID == "" {
		channelID, err = m.db.XBroadcastSourceChannelID(rawURL)
		if err != nil {
			return nil, nil, err
		}
		handle = strings.TrimPrefix(channelID, "twitter_")
	}
	name := info.Uploader
	if name == "" {
		name = info.Channel
	}
	status := info.LiveStatus
	if info.IsLive {
		status = "is_live"
	} else if status == "" {
		status = "was_live"
	}
	broadcastURL, _ := info.Metadata["webpage_url"].(string)
	broadcastURL = model.NormalizeXBroadcastURL(broadcastURL)
	if broadcastURL == "" {
		broadcastURL = rawURL
	}
	broadcast := model.XBroadcast{
		BroadcastID: info.ID, ChannelID: channelID, URL: broadcastURL, Title: info.Title,
		ThumbnailURL: info.Thumbnail, LiveStatus: status, ViewerCount: info.ConcurrentViewCount,
		ObservedAtMs: time.Now().UnixMilli(), Handle: handle, DisplayName: name,
	}
	if channelID == "" {
		return &broadcast, info, nil
	}
	broadcast.AvatarURL = "/api/media/avatar/" + channelID
	if err := m.db.ObserveChannels([]model.Channel{{
		ChannelID: channelID, SourceID: handle, Handle: handle, Name: handle, DisplayName: name,
		URL: "https://x.com/" + handle, Platform: "twitter",
	}}); err != nil {
		return nil, nil, err
	}
	if err := m.db.ObserveXBroadcast(rawURL, broadcast); err != nil {
		return nil, nil, err
	}
	m.KickProfileJobs()
	stored, err := m.db.GetXBroadcast(broadcast.BroadcastID)
	if err != nil {
		return nil, nil, err
	}
	return stored, info, nil
}
