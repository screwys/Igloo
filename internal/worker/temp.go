package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/download"
	"github.com/screwys/igloo/internal/model"
	"github.com/screwys/igloo/internal/subscribe"
)

const tempDownloadLeaseDuration = 3 * time.Hour

// TempDownloadResult holds the outcome of a temp download.
type TempDownloadResult struct {
	Success    bool
	Message    string
	VideoID    string
	PlaylistID string
	Cause      error
}

// EnqueueTempDownload persists a user request before network work begins.
func (m *Manager) EnqueueTempDownload(rawURL string) (bool, error) {
	rawURL = strings.TrimSpace(rawURL)
	platform := subscribe.DetectPlatform(rawURL, "")
	if err := subscribe.ValidateInput(rawURL, platform); err != nil {
		return false, err
	}
	if m.cfg != nil && !m.cfg.PlatformEnabled(platform) {
		return false, fmt.Errorf("%s is not enabled", platform)
	}
	queued, err := m.db.EnqueueTempDownload(rawURL, platform)
	if err != nil {
		return false, err
	}
	select {
	case m.tempDownloadKick <- struct{}{}:
	default:
	}
	return queued, nil
}

func (m *Manager) runTempDownloadLoop(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.tempDownloadKick:
		case <-timer.C:
		}
		if delay := m.externalRetryDelay(time.Now()); delay > 0 {
			resetMediaTimer(timer, delay)
			continue
		}

		work, claimed, err := m.db.ClaimTempDownloadWork(downloadPoolLeaseOwner(), time.Now().UnixMilli(), tempDownloadLeaseDuration)
		if err != nil {
			log.Printf("[temp-download] claim: %v", err)
			resetMediaTimer(timer, time.Minute)
			continue
		}
		if !claimed {
			resetMediaTimer(timer, 5*time.Second)
			continue
		}

		lane := tempDownloadLane(work.Origin)
		result := m.downloadTemp(ctx, work.URL, false, lane, tempDownloadSeedsRecommendations(work.Origin), work.Origin)
		if result.Success {
			if origin, originErr := m.db.TempDownloadOrigin(work.URL); originErr != nil {
				log.Printf("[temp-download] read origin %s: %v", work.URL, originErr)
			} else if origin == "discover" && result.VideoID != "" {
				if err := m.db.MarkDiscoverTempVideo(result.VideoID, time.Now().UnixMilli()); err != nil {
					log.Printf("[temp-download] mark discover video %s: %v", result.VideoID, err)
				}
			}
			if err := m.db.CompleteTempDownloadWork(work.URL, work.LeaseOwner); err != nil {
				log.Printf("[temp-download] complete %s: %v", work.URL, err)
			}
		} else {
			classification := classifyTempDownloadFailure(result, work.RetryCount+1)
			if classification.Permanent {
				if err := m.db.BlockTempDownloadWork(work.URL, work.LeaseOwner, classification.Kind, result.Message); err != nil {
					log.Printf("[temp-download] block %s: %v", work.URL, err)
				}
			} else if err := m.db.RetryTempDownloadWork(work.URL, work.LeaseOwner, classification.Kind, result.Message, classification.RetryDelay); err != nil {
				log.Printf("[temp-download] retry %s: %v", work.URL, err)
			}
		}
		resetMediaTimer(timer, 0)
	}
}

func tempDownloadLane(origin string) download.MediaLane {
	if origin == "discover" {
		return download.MediaLaneBulkBackground
	}
	return download.MediaLaneBulkInteractive
}

func tempDownloadSeedsRecommendations(origin string) bool {
	return origin != "discover"
}

func classifyTempDownloadFailure(result TempDownloadResult, attempt int) download.FailureClassification {
	cause := result.Cause
	if cause == nil {
		cause = errors.New(result.Message)
	}
	return download.ClassifyFailure(cause, nil, attempt)
}

// DownloadTemp handles an ad-hoc URL download.
func (m *Manager) DownloadTemp(ctx context.Context, rawURL string, saveChannel bool) TempDownloadResult {
	return m.downloadTemp(ctx, rawURL, saveChannel, download.MediaLaneBulkInteractive, true, "interactive")
}

