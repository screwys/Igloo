package db

import (
	"database/sql"
	"strings"
)

// InstagramVideoID reuses the stored identity for a shortcode seen in another tab.
func (db *DB) InstagramVideoID(videoID string) (string, error) {
	shortcode := ""
	for _, prefix := range []string{"instagram_post_", "instagram_reel_"} {
		if strings.HasPrefix(videoID, prefix) {
			shortcode = strings.TrimPrefix(videoID, prefix)
			break
		}
	}
	if shortcode == "" {
		return videoID, nil
	}
	postID, reelID := "instagram_post_"+shortcode, "instagram_reel_"+shortcode
	var storedID string
	err := db.reader().QueryRow(`
		SELECT video_id FROM (
			SELECT video_id, 0 AS priority FROM videos WHERE video_id IN (?, ?)
			UNION ALL SELECT video_id, 1 FROM download_queue WHERE video_id IN (?, ?)
			UNION ALL SELECT video_id, 2 FROM video_fetch_history WHERE video_id IN (?, ?)
		) ORDER BY priority, video_id LIMIT 1
	`, postID, reelID, postID, reelID, postID, reelID).Scan(&storedID)
	if err == sql.ErrNoRows {
		return postID, nil
	}
	return storedID, err
}
