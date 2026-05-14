package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type roomSchemaFile struct {
	Database struct {
		Version  int          `json:"version"`
		Entities []roomEntity `json:"entities"`
	} `json:"database"`
}

type roomEntity struct {
	TableName string      `json:"tableName"`
	Fields    []roomField `json:"fields"`
}

type roomField struct {
	ColumnName string `json:"columnName"`
}

func TestAndroidRoomSchemaKeepsServerSyncContractColumns(t *testing.T) {
	schema := readLatestAndroidRoomSchema(t)
	columnsByTable := map[string]map[string]bool{}
	for _, entity := range schema.Database.Entities {
		cols := map[string]bool{}
		for _, field := range entity.Fields {
			cols[field.ColumnName] = true
		}
		columnsByTable[entity.TableName] = cols
	}

	required := map[string][]string{
		"feed_items": {
			"tweet_id", "source_handle", "author_handle", "author_display_name",
			"author_avatar_url", "body_text", "lang", "is_retweet",
			"quote_tweet_id", "quote_author_handle", "quote_author_display_name",
			"quote_author_avatar_url", "quote_body_text", "quote_lang",
			"quote_media_json", "quote_published_at", "quote_canonical_url",
			"media_json", "media_status", "views", "likes", "retweets",
			"canonical_url", "canonical_tweet_id", "reply_to_handle",
			"reply_to_status", "is_reply", "is_ghost", "content_hash",
			"body_translation", "body_source_lang", "quote_translation",
			"quote_source_lang", "published_at", "sync_seq", "channel_id",
		},
		"videos": {
			"video_id", "channel_id", "title", "description", "duration",
			"duration_label", "thumbnail_path", "file_path", "file_size",
			"published_at", "downloaded_at", "media_kind", "media_mode",
			"slide_count", "source_kind", "metadata_json", "canonical_url",
			"display_title", "display_title_casual", "dearrow_title",
			"dearrow_title_casual", "dearrow_thumb_path", "dearrow_checked_at_ms",
			"sync_seq",
		},
		"channels": {
			"channel_id", "source_id", "name", "url", "platform",
			"avatar_url", "quality", "last_checked", "created_at",
		},
		"channel_profiles": {
			"channel_id", "platform", "handle", "display_name", "bio",
			"website", "followers", "followers_label", "following",
			"following_label", "verified", "verified_type", "protected",
			"avatar_url", "banner_url", "profile_url",
		},
		"android_sync_generations": {
			"generation_id", "created_at_ms", "status", "source_version",
			"retention_json", "item_count", "asset_count", "ready_asset_count",
			"server_missing_asset_count", "total_bytes", "content_counts_json",
			"asset_counts_json", "items_imported_at_ms", "assets_imported_at_ms",
			"items_importer_version",
		},
		"android_sync_items": {
			"generation_id", "seq", "item_kind", "item_id", "payload_json",
		},
		"android_sync_assets": {
			"generation_id", "seq", "asset_id", "asset_kind", "media_index", "owner_id",
			"owner_kind", "bucket", "server_url", "content_type", "size_bytes",
			"sha256", "server_state", "required_reason", "subtitle_is_auto",
			"audio_language", "effective_recency_ms", "state", "local_path",
			"file_size", "verified_at_ms", "attempt_count", "next_attempt_at_ms",
			"last_error", "updated_at_ms",
		},
	}

	var failures []string
	for table, requiredColumns := range required {
		got, ok := columnsByTable[table]
		if !ok {
			failures = append(failures, table+": missing table")
			continue
		}
		for _, column := range requiredColumns {
			if !got[column] {
				failures = append(failures, table+"."+column)
			}
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("Android Room schema version %d is missing server sync contract columns:\n%s", schema.Database.Version, strings.Join(failures, "\n"))
	}
}

func readLatestAndroidRoomSchema(t *testing.T) roomSchemaFile {
	t.Helper()
	dir := filepath.Join("..", "..", "android", "app", "schemas", "com.screwy.igloo.data.IglooDatabase")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read Android Room schema dir %s: %v", dir, err)
	}
	latestVersion := -1
	latestName := ""
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		version, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			continue
		}
		if version > latestVersion {
			latestVersion = version
			latestName = entry.Name()
		}
	}
	if latestName == "" {
		t.Fatalf("no Android Room schema JSON files found in %s", dir)
	}
	raw, err := os.ReadFile(filepath.Join(dir, latestName))
	if err != nil {
		t.Fatalf("read Android Room schema %s: %v", latestName, err)
	}
	var schema roomSchemaFile
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode Android Room schema %s: %v", latestName, err)
	}
	return schema
}
