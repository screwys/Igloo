package db

import (
	"database/sql"
	"fmt"
	"strings"
)

func cleanupUnfollowedChannelContentTx(tx *sql.Tx, channelID string, nowMs int64) ([]string, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil, nil
	}
	var stillFollowed int
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM channel_follows WHERE channel_id = ?)`, channelID).Scan(&stillFollowed); err != nil {
		return nil, err
	}
	if stillFollowed != 0 {
		return nil, nil
	}

	if strings.HasPrefix(strings.ToLower(channelID), "twitter_") || strings.HasPrefix(strings.ToLower(channelID), "x_") {
		if err := stopUnfollowedProfileWorkTx(tx, channelID, nowMs); err != nil {
			return nil, err
		}
		return collectUnreferencedXContentTx(tx, channelID)
	}

	var platform string
	if err := tx.QueryRow(`SELECT COALESCE(platform, '') FROM channels WHERE channel_id = ?`, channelID).Scan(&platform); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if platform != "youtube" && platform != "tiktok" && platform != "instagram" {
		return nil, nil
	}
	if err := stopUnfollowedProfileWorkTx(tx, channelID, nowMs); err != nil {
		return nil, err
	}
	return collectUnreferencedVideoContentTx(tx, channelID, nowMs)
}

func stopUnfollowedProfileWorkTx(tx *sql.Tx, channelID string, nowMs int64) error {
	_, err := tx.Exec(`
		UPDATE profile_jobs
		SET completed_revision = requested_revision,
		    lease_owner = '',
		    lease_until_ms = 0,
		    attempts = 0,
		    next_attempt_at_ms = 0,
		    last_error = '',
		    updated_at_ms = ?
		WHERE channel_id = ?
		  AND requested_revision > completed_revision
	`, nowMs, channelID)
	return err
}

func collectUnreferencedXContentTx(tx *sql.Tx, channelID string) ([]string, error) {
	rows, err := tx.Query(unreferencedXContentIDsQuery(), channelID, channelID, channelID)
	if err != nil {
		return nil, err
	}
	var tweetIDs []string
	for rows.Next() {
		var tweetID string
		if err := rows.Scan(&tweetID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		tweetIDs = append(tweetIDs, tweetID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM retweet_sources WHERE retweeter_channel_id = ?`, channelID); err != nil {
		return nil, err
	}
	if len(tweetIDs) == 0 {
		return nil, nil
	}

	var retiredFileKeys []string
	for _, chunk := range stringChunks(tweetIDs, 300) {
		keys, err := queryAssetFileKeysTx(tx, `
			SELECT DISTINCT current.file_path
			FROM assets a
			JOIN media_objects current ON current.object_id = a.object_id
			WHERE a.owner_kind = 'tweet'
			  AND a.owner_id IN (`+placeholders(len(chunk))+`)
			  AND current.published_revision > 0 AND current.file_path != ''
		`, stringsToAny(chunk)...)
		if err != nil {
			return nil, err
		}
		retiredFileKeys = append(retiredFileKeys, keys...)

		args := stringsToAny(chunk)
		for _, statement := range []string{
			`DELETE FROM assets WHERE owner_kind = 'tweet' AND owner_id IN (` + placeholders(len(chunk)) + `)`,
			`DELETE FROM download_queue WHERE video_id IN (` + placeholders(len(chunk)) + `)`,
			`DELETE FROM videos WHERE owner_kind = 'tweet' AND video_id IN (` + placeholders(len(chunk)) + `)`,
			`DELETE FROM feed_item_sources WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
			`DELETE FROM retweet_sources WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
			`DELETE FROM feed_rank_snapshot WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
			`DELETE FROM feed_rank_snapshot_history WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
			`DELETE FROM translation_jobs WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
			`DELETE FROM translations WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
			`DELETE FROM feed_items WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
		} {
			if _, err := tx.Exec(statement, args...); err != nil {
				return nil, err
			}
		}
	}
	return retiredFileKeys, nil
}

func unreferencedXContentIDsQuery() string {
	return fmt.Sprintf(`
		WITH RECURSIVE affected(tweet_id) AS (
			SELECT fi.tweet_id
			FROM feed_items fi INDEXED BY idx_feed_items_source_channel
			WHERE fi.source_channel_id = ?
			UNION
			SELECT fi.tweet_id
			FROM feed_items fi INDEXED BY idx_feed_items_channel
			WHERE fi.channel_id = ?
			UNION
			SELECT fi.tweet_id
			FROM feed_items fi INDEXED BY idx_feed_items_reposter_channel
			WHERE fi.reposter_channel_id IS NOT NULL
			  AND fi.reposter_channel_id != ''
			  AND fi.reposter_channel_id = ?
			UNION
			SELECT fi.canonical_tweet_id
			FROM feed_items fi JOIN affected a ON a.tweet_id = fi.tweet_id
			WHERE fi.canonical_tweet_id IS NOT NULL AND fi.canonical_tweet_id != ''
			UNION
			SELECT fi.quote_tweet_id
			FROM feed_items fi JOIN affected a ON a.tweet_id = fi.tweet_id
			WHERE fi.quote_tweet_id IS NOT NULL AND fi.quote_tweet_id != ''
			UNION
			SELECT fi.reply_to_status
			FROM feed_items fi JOIN affected a ON a.tweet_id = fi.tweet_id
			WHERE fi.reply_to_status IS NOT NULL AND fi.reply_to_status != ''
			UNION
			SELECT fi.tweet_id
			FROM feed_items fi INDEXED BY idx_feed_items_canonical_tweet
			JOIN affected a ON fi.canonical_tweet_id = a.tweet_id
			WHERE fi.canonical_tweet_id IS NOT NULL AND fi.canonical_tweet_id != ''
			UNION
			SELECT fi.tweet_id
			FROM feed_items fi INDEXED BY idx_feed_items_quote
			JOIN affected a ON fi.quote_tweet_id = a.tweet_id
			WHERE fi.quote_tweet_id IS NOT NULL AND fi.quote_tweet_id != ''
			UNION
			SELECT fi.tweet_id
			FROM feed_items fi INDEXED BY idx_feed_items_reply_parent
			JOIN affected a ON fi.reply_to_status = a.tweet_id
			WHERE fi.reply_to_status IS NOT NULL AND fi.reply_to_status != ''
			UNION
			SELECT peer.tweet_id
			FROM affected a
			JOIN feed_items current ON current.tweet_id = a.tweet_id
			JOIN feed_items peer INDEXED BY idx_feed_items_content_hash
			  ON peer.content_hash = current.content_hash
			WHERE current.content_hash IS NOT NULL AND current.content_hash != ''
			  AND peer.content_hash IS NOT NULL AND peer.content_hash != ''
		), roots(tweet_id) AS (
			SELECT fi.tweet_id
			FROM feed_items fi JOIN affected a ON a.tweet_id = fi.tweet_id
			WHERE %s
			   OR EXISTS (SELECT 1 FROM bookmarks b WHERE b.video_id = fi.tweet_id)
			   OR EXISTS (SELECT 1 FROM feed_likes fl WHERE fl.tweet_id = fi.tweet_id)
			   OR EXISTS (
			       SELECT 1 FROM videos v
			       WHERE v.video_id = fi.tweet_id AND COALESCE(v.is_pinned, 0) = 1
			   )
		), retained(tweet_id) AS (
			SELECT tweet_id FROM roots
			UNION
			SELECT fi.canonical_tweet_id
			FROM feed_items fi JOIN retained r ON r.tweet_id = fi.tweet_id
			WHERE fi.canonical_tweet_id IS NOT NULL AND fi.canonical_tweet_id != ''
			UNION
			SELECT fi.quote_tweet_id
			FROM feed_items fi JOIN retained r ON r.tweet_id = fi.tweet_id
			WHERE fi.quote_tweet_id IS NOT NULL AND fi.quote_tweet_id != ''
			UNION
			SELECT fi.reply_to_status
			FROM feed_items fi JOIN retained r ON r.tweet_id = fi.tweet_id
			WHERE fi.reply_to_status IS NOT NULL AND fi.reply_to_status != ''
		)
		SELECT a.tweet_id
		FROM affected a
		WHERE EXISTS (SELECT 1 FROM feed_items fi WHERE fi.tweet_id = a.tweet_id)
		  AND NOT EXISTS (SELECT 1 FROM retained r WHERE r.tweet_id = a.tweet_id)
		ORDER BY a.tweet_id
	`, feedActiveOwnerPredicate("fi"))
}

func collectUnreferencedVideoContentTx(tx *sql.Tx, channelID string, nowMs int64) ([]string, error) {
	rows, err := tx.Query(`
		SELECT DISTINCT video_id
		FROM video_desires
		WHERE source_channel_id = ?
		ORDER BY video_id
	`, channelID)
	if err != nil {
		return nil, err
	}
	var candidateIDs []string
	for rows.Next() {
		var videoID string
		if err := rows.Scan(&videoID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		candidateIDs = append(candidateIDs, videoID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM video_desires WHERE source_channel_id = ?`, channelID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM video_repost_sources WHERE reposter_channel_id = ?`, channelID); err != nil {
		return nil, err
	}
	if len(candidateIDs) == 0 {
		return nil, nil
	}

	var collectibleIDs []string
	for _, chunk := range stringChunks(candidateIDs, 300) {
		args := append([]any{nowMs}, stringsToAny(chunk)...)
		rows, err := tx.Query(`
			SELECT v.video_id
			FROM videos v
			WHERE `+collectibleVideoWhereSQL+`
			  AND v.video_id IN (`+placeholders(len(chunk))+`)
			ORDER BY v.video_id
		`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var videoID string
			if err := rows.Scan(&videoID); err != nil {
				_ = rows.Close()
				return nil, err
			}
			collectibleIDs = append(collectibleIDs, videoID)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}

	var retiredFileKeys []string
	for _, videoID := range collectibleIDs {
		var ownerKind string
		if err := tx.QueryRow(`SELECT owner_kind FROM videos WHERE video_id = ?`, videoID).Scan(&ownerKind); err != nil {
			return nil, err
		}
		keys, err := queryAssetFileKeysTx(tx, `
			SELECT DISTINCT current.file_path
			FROM assets a INDEXED BY idx_assets_owner
			JOIN media_objects current ON current.object_id = a.object_id
			WHERE a.owner_kind = ? AND a.owner_id = ?
			  AND current.published_revision > 0 AND current.file_path != ''
		`, ownerKind, videoID)
		if err != nil {
			return nil, err
		}
		retiredFileKeys = append(retiredFileKeys, keys...)
		if _, err := tx.Exec(`DELETE FROM download_queue WHERE video_id = ?`, videoID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`DELETE FROM assets WHERE owner_kind = ? AND owner_id = ?`, ownerKind, videoID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`DELETE FROM videos WHERE video_id = ? AND owner_kind = ?`, videoID, ownerKind); err != nil {
			return nil, err
		}
	}
	return retiredFileKeys, nil
}
