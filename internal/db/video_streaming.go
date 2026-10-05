package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const webVideoStreamsTableStatement = `CREATE TABLE IF NOT EXISTS web_video_streams (
	video_id TEXT PRIMARY KEY REFERENCES videos(video_id) ON DELETE CASCADE,
	observed_at_ms INTEGER NOT NULL
) WITHOUT ROWID`

func webOnlyStreamExistsSQL(videoID string) string {
	return `EXISTS (SELECT 1 FROM web_video_streams streamed
		JOIN videos stream_video ON stream_video.video_id = streamed.video_id
		WHERE streamed.video_id = ` + videoID + ` AND NOT (` + readyVideoMediaExistsSQL("stream_video") + `))`
}

// ObserveStreamVideo stores content without claiming that its media was downloaded.
func (db *DB) ObserveStreamVideo(video CompletedVideo) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		if err := requireVideoOwnerKindTx(tx, video.VideoID, video.OwnerKind); err != nil {
			return err
		}
		_, err := tx.Exec(bind(`
			INSERT INTO videos (video_id, channel_id, owner_kind, title, description,
				duration, published_at, metadata_json, media_kind, downloaded_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'video', 0)
			ON CONFLICT(video_id) DO UPDATE SET
				channel_id = CASE WHEN excluded.channel_id != '' THEN excluded.channel_id ELSE videos.channel_id END,
				title = CASE WHEN excluded.title != '' THEN excluded.title ELSE videos.title END,
				description = CASE WHEN excluded.description != '' THEN excluded.description ELSE videos.description END,
				duration = CASE WHEN excluded.duration > 0 THEN excluded.duration ELSE videos.duration END,
				published_at = CASE WHEN excluded.published_at > 0 THEN excluded.published_at ELSE videos.published_at END,
				metadata_json = CASE WHEN excluded.metadata_json != '' THEN excluded.metadata_json ELSE videos.metadata_json END
		`), video.VideoID, video.ChannelID, video.OwnerKind, video.Title, video.Description,
			video.Duration, video.PublishedAtMs, video.MetadataJSON)
		if err != nil {
			return err
		}
		if video.OwnerKind == "youtube_video" {
			if err := syncYouTubeBroadcastStatusTx(tx, video.VideoID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(bind(`INSERT INTO web_video_streams (video_id, observed_at_ms) VALUES (?, ?)
			ON CONFLICT(video_id) DO UPDATE SET observed_at_ms = excluded.observed_at_ms`), video.VideoID, time.Now().UnixMilli())
		return err
	})
}

func (db *DB) StreamVideoNeedsCapture(videoID string) (bool, error) {
	var pending bool
	err := db.reader().QueryRow(`SELECT EXISTS (
		SELECT 1 FROM videos v JOIN web_video_streams streamed ON streamed.video_id = v.video_id
		WHERE v.video_id = ? AND NOT (`+readyVideoMediaExistsSQL("v")+`)
	)`, videoID).Scan(&pending)
	return pending, err
}

type TempDownloadSaveIntent struct {
	ExpectedVideoID string            `json:"expected_video_id,omitempty"`
	Bookmark        *BookmarkMutation `json:"bookmark,omitempty"`
	Like            *LikeMutation     `json:"like,omitempty"`
	ArchiveBookmark bool              `json:"archive_bookmark,omitempty"`
	CombineImages   bool              `json:"combine_images,omitempty"`
}

type TempDownloadBookmarkArchive struct {
	VideoID       string
	CombineImages bool
}

func (db *DB) QueueStreamSave(videoID string, incoming TempDownloadSaveIntent) error {
	rawURL, err := db.StreamVideoSourceURL(videoID)
	if err != nil {
		return err
	}
	video, err := db.GetVideo(videoID)
	if err != nil {
		return err
	}
	return db.WithWrite(func(tx *sql.Tx) error {
		if _, err := enqueueTempDownloadTx(tx, rawURL, video.Platform, time.Now().UnixMilli()); err != nil {
			return err
		}
		var raw string
		if err := tx.QueryRow(bind(`SELECT save_intent_json FROM temp_download_queue WHERE url = ?`), rawURL).Scan(&raw); err != nil {
			return err
		}
		var intent TempDownloadSaveIntent
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &intent); err != nil {
				return err
			}
		}
		intent.ExpectedVideoID = videoID
		if incoming.Bookmark != nil {
			incoming.Bookmark.UpdatedAtMs = mutationTimestamp(incoming.Bookmark.UpdatedAtMs)
			if previous := intent.Bookmark; previous == nil || incoming.Bookmark.UpdatedAtMs >= previous.UpdatedAtMs {
				if previous != nil {
					if incoming.Bookmark.CategoryID == nil {
						incoming.Bookmark.CategoryID = previous.CategoryID
					}
					if incoming.Bookmark.CustomTitle == nil {
						incoming.Bookmark.CustomTitle = previous.CustomTitle
					}
					if incoming.Bookmark.AccountHandles == nil {
						incoming.Bookmark.AccountHandles = previous.AccountHandles
					}
					if incoming.Bookmark.MediaIndices == nil {
						incoming.Bookmark.MediaIndices = previous.MediaIndices
					}
				}
				intent.Bookmark = incoming.Bookmark
				intent.ArchiveBookmark, intent.CombineImages = incoming.ArchiveBookmark, incoming.CombineImages
			}
		}
		if incoming.Like != nil {
			incoming.Like.UpdatedAtMs = mutationTimestamp(incoming.Like.UpdatedAtMs)
			if previous := intent.Like; previous == nil || incoming.Like.UpdatedAtMs >= previous.UpdatedAtMs {
				intent.Like = incoming.Like
			}
		}
		encoded, err := json.Marshal(intent)
		if err != nil {
			return err
		}
		_, err = tx.Exec(bind(`UPDATE temp_download_queue SET save_intent_json = ? WHERE url = ?`), string(encoded), rawURL)
		return err
	})
}

