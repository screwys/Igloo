package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	dbquery "github.com/screwys/igloo/internal/db/query"
)

const identityMediaBatchSize = 64

// Captures stamp a sighting before taking the write lock, so content sighted
// shortly before planning may be missing from the plan's snapshot.
const identityMediaSightingGrace = time.Hour

// PruneUnusedIdentityMedia removes avatars and banners that no retained
// content, user state, or current recommendation shows. Profiles keep their
// metadata; dropping the finished profile job makes the next sighting fetch
// the media again. Files are removed by the unfollow cleanup file queue.
func (db *DB) PruneUnusedIdentityMedia(ctx context.Context) (int, error) {
	seenAfter := time.Now().Add(-identityMediaSightingGrace).UnixMilli()
	channels, err := db.queries().UnusedChannelMediaOwners(ctx)
	if err != nil {
		return 0, err
	}
	authors, err := db.queries().UnusedCommentAuthorMediaOwners(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, chunk := range stringChunks(channels, identityMediaBatchSize) {
		n, err := db.pruneIdentityMediaBatch(ctx, "channel", chunk, seenAfter)
		removed += n
		if err != nil {
			return removed, err
		}
	}
	for _, chunk := range stringChunks(authors, identityMediaBatchSize) {
		n, err := db.pruneIdentityMediaBatch(ctx, "comment_author", chunk, seenAfter)
		removed += n
		if err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func (db *DB) pruneIdentityMediaBatch(ctx context.Context, ownerKind string, ownerIDs []string, seenAfter int64) (int, error) {
	owners, err := json.Marshal(ownerIDs)
	if err != nil {
		return 0, err
	}
	removed := 0
	err = db.withWriteContext(ctx, func(tx *sql.Tx) error {
		q := dbquery.New(tx)
		objectIDs, profileIDs := []string{}, []string{}
		if ownerKind == "channel" {
			rows, err := q.DeleteUnusedChannelMedia(ctx, dbquery.DeleteUnusedChannelMediaParams{
				ChannelIds: owners, SeenAfterMs: seenAfter,
			})
			if err != nil {
				return err
			}
			for _, row := range rows {
				objectIDs = append(objectIDs, row.ObjectID, row.DesiredObjectID)
				profileIDs = append(profileIDs, row.OwnerID)
			}
		} else {
			rows, err := q.DeleteUnusedCommentAuthorMedia(ctx, owners)
			if err != nil {
				return err
			}
			for _, row := range rows {
				objectIDs = append(objectIDs, row.ObjectID, row.DesiredObjectID)
			}
		}
		if len(objectIDs) == 0 {
			return nil
		}
		removed = len(objectIDs) / 2
		objects, err := json.Marshal(objectIDs)
		if err != nil {
			return err
		}
		profiles, err := json.Marshal(profileIDs)
		if err != nil {
			return err
		}
		if err := q.QueueIdentityMediaFiles(ctx, objects); err != nil {
			return err
		}
		if err := q.DeleteIdentityMediaObjects(ctx, objects); err != nil {
			return err
		}
		return q.DeleteIdentityProfileJobs(ctx, profiles)
	})
	return removed, err
}
