package download

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PlaybackInfo retains the transport fields that typed download results omit.
type PlaybackInfo struct {
	ID                  string                        `json:"id"`
	Title               string                        `json:"title"`
	FullTitle           string                        `json:"fulltitle"`
	Description         string                        `json:"description"`
	Duration            float64                       `json:"duration"`
	ChannelID           string                        `json:"channel_id"`
	Channel             string                        `json:"channel"`
	ChannelURL          string                        `json:"channel_url"`
	UploaderID          string                        `json:"uploader_id"`
	Uploader            string                        `json:"uploader"`
	UploaderURL         string                        `json:"uploader_url"`
	LiveStatus          string                        `json:"live_status"`
	ConcurrentViewCount int64                         `json:"concurrent_view_count"`
	Thumbnail           string                        `json:"thumbnail"`
	Timestamp           float64                       `json:"timestamp"`
	UploadDate          string                        `json:"upload_date"`
	IsLive              bool                          `json:"is_live"`
	Formats             []PlaybackFormat              `json:"formats"`
	Subtitles           map[string][]PlaybackSubtitle `json:"subtitles"`
	AutomaticCaptions   map[string][]PlaybackSubtitle `json:"automatic_captions"`
	Headers             map[string]string             `json:"http_headers"`
	Metadata            map[string]any                `json:"-"`
}

type PlaybackFormat struct {
	ID                 string            `json:"format_id"`
	URL                string            `json:"url"`
	ManifestURL        string            `json:"manifest_url"`
	Protocol           string            `json:"protocol"`
	Ext                string            `json:"ext"`
	VideoCodec         string            `json:"vcodec"`
	AudioCodec         string            `json:"acodec"`
	Width              int               `json:"width"`
	Height             int               `json:"height"`
	FPS                float64           `json:"fps"`
	Bitrate            float64           `json:"tbr"`
	SampleRate         int               `json:"asr"`
	Channels           int               `json:"audio_channels"`
	Language           string            `json:"language"`
	LanguagePreference float64           `json:"language_preference"`
	Headers            map[string]string `json:"http_headers"`
	DownloaderOptions  map[string]any    `json:"downloader_options"`
}

type PlaybackSubtitle struct {
	URL  string `json:"url"`
	Ext  string `json:"ext"`
	Name string `json:"name"`
}

func (y *YtDlpWrapper) FetchPlayback(ctx context.Context, rawURL string, opts Opts) (*PlaybackInfo, error) {
	return y.fetchPlayback(ctx, rawURL, opts, false)
}

// FetchBroadcastInfo also returns upcoming and ended broadcasts without formats.
func (y *YtDlpWrapper) FetchBroadcastInfo(ctx context.Context, rawURL string, opts Opts) (*PlaybackInfo, error) {
	return y.fetchPlayback(ctx, rawURL, opts, true)
}

func (y *YtDlpWrapper) fetchPlayback(ctx context.Context, rawURL string, opts Opts, allowNoFormats bool) (*PlaybackInfo, error) {
	start := time.Now()
	platform := platformFromURL(rawURL)
	var lastErr error
	attempts := opts.cookieAttempts(platform)
	// Public Spaces remain accessible when a configured X session is locked.
	guestFirst := platform == "twitter" && strings.Contains(rawURL, "/i/spaces/") &&
		(attempts[0].File != "" || attempts[0].Browser != "")
	if guestFirst {
		attempts = append([]CookieSet{{}}, attempts...)
	}
	for index, auth := range attempts {
		used := opts.withCookieSet(auth)
		cmd := fetchInfoCommand(used)
		if allowNoFormats {
			cmd = cmd.IgnoreNoFormatsError()
		}
		result := (CommandRunner{}).RunBuilt(ctx, cmd.BuildCommand(ctx, rawURL))
		if result.Err != nil {
			lastErr = fmt.Errorf("yt-dlp stream extraction: %w: %s", result.Err, strings.TrimSpace(string(result.Stderr)))
			retryWithCookies := guestFirst && index == 0 && ctx.Err() == nil
			if !retryWithCookies &&
				!shouldTryNextCookieAttempt(result.Err) && !shouldTryNextCookieAttempt(fmt.Errorf("%s", result.Stderr)) {
				break
			}
			continue
		}
		var info PlaybackInfo
		if err := json.Unmarshal(result.Stdout, &info); err != nil {
			lastErr = fmt.Errorf("parse playback information: %w", err)
			break
		}
		if err := json.Unmarshal(result.Stdout, &info.Metadata); err != nil {
			return nil, err
		}
		if info.FullTitle != "" {
			info.Title = info.FullTitle
			info.Metadata["title"] = info.FullTitle
		}
		if info.ID == "" || !allowNoFormats && len(info.Formats) == 0 {
			lastErr = fmt.Errorf("no playable formats returned")
			break
		}
		y.recordYtDlpOperationWithCounts(ctx, platform+".stream", rawURL, start, nil, used, 1, 0, 0)
		return &info, nil
	}
	y.recordYtDlpOperationWithCounts(ctx, platform+".stream", rawURL, start, lastErr, opts, 0, 0, 0)
	return nil, lastErr
}
