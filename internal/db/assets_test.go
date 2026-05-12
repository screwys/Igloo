package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestAssetInventoryUpsertIsIdempotent(t *testing.T) {
	d := openWritableTestDB(t)

	asset := Asset{
		AssetID:        BuildManifestAssetID("twitter", "tweet", "tweet_asset_a", "post_media", 0),
		AssetKind:      "post_media",
		OwnerKind:      "tweet",
		OwnerID:        "tweet_asset_a",
		MediaIndex:     0,
		FilePath:       "media/twitter/sample/tweet_asset_a_0.jpg",
		ContentType:    "image/jpeg",
		SizeBytes:      123,
		State:          AssetStateReady,
		RequiredReason: "retention",
	}
	if err := d.UpsertAsset(asset, 1000); err != nil {
		t.Fatalf("first UpsertAsset: %v", err)
	}
	asset.SizeBytes = 456
	asset.FilePath = "media/twitter/sample/tweet_asset_a_0_new.jpg"
	if err := d.UpsertAsset(asset, 2000); err != nil {
		t.Fatalf("second UpsertAsset: %v", err)
	}

	got, err := d.GetAsset(asset.AssetID, asset.AssetKind)
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if got == nil {
		t.Fatal("asset missing after upsert")
	}
	if got.SizeBytes != 456 || got.FilePath != asset.FilePath {
		t.Fatalf("asset was not updated: %+v", *got)
	}
	if got.CreatedAtMs != 1000 || got.UpdatedAtMs != 2000 {
		t.Fatalf("timestamps = created %d updated %d, want 1000/2000", got.CreatedAtMs, got.UpdatedAtMs)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM assets WHERE asset_id = ? AND asset_kind = ?`, asset.AssetID, asset.AssetKind).Scan(&count); err != nil {
		t.Fatalf("count assets: %v", err)
	}
	if count != 1 {
		t.Fatalf("asset rows = %d, want 1", count)
	}
}

func TestRefreshAssetFileStateMarksReadyAndServerMissing(t *testing.T) {
	d := openWritableTestDB(t)
	relPath := filepath.Join("media", "twitter", "sample", "ready.jpg")
	writeDBTestFile(t, filepath.Join(d.dataDir, relPath), []byte("ready-image"))

	ready := Asset{
		AssetID:    BuildManifestAssetID("twitter", "tweet", "tweet_ready", "post_media", 0),
		AssetKind:  "post_media",
		OwnerKind:  "tweet",
		OwnerID:    "tweet_ready",
		FilePath:   relPath,
		MediaIndex: 0,
		State:      AssetStateQueued,
	}
	if err := d.UpsertAsset(ready, 1000); err != nil {
		t.Fatalf("upsert ready asset: %v", err)
	}
	if err := d.RefreshAssetFileState(ready.AssetID, ready.AssetKind, 2000); err != nil {
		t.Fatalf("refresh ready asset: %v", err)
	}
	gotReady, err := d.GetAsset(ready.AssetID, ready.AssetKind)
	if err != nil {
		t.Fatalf("get ready asset: %v", err)
	}
	if gotReady.State != AssetStateReady || gotReady.SizeBytes != int64(len("ready-image")) {
		t.Fatalf("ready asset state/size = %s/%d", gotReady.State, gotReady.SizeBytes)
	}

	missing := Asset{
		AssetID:    BuildManifestAssetID("twitter", "tweet", "tweet_missing", "post_media", 0),
		AssetKind:  "post_media",
		OwnerKind:  "tweet",
		OwnerID:    "tweet_missing",
		FilePath:   filepath.Join("media", "twitter", "sample", "missing.jpg"),
		MediaIndex: 0,
		State:      AssetStateQueued,
	}
	if err := d.UpsertAsset(missing, 1000); err != nil {
		t.Fatalf("upsert missing asset: %v", err)
	}
	if err := d.RefreshAssetFileState(missing.AssetID, missing.AssetKind, 2000); err != nil {
		t.Fatalf("refresh missing asset: %v", err)
	}
	gotMissing, err := d.GetAsset(missing.AssetID, missing.AssetKind)
	if err != nil {
		t.Fatalf("get missing asset: %v", err)
	}
	if gotMissing.State != AssetStateServerMissing || gotMissing.SizeBytes != 0 {
		t.Fatalf("missing asset state/size = %s/%d", gotMissing.State, gotMissing.SizeBytes)
	}
}

func TestBackfillAssetsFromExistingPaths(t *testing.T) {
	d := openWritableTestDB(t)

	writeDBTestFile(t, filepath.Join(d.dataDir, "media", "twitter", "sample", "tweet_media.jpg"), []byte("tweet-media"))
	writeDBTestFile(t, filepath.Join(d.dataDir, "media", "twitter", "sample", "quote_media.mp4"), []byte("quote-video"))
	writeDBTestFile(t, filepath.Join(d.dataDir, "thumbnails", "generated", "quote_asset.jpg"), []byte("quote-thumb"))
	writeDBTestFile(t, filepath.Join(d.dataDir, "videos", "youtube", "vid.mp4"), []byte("video-stream"))
	writeDBTestFile(t, filepath.Join(d.dataDir, "videos", "youtube", "vid.jpg"), []byte("video-thumb"))
	writeDBTestFile(t, filepath.Join(d.dataDir, "videos", "youtube", "vid.en.vtt"), []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nhi\n"))
	writeDBTestFile(t, filepath.Join(d.dataDir, "thumbnails", "dearrow", "youtube_vid.jpg"), []byte("dearrow-thumb"))
	writeDBTestFile(t, filepath.Join(d.dataDir, "thumbnails", "previews", "youtube_vid", "track.json"), []byte(`{"frames":[]}`))
	writeDBTestFile(t, filepath.Join(d.dataDir, "thumbnails", "previews", "youtube_vid", "sprite.jpg"), []byte("sprite"))
	writeDBTestFile(t, filepath.Join(d.dataDir, "thumbnails", "avatars", "youtube_chan.jpg"), []byte("avatar"))
	writeDBTestFile(t, filepath.Join(d.dataDir, "thumbnails", "banners", "youtube_chan.jpg"), []byte("banner"))

	if err := d.ExecRaw(`
		INSERT INTO media_files (owner_type, owner_id, media_index, file_path, media_type, source_url, file_size)
		VALUES
			('feed_media', 'tweet_asset', 0, 'media/twitter/sample/tweet_media.jpg', 'photo', 'https://example.test/tweet.jpg', 11),
			('quote_media', 'quote_asset', 0, 'media/twitter/sample/quote_media.mp4', 'video', 'https://example.test/quote.mp4', 11)
	`); err != nil {
		t.Fatalf("insert media_files: %v", err)
	}
	if err := d.ExecRaw(`
		INSERT INTO videos (
			video_id, channel_id, title, thumbnail_path, file_path, file_size,
			published_at, dearrow_thumb_path
		) VALUES (
			'youtube_vid', 'youtube_chan', 'Video',
			'videos/youtube/vid.jpg', 'videos/youtube/vid.mp4', 12,
			1234, 'thumbnails/dearrow/youtube_vid.jpg'
		)
	`); err != nil {
		t.Fatalf("insert video: %v", err)
	}
	if err := d.ExecRaw(`
		INSERT INTO channel_profiles (channel_id, platform, handle, avatar_url, banner_url, fetched_at)
		VALUES ('youtube_chan', 'youtube', 'chan', 'https://example.test/avatar.jpg', 'https://example.test/banner.jpg', 1234)
	`); err != nil {
		t.Fatalf("insert profile: %v", err)
	}

	n, err := d.BackfillAssetsFromExistingPaths(5000)
	if err != nil {
		t.Fatalf("BackfillAssetsFromExistingPaths: %v", err)
	}
	if n < 10 {
		t.Fatalf("backfilled %d assets, want at least 10", n)
	}

	want := []struct {
		id   string
		kind string
	}{
		{BuildManifestAssetID("twitter", "tweet", "tweet_asset", "post_media", 0), "post_media"},
		{BuildManifestAssetID("twitter", "tweet", "quote_asset", "post_media", 0), "post_media"},
		{BuildManifestAssetID("twitter", "tweet", "quote_asset", "post_thumbnail", 0), "post_thumbnail"},
		{BuildManifestAssetID("youtube", "youtube_video", "youtube_vid", "video_stream", 0), "video_stream"},
		{BuildManifestAssetID("youtube", "youtube_video", "youtube_vid", "post_thumbnail", 0), "post_thumbnail"},
		{BuildManifestAssetID("youtube", "youtube_video", "youtube_vid", "dearrow_thumbnail", 0), "dearrow_thumbnail"},
		{BuildManifestAssetID("youtube", "youtube_video", "youtube_vid", "subtitle", 0), "subtitle"},
		{BuildManifestAssetID("youtube", "youtube_video", "youtube_vid", "preview_track_json", 0), "preview_track_json"},
		{BuildManifestAssetID("youtube", "youtube_video", "youtube_vid", "preview_sprite", 0), "preview_sprite"},
		{BuildManifestAssetID("youtube", "channel", "youtube_chan", "avatar", 0), "avatar"},
		{BuildManifestAssetID("youtube", "channel", "youtube_chan", "banner", 0), "banner"},
	}
	for _, tt := range want {
		got, err := d.GetAsset(tt.id, tt.kind)
		if err != nil {
			t.Fatalf("GetAsset %s/%s: %v", tt.id, tt.kind, err)
		}
		if got == nil {
			t.Fatalf("missing asset %s/%s", tt.id, tt.kind)
		}
		if got.State != AssetStateReady {
			t.Fatalf("asset %s state = %s, want ready", tt.id, got.State)
		}
	}
}

func TestInsertMediaFileMaintainsAssetInventory(t *testing.T) {
	d := openWritableTestDB(t)
	relPath := filepath.Join("media", "twitter", "sample", "tweet_insert_0.jpg")
	writeDBTestFile(t, filepath.Join(d.dataDir, relPath), []byte("inserted-media"))

	if err := d.InsertMediaFile(model.MediaFile{
		OwnerType:  "feed_media",
		OwnerID:    "tweet_insert",
		MediaIndex: 0,
		FilePath:   relPath,
		MediaType:  "photo",
		SourceURL:  "https://example.test/insert.jpg",
		FileSize:   14,
	}); err != nil {
		t.Fatalf("InsertMediaFile: %v", err)
	}

	got, err := d.GetAsset(BuildManifestAssetID("twitter", "tweet", "tweet_insert", "post_media", 0), "post_media")
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if got == nil {
		t.Fatal("inserted media asset missing")
	}
	if got.State != AssetStateReady || got.FilePath != relPath || got.SourceURL != "https://example.test/insert.jpg" {
		t.Fatalf("inserted media asset mismatch: %+v", *got)
	}
}

func writeDBTestFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
