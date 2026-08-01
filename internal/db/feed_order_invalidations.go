package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const (
	feedOrderInvalidationTweetBatch      = 500
	feedOrderInvalidationCoarseThreshold = 1024
)

type feedOrderInvalidation struct {
	ownerKind string
	ownerID   string
}

func feedOrderOwnerKindForMutation(kind string) string {
	switch kind {
	case "like", "bookmark":
		return "tweet"
	case "follow", "star", "mute":
		return "channel"
	default:
		return ""
	}
}

func enqueueFeedOrderInvalidationForMutationTx(tx *sql.Tx, kind, itemKey string) error {
	return enqueueFeedOrderInvalidationTx(tx, feedOrderOwnerKindForMutation(kind), itemKey)
}

func enqueueFeedOrderInvalidationTx(tx *sql.Tx, ownerKind, ownerID string) error {
	ownerID = strings.TrimSpace(ownerID)
	if ownerKind == "" || ownerID == "" {
		return nil
	}
	_, err := tx.Exec(`
		INSERT INTO feed_order_invalidations (owner_kind, owner_id)
		VALUES (?, ?)
		ON CONFLICT(owner_kind, owner_id) DO NOTHING
	`, ownerKind, ownerID)
	return err
}

func enqueueFeedOrderInvalidationsForMutationQueryTx(
	tx *sql.Tx,
	kind string,
	itemsQuery string,
	args ...any,
) error {
	ownerKind := feedOrderOwnerKindForMutation(kind)
	if ownerKind == "" {
		return nil
	}
	query := `
		INSERT INTO feed_order_invalidations (owner_kind, owner_id)
		SELECT ?, TRIM(item_key)
		FROM (` + itemsQuery + `) AS mutation_items
		WHERE TRIM(item_key) != ''
		ON CONFLICT(owner_kind, owner_id) DO NOTHING
	`
	queryArgs := make([]any, 0, len(args)+1)
	queryArgs = append(queryArgs, ownerKind)
	queryArgs = append(queryArgs, args...)
	_, err := tx.Exec(query, queryArgs...)
	return err
}

// DrainFeedOrderInvalidations applies and removes one bounded queue batch in
// the same transaction. A crash therefore leaves either the complete batch or
// none of it committed. Large backlogs collapse to one global invalidation.
func (db *DB) DrainFeedOrderInvalidations(
	ctx context.Context,
	tweetLimit int,
	channelLimit int,
) (int, error) {
	if tweetLimit <= 0 || channelLimit <= 0 {
		return 0, fmt.Errorf("feed-order invalidation limits must be positive")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	processed := 0
	err := db.WithWrite(func(tx *sql.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var queued int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM (
				SELECT 1 FROM feed_order_invalidations LIMIT ?
			)
		`, feedOrderInvalidationCoarseThreshold+1).Scan(&queued); err != nil {
			return err
		}
		if queued == 0 {
			return nil
		}
		if queued > feedOrderInvalidationCoarseThreshold {
			if _, err := tx.ExecContext(ctx, `
				UPDATE feed_items SET algo_scored_at = 0
				WHERE algo_scored_at != 0
			`); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM feed_order_invalidations`); err != nil {
				return err
			}
			processed = queued
			return nil
		}

		rows, err := tx.QueryContext(ctx, `
			WITH tweet_batch AS (
				SELECT owner_kind, owner_id
				FROM feed_order_invalidations
				WHERE owner_kind = 'tweet'
				ORDER BY owner_id
				LIMIT ?
			), channel_batch AS (
				SELECT owner_kind, owner_id
				FROM feed_order_invalidations
				WHERE owner_kind = 'channel'
				ORDER BY owner_id
				LIMIT ?
			)
			SELECT owner_kind, owner_id
			FROM (
				SELECT owner_kind, owner_id, 0 AS lane FROM tweet_batch
				UNION ALL
				SELECT owner_kind, owner_id, 1 AS lane FROM channel_batch
			)
			ORDER BY lane, owner_id
		`, tweetLimit, channelLimit)
		if err != nil {
			return err
		}
		invalidations := make([]feedOrderInvalidation, 0, tweetLimit+channelLimit)
		for rows.Next() {
			var invalidation feedOrderInvalidation
			if err := rows.Scan(&invalidation.ownerKind, &invalidation.ownerID); err != nil {
				_ = rows.Close()
				return err
			}
			invalidations = append(invalidations, invalidation)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(invalidations) == 0 {
			return nil
		}

		tweetIDs := make([]string, 0, len(invalidations))
		channelIDs := make([]string, 0, channelLimit)
		for _, invalidation := range invalidations {
			switch invalidation.ownerKind {
			case "channel":
				channelIDs = append(channelIDs, invalidation.ownerID)
			case "tweet":
				tweetIDs = append(tweetIDs, invalidation.ownerID)
			default:
				return fmt.Errorf("unknown feed-order invalidation owner %q", invalidation.ownerKind)
			}
		}
		for start := 0; start < len(tweetIDs); start += feedOrderInvalidationTweetBatch {
			if err := ctx.Err(); err != nil {
				return err
			}
			end := min(start+feedOrderInvalidationTweetBatch, len(tweetIDs))
			if err := invalidateAlgoScoreTx(ctx, tx, tweetIDs[start:end]...); err != nil {
				return err
			}
		}
		for _, channelID := range channelIDs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := invalidateFeedWindowByChannelIDTx(ctx, tx, channelID); err != nil {
				return err
			}
		}

		deleteStatement, err := tx.PrepareContext(ctx, `
			DELETE FROM feed_order_invalidations
			WHERE owner_kind = ? AND owner_id = ?
		`)
		if err != nil {
			return err
		}
		defer func() { _ = deleteStatement.Close() }()
		for _, invalidation := range invalidations {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := deleteStatement.ExecContext(ctx, invalidation.ownerKind, invalidation.ownerID); err != nil {
				return err
			}
		}
		processed = len(invalidations)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return processed, nil
}
