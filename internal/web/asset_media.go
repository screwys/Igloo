package web

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/db"
)

type canonicalAssetFile struct {
	asset db.Asset
	path  string
}

func (s *Server) canonicalStreamAsset(owner db.AssetOwnerRef) *canonicalAssetFile {
	files := s.canonicalAssets(owner, "video_stream", "post_media")
	for i := range files {
		if files[i].asset.AssetKind == "video_stream" {
			return &files[i]
		}
	}
	for i := range files {
		contentType := files[i].asset.ContentType
		if strings.HasPrefix(contentType, "video/") || contentType == "image/gif" {
			return &files[i]
		}
	}
	return nil
}

func (s *Server) canonicalAsset(owner db.AssetOwnerRef, kind string, index int) *canonicalAssetFile {
	assets, err := s.db.ListReadyAssetsForOwners([]db.AssetOwnerRef{owner}, []string{kind})
	if err != nil {
		return nil
	}
	for _, asset := range assets {
		if asset.MediaIndex != index {
			continue
		}
		if file := s.canonicalAssetFile(asset); file != nil {
			return file
		}
	}
	return nil
}

func (s *Server) canonicalAssets(owner db.AssetOwnerRef, kinds ...string) []canonicalAssetFile {
	var out []canonicalAssetFile
	assets, err := s.db.ListReadyAssetsForOwners([]db.AssetOwnerRef{owner}, kinds)
	if err != nil {
		return nil
	}
	for _, asset := range assets {
		if file := s.canonicalAssetFile(asset); file != nil {
			out = append(out, *file)
		}
	}
	return out
}

func (s *Server) videoAssetOwner(videoID string) (db.AssetOwnerRef, bool) {
	video, err := s.db.GetVideo(strings.TrimSpace(videoID))
	if err != nil || video == nil {
		return db.AssetOwnerRef{}, false
	}
	switch video.OwnerKind {
	case "tweet", "youtube_video", "tiktok_video", "instagram_reel":
		ownerKind, ownerID := video.OwnerKind, video.VideoID
		if video.MediaOwnerKind != "" {
			ownerKind = video.MediaOwnerKind
		}
		if video.MediaOwnerID != "" {
			ownerID = video.MediaOwnerID
		}
		return db.AssetOwnerRef{OwnerKind: ownerKind, OwnerID: ownerID}, true
	default:
		return db.AssetOwnerRef{}, false
	}
}

func (s *Server) requestMediaAssetOwner(r *http.Request, id string) (db.AssetOwnerRef, bool) {
	ownerID := strings.TrimSpace(r.URL.Query().Get("owner_id"))
	if ownerID == "" {
		ownerID = strings.TrimSpace(id)
	}
	switch r.URL.Query().Get("owner_kind") {
	case "tweet":
		return db.AssetOwnerRef{OwnerKind: "tweet", OwnerID: ownerID}, ownerID != ""
	case "":
		return s.videoAssetOwner(id)
	default:
		return db.AssetOwnerRef{}, false
	}
}

func (s *Server) canonicalAssetFile(asset db.Asset) *canonicalAssetFile {
	contentType := strings.TrimSpace(asset.ContentType)
	// Published bytes can remain shared after this owner's retention expires.
	if strings.TrimSpace(asset.FilePath) == "" ||
		contentType == "" || contentType == "application/octet-stream" {
		return nil
	}
	asset.ContentType = contentType
	path, err := s.cfg.Storage.Path(asset.FilePath)
	if err != nil {
		return nil
	}
	if asset.OwnerKind == "tweet" && (asset.AssetKind == "post_media" || asset.AssetKind == "post_audio" || asset.AssetKind == "post_thumbnail") && asset.State == db.AssetStateReady {
		info, statErr := os.Stat(path)
		missing := os.IsNotExist(statErr)
		if statErr == nil {
			missing = !info.Mode().IsRegular() || info.Size() != asset.SizeBytes ||
				(asset.FileMtimeNs > 0 && info.ModTime().UnixNano() != asset.FileMtimeNs)
		}
		if missing {
			nowMs := time.Now().UnixMilli()
			changed, err := s.db.MarkReadyAssetUnavailable(asset, nowMs)
			if err != nil {
				slog.Warn("mark missing tweet media unavailable", "owner_id", asset.OwnerID, "asset_kind", asset.AssetKind, "err", err)
			} else if changed {
				queued, err := s.db.RequeueXContentAssets([]string{asset.OwnerID}, false, "retention", nowMs)
				if err != nil {
					slog.Warn("requeue missing tweet media", "owner_id", asset.OwnerID, "asset_kind", asset.AssetKind, "err", err)
				} else if queued > 0 && s.workers != nil {
					s.workers.KickMediaWork()
				}
			}
			return nil
		}
	}
	return &canonicalAssetFile{asset: asset, path: path}
}
