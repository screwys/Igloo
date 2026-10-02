package worker

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/download"
	"github.com/screwys/igloo/internal/model"
)

func (m *Manager) refreshYouTubeBroadcasts(ctx context.Context, channel model.Channel) {
	if m.downloader == nil || !m.externalWorkAllowed(time.Now()) {
		return
	}
	if _, cooling := m.activeDownloadPlatformBackoff("youtube", time.Now()); cooling {
		return
	}
	channelURL := channel.URL
	if channelURL == "" && strings.HasPrefix(channel.ChannelID, "youtube_UC") {
		channelURL = "https://www.youtube.com/channel/" + strings.TrimPrefix(channel.ChannelID, "youtube_")
	}
	workCtx, cancel := context.WithTimeout(ctx, discoveryChannelCheckTimeout)
	defer cancel()
	cookies, browser := m.cookiesFor("youtube")
	broadcasts, err := m.downloader.YtDlp.FetchYouTubeBroadcasts(workCtx, channelURL, channel.ChannelID, 24, download.Opts{
		Cookies: cookies, CookiesFromBrowser: browser,
	})
	if ctx.Err() != nil || !m.db.IsChannelFollowed(channel.ChannelID) {
		return
	}
	if m.ReportExternalResult(err) {
		return
	}
	if err != nil {
		m.recordDownloadPlatformBackoff("youtube", download.ClassifyFailure(err, nil, 0), err)
		log.Printf("[youtube-broadcasts] channel refresh failed: %v", err)
		return
	}
	if err := m.db.ReplaceYouTubeBroadcasts(channel.ChannelID, broadcasts, time.Now().UnixMilli()); err != nil {
		log.Printf("[youtube-broadcasts] store channel snapshot: %v", err)
		return
	}
	m.KickProfileJobs()
}
