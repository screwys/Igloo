package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/download"
)

const (
	videoMetadataLeaseDuration = 4 * time.Minute
	videoMetadataTimeout       = 3 * time.Minute
	videoMetadataMaxAttempts   = 8
)

func (m *Manager) QueueVideoMetadataRefresh(videoID string) error {
	if m == nil || m.db == nil {
		return errors.New("video metadata worker unavailable")
	}
	if err := m.db.QueueVideoMetadataJob(videoID, time.Now().UnixMilli()); err != nil {
		return err
	}
	select {
	case m.videoMetadataKick <- struct{}{}:
	default:
	}
	return nil
}

// runVideoMetadataLoop is the sole normal owner for changing YouTube metadata.
// Queue state survives shutdown, while the kick channel only shortens wake-up.
func (m *Manager) runVideoMetadataLoop(ctx context.Context) {
	log.Printf("[video-metadata] durable worker started")
	for {
		if delay := m.externalRetryDelay(time.Now()); delay > 0 {
			if !waitForVideoMetadata(ctx, m.videoMetadataKick, delay) {
				return
			}
			continue
		}
		worked := m.processVideoMetadataJob(ctx, m.youtubeMetadata)
		if ctx.Err() != nil {
			return
		}
		if worked {
			continue
		}
		delay, err := m.db.NextVideoMetadataJobDelay(time.Now().UnixMilli())
		if err != nil {
			log.Printf("[video-metadata] next due: %v", err)
			delay = time.Minute
		}
		if !waitForVideoMetadata(ctx, m.videoMetadataKick, delay) {
			return
		}
	}
}

func waitForVideoMetadata(ctx context.Context, kick <-chan struct{}, delay time.Duration) bool {
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-kick:
		return true
	case <-timer.C:
		return true
	}
}

func (m *Manager) processVideoMetadataJob(ctx context.Context, fetcher youtubeMetadataFetcher) bool {
	if m == nil || m.db == nil || fetcher == nil || ctx.Err() != nil {
		return false
	}
	job, ok, err := m.db.ClaimVideoMetadataJob(db.LeaseOptions{
		Owner:   videoMetadataLeaseOwner(),
		LeaseMs: videoMetadataLeaseDuration.Milliseconds(),
		Limit:   1,
	})
	if err != nil {
		log.Printf("[video-metadata] claim: %v", err)
		return false
	}
	if !ok {
		return false
	}
	if !m.externalWorkAllowed(time.Now()) {
		if err := m.db.ReleaseVideoMetadataJob(job, time.Now().UnixMilli()); err != nil {
			log.Printf("[video-metadata] release %s while network probe is active: %v", job.VideoID, err)
		}
		return true
	}

	workCtx, cancel := context.WithTimeout(ctx, videoMetadataTimeout)
	defer cancel()
	cookies, browser := m.cookiesFor("youtube")
	result, err := fetcher.FetchVideoMetadata(
		workCtx,
		"https://www.youtube.com/watch?v="+job.VideoID,
		download.DefaultCommentFetchLimit,
		download.Opts{Cookies: cookies, CookiesFromBrowser: browser},
	)
	if err != nil {
		if m.ReportExternalResult(err) {
			if releaseErr := m.db.ReleaseVideoMetadataJob(job, time.Now().UnixMilli()); releaseErr != nil {
				log.Printf("[video-metadata] release %s after network failure: %v", job.VideoID, releaseErr)
			}
			return true
		}
		m.retryVideoMetadataJob(job, err)
		return true
	}
	m.ReportExternalResult(nil)

	videoPath := ""
	if asset, assetErr := m.db.GetAssetByOwnerIdentity("video_stream", "youtube_video", job.VideoID, 0); assetErr == nil && asset != nil {
		videoPath = asset.FilePath
	}
	enrichCtx, enrichCancel := context.WithTimeout(workCtx, time.Minute)
	m.triggerYoutubeEnrichFetch(enrichCtx, job.VideoID, videoPath, "youtube")
	enrichCancel()

	nowMs := time.Now().UnixMilli()
	if err := m.db.CompleteVideoMetadataJob(job, result, nowMs); err != nil {
		log.Printf("[video-metadata] complete %s: %v", job.VideoID, err)
		m.retryVideoMetadataJob(job, err)
		return true
	}
	if len(result.Comments) > 0 {
		m.KickMediaWork()
	}
	log.Printf("[video-metadata] refreshed %s: comments=%d", job.VideoID, len(result.Comments))
	return true
}

func (m *Manager) retryVideoMetadataJob(job db.VideoMetadataJob, cause error) {
	nowMs := time.Now().UnixMilli()
	if errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
		if err := m.db.ReleaseVideoMetadataJob(job, nowMs); err != nil {
			log.Printf("[video-metadata] release %s: %v", job.VideoID, err)
		}
		return
	}
	message := download.RedactText(cause.Error())
	if job.Attempts+1 >= videoMetadataMaxAttempts {
		if err := m.db.BlockVideoMetadataJob(job, message, nowMs); err != nil {
			log.Printf("[video-metadata] block %s: %v", job.VideoID, err)
			return
		}
		log.Printf("[video-metadata] stopped %s after %d attempts: %s", job.VideoID, job.Attempts+1, message)
		return
	}
	delay := videoMetadataRetryDelay(job.Attempts + 1)
	if err := m.db.RetryVideoMetadataJob(job, message, delay, nowMs); err != nil {
		log.Printf("[video-metadata] retry %s: %v", job.VideoID, err)
		return
	}
	log.Printf("[video-metadata] retry %s in %s: %s", job.VideoID, delay, message)
}

func videoMetadataRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Minute
	for i := 1; i < attempt && delay < 6*time.Hour; i++ {
		delay *= 2
		if delay > 6*time.Hour {
			delay = 6 * time.Hour
		}
	}
	return delay
}

func videoMetadataLeaseOwner() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("video-metadata:%s:%d", host, os.Getpid())
}
