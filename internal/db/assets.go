package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	AssetStateQueued           = "queued"
	AssetStateDownloading      = "downloading"
	AssetStateReady            = "ready"
	AssetStateFailed           = "failed"
	AssetStateServerMissing    = "server_missing"
	AssetStatePermanentMissing = "permanent_missing"
	AssetStateStale            = "stale"
)

// Asset is the server-owned inventory row for a binary or derived media asset.
type Asset struct {
	ID              int64
	AssetID         string
	AssetKind       string
	OwnerKind       string
	OwnerID         string
	MediaIndex      int
	SourceURL       string
	FilePath        string
	ContentType     string
	SizeBytes       int64
	SHA256          string
	State           string
	RequiredReason  string
	LastErrorKind   string
	LastError       string
	Attempts        int
	NextAttemptAtMs int64
	CreatedAtMs     int64
	UpdatedAtMs     int64
}

// UpsertAsset inserts or updates an inventory row. The asset identity follows
// the Android/manifest asset_id contract while this table remains additive.
func (db *DB) UpsertAsset(asset Asset, nowMs int64) error {
	asset = normalizeAsset(asset, nowMs)
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO assets (
				asset_id, asset_kind, owner_kind, owner_id, media_index,
				source_url, file_path, content_type, size_bytes, sha256, state,
				required_reason, last_error_kind, last_error, attempts,
				next_attempt_at_ms, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(asset_kind, owner_kind, owner_id, media_index) DO UPDATE SET
				asset_id = excluded.asset_id,
				source_url = excluded.source_url,
				file_path = excluded.file_path,
				content_type = excluded.content_type,
				size_bytes = excluded.size_bytes,
				sha256 = excluded.sha256,
				state = excluded.state,
				required_reason = excluded.required_reason,
				last_error_kind = excluded.last_error_kind,
				last_error = excluded.last_error,
				attempts = excluded.attempts,
				next_attempt_at_ms = excluded.next_attempt_at_ms,
				updated_at_ms = excluded.updated_at_ms
		`, asset.AssetID, asset.AssetKind, asset.OwnerKind, asset.OwnerID, asset.MediaIndex,
			asset.SourceURL, asset.FilePath, asset.ContentType, asset.SizeBytes, asset.SHA256, asset.State,
			asset.RequiredReason, asset.LastErrorKind, asset.LastError, asset.Attempts,
			asset.NextAttemptAtMs, asset.CreatedAtMs, asset.UpdatedAtMs)
		return err
	})
}

func normalizeAsset(asset Asset, nowMs int64) Asset {
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	asset.AssetID = strings.TrimSpace(asset.AssetID)
	asset.AssetKind = strings.TrimSpace(asset.AssetKind)
	asset.OwnerKind = strings.TrimSpace(asset.OwnerKind)
	asset.OwnerID = strings.TrimSpace(asset.OwnerID)
	asset.SourceURL = strings.TrimSpace(asset.SourceURL)
	asset.FilePath = strings.TrimSpace(asset.FilePath)
	asset.ContentType = strings.TrimSpace(asset.ContentType)
	asset.SHA256 = strings.TrimSpace(asset.SHA256)
	asset.State = strings.TrimSpace(asset.State)
	asset.RequiredReason = strings.TrimSpace(asset.RequiredReason)
	asset.LastErrorKind = strings.TrimSpace(asset.LastErrorKind)
	asset.LastError = strings.TrimSpace(asset.LastError)
	if asset.State == "" {
		asset.State = AssetStateQueued
	}
	if asset.CreatedAtMs <= 0 {
		asset.CreatedAtMs = nowMs
	}
	if asset.UpdatedAtMs <= 0 {
		asset.UpdatedAtMs = nowMs
	}
	return asset
}

// GetAsset returns one inventory row by public asset identity.
func (db *DB) GetAsset(assetID, assetKind string) (*Asset, error) {
	row := db.conn.QueryRow(`
		SELECT id, asset_id, asset_kind, owner_kind, owner_id, media_index,
		       source_url, file_path, content_type, size_bytes, sha256, state,
		       required_reason, last_error_kind, last_error, attempts,
		       next_attempt_at_ms, created_at_ms, updated_at_ms
		FROM assets
		WHERE asset_id = ? AND asset_kind = ?
	`, strings.TrimSpace(assetID), strings.TrimSpace(assetKind))
	asset, err := scanAsset(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

// ListAndroidSyncAssetInventoryRows returns inventory rows that can contribute
// directly to Android sync generation for the desired owner sets.
func (db *DB) ListAndroidSyncAssetInventoryRows(sets AndroidSyncDesiredSets) ([]Asset, error) {
	ownerIDs := map[string]struct{}{}
	for _, id := range sets.SortedTweets() {
		ownerIDs[id] = struct{}{}
	}
	for _, id := range sets.SortedVideos() {
		ownerIDs[id] = struct{}{}
	}
	for _, id := range sets.SortedMediaVideos() {
		ownerIDs[id] = struct{}{}
	}
	for _, id := range sets.SortedChannels() {
		ownerIDs[id] = struct{}{}
	}
	var out []Asset
	for _, chunk := range stringChunks(sortedKeys(ownerIDs), 400) {
		if len(chunk) == 0 {
			continue
		}
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := db.conn.Query(`
			SELECT id, asset_id, asset_kind, owner_kind, owner_id, media_index,
			       source_url, file_path, content_type, size_bytes, sha256, state,
			       required_reason, last_error_kind, last_error, attempts,
			       next_attempt_at_ms, created_at_ms, updated_at_ms
			FROM assets
			WHERE owner_id IN (`+placeholders(len(chunk))+`)
			  AND state IN ('ready', 'server_missing')
			ORDER BY id ASC
		`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			asset, err := scanAsset(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, asset)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

type assetScanner interface {
	Scan(dest ...any) error
}

func scanAsset(row assetScanner) (Asset, error) {
	var asset Asset
	err := row.Scan(
		&asset.ID,
		&asset.AssetID,
		&asset.AssetKind,
		&asset.OwnerKind,
		&asset.OwnerID,
		&asset.MediaIndex,
		&asset.SourceURL,
		&asset.FilePath,
		&asset.ContentType,
		&asset.SizeBytes,
		&asset.SHA256,
		&asset.State,
		&asset.RequiredReason,
		&asset.LastErrorKind,
		&asset.LastError,
		&asset.Attempts,
		&asset.NextAttemptAtMs,
		&asset.CreatedAtMs,
		&asset.UpdatedAtMs,
	)
	return asset, err
}

// RefreshAssetFileState reconciles one asset's ready/server_missing state from
// its recorded file path. It does not compute checksums.
func (db *DB) RefreshAssetFileState(assetID, assetKind string, nowMs int64) error {
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	asset, err := db.GetAsset(assetID, assetKind)
	if err != nil || asset == nil {
		return err
	}
	absPath := resolveManifestDataPath(db.dataDir, asset.FilePath)
	state := AssetStateServerMissing
	sizeBytes := int64(0)
	contentType := asset.ContentType
	if asset.FilePath == "" {
		state = AssetStateQueued
	} else if info, statErr := os.Stat(absPath); statErr == nil {
		state = AssetStateReady
		sizeBytes = info.Size()
		if contentType == "" {
			contentType = contentTypeForPath(asset.FilePath, "")
		}
	}
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE assets
			SET state = ?, size_bytes = ?, content_type = ?, updated_at_ms = ?
			WHERE asset_id = ? AND asset_kind = ?
		`, state, sizeBytes, contentType, nowMs, asset.AssetID, asset.AssetKind)
		return err
	})
}

