package db

import (
	"database/sql"
	"strings"

	"github.com/screwys/igloo/internal/model"
)

const feedSearchCandidateLimit = 400

// compileFTSQuery converts a user query into FTS5 syntax.
// Wraps each term in quotes for prefix matching.
func compileFTSQuery(q string) string {
	terms := strings.Fields(strings.TrimSpace(q))
	if len(terms) == 0 {
		return ""
	}
	var parts []string
	for _, t := range terms {
		t = strings.ReplaceAll(t, `"`, `""`)
		parts = append(parts, `"`+t+`"*`)
	}
	return strings.Join(parts, " AND ")
}

// SearchChannels searches followed channels using the FTS5 index.
func (db *DB) SearchChannels(q string, limit int) ([]model.Channel, error) {
	fts := compileFTSQuery(q)
	if fts == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.conn.Query(`
		SELECT f.channel_id_pk,
		       COALESCE(NULLIF(cp.display_name, ''), NULLIF(c.name, ''), f.name) AS display_name,
		       COALESCE(NULLIF(c.channel_id, ''), f.channel_id_pk) AS channel_id,
		       COALESCE(c.source_id, '') AS source_id,
		       COALESCE(c.platform, 'youtube') AS platform,
		       CASE WHEN cs.channel_id IS NOT NULL THEN 1 ELSE 0 END AS is_starred,
		       COALESCE(cp.handle, '') AS handle,
		       COALESCE(cp.display_name, '') AS profile_display_name
		FROM search_channels_fts f
		JOIN channel_follows cf ON cf.channel_id = f.channel_id_pk
		LEFT JOIN channels c ON c.channel_id = f.channel_id_pk
		LEFT JOIN channel_stars cs ON cs.channel_id = f.channel_id_pk
		LEFT JOIN channel_profiles cp ON cp.channel_id = f.channel_id_pk AND cp.tombstone = 0
		WHERE search_channels_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, fts, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var channels []model.Channel
	for rows.Next() {
		var ch model.Channel
		var channelIDPK string
		if err := rows.Scan(&channelIDPK, &ch.Name, &ch.ChannelID, &ch.SourceID, &ch.Platform, &ch.IsStarred, &ch.Handle, &ch.DisplayName); err != nil {
			return nil, err
		}
		if ch.ChannelID == "" {
			ch.ChannelID = channelIDPK
		}
		ch.AvatarURL = "/api/media/avatar/" + ch.ChannelID
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// SearchVideosFast searches videos using FTS5 index.
func (db *DB) SearchVideosFast(q string, limit int) ([]model.Video, error) {
	fts := compileFTSQuery(q)
	if fts == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	rows, err := db.conn.Query(`
		SELECT f.video_id_pk,
		       COALESCE(v.title, f.title),
		       COALESCE(cp.display_name,''),
		       COALESCE(v.channel_id, ''),
		       COALESCE(c.platform, 'youtube'),
		       v.published_at,
		       COALESCE(v.is_temp, 0),
		       v.dearrow_title, v.dearrow_title_casual
		FROM search_videos_fts f
		LEFT JOIN videos v ON v.video_id = f.video_id_pk
		LEFT JOIN channels c ON c.channel_id = v.channel_id
		LEFT JOIN channel_profiles cp ON cp.channel_id = v.channel_id
		WHERE search_videos_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, fts, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var videos []model.Video
	for rows.Next() {
		var v model.Video
		var pubAt sql.NullInt64
		var isTemp bool
		if err := rows.Scan(&v.VideoID, &v.Title, &v.ChannelName, &v.ChannelID, &v.Platform, &pubAt, &isTemp,
			&v.DearrowTitle, &v.DearrowTitleCasual); err != nil {
			return nil, err
		}
		v.PublishedAt = millisToTimePtr(pubAt)
		v.IsTemp = isTemp
		v.ThumbnailURL = "/api/media/thumbnail/" + v.VideoID
		if v.ChannelID != "" {
			v.AvatarURL = "/api/media/avatar/" + v.ChannelID
		}
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

// SearchFeedItems searches X posts by body text, author handle, or display name.
func (db *DB) SearchFeedItems(q string, limit int) ([]model.FeedItem, error) {
	if limit <= 0 {
		limit = 50
	}
	like := "%" + q + "%"
	query := `
		SELECT ` + feedItemSelectSQL("feed_items") + `
		FROM feed_items_resolved AS feed_items
		WHERE body_text LIKE ? OR author_handle LIKE ? OR author_display_name LIKE ?
		ORDER BY published_at DESC, tweet_id DESC
		LIMIT ?`
	args := []any{like, like, like, limit}
	if feedSearchHasIndexedTerm(q) {
		if db.readTx == nil {
			var items []model.FeedItem
			err := db.WithReadSnapshot(func(snapshot *DB) error {
				var err error
				items, err = snapshot.SearchFeedItems(q, limit)
				return err
			})
			return items, err
		}
		candidates, err := db.reader().Query(`
			WITH matching_profiles AS MATERIALIZED (
				SELECT DISTINCT rowid FROM (
					SELECT rowid FROM search_profile_text_fts WHERE handle LIKE ?1
					UNION ALL
					SELECT rowid FROM search_profile_text_fts WHERE display_name LIKE ?2
				) LIMIT ?4
			), matching_posts AS (
				SELECT fi.tweet_id
				FROM search_feed_text_fts search
				JOIN feed_items fi ON fi.rowid = search.rowid
				WHERE search.body_text LIKE ?3
				UNION ALL
				SELECT NULL WHERE (SELECT COUNT(*) FROM matching_profiles) = ?4
				UNION ALL
				SELECT fi.tweet_id
				FROM matching_profiles matched
				CROSS JOIN channel_profiles cp ON cp.rowid = matched.rowid AND cp.tombstone = 0
				CROSS JOIN feed_items fi ON fi.channel_id = cp.channel_id
			)
			SELECT DISTINCT tweet_id FROM matching_posts LIMIT ?4
		`, like, like, like, feedSearchCandidateLimit)
		if err != nil {
			return nil, err
		}
		defer func() { _ = candidates.Close() }()
		var ids []string
		broadProfiles := false
		for candidates.Next() {
			var id sql.NullString
			if err := candidates.Scan(&id); err != nil {
				return nil, err
			}
			if !id.Valid {
				broadProfiles = true
				break
			}
			ids = append(ids, id.String)
		}
		if err := candidates.Err(); err != nil {
			return nil, err
		}
		if err := candidates.Close(); err != nil {
			return nil, err
		}
		if len(ids) == 0 && !broadProfiles {
			return nil, nil
		}
		// Broad matches use publication order's early stop instead of sorting
		// every index hit. The limit bounds query work, never search results.
		if !broadProfiles && len(ids) < feedSearchCandidateLimit {
			query = `SELECT ` + feedItemSelectSQL("feed_items") + `
				FROM feed_items_resolved AS feed_items
				WHERE tweet_id IN (` + placeholders(len(ids)) + `)
				ORDER BY published_at DESC, tweet_id DESC LIMIT ?`
			args = append(stringsToAny(ids), limit)
		}
	}
	rows, err := db.reader().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	return scanFeedItems(rows)
}

// LIKE needs three consecutive literal characters to use a trigram index.
func feedSearchHasIndexedTerm(query string) bool {
	literals := 0
	for _, char := range query {
		switch char {
		case 0:
			return false
		case '%', '_':
			literals = 0
		default:
			literals++
			if literals >= 3 {
				return true
			}
		}
	}
	return false
}