func (m *Manager) downloadTemp(ctx context.Context, rawURL string, saveChannel bool, lane download.MediaLane, seedRecommendations bool, origin string) TempDownloadResult {
	platform := subscribe.DetectPlatform(rawURL, "")
	if err := subscribe.ValidateInput(rawURL, platform); err != nil {
		return TempDownloadResult{Message: "Unsupported download URL"}
	}
	if m.cfg != nil && !m.cfg.PlatformEnabled(platform) {
		return TempDownloadResult{Message: fmt.Sprintf("%s is not enabled on this Igloo server", platform)}
	}

	cookiesFile, cookiesBrowser := m.cookiesFor(platform)
	cookieSets := m.cookieSetsFor(platform)
	authOpts := download.Opts{
		Cookies:            cookiesFile,
		CookiesFromBrowser: cookiesBrowser,
		CookieAlternates:   cookieSets,
	}

	// Check for YouTube playlist.
	if platform == "youtube" {
		if playlistID := extractPlaylistID(rawURL); playlistID != "" {
			return m.downloadPlaylist(ctx, rawURL, playlistID, authOpts)
		}
	}

	var info map[string]any
	if origin == "discover" && platform == "youtube" {
		parsed, err := url.Parse(rawURL)
		if err == nil {
			candidate, candidateErr := m.db.GetPreparedDiscoverVideo(parsed.Query().Get("v"))
			if candidateErr != nil {
				return TempDownloadResult{Message: fmt.Sprintf("Read prepared video: %v", candidateErr), Cause: candidateErr}
			}
			if candidate != nil {
				info = preparedDiscoverMetadata(*candidate)
			}
		}
	}
	if info == nil {
		var err error
		info, err = m.downloader.YtDlp.FetchInfo(ctx, rawURL, authOpts)
		if err != nil {
			return TempDownloadResult{Message: fmt.Sprintf("Could not fetch info: %v", err), Cause: err}
		}
	}

	videoID, _ := info["id"].(string)
	if videoID == "" {
		return TempDownloadResult{Message: "No video ID in metadata"}
	}
	title, _ := info["title"].(string)
	if title == "" {
		title = videoID
	}

	channelID, _ := info["channel_id"].(string)
	if channelID == "" {
		if v, ok := info["uploader_id"].(string); ok {
			channelID = v
		}
	}
	channelName, _ := info["channel"].(string)
	if channelName == "" {
		if v, ok := info["uploader"].(string); ok {
			channelName = v
		}
	}
	if channelID == "" {
		channelID = "temp"
	}
	if channelName == "" {
		channelName = "Temp"
	}
	ownerKind, ok := db.VideoOwnerKindForPlatform(platform)
	if !ok || ownerKind == "tweet" {
		return TempDownloadResult{Message: fmt.Sprintf("Unsupported platform: %s", platform)}
	}

	// Normalize channel_id to the platform_id convention used by every other
	// channel in the DB, so avatar resolution and channel_name stripping helpers
	// work consistently.
	channelURL := firstNonEmptyString(
		stringFromMap(info, "channel_url"),
		stringFromMap(info, "uploader_url"),
	)
	if platform == "youtube" {
		channelID = download.CanonicalizeYouTubeChannelID(channelID, channelURL, rawURL)
		if channelURL == "" && strings.HasPrefix(channelID, "youtube_UC") {
			channelURL = "https://www.youtube.com/channel/" + strings.TrimPrefix(channelID, "youtube_")
		}
	} else {
		channelID = normalizeChannelID(platform, channelID)
	}

	// Download to temp dir.
	tempDir, err := m.cfg.Storage.WritePath("media/temp")
	if err != nil {
		return TempDownloadResult{Message: fmt.Sprintf("Storage path: %v", err), Cause: err}
	}
	if err := m.downloader.RunMedia(ctx, lane, func() error { return os.MkdirAll(tempDir, 0o755) }); err != nil {
		return TempDownloadResult{Message: fmt.Sprintf("Create storage directory: %v", err), Cause: err}
	}
	outputID, err := downloadOutputID(videoID)
	if err != nil {
		return TempDownloadResult{Message: fmt.Sprintf("Download output: %v", err), Cause: err}
	}
	subtitleDir, err := m.cfg.Storage.WritePath("subtitles/" + platform)
	if err != nil {
		return TempDownloadResult{Message: fmt.Sprintf("Subtitle storage: %v", err), Cause: err}
	}

	opts := download.Opts{
		OutputDir:          tempDir,
		ID:                 outputID,
		Cookies:            cookiesFile,
		CookiesFromBrowser: cookiesBrowser,
		CookieAlternates:   cookieSets,
		Subtitles:          true,
		SubtitleDir:        subtitleDir,
	}

	completed, dlErr := m.downloader.DownloadCompleted(ctx, lane, rawURL, "video", opts)
	if dlErr != nil || len(completed.MediaPaths) == 0 {
		cause := dlErr
		if cause == nil {
			cause = errors.New("download returned no media files")
		}
		if origin == "discover" {
			m.ReportExternalResult(cause)
		}
		m.removeFailedAttempt(ctx, lane, completedVideoFiles{}, completed)
		msg := "Download failed"
		if dlErr != nil {
			msg = dlErr.Error()
		}
		return TempDownloadResult{Message: msg, Cause: cause}
	}
	if origin == "discover" {
		m.ReportExternalResult(nil)
	}
	for key, value := range completed.Metadata {
		info[key] = value
	}

	files, err := m.prepareCompletedVideoFiles(ctx, lane, completed)
	if err != nil {
		m.removeFailedAttempt(ctx, lane, files, completed)
		return TempDownloadResult{Message: fmt.Sprintf("Prepare completed outputs: %v", err), Cause: err}
	}

	publishedAt := extractPublishedAt(info)
	description, _ := info["description"].(string)
	duration := extractDurationFromMetadata(info)
	if len(files.imageKeys) > 1 {
		slides := make([]any, len(files.imageKeys))
		for i, key := range files.imageKeys {
			slides[i] = map[string]any{"path": key}
		}
		if info == nil {
			info = map[string]any{}
		}
		info["slides"] = slides
		info["vcodec"] = "none"
	}

	metadataJSON := ""
	var mediaKind string
	var slideCount int
	if info != nil {
		stripped := model.StripVideoMetadata(info)
		if stripped != nil {
			if b, err := json.Marshal(stripped); err == nil {
				metadataJSON = string(b)
			}
		}
	}
	if metadataJSON != "" {
		var meta model.VideoMetadata
		if err := json.Unmarshal([]byte(metadataJSON), &meta); err == nil {
			mediaKind, slideCount = model.ComputeMediaKind(&meta, files.primaryKey)
		}
	}
	if mediaKind == "" {
		mediaKind, slideCount = model.ComputeMediaKind(nil, files.primaryKey)
	}

	// Commit the discovered identity even when search/recommendation observation
	// created the channel first. Following remains an explicit user action.
	sourceID := strings.TrimPrefix(stringFromMap(info, "uploader_id"), "@")
	if platform == "youtube" {
		sourceID = strings.TrimPrefix(channelID, "youtube_")
	}
	if err := m.db.ObserveChannels([]model.Channel{{
		ChannelID: channelID, SourceID: sourceID, Name: channelName, DisplayName: channelName,
		Handle: stringFromMap(info, "uploader_id"), URL: channelURL, Platform: platform,
	}}); err != nil {
		return TempDownloadResult{Message: fmt.Sprintf("Store channel identity: %v", err), Cause: err}
	}
	if saveChannel {
		if err := m.db.FollowChannel(channelID); err != nil {
			return TempDownloadResult{Message: fmt.Sprintf("Follow channel: %v", err), Cause: err}
		}
	}

	if err := m.db.StoreCompletedVideo(db.CompletedVideo{
		VideoID: videoID, ChannelID: channelID, OwnerKind: ownerKind, Title: title, Description: description,
		Duration: duration, PublishedAtMs: publishedAt, MetadataJSON: metadataJSON,
		MediaKind: mediaKind, SlideCount: slideCount, IsTemp: true,
		Assets: files.assets,
	}); err != nil {
		m.removeFailedAttempt(ctx, lane, files, completed)
		return TempDownloadResult{Message: fmt.Sprintf("DB insert: %v", err), Cause: err}
	}
	if err := m.publishCompletedVideoThumbnail(ctx, lane, videoID, platform, outputID, files); err != nil {
		log.Printf("[temp] thumbnail publish failed for %s: %v", videoID, err)
	}
	if err := m.storeCompletedSubtitles(ctx, videoID, files, completed); err != nil {
		log.Printf("[temp] subtitle publish failed for %s: %v", videoID, err)
	}
	m.removeTransientFiles(ctx, lane, files)

	if platform == "youtube" {
		m.RequestVideoPreview(videoID)
	}

	// Channel creation owns the durable profile job. Wake its consumer without
	// creating a synchronous render-time identity path.
	m.KickProfileJobs()

	if platform == "youtube" {
		if err := m.QueueVideoMetadataRefresh(videoID); err != nil {
			log.Printf("[video-metadata] queue temp video %s: %v", videoID, err)
		}
		if seedRecommendations {
			if err := m.QueueYouTubeRecommendations(videoID); err != nil {
				log.Printf("[youtube-recommendations] queue temp video %s: %v", videoID, err)
			}
		}
	} else {
		// TikTok does not use the YouTube metadata owner.
		commentsCtx, commentsCancel := context.WithTimeout(ctx, 2*time.Minute)
		comments, commentsErr := m.downloader.YtDlp.FetchComments(commentsCtx, rawURL, download.DefaultCommentFetchLimit, opts)
		commentsCancel()
		if commentsErr != nil {
			log.Printf("[temp] comments fetch failed for %s: %v", videoID, commentsErr)
		} else if len(comments) > 0 {
			inserted, err := m.db.AddComments(videoID, comments)
			if err != nil {
				log.Printf("[temp] store comments for %s: %v", videoID, err)
			} else {
				m.KickMediaWork()
				log.Printf("[temp] fetched %d comments for %s", inserted, videoID)
			}
		}
	}

	return TempDownloadResult{
		Success: true,
		Message: fmt.Sprintf("Downloaded: %s", title),
		VideoID: videoID,
	}
}

