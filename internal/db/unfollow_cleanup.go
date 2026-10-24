package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbquery "github.com/screwys/igloo/internal/db/query"
)

const unfollowCleanupBatchSize = 16
const unfollowCleanupWriteTimeout = 250 * time.Millisecond

var errUnfollowCleanupChanged = errors.New("unfollow cleanup ownership changed")

type unfollowCleanupPlan struct {
	channelID   string
	requestedAt int64
	revision    int64
	following   bool
	tweetIDs    []string
	videoIDs    []string
}

// ProcessUnfollowCleanup resumes one persisted unfollow. Reads and file I/O
// run outside the write lock; each write handles at most one small batch.
func (db *DB) ProcessUnfollowCleanup(ctx context.Context) (bool, error) {
	files, err := db.queries().NextUnfollowCleanupFiles(ctx, unfollowCleanupBatchSize)
	if err != nil {
		return false, err
	}
	for _, key := range files {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if _, err := db.removeAssetFileIfUnreferencedContext(ctx, key); err != nil {
			return false, err
		}
		if err := db.writeUnfollowCleanup(ctx, func(ctx context.Context, tx *sql.Tx) error {
			return dbquery.New(tx).FinishUnfollowCleanupFile(ctx, key)
		}); err != nil {
			return false, err
		}
	}
	job, err := db.queries().NextUnfollowCleanup(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return len(files) > 0, nil
	}
	if err != nil {
		return false, err
	}
	plan, err := db.planUnfollowCleanup(ctx, job.ChannelID, job.RequestedAtMs)
	if err == nil {
		err = db.applyUnfollowCleanup(ctx, plan)
	}
	if errors.Is(err, errUnfollowCleanupChanged) {
		// A save, follow, or capture won the race. Retry from current data.
		return false, nil
	}
	return err == nil, err
}

