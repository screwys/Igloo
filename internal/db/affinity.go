package db

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

const StateAccountScoresRefreshedAtSetting = "feed_state_account_scores_refreshed_at_ms"
const StateAccountScoresRefreshInterval = 24 * time.Hour

// AffinityRow holds a single affinity score with timing info.
type AffinityRow struct {
	Score       float64
	LastEventMs int64
	EventCount  int
}

// GetAccountAffinityScores reads share-based account affinity.
func (db *DB) GetAccountAffinityScores(handles []string) (map[string]AffinityRow, error) {
	if len(handles) == 0 {
		return nil, nil
	}
	result := make(map[string]AffinityRow)
	if err := db.queryAffinityTable("feed_share_account_affinity", "handle", handles, result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetTokenAffinityScores reads share-based token affinity.
func (db *DB) GetTokenAffinityScores(tokens []string) (map[string]AffinityRow, error) {
	if len(tokens) == 0 {
		return nil, nil
	}
	result := make(map[string]AffinityRow)
	if err := db.queryAffinityTable("feed_share_token_affinity", "token", tokens, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (db *DB) queryAffinityTable(table, keyCol string, keys []string, result map[string]AffinityRow) error {
	placeholders := make([]byte, 0, len(keys)*2)
	args := make([]any, 0, len(keys))
	for i, k := range keys {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, k)
	}

	query := "SELECT " + keyCol + ", COALESCE(score,0), COALESCE(last_event_at_ms,0), COALESCE(event_count,0) " +
		"FROM " + table + " WHERE " + keyCol + " IN (" + string(placeholders) + ")"

	rows, err := db.conn.Query(bind(query), args...)
	if err != nil {
		return nil // A missing affinity table leaves scores empty.
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var key string
		var row AffinityRow
		if err := rows.Scan(&key, &row.Score, &row.LastEventMs, &row.EventCount); err != nil {
			continue
		}
		existing := result[key]
		existing.Score += row.Score
		if row.LastEventMs > existing.LastEventMs {
			existing.LastEventMs = row.LastEventMs
		}
		existing.EventCount += row.EventCount
		result[key] = existing
	}
	return rows.Err()
}

// UpsertShareAccountAffinity updates the share-based account affinity score.
func (db *DB) UpsertShareAccountAffinity(handle string, scoreDelta float64, eventAtMs int64) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO feed_share_account_affinity (handle, score, last_event_at_ms, event_count)
			VALUES ($1, $2, $3, 1)
			ON CONFLICT(handle) DO UPDATE SET
				score = feed_share_account_affinity.score + excluded.score,
				last_event_at_ms = CASE WHEN feed_share_account_affinity.last_event_at_ms IS NULL THEN NULL
					ELSE GREATEST(feed_share_account_affinity.last_event_at_ms, excluded.last_event_at_ms) END,
				event_count = feed_share_account_affinity.event_count + 1
		`, handle, scoreDelta, eventAtMs)
		return err
	})
}

// UpsertShareTokenAffinity updates the share-based token affinity score.
func (db *DB) UpsertShareTokenAffinity(token string, scoreDelta float64, eventAtMs int64) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO feed_share_token_affinity (token, score, last_event_at_ms, event_count)
			VALUES ($1, $2, $3, 1)
			ON CONFLICT(token) DO UPDATE SET
				score = feed_share_token_affinity.score + excluded.score,
				last_event_at_ms = CASE WHEN feed_share_token_affinity.last_event_at_ms IS NULL THEN NULL
					ELSE GREATEST(feed_share_token_affinity.last_event_at_ms, excluded.last_event_at_ms) END,
				event_count = feed_share_token_affinity.event_count + 1
		`, token, scoreDelta, eventAtMs)
		return err
	})
}