func (db *DB) StreamVideoSourceURL(videoID string) (string, error) {
	video, err := db.GetVideo(videoID)
	if err != nil {
		return "", err
	}
	if video == nil {
		return "", sql.ErrNoRows
	}
	if video.Platform == "youtube" {
		return "https://www.youtube.com/watch?v=" + videoID, nil
	}
	if meta := video.ParseMetadata(); meta != nil && meta.WebpageURL != "" {
		return meta.WebpageURL, nil
	}
	return "", fmt.Errorf("stream source is unavailable")
}

func (db *DB) applyTempDownloadSaveIntentTx(tx *sql.Tx, rawURL, videoID string) (*TempDownloadBookmarkArchive, error) {
	var raw string
	err := tx.QueryRow(bind(`SELECT save_intent_json FROM temp_download_queue WHERE url = ?`), rawURL).Scan(&raw)
	if err == sql.ErrNoRows || raw == "" && err == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var intent TempDownloadSaveIntent
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		return nil, err
	}
	var archive *TempDownloadBookmarkArchive
	if intent.Bookmark != nil {
		intent.Bookmark.VideoID = videoID
		if intent.Bookmark.CategoryID != nil && *intent.Bookmark.CategoryID != 0 {
			var exists bool
			if err := tx.QueryRow(bind(`SELECT EXISTS (SELECT 1 FROM bookmark_categories WHERE id = ?)`), *intent.Bookmark.CategoryID).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				uncategorized := int64(0)
				intent.Bookmark.CategoryID = &uncategorized
			}
		}
		var result MutationResult
		if err := db.mutateBookmarkTx(tx, *intent.Bookmark, &result); err != nil && !IsStaleMutation(err) {
			return nil, err
		}
		if intent.ArchiveBookmark && result.Applied && (result.Affected > 0 || intent.CombineImages) {
			archive = &TempDownloadBookmarkArchive{VideoID: videoID, CombineImages: intent.CombineImages}
		}
	}
	if intent.Like != nil {
		intent.Like.TweetID = videoID
		var result MutationResult
		if err := db.mutateLikeTx(tx, *intent.Like, &result); err != nil && !IsStaleMutation(err) {
			return nil, err
		}
	}
	return archive, nil
}

func finishStreamCaptureTx(tx *sql.Tx, videoID string) error {
	removed, err := tx.Exec(bind(`DELETE FROM web_video_streams WHERE video_id = ?`), videoID)
	if err != nil {
		return err
	}
	count, err := removed.RowsAffected()
	if err != nil || count == 0 {
		return err
	}
	var hasHistory bool
	if err := tx.QueryRow(bind(`SELECT EXISTS (SELECT 1 FROM watch_history WHERE video_id = ?)`), videoID).Scan(&hasHistory); err != nil {
		return err
	}
	if hasHistory {
		return touchAndroidSyncHeadTx(tx, "watch_history", videoID)
	}
	return nil
}

func clearStreamSaveIntentTx(tx *sql.Tx, videoID, kind string, updatedAtMs int64) error {
	rawURL := "https://www.youtube.com/watch?v=" + videoID
	var raw string
	err := tx.QueryRow(bind(`SELECT save_intent_json FROM temp_download_queue WHERE url = ?`), rawURL).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if raw == "" {
		return nil
	}
	var intent TempDownloadSaveIntent
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		return err
	}
	if kind == "bookmark" && intent.Bookmark != nil && intent.Bookmark.UpdatedAtMs <= updatedAtMs {
		intent.Bookmark = nil
		intent.ArchiveBookmark, intent.CombineImages = false, false
	} else if kind == "like" && intent.Like != nil && intent.Like.UpdatedAtMs <= updatedAtMs {
		intent.Like = nil
	}
	encoded, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	_, err = tx.Exec(bind(`UPDATE temp_download_queue SET save_intent_json = ? WHERE url = ?`), string(encoded), rawURL)
	return err
}