func preparedDiscoverMetadata(candidate model.DiscoveryVideo) map[string]any {
	return map[string]any{
		"id": candidate.VideoID, "title": candidate.Title, "duration": float64(candidate.Duration),
		"channel_id": strings.TrimPrefix(candidate.ChannelID, "youtube_"),
		"channel":    candidate.ChannelName, "uploader": candidate.ChannelName,
		"uploader_id": candidate.ChannelHandle, "channel_url": candidate.ChannelURL,
	}
}

func (m *Manager) downloadPlaylist(ctx context.Context, rawURL, playlistID string, authOpts download.Opts) TempDownloadResult {
	info, err := m.downloader.YtDlp.FetchPlaylistInfo(ctx, rawURL, authOpts)
	if err != nil {
		return TempDownloadResult{Message: fmt.Sprintf("Could not inspect playlist: %v", err)}
	}

	entries, _ := info["entries"].([]any)
	if len(entries) == 0 {
		return TempDownloadResult{Message: "Playlist has no entries"}
	}

	playlistTitle, _ := info["title"].(string)
	if playlistTitle == "" {
		playlistTitle = "Playlist " + playlistID
	}

	targetDir, err := m.cfg.Storage.WritePath("media/playlists/" + safeFolderName(playlistTitle))
	if err != nil {
		return TempDownloadResult{Message: fmt.Sprintf("Storage path: %v", err)}
	}
	if err := m.downloader.RunMedia(ctx, download.MediaLaneBulkInteractive, func() error { return os.MkdirAll(targetDir, 0o755) }); err != nil {
		return TempDownloadResult{Message: fmt.Sprintf("Create storage directory: %v", err)}
	}

	playlistChannelID := "playlist_" + playlistID

	downloaded := 0
	failed := 0
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		videoID, _ := entryMap["id"].(string)
		if videoID == "" {
			continue
		}
		entryTitle, _ := entryMap["title"].(string)
		if entryTitle == "" {
			entryTitle = videoID
		}

		if ok, _ := m.db.IsVideoDownloaded(videoID); ok {
			downloaded++
			continue
		}

		videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
		outputID, attemptErr := downloadOutputID(videoID)
		if attemptErr != nil {
			log.Printf("[temp] playlist item %s output preparation failed: %v", videoID, attemptErr)
			failed++
			continue
		}
		opts := download.Opts{
			OutputDir:          targetDir,
			ID:                 outputID,
			Cookies:            authOpts.Cookies,
			CookiesFromBrowser: authOpts.CookiesFromBrowser,
		}
		completed, dlErr := m.downloader.DownloadCompleted(ctx, download.MediaLaneBulkInteractive, videoURL, "video", opts)
		if dlErr != nil || len(completed.MediaPaths) == 0 {
			m.removeFailedAttempt(ctx, download.MediaLaneBulkInteractive, completedVideoFiles{}, completed)
			log.Printf("[temp] playlist item %s failed: %v", videoID, dlErr)
			failed++
			continue
		}

		files, prepareErr := m.prepareCompletedVideoFiles(ctx, download.MediaLaneBulkInteractive, completed)
		if prepareErr != nil {
			m.removeFailedAttempt(ctx, download.MediaLaneBulkInteractive, files, completed)
			log.Printf("[temp] playlist item %s output preparation failed: %v", videoID, prepareErr)
			failed++
			continue
		}
		metadata := completed.Metadata
		publishedAt := extractPublishedAt(metadata)
		description, _ := metadata["description"].(string)
		duration := extractDurationFromMetadata(metadata)
		metaJSON := ""
		if b, err := json.Marshal(metadata); err == nil {
			metaJSON = string(b)
		}
		if err := m.db.StoreCompletedVideo(db.CompletedVideo{
			VideoID: videoID, ChannelID: playlistChannelID, OwnerKind: "youtube_video", Title: entryTitle, Description: description,
			Duration: duration, PublishedAtMs: publishedAt, MetadataJSON: metaJSON,
			SourceKind: "playlist", Assets: files.assets,
		}); err != nil {
			m.removeFailedAttempt(ctx, download.MediaLaneBulkInteractive, files, completed)
			log.Printf("[temp] playlist item %s DB insert failed: %v", videoID, err)
			failed++
			continue
		}
		if err := m.publishCompletedVideoThumbnail(ctx, download.MediaLaneBulkInteractive, videoID, "youtube", outputID, files); err != nil {
			log.Printf("[temp] playlist item %s thumbnail publish failed: %v", videoID, err)
		}
		m.removeTransientFiles(ctx, download.MediaLaneBulkInteractive, files)
		m.RequestVideoPreview(videoID)
		downloaded++
	}

	if downloaded == 0 {
		return TempDownloadResult{Message: "Playlist download failed for all videos"}
	}

	msg := fmt.Sprintf("Playlist ready: %s (%d/%d)", playlistTitle, downloaded, len(entries))
	if failed > 0 {
		msg += fmt.Sprintf(", %d failed", failed)
	}
	return TempDownloadResult{
		Success:    true,
		Message:    msg,
		PlaylistID: playlistID,
	}
}

// normalizeChannelID returns the channel_id in the platform_id convention used
// throughout the DB (e.g. "youtube_UCxxx", "twitter_handle"). yt-dlp returns
// raw IDs without the platform prefix, so we add it here to keep lookups,
// avatar resolution, and display helpers consistent.
func normalizeChannelID(platform, raw string) string {
	if raw == "" || raw == "temp" {
		return raw
	}
	prefix := platform + "_"
	if strings.HasPrefix(raw, prefix) {
		return raw
	}
	switch platform {
	case "twitter", "tiktok":
		return prefix + strings.ToLower(strings.TrimPrefix(raw, "@"))
	default:
		return prefix + raw
	}
}

func extractPlaylistID(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("list")
}

func safeFolderName(raw string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "?", "_", "*", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	name := replacer.Replace(strings.TrimSpace(raw))
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || filepath.Clean(name) != name {
		return "playlist"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

func stringFromMap(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