// PruneShareTokenAffinity keeps only the top N token affinities.
func (db *DB) PruneShareTokenAffinity(keepTop int) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			DELETE FROM feed_share_token_affinity
			WHERE token NOT IN (
				SELECT token FROM feed_share_token_affinity
				ORDER BY score DESC NULLS LAST
				LIMIT $1
			)
		`, keepTop)
		return err
	})
}

// GetStateAccountScores reads the daily saved-state scores for a scoring batch.
func (db *DB) GetStateAccountScores(handles []string) (map[string]float64, error) {
	if len(handles) == 0 {
		return nil, nil
	}
	rows, err := db.conn.Query(bind(`
		SELECT handle, score
		FROM feed_state_account_scores
		WHERE handle IN (`+placeholders(len(handles))+`)
	`), stringsToAny(handles)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	accountScores := make(map[string]float64, len(handles))
	for rows.Next() {
		var handle string
		var score float64
		if err := rows.Scan(&handle, &score); err != nil {
			return nil, err
		}
		accountScores[handle] = score
	}
	return accountScores, rows.Err()
}

// RefreshStateAccountScores replaces the daily saved-state scores only when due.
// The score rows and completion time commit together, so restarts reuse them.
func (db *DB) RefreshStateAccountScores(ctx context.Context, nowMs int64) (bool, error) {
	refreshed := false
	err := db.WithWrite(func(tx *sql.Tx) error {
		var raw string
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE((SELECT value FROM settings WHERE key = $1), '0')
		`, StateAccountScoresRefreshedAtSetting).Scan(&raw); err != nil {
			return err
		}
		lastRefresh, _ := strconv.ParseInt(raw, 10, 64)
		if lastRefresh > 0 && nowMs-lastRefresh < StateAccountScoresRefreshInterval.Milliseconds() {
			return nil
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM feed_state_account_scores"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			WITH saved AS MATERIALIZED (
				SELECT fi.channel_id, 1 AS weight
				FROM feed_likes likes
				JOIN feed_items fi ON fi.tweet_id = likes.tweet_id
				UNION ALL
				SELECT fi.source_channel_id, 2 AS weight
				FROM bookmarks
				JOIN feed_items fi ON fi.tweet_id = bookmarks.video_id
			), accounts AS MATERIALIZED (
				SELECT channel_id, SUM(weight) AS score
				FROM saved
				GROUP BY channel_id
			)
			INSERT INTO feed_state_account_scores (handle, score)
			SELECT LOWER(profile.handle), SUM(accounts.score)::DOUBLE PRECISION
			FROM accounts
			JOIN channel_profiles profile ON profile.channel_id = accounts.channel_id
			WHERE profile.tombstone = 0 AND COALESCE(profile.handle, '') <> ''
			GROUP BY LOWER(profile.handle)
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE feed_items fi SET algo_scored_at = 0
			FROM feed_rank_snapshot snapshot
			WHERE snapshot.tweet_id = fi.tweet_id AND fi.algo_scored_at <> 0
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings (key, value) VALUES ($1, $2)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
		`, StateAccountScoresRefreshedAtSetting, strconv.FormatInt(nowMs, 10)); err != nil {
			return err
		}
		refreshed = true
		return nil
	})
	return refreshed && err == nil, err
}

// FindSiblingTweetIDsForLikes finds sibling tweet IDs (same content hash)
// for retweet->original like propagation.
func (db *DB) FindSiblingTweetIDsForLikes(tweetIDs []string) (map[string][]string, error) {
	if len(tweetIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]byte, 0, len(tweetIDs)*2)
	args := make([]any, 0, len(tweetIDs))
	for i, id := range tweetIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}

	hashRows, err := db.conn.Query(
		bind("SELECT tweet_id, content_hash FROM feed_items WHERE tweet_id IN ("+string(placeholders)+") AND content_hash IS NOT NULL AND content_hash <> ''"),
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = hashRows.Close()
	}()

	tweetToHash := make(map[string]string)
	hashSet := make(map[string]bool)
	for hashRows.Next() {
		var tid, hash string
		_ = hashRows.Scan(&tid, &hash)
		tweetToHash[tid] = hash
		hashSet[hash] = true
	}
	if err := hashRows.Err(); err != nil {
		return nil, err
	}

	if len(hashSet) == 0 {
		return nil, nil
	}

	hashArgs := make([]any, 0, len(hashSet))
	hashPH := make([]byte, 0, len(hashSet)*2)
	first := true
	for h := range hashSet {
		if !first {
			hashPH = append(hashPH, ',')
		}
		hashPH = append(hashPH, '?')
		hashArgs = append(hashArgs, h)
		first = false
	}

	sibRows, err := db.conn.Query(
		bind("SELECT tweet_id, content_hash FROM feed_items WHERE content_hash IN ("+string(hashPH)+") AND content_hash IS NOT NULL AND content_hash <> ''"),
		hashArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = sibRows.Close()
	}()

	hashToTweets := make(map[string][]string)
	for sibRows.Next() {
		var tid, hash string
		_ = sibRows.Scan(&tid, &hash)
		hashToTweets[hash] = append(hashToTweets[hash], tid)
	}
	if err := sibRows.Err(); err != nil {
		return nil, err
	}

	result := make(map[string][]string)
	for tid, hash := range tweetToHash {
		siblings := hashToTweets[hash]
		if len(siblings) > 1 {
			var others []string
			for _, s := range siblings {
				if s != tid {
					others = append(others, s)
				}
			}
			if len(others) > 0 {
				result[tid] = others
			}
		}
	}
	return result, nil
}