// BackfillAssetsFromExistingPaths creates inventory rows from the legacy media
// path columns and conventional cache directories. It is safe to run repeatedly.
func (db *DB) BackfillAssetsFromExistingPaths(nowMs int64) (int, error) {
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	total := 0
	for _, fn := range []func(int64) (int, error){
		db.backfillMediaFileAssets,
		db.backfillVideoAssets,
		db.backfillProfileAssets,
	} {
		n, err := fn(nowMs)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (db *DB) backfillMediaFileAssets(nowMs int64) (int, error) {
	rows, err := db.conn.Query(`
		SELECT owner_id, COALESCE(media_type, ''), media_index,
		       COALESCE(file_size, 0), COALESCE(file_path, ''), COALESCE(source_url, '')
		FROM media_files
		WHERE owner_type IN ('feed_media', 'quote_media')
		ORDER BY id ASC
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var ownerID, mediaType, filePath, sourceURL string
		var mediaIndex int
		var fileSize int64
		if err := rows.Scan(&ownerID, &mediaType, &mediaIndex, &fileSize, &filePath, &sourceURL); err != nil {
			return count, err
		}
		if manifestSkipsFile(filePath) {
			continue
		}
		asset := db.assetFromLegacyPath(Asset{
			AssetID:        BuildManifestAssetID("twitter", "tweet", ownerID, "post_media", mediaIndex),
			AssetKind:      "post_media",
			OwnerKind:      "tweet",
			OwnerID:        ownerID,
			MediaIndex:     mediaIndex,
			SourceURL:      sourceURL,
			FilePath:       filePath,
			ContentType:    contentTypeForMediaPath(filePath, mediaType, "image/jpeg"),
			SizeBytes:      fileSize,
			State:          AssetStateQueued,
			RequiredReason: "backfill",
		})
		if err := db.UpsertAsset(asset, nowMs); err != nil {
			return count, err
		}
		count++

		if mediaIndex == 0 && (mediaType == "video" || mediaType == "gif" || !manifestUsesImageTransport(filePath, mediaType)) {
			thumbRel := filepath.Join("thumbnails", "generated", ownerID+".jpg")
			thumb := db.assetFromLegacyPath(Asset{
				AssetID:        BuildManifestAssetID("twitter", "tweet", ownerID, "post_thumbnail", 0),
				AssetKind:      "post_thumbnail",
				OwnerKind:      "tweet",
				OwnerID:        ownerID,
				FilePath:       thumbRel,
				ContentType:    "image/jpeg",
				State:          AssetStateQueued,
				RequiredReason: "backfill",
			})
			if err := db.UpsertAsset(thumb, nowMs); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, rows.Err()
}

func (db *DB) backfillVideoAssets(nowMs int64) (int, error) {
	rows, err := db.conn.Query(`
		SELECT video_id, COALESCE(channel_id, ''), COALESCE(thumbnail_path, ''),
		       COALESCE(file_path, ''), COALESCE(file_size, 0), COALESCE(dearrow_thumb_path, '')
		FROM videos
		ORDER BY id ASC
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var videoID, channelID, thumbnailPath, filePath, dearrowPath string
		var fileSize int64
		if err := rows.Scan(&videoID, &channelID, &thumbnailPath, &filePath, &fileSize, &dearrowPath); err != nil {
			return count, err
		}
		platform := videoPlatformFromChannelID(channelID)
		if platform == "" {
			platform = videoPlatform(videoID)
		}
		ownerKind := videoOwnerKindForPlatform(platform)
		for _, asset := range []Asset{
			{
				AssetID:        BuildManifestAssetID(platform, ownerKind, videoID, "video_stream", 0),
				AssetKind:      "video_stream",
				OwnerKind:      ownerKind,
				OwnerID:        videoID,
				FilePath:       filePath,
				ContentType:    contentTypeForMediaPath(filePath, "", "video/mp4"),
				SizeBytes:      fileSize,
				State:          AssetStateQueued,
				RequiredReason: "backfill",
			},
			{
				AssetID:        BuildManifestAssetID(platform, ownerKind, videoID, "post_thumbnail", 0),
				AssetKind:      "post_thumbnail",
				OwnerKind:      ownerKind,
				OwnerID:        videoID,
				FilePath:       thumbnailPath,
				ContentType:    contentTypeForPath(thumbnailPath, "image/jpeg"),
				State:          AssetStateQueued,
				RequiredReason: "backfill",
			},
			{
				AssetID:        BuildManifestAssetID(platform, ownerKind, videoID, "dearrow_thumbnail", 0),
				AssetKind:      "dearrow_thumbnail",
				OwnerKind:      ownerKind,
				OwnerID:        videoID,
				FilePath:       dearrowPath,
				ContentType:    contentTypeForPath(dearrowPath, "image/jpeg"),
				State:          AssetStateQueued,
				RequiredReason: "backfill",
			},
		} {
			if strings.TrimSpace(asset.FilePath) == "" {
				continue
			}
			asset = db.assetFromLegacyPath(asset)
			if err := db.UpsertAsset(asset, nowMs); err != nil {
				return count, err
			}
			count++
		}
		if subtitleRel := db.findSubtitleRelativePath(filePath); subtitleRel != "" {
			asset := db.assetFromLegacyPath(Asset{
				AssetID:        BuildManifestAssetID(platform, ownerKind, videoID, "subtitle", 0),
				AssetKind:      "subtitle",
				OwnerKind:      ownerKind,
				OwnerID:        videoID,
				FilePath:       subtitleRel,
				ContentType:    "text/vtt",
				State:          AssetStateQueued,
				RequiredReason: "backfill",
			})
			if err := db.UpsertAsset(asset, nowMs); err != nil {
				return count, err
			}
			count++
		}
		for _, preview := range []struct {
			name        string
			assetKind   string
			contentType string
		}{
			{name: "track.json", assetKind: "preview_track_json", contentType: "application/json"},
			{name: "sprite.jpg", assetKind: "preview_sprite", contentType: "image/jpeg"},
		} {
			rel := filepath.Join("thumbnails", "previews", videoID, preview.name)
			if _, err := os.Stat(resolveManifestDataPath(db.dataDir, rel)); err != nil {
				continue
			}
			asset := db.assetFromLegacyPath(Asset{
				AssetID:        BuildManifestAssetID(platform, ownerKind, videoID, preview.assetKind, 0),
				AssetKind:      preview.assetKind,
				OwnerKind:      ownerKind,
				OwnerID:        videoID,
				FilePath:       rel,
				ContentType:    preview.contentType,
				State:          AssetStateQueued,
				RequiredReason: "backfill",
			})
			if err := db.UpsertAsset(asset, nowMs); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, rows.Err()
}

func (db *DB) backfillProfileAssets(nowMs int64) (int, error) {
	rows, err := db.conn.Query(`
		SELECT channel_id, COALESCE(platform, ''), COALESCE(avatar_url, ''), COALESCE(banner_url, '')
		FROM channel_profiles
		WHERE COALESCE(tombstone, 0) = 0
		ORDER BY rowid ASC
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var channelID, platform, avatarURL, bannerURL string
		if err := rows.Scan(&channelID, &platform, &avatarURL, &bannerURL); err != nil {
			return count, err
		}
		if platform == "" {
			platform = videoPlatformFromChannelID(channelID)
		}
		if platform == "" {
			platform = strings.SplitN(channelID, "_", 2)[0]
		}
		for _, asset := range []Asset{
			{
				AssetID:        BuildManifestAssetID(platform, "channel", channelID, "avatar", 0),
				AssetKind:      "avatar",
				OwnerKind:      "channel",
				OwnerID:        channelID,
				SourceURL:      avatarURL,
				FilePath:       db.findAvatarRelativePath(channelID),
				ContentType:    "image/jpeg",
				State:          AssetStateQueued,
				RequiredReason: "backfill",
			},
			{
				AssetID:        BuildManifestAssetID(platform, "channel", channelID, "banner", 0),
				AssetKind:      "banner",
				OwnerKind:      "channel",
				OwnerID:        channelID,
				SourceURL:      bannerURL,
				FilePath:       db.findBannerRelativePath(channelID),
				ContentType:    "image/jpeg",
				State:          AssetStateQueued,
				RequiredReason: "backfill",
			},
		} {
			if asset.FilePath == "" && asset.SourceURL == "" {
				continue
			}
			asset = db.assetFromLegacyPath(asset)
			if err := db.UpsertAsset(asset, nowMs); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, rows.Err()
}

func (db *DB) assetFromLegacyPath(asset Asset) Asset {
	path := strings.TrimSpace(asset.FilePath)
	if path == "" {
		asset.State = AssetStateQueued
		asset.SizeBytes = 0
		return asset
	}
	if info, err := os.Stat(resolveManifestDataPath(db.dataDir, path)); err == nil {
		asset.State = AssetStateReady
		asset.SizeBytes = info.Size()
		if asset.ContentType == "" {
			asset.ContentType = contentTypeForPath(path, "")
		}
		return asset
	}
	asset.State = AssetStateServerMissing
	asset.SizeBytes = 0
	return asset
}

// AssetServerURL maps an inventory row to the existing server media endpoint
// contract used by legacy manifest and Android sync asset rows.
func AssetServerURL(asset Asset) string {
	switch asset.AssetKind {
	case "avatar":
		return "/api/media/avatar/" + asset.OwnerID
	case "banner":
		return "/api/media/banner/" + asset.OwnerID
	case "post_thumbnail":
		return "/api/media/thumbnail/" + asset.OwnerID
	case "dearrow_thumbnail":
		return "/api/media/thumbnail/" + asset.OwnerID + "?da=1"
	case "video_stream":
		return "/api/media/stream/" + asset.OwnerID
	case "subtitle":
		return "/api/media/subtitle/" + asset.OwnerID
	case "post_audio":
		return "/api/media/audio/" + asset.OwnerID
	case "preview_track_json":
		return "/api/media/preview-track-json/" + asset.OwnerID
	case "preview_sprite":
		return "/api/media/preview-sprite/" + asset.OwnerID
	case "post_media":
		return fmt.Sprintf("/api/media/slide/%s/%d", asset.OwnerID, asset.MediaIndex)
	default:
		return ""
	}
}
