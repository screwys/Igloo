package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/download"
	"github.com/screwys/igloo/internal/model"
	"github.com/screwys/igloo/internal/settings"
)

const (
	youtubeRecommendationLease       = 4 * time.Minute
	youtubeRecommendationTimeout     = 2 * time.Minute
	youtubeRecommendationMaxAttempts = 6
)

func (m *Manager) QueueYouTubeRecommendations(videoID string) error {
	if m == nil || m.db == nil {
		return errors.New("youtube recommendation worker unavailable")
	}
	if err := m.db.QueueYouTubeRecommendations(videoID, time.Now().UnixMilli()); err != nil {
		return err
	}
	m.kickYouTubeRecommendations()
	return nil
}

func (m *Manager) QueueFollowedYouTubeChannelRecommendations() (int, error) {
	if m == nil || m.db == nil {
		return 0, errors.New("youtube recommendation worker unavailable")
	}
	queued, err := m.db.QueueFollowedYouTubeChannelRecommendations(time.Now().UnixMilli())
	if err != nil {
		return 0, err
	}
	m.kickYouTubeRecommendations()
	return queued, nil
}

func (m *Manager) kickYouTubeRecommendations() {
	if m == nil || m.youtubeRecommendationKick == nil {
		return
	}
	select {
	case m.youtubeRecommendationKick <- struct{}{}:
	default:
	}
}

func (m *Manager) runYouTubeRecommendationLoop(ctx context.Context) {
	log.Printf("[youtube-recommendations] durable worker started")
	if stored, err := m.db.BootstrapPreparedDiscoverGeneration(time.Now().UnixMilli(), 80); err != nil {
		log.Printf("[youtube-recommendations] bootstrap prepared Discover page: %v", err)
	} else if stored {
		log.Printf("[youtube-recommendations] preserved existing Discover cache as prepared page")
	}
	m.reconcileDiscoverPrefetch()
	for {
		if m.maintainDiscoverGeneration() {
			continue
		}
		if delay := m.externalRetryDelay(time.Now()); delay > 0 {
			if !waitForVideoMetadata(ctx, m.youtubeRecommendationKick, delay) {
				return
			}
			continue
		}
		worked := m.processYouTubeRecommendationJob(ctx, m.youtubeRecommendations)
		if ctx.Err() != nil {
			return
		}
		if worked {
			continue
		}
		delay, err := m.db.NextYouTubeRecommendationDelay(time.Now().UnixMilli())
		if err != nil {
			log.Printf("[youtube-recommendations] next due: %v", err)
			delay = time.Minute
		}
		if !waitForVideoMetadata(ctx, m.youtubeRecommendationKick, delay) {
			return
		}
	}
}

func (m *Manager) maintainDiscoverGeneration() bool {
	nowMs := time.Now().UnixMilli()
	started, anchors, err := m.db.BeginDiscoverRefresh(nowMs)
	if err != nil {
		log.Printf("[youtube-recommendations] begin Discover refresh: %v", err)
		return false
	}
	if started && anchors > 0 {
		log.Printf("[youtube-recommendations] preparing Discover generation: anchors=%d", anchors)
		return true
	}
	published, retired, err := m.db.PublishDiscoverGeneration(nowMs, 80)
	if err != nil {
		log.Printf("[youtube-recommendations] publish Discover generation: %v", err)
		return false
	}
	if !published {
		return false
	}
	if err := m.db.ResetDiscoverTempDownloadQueue(); err != nil {
		log.Printf("[youtube-recommendations] reset Discover download attempts: %v", err)
	}
	if err := m.db.RetireDiscoverDownloads(retired); err != nil {
		log.Printf("[youtube-recommendations] retire previous Discover downloads: %v", err)
	} else if _, err := m.db.MaintainVideoRetention(nowMs); err != nil {
		log.Printf("[youtube-recommendations] collect previous Discover downloads: %v", err)
	}
	m.reconcileDiscoverPrefetch()
	log.Printf("[youtube-recommendations] published prepared Discover generation: retired=%d", len(retired))
	return true
}

