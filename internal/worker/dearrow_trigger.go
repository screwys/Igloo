package worker

import (
	"context"
	"log"
	"time"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/download"
)

// triggerDearrowFetch runs a DeArrow check for videoID in the background.
// videoRelPath is the video's logical storage key; it is used
// for ffmpeg frame extraction when DeArrow returns a thumbnail timestamp.
//
// Swallowed errors are logged. On complete failure (network error, all nil)
// this still marks the video as "checked now" so the background worker
// doesn't immediately re-fetch it.
//
// Does nothing if the manager has no dearrowFetcher (test-mode) or if
// platform != "youtube".
func (m *Manager) triggerDearrowFetch(ctx context.Context, videoID, videoRelPath, platform string) {
	if m.dearrowFetcher == nil || platform != "youtube" {
		return
	}

	absPath := ""
	if videoRelPath != "" {
		var pathErr error
		absPath, pathErr = m.cfg.Storage.Path(videoRelPath)
		if pathErr != nil {
			log.Printf("[dearrow] storage path %s: %v", videoID, pathErr)
			return
		}
	}

	res, err := m.dearrowFetcher.FetchAndProcess(ctx, videoID, absPath)
	nowMs := time.Now().UnixMilli()
	if err != nil {
		log.Printf("[dearrow] fetch %s: %v", videoID, err)
		// Partial: extractor may have failed but titles may still be set.
		if res.Title == nil && res.CasualTitle == nil && res.ThumbPath == nil {
			if mErr := m.db.MarkDearrowChecked(videoID, nowMs); mErr != nil {
				log.Printf("[dearrow] mark-checked %s: %v", videoID, mErr)
			}
			return
		}
		if saveErr := m.db.SetDearrowTitles(videoID, res.Title, res.CasualTitle, nowMs); saveErr != nil {
			log.Printf("[dearrow] save partial %s: %v", videoID, saveErr)
		}
		return
	}

	var thumbRel *string
	if res.ThumbPath != nil {
		rel, rErr := m.cfg.Storage.Key(*res.ThumbPath)
		if rErr == nil {
			thumbRel = &rel
		} else {
			log.Printf("[dearrow] reject thumbnail path %s: %v", videoID, rErr)
			m.removeMediaPaths(ctx, download.MediaLaneBulkBackground, *res.ThumbPath)
			if saveErr := m.db.SetDearrowTitles(videoID, res.Title, res.CasualTitle, nowMs); saveErr != nil {
				log.Printf("[dearrow] save partial %s: %v", videoID, saveErr)
			}
			return
		}
	}
	if sErr := m.db.SetDearrowData(videoID, res.Title, res.CasualTitle, thumbRel, nowMs); sErr != nil {
		log.Printf("[dearrow] save %s: %v", videoID, sErr)
		if res.ThumbPath != nil {
			m.removeMediaPaths(ctx, download.MediaLaneBulkBackground, *res.ThumbPath)
		}
	}
}

// triggerYoutubeEnrichFetch runs the DeArrow and SponsorBlock parts of the
// durable video-metadata pass. Silently no-ops for non-YouTube platforms.
func (m *Manager) triggerYoutubeEnrichFetch(ctx context.Context, videoID, videoRelPath, platform string) {
	if platform != "youtube" {
		return
	}
	var dearrowCheckedAtMs int64
	var publishedAtMs int64
	if v, err := m.db.GetVideo(videoID); err == nil && v != nil && v.PublishedAt != nil {
		publishedAtMs = v.PublishedAt.UnixMilli()
		if v.DearrowCheckedAtMs != nil {
			dearrowCheckedAtMs = *v.DearrowCheckedAtMs
		}
	}
	nowMs := time.Now().UnixMilli()
	if videoMetadataComponentDue(dearrowCheckedAtMs, publishedAtMs, nowMs) {
		m.triggerDearrowFetch(ctx, videoID, videoRelPath, platform)
	}

	checked, _ := m.db.GetSponsorBlockChecked(videoID)
	if checked == nil || (checked.VideoAgeAtCheck != "old" && nowMs-checked.CheckedAtMs >= db.VideoMetadataRefresh.Milliseconds()) {
		m.fetchSponsorBlockFor(ctx, videoID, publishedAtMs)
	}
}

func videoMetadataComponentDue(checkedAtMs, publishedAtMs, nowMs int64) bool {
	if checkedAtMs <= 0 {
		return true
	}
	if publishedAtMs <= 0 || checkedAtMs-publishedAtMs >= db.VideoMetadataYoungAge.Milliseconds() {
		return false
	}
	return nowMs-checkedAtMs >= db.VideoMetadataRefresh.Milliseconds()
}
