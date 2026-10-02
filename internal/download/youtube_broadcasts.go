package download

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	ytdlp "github.com/lrstanley/go-ytdlp"

	"github.com/screwys/igloo/internal/model"
)

// FetchYouTubeBroadcasts reads the channel's streams tab without fetching media.
func (y *YtDlpWrapper) FetchYouTubeBroadcasts(ctx context.Context, channelURL, channelID string, limit int, opts Opts) ([]model.YouTubeBroadcast, error) {
	target, err := youtubeBroadcastURL(channelURL)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 24
	}
	start := time.Now()
	command := applyCookieAuth(ytdlp.New().
		FlatPlaylist().
		SkipDownload().
		NoWarnings().
		PlaylistItems(fmt.Sprintf(":%d", limit)).
		DumpJSON(), opts)
	result, err := runYtDlpCommand(ctx, command, target)
	if err != nil {
		if ctx.Err() == nil && result != nil && result.ExitCode == 1 && result.Stdout == "" &&
			strings.Contains(result.Stderr, "ERROR: [youtube:tab]") &&
			strings.HasSuffix(result.Stderr, ": This channel does not have a streams tab") {
			y.recordYtDlpOperationWithCounts(ctx, "youtube.broadcasts", target, start, nil, opts, 0, 0, 0)
			return []model.YouTubeBroadcast{}, nil
		}
		y.recordYtDlpOperationWithCounts(ctx, "youtube.broadcasts", target, start, err, opts, 0, 0, 0)
		return nil, fmt.Errorf("yt-dlp youtube broadcasts: %w", err)
	}
	infos, err := result.GetExtractedInfo()
	if err != nil {
		y.recordYtDlpOperationWithCounts(ctx, "youtube.broadcasts", target, start, err, opts, 0, 0, 0)
		return nil, fmt.Errorf("parse youtube broadcasts: %w", err)
	}
	observedAtMs := time.Now().UnixMilli()
	broadcasts := make([]model.YouTubeBroadcast, 0, len(infos))
	for rank, info := range infos {
		if info == nil || strings.TrimSpace(info.ID) == "" {
			continue
		}
		broadcast := model.YouTubeBroadcast{
			VideoID: info.ID, ChannelID: channelID, ObservedAtMs: observedAtMs, SourceRank: rank,
		}
		if info.Title != nil {
			broadcast.Title = *info.Title
		}
		if info.LiveStatus != nil {
			broadcast.LiveStatus = string(*info.LiveStatus)
		}
		if info.Timestamp != nil {
			broadcast.PublishedAtMs = int64(*info.Timestamp * 1000)
		}
		if info.ReleaseTimestamp != nil {
			broadcast.StartsAtMs = int64(*info.ReleaseTimestamp * 1000)
		}
		if info.ConcurrentViewCount != nil {
			count := int64(*info.ConcurrentViewCount)
			broadcast.ConcurrentViewCount = &count
		}
		if info.Thumbnail != nil {
			broadcast.ThumbnailURL = *info.Thumbnail
		}
		if broadcast.ThumbnailURL == "" {
			for _, thumbnail := range info.Thumbnails {
				if thumbnail != nil && thumbnail.URL != "" {
					broadcast.ThumbnailURL = thumbnail.URL
				}
			}
		}
		broadcasts = append(broadcasts, broadcast)
	}
	y.recordYtDlpOperationWithCounts(ctx, "youtube.broadcasts", target, start, nil, opts, len(broadcasts), 0, 0)
	return broadcasts, nil
}

func youtubeBroadcastURL(channelURL string) (string, error) {
	target, err := url.Parse(strings.TrimSpace(channelURL))
	if err != nil {
		return "", fmt.Errorf("youtube channel URL: %w", err)
	}
	if !isYouTubeURL(channelURL) {
		return "", fmt.Errorf("youtube channel URL is required")
	}
	path := strings.TrimRight(target.Path, "/")
	for _, tab := range []string{"/videos", "/streams", "/shorts", "/featured", "/live", "/playlists"} {
		path = strings.TrimSuffix(path, tab)
	}
	target.Path = path + "/streams"
	target.RawQuery, target.Fragment = "", ""
	return target.String(), nil
}