func (db *DB) planUnfollowCleanup(ctx context.Context, channelID string, requestedAt int64) (*unfollowCleanupPlan, error) {
	tx, err := db.conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	plan := &unfollowCleanupPlan{channelID: channelID, requestedAt: requestedAt}
	q := dbquery.New(tx)
	plan.revision, err = q.UnfollowCleanupRevision(ctx)
	if err != nil {
		return nil, err
	}
	plan.following, err = q.UnfollowCleanupChannelIsFollowed(ctx, sql.NullString{String: channelID, Valid: true})
	if err != nil {
		return nil, err
	}
	if !plan.following {
		if strings.HasPrefix(strings.ToLower(channelID), "twitter_") || strings.HasPrefix(strings.ToLower(channelID), "x_") {
			rows, err := tx.QueryContext(ctx, bind(unreferencedXContentIDsQuery()), channelID, channelID, channelID, channelID)
			if err != nil {
				return nil, err
			}
			plan.tweetIDs, err = readUnfollowCleanupIDs(rows)
			if err != nil {
				return nil, err
			}
		} else {
			platform, err := q.UnfollowCleanupPlatform(ctx, channelID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			if platform == "youtube" || platform == "tiktok" || platform == "instagram" {
				plan.videoIDs, err = q.UnfollowVideoCleanupItems(ctx, channelID)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return plan, tx.Commit()
}

func readUnfollowCleanupIDs(rows *sql.Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (db *DB) writeUnfollowCleanup(ctx context.Context, fn func(context.Context, *sql.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, unfollowCleanupWriteTimeout)
	defer cancel()
	return db.withWriteContext(ctx, func(tx *sql.Tx) error {
		return fn(ctx, tx)
	})
}

func (db *DB) writeUnfollowCleanupPlan(ctx context.Context, plan *unfollowCleanupPlan, fn func(context.Context, *sql.Tx) error) error {
	var nextRevision int64
	err := db.writeUnfollowCleanup(ctx, func(ctx context.Context, tx *sql.Tx) error {
		q := dbquery.New(tx)
		revision, err := q.LockUnfollowCleanupRevision(ctx)
		if err != nil {
			return err
		}
		if revision != plan.revision {
			return errUnfollowCleanupChanged
		}
		current, err := q.UnfollowCleanupJobIsCurrent(ctx, dbquery.UnfollowCleanupJobIsCurrentParams{
			ChannelID: plan.channelID, RequestedAtMs: plan.requestedAt,
		})
		if err != nil {
			return err
		}
		if !current {
			return errUnfollowCleanupChanged
		}
		if err := fn(ctx, tx); err != nil {
			return err
		}
		nextRevision, err = q.UnfollowCleanupRevision(ctx)
		return err
	})
	if err == nil {
		plan.revision = nextRevision
	}
	return err
}

func (db *DB) applyUnfollowCleanup(ctx context.Context, plan *unfollowCleanupPlan) error {
	// Save every candidate before deleting any. Dependencies must remain
	// discoverable after their original source row is deleted or a restart.
	for _, owners := range []struct {
		kind string
		ids  []string
	}{{"tweet", plan.tweetIDs}, {"video", plan.videoIDs}} {
		for _, chunk := range stringChunks(owners.ids, unfollowCleanupBatchSize) {
			if err := db.writeUnfollowCleanupPlan(ctx, plan, func(ctx context.Context, tx *sql.Tx) error {
				for _, id := range chunk {
					if err := dbquery.New(tx).QueueUnfollowCleanupItem(ctx, dbquery.QueueUnfollowCleanupItemParams{
						ChannelID: plan.channelID, OwnerKind: owners.kind, OwnerID: id,
					}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
	}
	if !plan.following {
		for {
			var removed int64
			if err := db.writeUnfollowCleanupPlan(ctx, plan, func(ctx context.Context, tx *sql.Tx) error {
				var err error
				q := dbquery.New(tx)
				removed, err = q.RetireUnfollowReposts(ctx, dbquery.RetireUnfollowRepostsParams{RetweeterChannelID: plan.channelID, Limit: unfollowCleanupBatchSize})
				return err
			}); err != nil {
				return err
			}
			if removed == 0 {
				break
			}
		}
		for {
			var removed int64
			if err := db.writeUnfollowCleanupPlan(ctx, plan, func(ctx context.Context, tx *sql.Tx) error {
				q := dbquery.New(tx)
				n, err := q.RetireUnfollowVideoDesires(ctx, dbquery.RetireUnfollowVideoDesiresParams{SourceChannelID: plan.channelID, Limit: unfollowCleanupBatchSize})
				if err != nil {
					return err
				}
				removed = n
				n, err = q.RetireUnfollowVideoReposts(ctx, dbquery.RetireUnfollowVideoRepostsParams{ReposterChannelID: plan.channelID, Limit: unfollowCleanupBatchSize})
				removed += n
				return err
			}); err != nil {
				return err
			}
			if removed == 0 {
				break
			}
		}
		for _, chunk := range stringChunks(plan.tweetIDs, unfollowCleanupBatchSize) {
			if err := db.writeUnfollowCleanupPlan(ctx, plan, func(ctx context.Context, tx *sql.Tx) error {
				ownerIDs, err := json.Marshal(chunk)
				if err != nil {
					return err
				}
				if err := dbquery.New(tx).QueueUnfollowTweetFiles(ctx, ownerIDs); err != nil {
					return err
				}
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
					if _, err := tx.ExecContext(ctx, bind(statement), args...); err != nil {
						return err
					}
				}
				return dbquery.New(tx).FinishUnfollowCleanupItems(ctx, dbquery.FinishUnfollowCleanupItemsParams{
					ChannelID: plan.channelID, OwnerKind: "tweet", OwnerIds: ownerIDs,
				})
			}); err != nil {
				return err
			}
		}
		for _, chunk := range stringChunks(plan.videoIDs, unfollowCleanupBatchSize) {
			if err := db.writeUnfollowCleanupPlan(ctx, plan, func(ctx context.Context, tx *sql.Tx) error {
				return collectUnfollowVideoBatch(ctx, tx, plan.channelID, chunk)
			}); err != nil {
				return err
			}
		}
	}
	// Discard candidates that became protected, or the whole job after refollow.
	for {
		var removed int64
		if err := db.writeUnfollowCleanupPlan(ctx, plan, func(ctx context.Context, tx *sql.Tx) error {
			var err error
			removed, err = dbquery.New(tx).DiscardUnfollowCleanupItems(ctx, dbquery.DiscardUnfollowCleanupItemsParams{ChannelID: plan.channelID, Limit: unfollowCleanupBatchSize})
			return err
		}); err != nil {
			return err
		}
		if removed == 0 {
			break
		}
	}
	return db.writeUnfollowCleanupPlan(ctx, plan, func(ctx context.Context, tx *sql.Tx) error {
		return dbquery.New(tx).FinishUnfollowCleanup(ctx, dbquery.FinishUnfollowCleanupParams{ChannelID: plan.channelID, RequestedAtMs: plan.requestedAt})
	})
}

func collectUnfollowVideoBatch(ctx context.Context, tx *sql.Tx, channelID string, ids []string) error {
	args := append([]any{time.Now().UnixMilli()}, stringsToAny(ids)...)
	rows, err := tx.QueryContext(ctx, bind(`SELECT v.video_id FROM videos v WHERE `+collectibleVideoWhereSQL+` AND v.video_id IN (`+placeholders(len(ids))+`)`), args...)
	if err != nil {
		return err
	}
	collectible, err := readUnfollowCleanupIDs(rows)
	if err != nil {
		return err
	}
	q := dbquery.New(tx)
	if len(collectible) > 0 {
		ownerIDs, err := json.Marshal(collectible)
		if err != nil {
			return err
		}
		if err := q.QueueUnfollowVideoFiles(ctx, ownerIDs); err != nil {
			return err
		}
		if err := q.DeleteUnfollowVideoAssets(ctx, ownerIDs); err != nil {
			return err
		}
		for _, table := range []string{"download_queue", "videos"} {
			if _, err := tx.ExecContext(ctx, bind(`DELETE FROM `+table+` WHERE video_id IN (`+placeholders(len(collectible))+`)`), stringsToAny(collectible)...); err != nil {
				return err
			}
		}
	}
	ownerIDs, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return q.FinishUnfollowCleanupItems(ctx, dbquery.FinishUnfollowCleanupItemsParams{ChannelID: channelID, OwnerKind: "video", OwnerIds: ownerIDs})
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
		    updated_at_ms = $1
		WHERE channel_id = $2
		  AND requested_revision > completed_revision
	`, nowMs, channelID)
	return err
}

func unreferencedXContentIDsQuery() string {
	rootPredicate := feedActiveOwnerPredicate("fi") + `
		OR EXISTS (SELECT 1 FROM bookmarks b WHERE b.video_id = fi.tweet_id)
		OR EXISTS (SELECT 1 FROM feed_likes fl WHERE fl.tweet_id = fi.tweet_id)
		OR EXISTS (
		    SELECT 1 FROM videos v
		    WHERE v.video_id = fi.tweet_id AND COALESCE(v.is_pinned, 0) = 1
		)`
	// A retained post cannot lose its dependencies through this unfollow. Keep
	// it as a root, but do not walk through it into unrelated conversations.
	return fmt.Sprintf(`
		WITH RECURSIVE seeds(tweet_id) AS (
			SELECT fi.tweet_id
			FROM feed_items fi
			WHERE fi.source_channel_id = ?
			UNION
			SELECT fi.tweet_id
			FROM feed_items fi
			WHERE fi.channel_id = ?
			UNION
			SELECT fi.tweet_id
			FROM feed_items fi
			WHERE fi.reposter_channel_id IS NOT NULL
			  AND fi.reposter_channel_id != ''
			  AND fi.reposter_channel_id = ?
			UNION
			SELECT owner_id FROM unfollow_cleanup_items
			WHERE channel_id = ? AND owner_kind = 'tweet'
		), affected(tweet_id) AS (
			SELECT tweet_id FROM seeds
			UNION
			SELECT neighbor.tweet_id
			FROM affected a
			JOIN feed_items fi ON fi.tweet_id = a.tweet_id
			CROSS JOIN LATERAL (
				SELECT fi.canonical_tweet_id AS tweet_id
				FROM feed_items fi WHERE fi.tweet_id = a.tweet_id
				  AND fi.canonical_tweet_id IS NOT NULL AND fi.canonical_tweet_id != ''
				UNION
				SELECT fi.quote_tweet_id
				FROM feed_items fi WHERE fi.tweet_id = a.tweet_id
				  AND fi.quote_tweet_id IS NOT NULL AND fi.quote_tweet_id != ''
				UNION
				SELECT fi.reply_to_status
				FROM feed_items fi WHERE fi.tweet_id = a.tweet_id
				  AND fi.reply_to_status IS NOT NULL AND fi.reply_to_status != ''
				UNION
				SELECT fi.tweet_id FROM feed_items fi
				WHERE fi.canonical_tweet_id = a.tweet_id
				  AND fi.canonical_tweet_id IS NOT NULL AND fi.canonical_tweet_id != ''
				UNION
				SELECT fi.tweet_id FROM feed_items fi
				WHERE fi.quote_tweet_id = a.tweet_id
				  AND fi.quote_tweet_id IS NOT NULL AND fi.quote_tweet_id != ''
				UNION
				SELECT fi.tweet_id FROM feed_items fi
				WHERE fi.reply_to_status = a.tweet_id
				  AND fi.reply_to_status IS NOT NULL AND fi.reply_to_status != ''
				UNION
				SELECT peer.tweet_id
				FROM feed_items current
				JOIN feed_items peer ON peer.content_hash = current.content_hash
				WHERE current.tweet_id = a.tweet_id
				  AND current.content_hash IS NOT NULL AND current.content_hash != ''
				  AND peer.content_hash IS NOT NULL AND peer.content_hash != ''
			) neighbor
			WHERE NOT (%s)
		), roots(tweet_id) AS (
			SELECT fi.tweet_id
			FROM feed_items fi JOIN affected a ON a.tweet_id = fi.tweet_id
			WHERE %s
		), retained(tweet_id) AS (
			SELECT tweet_id FROM roots
			UNION
			SELECT dependency.tweet_id
			FROM retained r
			CROSS JOIN LATERAL (
				SELECT fi.canonical_tweet_id AS tweet_id
				FROM feed_items fi WHERE fi.tweet_id = r.tweet_id
				  AND fi.canonical_tweet_id IS NOT NULL AND fi.canonical_tweet_id != ''
				UNION
				SELECT fi.quote_tweet_id
				FROM feed_items fi WHERE fi.tweet_id = r.tweet_id
				  AND fi.quote_tweet_id IS NOT NULL AND fi.quote_tweet_id != ''
				UNION
				SELECT fi.reply_to_status
				FROM feed_items fi WHERE fi.tweet_id = r.tweet_id
				  AND fi.reply_to_status IS NOT NULL AND fi.reply_to_status != ''
			) dependency
			JOIN affected a ON a.tweet_id = dependency.tweet_id
		)
		SELECT a.tweet_id
		FROM affected a
		WHERE EXISTS (SELECT 1 FROM feed_items fi WHERE fi.tweet_id = a.tweet_id)
		  AND NOT EXISTS (SELECT 1 FROM retained r WHERE r.tweet_id = a.tweet_id)
		ORDER BY a.tweet_id
	`, rootPredicate, rootPredicate)
}
