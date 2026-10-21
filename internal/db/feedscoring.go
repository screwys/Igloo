package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ScoringItem holds the minimal fields needed to compute algo_interest.
type ScoringItem struct {
	TweetID      string
	SourceHandle string
	AuthorHandle string
	BodyText     string
	MediaJSON    string
	HasMedia     bool
	IsRetweet    bool
	IsStarred    bool
	IsFollowed   bool
	PublishedAt  string
}

func (db *DB) GetUnscoredFeedItems(limit int) ([]ScoringItem, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("unscored feed limit must be positive")
	}
	rows, err := db.conn.Query(bind(`
		WITH candidates(tweet_id) AS MATERIALIZED (
			SELECT candidate.tweet_id
			FROM feed_items candidate
			WHERE candidate.algo_scored_at = 0
			  AND `+feedPrimaryItemPredicate("candidate")+`
			  AND `+feedActiveOwnerPredicate("candidate")+`
			ORDER BY candidate.ingest_order DESC
			LIMIT ?
		), items AS MATERIALIZED (
			SELECT resolved.tweet_id, resolved.source_handle, resolved.author_handle,
			       COALESCE(resolved.body_text,'') AS body_text, COALESCE(resolved.media_json,'') AS media_json,
			       COALESCE(resolved.is_retweet,0) AS is_retweet, COALESCE(resolved.published_at::TEXT,'') AS published_at
			FROM candidates JOIN feed_items_resolved resolved ON resolved.tweet_id = candidates.tweet_id
		), flags AS MATERIALIZED (
			SELECT SUBSTRING(LOWER(follows.channel_id) FROM POSITION('_' IN follows.channel_id) + 1) AS handle,
			       BOOL_OR(stars.channel_id IS NOT NULL) AS starred
			FROM channel_follows follows
			LEFT JOIN channel_stars stars ON stars.channel_id = follows.channel_id
			WHERE SUBSTRING(LOWER(follows.channel_id) FROM POSITION('_' IN follows.channel_id) + 1) IN (
				SELECT LOWER(author_handle) FROM items UNION SELECT LOWER(source_handle) FROM items
			)
			GROUP BY handle
		)
		SELECT items.*,
		       COALESCE(author.starred, false) OR COALESCE(source.starred, false),
		       author.handle IS NOT NULL OR source.handle IS NOT NULL
		FROM items
		LEFT JOIN flags author ON author.handle = LOWER(items.author_handle)
		LEFT JOIN flags source ON source.handle = LOWER(items.source_handle)
	`), limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var items []ScoringItem
	for rows.Next() {
		var si ScoringItem
		if err := rows.Scan(&si.TweetID, &si.SourceHandle, &si.AuthorHandle,
			&si.BodyText, &si.MediaJSON, &si.IsRetweet, &si.PublishedAt, &si.IsStarred, &si.IsFollowed); err != nil {
			return nil, err
		}
		items = append(items, si)
	}
	return items, rows.Err()
}

// UpdateAlgoInterest sets algo_interest and algo_scored_at for a batch of items.
func (db *DB) UpdateAlgoInterest(scores map[string]float64) error {
	if len(scores) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	return db.WithWrite(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare("UPDATE feed_items SET algo_interest = $1, algo_scored_at = $2 WHERE tweet_id = $3")
		if err != nil {
			return err
		}
		defer func() {
			_ = stmt.Close()
		}()
		for tweetID, score := range scores {
			if _, err := stmt.Exec(score, now, tweetID); err != nil {
				return err
			}
		}
		return nil
	})
}

// InvalidateAlgoScore resets algo_scored_at to 0 for the given tweet IDs,
// causing the scoring worker to recompute them on its next cycle.
func (db *DB) InvalidateAlgoScore(tweetIDs ...string) error {
	if len(tweetIDs) == 0 {
		return nil
	}
	return db.WithWrite(func(tx *sql.Tx) error {
		return invalidateAlgoScoreTx(context.Background(), tx, tweetIDs...)
	})
}

func invalidateAlgoScoreTx(ctx context.Context, tx *sql.Tx, tweetIDs ...string) error {
	if len(tweetIDs) == 0 {
		return nil
	}
	ph := strings.Repeat("?,", len(tweetIDs))
	ph = ph[:len(ph)-1]
	args := make([]any, len(tweetIDs))
	for i, id := range tweetIDs {
		args[i] = id
	}
	_, err := tx.ExecContext(ctx, bind("UPDATE feed_items SET algo_scored_at = 0 WHERE tweet_id IN ("+ph+")"), args...)
	return err
}

const feedWindowChannelCandidateLimit = 2000

func (db *DB) InvalidateFeedWindowByChannelID(channelID string) error {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil
	}
	return db.WithWrite(func(tx *sql.Tx) error {
		return invalidateFeedWindowByChannelIDTx(context.Background(), tx, channelID)
	})
}

func invalidateFeedWindowByChannelIDTx(ctx context.Context, tx *sql.Tx, channelID string) error {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil
	}
	query := fmt.Sprintf(`
		WITH candidates(tweet_id) AS MATERIALIZED (
			SELECT snapshot.tweet_id
			FROM feed_rank_snapshot snapshot
			JOIN feed_items fi ON fi.tweet_id = snapshot.tweet_id
			WHERE fi.channel_id = ?
			   OR fi.source_channel_id = ?
			   OR fi.reposter_channel_id = ?
			UNION
			SELECT tweet_id FROM (
				SELECT tweet_id
				FROM feed_items
				WHERE channel_id = ?
				  AND channel_id IS NOT NULL
				  AND channel_id != ''
				ORDER BY fetched_at DESC, published_at DESC, tweet_id DESC NULLS LAST
				LIMIT %d
			)
			UNION
			SELECT tweet_id FROM (
				SELECT tweet_id
				FROM feed_items
				WHERE source_channel_id = ?
				ORDER BY ingest_order DESC
				LIMIT %d
			)
			UNION
			SELECT tweet_id FROM (
				SELECT tweet_id
				FROM feed_items
				WHERE reposter_channel_id = ?
				  AND reposter_channel_id IS NOT NULL
				  AND reposter_channel_id != ''
				ORDER BY published_at DESC, tweet_id DESC NULLS LAST
				LIMIT %d
			)
		)
		UPDATE feed_items
		SET algo_scored_at = 0
		WHERE tweet_id IN (SELECT tweet_id FROM candidates)
	`, feedWindowChannelCandidateLimit, feedWindowChannelCandidateLimit, feedWindowChannelCandidateLimit)
	_, err := tx.ExecContext(ctx, bind(query),
		channelID, channelID, channelID,
		channelID, channelID, channelID,
	)
	return err
}
