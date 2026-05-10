package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/screwys/igloo/internal/config"
	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/fullimport"
)

type counters struct {
	bookmarkDatesUnavailable int
	bookmarkDatesUpdated     int
	bookmarkMetadataUpdated  int
	likeDatesUnavailable     int
	likeDatesUpdated         int
	likePublishedUpdated     int
	likeMetadataUpdated      int
	feedItemPublishedUpdated int
	likeSnowflakeUpdated     int
	videoSnowflakeUpdated    int
	videoPublishedUpdated    int
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("repair_full_export_state", flag.ContinueOnError)
	exportPath := fs.String("export", "", "path to the igloo-full-*.zip used for import; optional for local published-at repair")
	userID := fs.String("user", "", "user-owned rows to repair; defaults to exported user_id")
	dbPath := fs.String("db", "", "database path; defaults to configured Igloo database")
	apply := fs.Bool("apply", false, "write changes; without this flag the command only reports")
	overwrite := fs.Bool("overwrite", false, "replace non-empty current values with export values")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var exportCfg *db.ConfigExport
	if strings.TrimSpace(*exportPath) != "" {
		data, err := os.ReadFile(*exportPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read export: %v\n", err)
			return 1
		}
		cfg, err := fullimport.ReadExportConfig(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read full export: %v\n", err)
			return 1
		}
		exportCfg = &cfg
	}
	owner := strings.TrimSpace(*userID)
	if owner == "" && exportCfg != nil {
		owner = strings.TrimSpace(exportCfg.UserID)
	}
	if owner == "" && exportCfg != nil {
		fmt.Fprintln(os.Stderr, "user is required because the export has no user_id")
		return 2
	}

	appCfg := config.Load()
	if appCfg.ConfigError != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", appCfg.ConfigError)
		return 1
	}
	path := strings.TrimSpace(*dbPath)
	if path == "" {
		path = appCfg.DatabasePath
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(30000)&_pragma=foreign_keys(on)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		return 1
	}
	defer conn.Close()
	if err := conn.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "ping database: %v\n", err)
		return 1
	}

	tx, err := conn.Begin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "begin transaction: %v\n", err)
		return 1
	}
	stats, err := repair(tx, exportCfg, owner, *overwrite)
	if err != nil {
		tx.Rollback()
		fmt.Fprintf(os.Stderr, "repair: %v\n", err)
		return 1
	}
	if !*apply {
		tx.Rollback()
		fmt.Println("mode=dry_run")
	} else if err := tx.Commit(); err != nil {
		fmt.Fprintf(os.Stderr, "commit: %v\n", err)
		return 1
	} else {
		fmt.Println("mode=applied")
	}

	fmt.Printf("database=%s\n", path)
	fmt.Printf("owner=%s\n", owner)
	fmt.Printf("bookmark_dates_updated=%d\n", stats.bookmarkDatesUpdated)
	fmt.Printf("bookmark_metadata_updated=%d\n", stats.bookmarkMetadataUpdated)
	fmt.Printf("like_dates_updated=%d\n", stats.likeDatesUpdated)
	fmt.Printf("like_published_updated=%d\n", stats.likePublishedUpdated)
	fmt.Printf("like_metadata_updated=%d\n", stats.likeMetadataUpdated)
	fmt.Printf("like_snowflake_published_updated=%d\n", stats.likeSnowflakeUpdated)
	fmt.Printf("feed_item_snowflake_published_updated=%d\n", stats.feedItemPublishedUpdated)
	fmt.Printf("video_snowflake_published_updated=%d\n", stats.videoSnowflakeUpdated)
	fmt.Printf("video_published_updated=%d\n", stats.videoPublishedUpdated)
	if stats.bookmarkDatesUnavailable > 0 || stats.likeDatesUnavailable > 0 {
		fmt.Printf("bookmark_dates_unavailable_in_export=%d\n", stats.bookmarkDatesUnavailable)
		fmt.Printf("like_dates_unavailable_in_export=%d\n", stats.likeDatesUnavailable)
	}
	return 0
}