func (m *Manager) processYouTubeRecommendationJob(ctx context.Context, fetcher youtubeRecommendationFetcher) bool {
	if m == nil || m.db == nil || fetcher == nil || ctx.Err() != nil {
		return false
	}
	job, ok, err := m.db.ClaimYouTubeRecommendationJob(db.LeaseOptions{
		Owner: youtubeRecommendationLeaseOwner(), LeaseMs: youtubeRecommendationLease.Milliseconds(), Limit: 1,
	})
	if err != nil {
		log.Printf("[youtube-recommendations] claim: %v", err)
		return false
	}
	if !ok {
		return false
	}
	if !m.externalWorkAllowed(time.Now()) {
		_ = m.db.ReleaseYouTubeRecommendationJob(job, time.Now().UnixMilli())
		return true
	}
	workCtx, cancel := context.WithTimeout(ctx, youtubeRecommendationTimeout)
	defer cancel()
	cookies, browser := m.cookiesFor("youtube")
	opts := download.Opts{Cookies: cookies, CookiesFromBrowser: browser}
	mix, mixErr := fetcher.FetchYouTubeMix(workCtx, job.AnchorVideoID, 24, opts)
	if len(mix) == 0 && strings.TrimSpace(job.AnchorTitle) != "" {
		mix, mixErr = fetcher.SearchYouTube(workCtx, job.AnchorTitle, 20, opts)
	}
	var channelRefs []download.VideoRef
	var channelErr error
	if strings.TrimSpace(job.ChannelURL) != "" {
		snapshot, err := fetcher.ChannelCheck(workCtx, job.ChannelURL, 6, false)
		channelErr = err
		channelRefs = snapshot.FlattenRefs(6)
	}
	if mixErr != nil && channelErr != nil {
		if m.ReportExternalResult(mixErr) {
			_ = m.db.ReleaseYouTubeRecommendationJob(job, time.Now().UnixMilli())
			return true
		}
		m.retryYouTubeRecommendationJob(job, mixErr)
		return true
	}
	m.ReportExternalResult(nil)
	candidates := mergeYouTubeRecommendations(job, channelRefs, mix, 24)
	channels := make([]model.Channel, 0, len(candidates))
	for _, candidate := range candidates {
		channels = append(channels, model.Channel{
			ChannelID: candidate.ChannelID, SourceID: strings.TrimPrefix(candidate.ChannelID, "youtube_"),
			Name: candidate.ChannelName, DisplayName: candidate.ChannelName, Handle: candidate.ChannelHandle,
			URL: candidate.ChannelURL, Platform: "youtube",
		})
	}
	if err := m.db.ObserveChannels(channels); err != nil {
		m.retryYouTubeRecommendationJob(job, err)
		return true
	}
	if err := m.db.CompleteYouTubeRecommendationJob(job, candidates, time.Now().UnixMilli()); err != nil {
		m.retryYouTubeRecommendationJob(job, err)
		return true
	}
	m.KickProfileJobs()
	log.Printf("[youtube-recommendations] refreshed %s: candidates=%d", job.AnchorVideoID, len(candidates))
	return true
}

