package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/db"
)

const (
	androidSyncAssetPackMagic         = "IGLOO-ASSET-PACK-1"
	androidSyncAssetPackMaxEntries    = 128
	androidSyncAssetPackMaxEntryBytes = 1 << 20
	androidSyncAssetPackMaxBytes      = 16 << 20
)

type androidSyncAssetPackRequest struct {
	Assets []androidSyncAssetPackRef `json:"assets"`
}

type androidSyncAssetPackRef struct {
	AssetID  string `json:"asset_id"`
	Revision int64  `json:"revision"`
}

type androidSyncAssetPackHeader struct {
	AssetID     string `json:"asset_id"`
	Revision    int64  `json:"revision"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentType string `json:"content_type"`
}

type androidSyncOpenPackAsset struct {
	asset db.Asset
	file  *os.File
}

func (s *Server) handleAndroidSyncAssetPack(w http.ResponseWriter, r *http.Request) {
	if userFromContext(r.Context()) == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	var body androidSyncAssetPackRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_json", "invalid asset pack payload")
		return
	}
	if len(body.Assets) == 0 || len(body.Assets) > androidSyncAssetPackMaxEntries {
		writeJSONError(w, http.StatusBadRequest, "invalid_assets", "asset pack count is out of range")
		return
	}
	if !s.tryAcquireAndroidSyncAssetServeSlot() {
		w.Header().Set("Retry-After", fmt.Sprint(androidSyncAssetRetryAfterSecs))
		http.Error(w, "asset server busy", http.StatusTooManyRequests)
		return
	}
	defer s.releaseAndroidSyncAssetServeSlot()

	opened := make([]androidSyncOpenPackAsset, 0, len(body.Assets))
	defer func() {
		for _, entry := range opened {
			_ = entry.file.Close()
		}
	}()
	seen := make(map[string]struct{}, len(body.Assets))
	var totalBytes int64
	for _, requested := range body.Assets {
		requested.AssetID = strings.TrimSpace(requested.AssetID)
		if requested.AssetID == "" || requested.Revision <= 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid_asset_revision", "asset id and positive revision are required")
			return
		}
		if _, exists := seen[requested.AssetID]; exists {
			writeJSONError(w, http.StatusBadRequest, "duplicate_asset", "asset pack contains a duplicate")
			return
		}
		seen[requested.AssetID] = struct{}{}
		asset, err := s.db.GetAndroidSyncAssetByID(requested.AssetID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "asset_lookup_failed", "asset lookup failed")
			return
		}
		if asset == nil || asset.State != db.AssetStateReady || asset.Revision != requested.Revision ||
			asset.SizeBytes <= 0 || asset.SizeBytes > androidSyncAssetPackMaxEntryBytes || asset.FilePath == "" {
			writeJSONError(w, http.StatusConflict, "asset_changed", "asset descriptor changed")
			return
		}
		totalBytes += asset.SizeBytes
		if totalBytes > androidSyncAssetPackMaxBytes {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "asset_pack_too_large", "asset pack exceeds its byte limit")
			return
		}
		path, err := s.cfg.Storage.Path(asset.FilePath)
		if err != nil {
			s.withdrawAndroidSyncPackAsset(w, *asset)
			return
		}
		file, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				s.withdrawAndroidSyncPackAsset(w, *asset)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "asset_read_failed", "asset file could not be read")
			return
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != asset.SizeBytes ||
			(asset.FileMtimeNs > 0 && info.ModTime().UnixNano() != asset.FileMtimeNs) {
			_ = file.Close()
			s.withdrawAndroidSyncPackAsset(w, *asset)
			return
		}
		opened = append(opened, androidSyncOpenPackAsset{asset: *asset, file: file})
	}

	w.Header().Set("Content-Type", "application/x-igloo-asset-pack")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Igloo-Asset-Count", fmt.Sprint(len(opened)))
	if _, err := io.WriteString(w, androidSyncAssetPackMagic+"\n"); err != nil {
		return
	}
	for _, entry := range opened {
		header, err := json.Marshal(androidSyncAssetPackHeader{
			AssetID: entry.asset.AssetID, Revision: entry.asset.Revision,
			SizeBytes: entry.asset.SizeBytes, ContentType: entry.asset.ContentType,
		})
		if err != nil {
			return
		}
		if _, err := w.Write(append(header, '\n')); err != nil {
			return
		}
		if _, err := io.CopyN(w, entry.file, entry.asset.SizeBytes); err != nil {
			return
		}
	}
	slog.Info("android_sync_asset_pack_served", "assets", len(opened), "bytes", totalBytes)
}

func (s *Server) withdrawAndroidSyncPackAsset(w http.ResponseWriter, asset db.Asset) {
	if _, err := s.db.MarkReadyAssetUnavailable(asset, time.Now().UnixMilli()); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "asset_update_failed", "asset state could not be updated")
		return
	}
	writeJSONError(w, http.StatusConflict, "asset_changed", "asset bytes changed")
}
