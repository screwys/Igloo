package db

import (
	"sort"

	"github.com/screwys/igloo/internal/model"
)

// AndroidSyncCommentAuthorAsset is a canonical commenter avatar selected for
// comments attached to a retained YouTube video.
type AndroidSyncCommentAuthorAsset struct {
	Asset     Asset
	RecencyMs int64
}

// ListAndroidSyncCommentAuthorAssets returns canonical comment-author avatars
// for the same top comments included in Android video payloads. Commenters stay
// out of channel/profile identity; callers may omit a row when the same owner
// already has a selected channel avatar.
func (db *DB) ListAndroidSyncCommentAuthorAssets(videoIDs []string, limitPerVideo int) ([]AndroidSyncCommentAuthorAsset, error) {
	if limitPerVideo <= 0 || len(videoIDs) == 0 {
		return nil, nil
	}

	recencyByOwner := make(map[string]int64)
	for _, chunk := range stringChunks(videoIDs, 400) {
		args := make([]any, 0, len(chunk)+1)
		for _, id := range chunk {
			args = append(args, id)
		}
		args = append(args, limitPerVideo)
		rows, err := db.reader().Query(`
			WITH desired(video_id) AS (
				SELECT DISTINCT column1 FROM (VALUES `+androidSyncProjectionValues(len(chunk))+`)
			)
			SELECT COALESCE(vc.author_id, ''),
			       COALESCE(NULLIF(vc.published_at, 0), NULLIF(v.published_at, 0), 0)
			FROM desired d
			JOIN videos v ON v.video_id = d.video_id
			`+androidSyncTopCommentsJoinSQL+`
			WHERE v.channel_id ILIKE 'youtube_%'
		`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var authorID string
			var recencyMs int64
			if err := rows.Scan(&authorID, &recencyMs); err != nil {
				_ = rows.Close()
				return nil, err
			}
			ownerID := model.YouTubeCommentAuthorChannelID(authorID)
			if ownerID == "" {
				continue
			}
			if current, ok := recencyByOwner[ownerID]; !ok || recencyMs > current {
				recencyByOwner[ownerID] = recencyMs
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	if len(recencyByOwner) == 0 {
		return nil, nil
	}

	owners := make([]string, 0, len(recencyByOwner))
	for ownerID := range recencyByOwner {
		owners = append(owners, ownerID)
	}
	sort.Strings(owners)

	var out []AndroidSyncCommentAuthorAsset
	for _, chunk := range stringChunks(owners, 400) {
		args := make([]any, 0, len(chunk)+2)
		args = append(args, "avatar", "comment_author")
		for _, ownerID := range chunk {
			args = append(args, ownerID)
		}
		rows, err := db.reader().Query(`SELECT `+assetProjectionSQL+assetJoinsSQL+`
				WHERE a.asset_kind = ?
				  AND a.owner_kind = ?
				  AND a.lifecycle_state != 'pruned'
				  AND a.owner_id IN (`+placeholders(len(chunk))+`)
		`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			asset, err := scanAsset(rows)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			out = append(out, AndroidSyncCommentAuthorAsset{
				Asset:     asset,
				RecencyMs: recencyByOwner[asset.OwnerID],
			})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].RecencyMs != out[j].RecencyMs {
			return out[i].RecencyMs > out[j].RecencyMs
		}
		return out[i].Asset.AssetID < out[j].Asset.AssetID
	})
	return out, nil
}
