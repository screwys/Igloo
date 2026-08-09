package db

import (
	"database/sql"
	"strings"
	"time"
)

// PruneXProfileHistory bounds the ordinary timeline rows owned by one X
// profile. Saved posts and rows still owned by another feed surface or thread
// remain in the database and do not consume the profile's history window.
func (db *DB) PruneXProfileHistory(channelID string, limit int, nowMs int64) (XMediaRetentionResult, error) {
	result := XMediaRetentionResult{Limit: 1, RetentionLimit: limit}
	channelID = strings.TrimSpace(channelID)
	if !strings.HasPrefix(channelID, "twitter_") || limit <= 0 {
		return result, nil
	}

	nowMs = normalizeNowMs(nowMs)
	androidCutoffMs := int64(0)
	if state, err := db.GetAndroidFeedRetention(); err != nil {
		return result, err
	} else if state != nil && state.FeedDays > 0 {
		androidCutoffMs = nowMs - int64(state.FeedDays)*int64(24*time.Hour/time.Millisecond)
	}

	items, err := db.xProfileHistoryRetentionItems(channelID, androidCutoffMs)
	if err != nil {
		return result, err
	}
	pruneIDs := addXRetentionStats(&result, items, limit)
	if len(pruneIDs) == 0 {
		return result, nil
	}
	candidates, err := db.xAssetOwnerIDsForTweets(pruneIDs)
	if err != nil {
		return result, err
	}
	if err := db.deleteXProfileHistoryItems(pruneIDs); err != nil {
		return result, err
	}
	retained, err := db.xRetainedMediaOwnerSet(nowMs, 0, candidates)
	if err != nil {
		return result, err
	}
	return db.reconcileXMediaOwnerSet(result, retained, candidates, false, DownloadLaneBackfill, nowMs)
}

func (db *DB) xProfileHistoryRetentionItems(channelID string, androidCutoffMs int64) ([]xRetentionItem, error) {
	rows, err := db.conn.Query(`
		SELECT fi.tweet_id,
		       CASE WHEN EXISTS (SELECT 1 FROM bookmarks b WHERE b.video_id = fi.tweet_id)
		              OR EXISTS (SELECT 1 FROM feed_likes fl WHERE fl.tweet_id = fi.tweet_id)
		              OR EXISTS (SELECT 1 FROM feed_item_sources fis WHERE fis.tweet_id = fi.tweet_id)
		              OR EXISTS (
		                   SELECT 1 FROM feed_items child
		                   WHERE child.reply_to_status = fi.tweet_id OR child.quote_tweet_id = fi.tweet_id
		                 )
		              OR (? > 0 AND fi.published_at >= ?)
		            THEN 1 ELSE 0 END
		FROM feed_items fi
		WHERE fi.source_channel_id = ?
		ORDER BY fi.published_at DESC, fi.tweet_id DESC
	`, androidCutoffMs, androidCutoffMs, channelID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []xRetentionItem
	for rows.Next() {
		var item xRetentionItem
		var protected int
		if err := rows.Scan(&item.tweetID, &protected); err != nil {
			return nil, err
		}
		item.protected = protected != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

func (db *DB) deleteXProfileHistoryItems(tweetIDs []string) error {
	for _, chunk := range stringChunks(uniqueStrings(tweetIDs), 300) {
		if err := db.WithWrite(func(tx *sql.Tx) error {
			args := stringsToAny(chunk)
			for _, statement := range []string{
				`DELETE FROM feed_rank_snapshot WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
				`DELETE FROM feed_rank_snapshot_history WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
				`DELETE FROM translation_jobs WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
				`DELETE FROM translations WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
				`DELETE FROM retweet_sources WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
				`DELETE FROM feed_items WHERE tweet_id IN (` + placeholders(len(chunk)) + `)`,
			} {
				if _, err := tx.Exec(statement, args...); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