func mergeYouTubeRecommendations(job db.YouTubeRecommendationJob, channelRefs, mix []download.VideoRef, limit int) []model.DiscoveryVideo {
	if limit <= 0 {
		limit = 24
	}
	seen := map[string]struct{}{job.AnchorVideoID: {}}
	out := make([]model.DiscoveryVideo, 0, limit)
	appendRef := func(ref download.VideoRef, source string, channelID, channelName, channelHandle, channelURL string) {
		if len(out) >= limit || strings.TrimSpace(ref.VideoID) == "" {
			return
		}
		if _, duplicate := seen[ref.VideoID]; duplicate {
			return
		}
		if ref.ChannelID != "" {
			channelID = ref.ChannelID
		}
		if ref.AuthorDisplayName != "" {
			channelName = ref.AuthorDisplayName
		}
		if ref.AuthorHandle != "" {
			channelHandle = ref.AuthorHandle
		}
		if strings.TrimSpace(channelID) == "" {
			return
		}
		if channelURL == "" && strings.HasPrefix(channelID, "youtube_UC") {
			channelURL = "https://www.youtube.com/channel/" + strings.TrimPrefix(channelID, "youtube_")
		}
		avatarURL := ref.AuthorAvatarURL
		if avatarURL == "" {
			avatarURL = "/api/media/avatar/" + channelID
		}
		seen[ref.VideoID] = struct{}{}
		out = append(out, model.DiscoveryVideo{
			VideoID: ref.VideoID, Title: ref.Title, Duration: ref.Duration,
			ChannelID: channelID, ChannelName: channelName, ChannelHandle: channelHandle, ChannelURL: channelURL,
			AvatarURL: avatarURL, ThumbnailURL: "https://i.ytimg.com/vi/" + ref.VideoID + "/mqdefault.jpg",
			Source: source, Rank: len(out),
		})
	}
	for _, ref := range channelRefs {
		appendRef(ref, "channel", job.ChannelID, job.ChannelName, job.ChannelHandle, job.ChannelURL)
		if len(out) >= 4 {
			break
		}
	}
	for _, ref := range mix {
		appendRef(ref, "related", "", "", "", "")
	}
	return out
}

func (m *Manager) reconcileDiscoverPrefetch() {
	if m == nil || m.db == nil {
		return
	}
	target := settings.ClampDiscoverPrefetchCount(m.db.IntSetting("discover_prefetch_count"))
	if target <= 0 {
		return
	}
	candidates, err := m.db.ListPreparedDiscoverVideos(100)
	if err != nil {
		log.Printf("[youtube-recommendations] list prefetch candidates: %v", err)
		return
	}
	added, err := m.db.EnqueueDiscoverTempDownloads(discoverPrefetchCandidates(candidates), target)
	if err != nil {
		log.Printf("[youtube-recommendations] enqueue prefetch: %v", err)
		return
	}
	if added > 0 {
		select {
		case m.tempDownloadKick <- struct{}{}:
		default:
		}
	}
}

func discoverPrefetchCandidates(candidates []model.DiscoveryVideo) []model.DiscoveryVideo {
	out := make([]model.DiscoveryVideo, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Source == "related" {
			out = append(out, candidate)
		}
	}
	return out
}

func (m *Manager) RefreshDiscoverPrefetch() {
	m.reconcileDiscoverPrefetch()
}

func (m *Manager) RescheduleDiscoverRefresh(force bool) {
	if err := m.db.RescheduleDiscoverRefresh(force); err != nil {
		log.Printf("[youtube-recommendations] reschedule Discover refresh: %v", err)
		return
	}
	m.kickYouTubeRecommendations()
}

func (m *Manager) retryYouTubeRecommendationJob(job db.YouTubeRecommendationJob, cause error) {
	nowMs := time.Now().UnixMilli()
	if errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
		_ = m.db.ReleaseYouTubeRecommendationJob(job, nowMs)
		return
	}
	message := download.RedactText(cause.Error())
	if job.Attempts+1 >= youtubeRecommendationMaxAttempts {
		_ = m.db.BlockYouTubeRecommendationJob(job, message, nowMs)
		return
	}
	delay := videoMetadataRetryDelay(job.Attempts + 1)
	if err := m.db.RetryYouTubeRecommendationJob(job, message, delay, nowMs); err != nil {
		log.Printf("[youtube-recommendations] retry %s: %v", job.AnchorVideoID, err)
	}
}

func youtubeRecommendationLeaseOwner() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("youtube-recommendations:%s:%d", host, os.Getpid())
}
