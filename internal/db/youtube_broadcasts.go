package db

import (
	"database/sql"
	"slices"

	"github.com/screwys/igloo/internal/model"
)

const YouTubeBroadcastSchema = `CREATE TABLE IF NOT EXISTS youtube_broadcasts (
	channel_id TEXT NOT NULL,
	video_id TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	thumbnail_url TEXT NOT NULL DEFAULT '',
	live_status TEXT NOT NULL DEFAULT '',
	published_at_ms INTEGER NOT NULL DEFAULT 0,
	starts_at_ms INTEGER NOT NULL DEFAULT 0,
	concurrent_view_count INTEGER,
	observed_at_ms INTEGER NOT NULL,
	source_rank INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (channel_id, video_id)
)`

// ReplaceYouTubeBroadcasts commits one successful channel snapshot atomically.
func (db *DB) ReplaceYouTubeBroadcasts(channelID string, broadcasts []model.YouTubeBroadcast, observedAtMs int64) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM youtube_broadcasts WHERE channel_id = $1`, channelID); err != nil {
			return err
		}
		for _, broadcast := range broadcasts {
			if _, err := tx.Exec(`
				INSERT INTO youtube_broadcasts (
					channel_id, video_id, title, thumbnail_url, live_status, published_at_ms,
					starts_at_ms, concurrent_view_count, observed_at_ms, source_rank
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				ON CONFLICT (channel_id, video_id) DO NOTHING
			`, channelID, broadcast.VideoID, broadcast.Title, broadcast.ThumbnailURL,
				broadcast.LiveStatus, broadcast.PublishedAtMs, broadcast.StartsAtMs,
				broadcast.ConcurrentViewCount, observedAtMs, broadcast.SourceRank); err != nil {
				return err
			}
		}
		return nil
	})
}

// YouTubeBroadcastQuery selects broadcasts from followed channels.
type YouTubeBroadcastQuery struct {
	ChannelIDs  []string
	StarredOnly bool
	States      []string
	Order       string
	Limit       int
}

func (db *DB) ListYouTubeBroadcasts(opts YouTubeBroadcastQuery) ([]model.YouTubeBroadcast, error) {
	channelIDs, starredOnly, limit, states := opts.ChannelIDs, opts.StarredOnly, opts.Limit, opts.States
	if limit == 0 {
		limit = 24
	}
	query := `
		SELECT b.video_id, b.channel_id, b.title,
		       CASE WHEN EXISTS (
				SELECT 1 FROM assets a
				JOIN videos v ON v.video_id = b.video_id AND v.owner_kind = 'youtube_video'
				JOIN media_objects current ON current.object_id = a.object_id
				WHERE a.owner_kind = 'youtube_video' AND a.owner_id = b.video_id
				  AND a.asset_kind = 'post_thumbnail' AND current.published_revision > 0
				  AND a.lifecycle_state = 'active' AND current.file_path != ''
				  AND current.content_type LIKE 'image/%'
		       ) THEN '/api/media/thumbnail/' || b.video_id ELSE b.thumbnail_url END,
		       b.live_status, b.published_at_ms, b.starts_at_ms,
		       b.concurrent_view_count, b.observed_at_ms, b.source_rank
		FROM youtube_broadcasts b
		JOIN channel_follows cf ON cf.channel_id = b.channel_id
		WHERE 1 = 1`
	args := make([]any, 0, len(channelIDs)+1)
	if starredOnly {
		query += ` AND EXISTS (SELECT 1 FROM channel_stars stars WHERE stars.channel_id = b.channel_id)`
	}
	if len(channelIDs) > 0 {
		query += ` AND b.channel_id IN (` + placeholders(len(channelIDs)) + `)`
		args = append(args, stringsToAny(channelIDs)...)
	}
	if len(states) > 0 {
		query += ` AND (b.live_status IN (` + placeholders(len(states)) + `)`
		args = append(args, stringsToAny(states)...)
		if slices.Contains(states, "was_live") {
			query += ` OR b.live_status NOT IN ('is_live','is_upcoming')`
		}
		query += `)`
	}
	order := `
		ORDER BY CASE b.live_status WHEN 'is_live' THEN 0 WHEN 'is_upcoming' THEN 1 ELSE 2 END,
		         CASE WHEN b.live_status = 'is_live' THEN b.concurrent_view_count END DESC,
		         CASE WHEN b.live_status = 'is_upcoming' THEN NULLIF(b.starts_at_ms, 0) END ASC NULLS LAST,
		         b.published_at_ms DESC, b.source_rank, b.channel_id, b.video_id
		`
	if opts.Order == "account" {
		order = ` ORDER BY LOWER(COALESCE((SELECT NULLIF(cp.display_name,'') FROM channel_profiles cp WHERE cp.channel_id = b.channel_id),(SELECT c.name FROM channels c WHERE c.channel_id = b.channel_id),b.channel_id) COLLATE "C"), COALESCE(NULLIF(b.starts_at_ms,0),b.published_at_ms) DESC,b.source_rank,b.video_id`
	}
	if opts.Order == "newest" || opts.Order == "recent" {
		order = ` ORDER BY COALESCE(NULLIF(b.starts_at_ms,0),b.published_at_ms) DESC,b.source_rank,b.channel_id,b.video_id`
	}
	query += order + ` LIMIT ?`
	args = append(args, limit)
	rows, err := db.reader().Query(bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	broadcasts := []model.YouTubeBroadcast{}
	for rows.Next() {
		var broadcast model.YouTubeBroadcast
		var count sql.NullInt64
		if err := rows.Scan(&broadcast.VideoID, &broadcast.ChannelID, &broadcast.Title,
			&broadcast.ThumbnailURL, &broadcast.LiveStatus, &broadcast.PublishedAtMs,
			&broadcast.StartsAtMs, &count, &broadcast.ObservedAtMs, &broadcast.SourceRank); err != nil {
			return nil, err
		}
		if count.Valid {
			broadcast.ConcurrentViewCount = &count.Int64
		}
		broadcasts = append(broadcasts, broadcast)
	}
	return broadcasts, rows.Err()
}
