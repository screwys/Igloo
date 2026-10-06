package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/download"
	"github.com/screwys/igloo/internal/model"
)

func (m *Manager) ResolveYouTubePlayback(ctx context.Context, videoID string) (*download.PlaybackInfo, error) {
	if m == nil || m.downloader == nil {
		return nil, fmt.Errorf("YouTube playback worker unavailable")
	}
	if !m.cfg.PlatformEnabled("youtube") {
		return nil, fmt.Errorf("YouTube is not enabled")
	}
	rawURL := "https://www.youtube.com/watch?v=" + videoID
	file, browser := m.cookiesFor("youtube")
	info, err := m.downloader.YtDlp.FetchPlayback(ctx, rawURL, download.Opts{
		Cookies: file, CookiesFromBrowser: browser, CookieAlternates: m.cookieSetsFor("youtube"),
	})
	if err != nil {
		return nil, download.WithOperationContext(err, "yt-dlp", "")
	}
	channelID := download.CanonicalizeYouTubeChannelID(info.ChannelID, info.ChannelURL, rawURL)
	if channelID == "" {
		channelID = download.CanonicalizeYouTubeChannelID(info.UploaderID, info.ChannelURL, rawURL)
	}
	if err := m.db.ObserveChannels([]model.Channel{{
		ChannelID: channelID, SourceID: strings.TrimPrefix(channelID, "youtube_"),
		Name: info.Channel, DisplayName: info.Channel, Handle: info.UploaderID,
		URL: info.ChannelURL, Platform: "youtube",
	}}); err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(model.StripVideoMetadata(info.Metadata))
	if err != nil {
		return nil, err
	}
	if err := m.db.ObserveStreamVideo(db.CompletedVideo{
		VideoID: info.ID, ChannelID: channelID, OwnerKind: "youtube_video", Title: info.Title,
		Description: info.Description, Duration: int(info.Duration),
		PublishedAtMs: extractPublishedAt(info.Metadata), MetadataJSON: string(metadata),
	}); err != nil {
		return nil, err
	}
	if info.Thumbnail != "" {
		if err := m.db.DeclareAsset(db.Asset{
			AssetID:   db.BuildAssetID("youtube", "youtube_video", info.ID, "post_thumbnail", 0),
			AssetKind: "post_thumbnail", OwnerKind: "youtube_video", OwnerID: info.ID,
			SourceURL: info.Thumbnail, RequiredReason: "retention",
		}, 0); err != nil {
			return nil, err
		}
		m.KickMediaWork()
	}
	if err := m.QueueVideoMetadataRefresh(info.ID); err != nil {
		return nil, err
	}
	if err := m.QueueYouTubeRecommendations(info.ID); err != nil {
		return nil, err
	}
	return info, nil
}