func repair(tx *sql.Tx, cfg *db.ConfigExport, owner string, overwrite bool) (counters, error) {
	var stats counters

	if cfg == nil {
		return repairLocalPublishedAt(tx, owner, overwrite, stats)
	}

	for _, bm := range cfg.Bookmarks {
		videoID := strings.TrimSpace(bm.VideoID)
		if videoID == "" {
			continue
		}
		if bm.BookmarkedAt > 0 {
			n, err := updateBookmarkDate(tx, owner, videoID, bm.BookmarkedAt, overwrite)
			if err != nil {
				return stats, err
			}
			stats.bookmarkDatesUpdated += n
		} else {
			stats.bookmarkDatesUnavailable++
		}
		n, err := updateBookmarkMetadata(tx, owner, bm, overwrite)
		if err != nil {
			return stats, err
		}
		stats.bookmarkMetadataUpdated += n
	}

	for _, lp := range cfg.LikedPosts {
		tweetID := strings.TrimSpace(lp.TweetID)
		if tweetID == "" {
			continue
		}
		if lp.LikedAt > 0 {
			n, err := updateLikeDate(tx, owner, tweetID, lp.LikedAt, lp.UpdatedAt, overwrite)
			if err != nil {
				return stats, err
			}
			stats.likeDatesUpdated += n
		} else {
			stats.likeDatesUnavailable++
		}
		publishedAt := timestampMillis(lp.PublishedAtMs, lp.PublishedAt)
		if publishedAt > 0 {
			n, err := updateLikePublishedAt(tx, owner, tweetID, publishedAt, overwrite)
			if err != nil {
				return stats, err
			}
			stats.likePublishedUpdated += n
		}
		n, err := updateLikeMetadata(tx, owner, lp, overwrite)
		if err != nil {
			return stats, err
		}
		stats.likeMetadataUpdated += n
	}

	for _, bv := range cfg.BookmarkedVideos {
		videoID := strings.TrimSpace(bv.VideoID)
		if videoID == "" {
			continue
		}
		if bv.BookmarkedAt > 0 {
			n, err := updateBookmarkDate(tx, owner, videoID, bv.BookmarkedAt, overwrite)
			if err != nil {
				return stats, err
			}
			stats.bookmarkDatesUpdated += n
		}
		publishedAt := timestampMillis(bv.PublishedAtMs, bv.PublishedAt)
		if publishedAt > 0 {
			n, err := updateVideoPublishedAt(tx, videoID, publishedAt, overwrite)
			if err != nil {
				return stats, err
			}
			stats.videoPublishedUpdated += n
		}
	}

	return repairLocalPublishedAt(tx, owner, overwrite, stats)
}

func updateBookmarkDate(tx *sql.Tx, owner, videoID string, at int64, overwrite bool) (int, error) {
	where := "user_id = ? AND video_id = ? AND bookmarked_at = 0"
	if overwrite {
		where = "user_id = ? AND video_id = ?"
	}
	res, err := tx.Exec("UPDATE bookmarks SET bookmarked_at = ? WHERE "+where, at, owner, videoID)
	return rowsAffected(res, err)
}

