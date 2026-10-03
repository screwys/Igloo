package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/download"
)

func (m *Manager) QueueYouTubeReplayChat(videoID string) error {
	if !m.db.BoolSetting("youtube_broadcasts_enabled") || (m.cfg != nil && !m.cfg.PlatformEnabled("youtube")) {
		return nil
	}
	media, err := m.db.ListReadyAssetsForOwners(
		[]db.AssetOwnerRef{{OwnerKind: "youtube_video", OwnerID: videoID}},
		[]string{"video_stream", "post_media", "post_audio"},
	)
	if err != nil || len(media) == 0 {
		return err
	}
	if err := m.db.DeclareAsset(db.Asset{
		AssetID:   db.BuildAssetID("youtube", "youtube_video", videoID, "live_chat", 0),
		AssetKind: "live_chat", OwnerKind: "youtube_video", OwnerID: videoID,
		SourceURL:   "https://www.youtube.com/watch?v=" + videoID,
		ContentType: "application/x-ndjson", RequiredReason: "retention",
	}, 0); err != nil {
		return err
	}
	m.KickMediaWork()
	return nil
}

func (m *Manager) StreamYouTubeChat(ctx context.Context, videoID string, emit func(json.RawMessage) error) error {
	if m.downloader == nil || !m.cfg.PlatformEnabled("youtube") || !m.db.BoolSetting("youtube_broadcasts_enabled") {
		return fmt.Errorf("YouTube broadcasts are disabled")
	}
	if !m.externalWorkAllowed(time.Now()) {
		return fmt.Errorf("external work is paused")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	file, browser := m.cookiesFor("youtube")
	return m.downloader.YtDlp.StreamYouTubeChat(ctx, "https://www.youtube.com/watch?v="+videoID,
		download.Opts{Cookies: file, CookiesFromBrowser: browser, CookieAlternates: m.cookieSetsFor("youtube")}, func(record json.RawMessage) error {
			if !m.db.BoolSetting("youtube_broadcasts_enabled") || !m.externalWorkAllowed(time.Now()) {
				return fmt.Errorf("chat fetching is disabled")
			}
			return emit(record)
		})
}