func updateBookmarkMetadata(tx *sql.Tx, owner string, bm db.BookmarkExport, overwrite bool) (int, error) {
	total := 0
	for _, field := range []struct {
		column string
		value  string
	}{
		{"custom_title", bm.CustomTitle},
		{"account_handles", bm.AccountHandles},
		{"media_indices", bm.MediaIndices},
	} {
		if strings.TrimSpace(field.value) == "" {
			continue
		}
		where := "user_id = ? AND video_id = ? AND (COALESCE(" + field.column + ", '') = '')"
		if overwrite {
			where = "user_id = ? AND video_id = ?"
		}
		res, err := tx.Exec("UPDATE bookmarks SET "+field.column+" = ? WHERE "+where, field.value, owner, bm.VideoID)
		n, err := rowsAffected(res, err)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func updateLikeDate(tx *sql.Tx, owner, tweetID string, likedAt, updatedAt int64, overwrite bool) (int, error) {
	if updatedAt == 0 {
		updatedAt = likedAt
	}
	where := "username = ? AND tweet_id = ? AND (liked_at = 0 OR liked_at = updated_at)"
	if overwrite {
		where = "username = ? AND tweet_id = ?"
	}
	res, err := tx.Exec("UPDATE feed_likes SET liked_at = ?, updated_at = ? WHERE "+where, likedAt, updatedAt, owner, tweetID)
	return rowsAffected(res, err)
}

func updateLikePublishedAt(tx *sql.Tx, owner, tweetID string, publishedAt int64, overwrite bool) (int, error) {
	where := "username = ? AND tweet_id = ? AND published_at = 0"
	if overwrite {
		where = "username = ? AND tweet_id = ?"
	}
	res, err := tx.Exec("UPDATE feed_likes SET published_at = ? WHERE "+where, publishedAt, owner, tweetID)
	return rowsAffected(res, err)
}

func updateLikeMetadata(tx *sql.Tx, owner string, lp db.LikedPostExport, overwrite bool) (int, error) {
	total := 0
	for _, field := range []struct {
		column string
		value  string
	}{
		{"source_handle", lp.SourceHandle},
		{"author_handle", lp.AuthorHandle},
		{"author_display_name", lp.AuthorDisplayName},
		{"body_text", lp.BodyText},
		{"link", lp.Link},
		{"canonical_x_link", lp.CanonicalXLink},
		{"media_url", lp.MediaURL},
		{"avatar_url", lp.AvatarURL},
		{"media_json", lp.MediaJSON},
		{"platform", lp.Platform},
		{"quote_payload_json", lp.QuotePayloadJSON},
	} {
		if strings.TrimSpace(field.value) == "" {
			continue
		}
		where := "username = ? AND tweet_id = ? AND (COALESCE(" + field.column + ", '') = '')"
		if overwrite {
			where = "username = ? AND tweet_id = ?"
		}
		res, err := tx.Exec("UPDATE feed_likes SET "+field.column+" = ? WHERE "+where, field.value, owner, lp.TweetID)
		n, err := rowsAffected(res, err)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func updateVideoPublishedAt(tx *sql.Tx, videoID string, publishedAt int64, overwrite bool) (int, error) {
	where := "video_id = ? AND published_at = 0"
	if overwrite {
		where = "video_id = ?"
	}
	res, err := tx.Exec("UPDATE videos SET published_at = ? WHERE "+where, publishedAt, videoID)
	return rowsAffected(res, err)
}

func repairLocalPublishedAt(tx *sql.Tx, owner string, overwrite bool, stats counters) (counters, error) {
	likeWhere := "(platform IS NULL OR platform = '' OR platform IN ('twitter', 'x'))"
	likeArgs := []any{}
	if owner != "" {
		likeWhere += " AND username = ?"
		likeArgs = append(likeArgs, owner)
	}
	if !overwrite {
		likeWhere += " AND published_at = 0"
	}
	likeRows, err := tx.Query("SELECT username, tweet_id FROM feed_likes WHERE "+likeWhere, likeArgs...)
	if err != nil {
		return stats, err
	}
	for likeRows.Next() {
		var username, tweetID string
		if err := likeRows.Scan(&username, &tweetID); err != nil {
			likeRows.Close()
			return stats, err
		}
		publishedAt := twitterSnowflakeMillis(tweetID)
		if publishedAt == 0 {
			continue
		}
		res, err := tx.Exec(
			`UPDATE feed_likes SET published_at = ? WHERE username = ? AND tweet_id = ?`,
			publishedAt, username, tweetID,
		)
		n, err := rowsAffected(res, err)
		if err != nil {
			likeRows.Close()
			return stats, err
		}
		stats.likeSnowflakeUpdated += n
	}
	if err := likeRows.Close(); err != nil {
		return stats, err
	}

	feedWhere := `
		fi.tweet_id GLOB '[0-9]*'
		AND (
			EXISTS (
				SELECT 1 FROM feed_likes l
				WHERE l.tweet_id = fi.tweet_id
				  AND (? = '' OR l.username = ?)
				  AND (l.platform IS NULL OR l.platform = '' OR l.platform IN ('twitter', 'x'))
			)
			OR EXISTS (
				SELECT 1 FROM bookmarks b
				JOIN videos v ON v.video_id = b.video_id
				WHERE b.video_id = fi.tweet_id
				  AND (? = '' OR b.user_id = ?)
				  AND (v.channel_id LIKE 'twitter_%' OR v.channel_id LIKE 'x_%')
			)
		)
	`
	feedArgs := []any{owner, owner, owner, owner}
	if !overwrite {
		feedWhere += " AND (fi.published_at = 0 OR fi.published_at = fi.fetched_at)"
	}
	feedRows, err := tx.Query("SELECT fi.tweet_id FROM feed_items fi WHERE "+feedWhere, feedArgs...)
	if err != nil {
		return stats, err
	}
	for feedRows.Next() {
		var tweetID string
		if err := feedRows.Scan(&tweetID); err != nil {
			feedRows.Close()
			return stats, err
		}
		publishedAt := twitterSnowflakeMillis(tweetID)
		if publishedAt == 0 {
			continue
		}
		res, err := tx.Exec(`UPDATE feed_items SET published_at = ? WHERE tweet_id = ?`, publishedAt, tweetID)
		n, err := rowsAffected(res, err)
		if err != nil {
			feedRows.Close()
			return stats, err
		}
		stats.feedItemPublishedUpdated += n
	}
	if err := feedRows.Close(); err != nil {
		return stats, err
	}

	videoWhere := "video_id GLOB '[0-9]*' AND (channel_id LIKE 'twitter_%' OR channel_id LIKE 'x_%')"
	if !overwrite {
		videoWhere += " AND published_at = 0"
	}
	videoRows, err := tx.Query("SELECT video_id FROM videos WHERE " + videoWhere)
	if err != nil {
		return stats, err
	}
	for videoRows.Next() {
		var videoID string
		if err := videoRows.Scan(&videoID); err != nil {
			videoRows.Close()
			return stats, err
		}
		publishedAt := twitterSnowflakeMillis(videoID)
		if publishedAt == 0 {
			continue
		}
		res, err := tx.Exec(`UPDATE videos SET published_at = ? WHERE video_id = ?`, publishedAt, videoID)
		n, err := rowsAffected(res, err)
		if err != nil {
			videoRows.Close()
			return stats, err
		}
		stats.videoSnowflakeUpdated += n
	}
	return stats, videoRows.Close()
}

func rowsAffected(res sql.Result, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func timestampMillis(ms int64, legacy string) int64 {
	if ms > 0 {
		return ms
	}
	legacy = strings.TrimSpace(legacy)
	if legacy == "" {
		return 0
	}
	if n, err := strconv.ParseInt(legacy, 10, 64); err == nil {
		if n > 1_000_000_000_000 {
			return n
		}
		if n > 0 {
			return n * 1000
		}
		return 0
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		time.RFC3339,
		"Mon Jan 02 15:04:05 +0000 2006",
	} {
		if t, err := time.Parse(layout, legacy); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

func twitterSnowflakeMillis(id string) int64 {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	ms := (n >> 22) + 1288834974657
	if ms <= 1288834974657 {
		return 0
	}
	return ms
}
