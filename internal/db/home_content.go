package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/home"
	"github.com/screwys/igloo/internal/model"
)

func homeValuesSQL(values []string, args *[]any) string {
	for _, value := range values {
		*args = append(*args, value)
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")
}

type HomeVideo struct {
	Video    model.Video
	SortAtMs int64
}

func homeFeedHasContentSQL(alias string) string {
	return fmt.Sprintf(`(NULLIF(TRIM(COALESCE(%[1]s.body_text,'')),'') IS NOT NULL
		OR NULLIF(TRIM(COALESCE(%[1]s.article_title,'')),'') IS NOT NULL
		OR NULLIF(TRIM(COALESCE(%[1]s.poll_json,'')),'') IS NOT NULL
		OR NULLIF(TRIM(COALESCE(%[1]s.community_note,'')),'') IS NOT NULL
		OR COALESCE(json_array_length(NULLIF(%[1]s.media_json,'')),0) > 0
		OR NULLIF(%[1]s.quote_tweet_id,'') IS NOT NULL
		OR NULLIF(TRIM(COALESCE(%[1]s.quote_body_text,'')),'') IS NOT NULL
		OR COALESCE(json_array_length(NULLIF(%[1]s.quote_media_json,'')),0) > 0)`, alias)
}

func homeSourceScopeSQL(widget home.Widget, channelID string, args *[]any) string {
	where := []string{`NULLIF(` + channelID + `,'') IS NOT NULL`}
	if len(widget.Channels) > 0 {
		where = append(where, channelID+` IN (`+homeValuesSQL(widget.Channels, args)+`)`)
	} else if widget.Type != "saved" && widget.Type != "continue" {
		where = append(where, `EXISTS (SELECT 1 FROM channel_follows f WHERE f.channel_id = `+channelID+`)`)
	}
	if widget.Type == "starred" || widget.StarredOnly {
		where = append(where, `EXISTS (SELECT 1 FROM channel_stars s WHERE s.channel_id = `+channelID+`)`)
	}
	if widget.Type != "saved" && widget.Type != "account" && widget.Type != "continue" {
		where = append(where, `NOT EXISTS (SELECT 1 FROM muted_channels m WHERE m.channel_id = `+channelID+`)`)
	}
	return strings.Join(where, ` AND `)
}

func (db *DB) GetHomeVideos(widget home.Widget) ([]HomeVideo, error) {
	if widget.Type == "account" && len(widget.Channels) == 0 {
		return nil, nil
	}
	var args []any
	platform := `COALESCE(NULLIF(c.platform,''), CASE v.owner_kind WHEN 'youtube_video' THEN 'youtube' WHEN 'tweet' THEN 'twitter' WHEN 'instagram_reel' THEN 'instagram' WHEN 'tiktok_video' THEN 'tiktok' END)`
	starredOnly := widget.Type == "starred" || widget.StarredOnly
	excludeMuted := widget.Type != "saved" && widget.Type != "account" && widget.Type != "continue"
	repostWhere := []string{`COALESCE(rs_settings.include_reposts,1) != 0`,
		homeSourceScopeSQL(widget, "rs.reposter_channel_id", &args)}
	sourceRows := `SELECT rs.video_id, rs.reposter_channel_id, rs.reposted_at_ms, rs.first_seen_at_ms
			FROM video_repost_sources rs JOIN videos owner ON owner.video_id = rs.video_id
			WHERE ` + sourceWindowPlatformEnabledClause("owner", db.MomentsIncludeRepostsEnabled(), db.InstagramIncludeTaggedEnabled())
	videoFeedRows := ""
	if widget.IncludesPlatform("twitter") && widget.Type != "moments" {
		videoFeedRows = `video_feed_rows AS (
			SELECT owner.video_id, owner.channel_id AS owner_channel_id, fi.content_hash,
			       fi.published_at, fi.fetched_at, fi.source_channel_id, fi.reposter_channel_id
			FROM videos owner JOIN feed_items fi ON fi.tweet_id = owner.video_id
			WHERE owner.owner_kind = 'tweet' AND ` + feedPrimaryItemPredicate("fi") + `
			UNION ALL
			SELECT owner.video_id, owner.channel_id, fi.content_hash,
			       fi.published_at, fi.fetched_at, fi.source_channel_id, fi.reposter_channel_id
			FROM videos owner JOIN feed_items fi ON fi.canonical_tweet_id = owner.video_id
			WHERE owner.owner_kind = 'tweet' AND fi.canonical_tweet_id IS NOT NULL AND fi.canonical_tweet_id != ''
			  AND fi.tweet_id != owner.video_id AND ` + feedPrimaryItemPredicate("fi") + `
		), `
		sourceRows += ` UNION ALL
			SELECT fi.video_id, rs.retweeter_channel_id, rs.published_at, fi.fetched_at
			FROM video_feed_rows fi
			JOIN retweet_sources rs ON rs.content_hash = NULLIF(fi.content_hash,'')
			UNION ALL
			SELECT fi.video_id, fi.source_channel_id, fi.published_at, fi.fetched_at
			FROM video_feed_rows fi
			WHERE NULLIF(fi.source_channel_id,'') IS NOT NULL AND fi.source_channel_id != fi.owner_channel_id
			UNION ALL
			SELECT fi.video_id, fi.reposter_channel_id, fi.published_at, fi.fetched_at
			FROM video_feed_rows fi
			WHERE NULLIF(fi.reposter_channel_id,'') IS NOT NULL AND fi.reposter_channel_id != fi.owner_channel_id`
	}
	with := `WITH ` + videoFeedRows + `introductions AS (
		SELECT video_id, reposter_channel_id, MAX(reposted_at_ms) AS reposted_at_ms,
		       COALESCE(MIN(NULLIF(first_seen_at_ms,0)),0) AS first_seen_at_ms
		FROM (` + sourceRows + `)
		GROUP BY video_id, reposter_channel_id
	), eligible_reposts AS (
		SELECT rs.*, COALESCE(cp.handle,'') AS reposter_handle,
		       COALESCE(NULLIF(cp.display_name,''),NULLIF(c.name,''),'') AS reposter_display_name,
		       COUNT(*) OVER (PARTITION BY rs.video_id) AS repost_count,
		       ROW_NUMBER() OVER (PARTITION BY rs.video_id ORDER BY ` + momentRepostHeadOrderSQL("rs") + `) AS source_rank
		FROM introductions rs
		LEFT JOIN channel_profiles cp ON cp.channel_id = rs.reposter_channel_id AND cp.tombstone = 0
		LEFT JOIN channels c ON c.channel_id = rs.reposter_channel_id
		LEFT JOIN channel_settings rs_settings ON rs_settings.channel_id = rs.reposter_channel_id
		WHERE ` + strings.Join(repostWhere, ` AND `) + `) `
	var where []string
	if widget.Type != "continue" && widget.Type != "latest" {
		where = append(where,
			`NOT EXISTS (SELECT 1 FROM feed_items fi WHERE fi.tweet_id = v.video_id AND `+feedPrimaryItemPredicate("fi")+` AND `+homeFeedHasContentSQL("fi")+`)`,
			`NOT EXISTS (SELECT 1 FROM feed_items fi WHERE fi.canonical_tweet_id = v.video_id AND fi.canonical_tweet_id IS NOT NULL AND fi.canonical_tweet_id != '' AND `+feedPrimaryItemPredicate("fi")+` AND `+homeFeedHasContentSQL("fi")+`)`)
	}
	if len(widget.Platforms) > 0 {
		where = append(where, platform+` IN (`+homeValuesSQL(widget.Platforms, &args)+`)`)
	}
	if len(widget.Channels) > 0 {
		where = append(where, `(v.channel_id IN (`+homeValuesSQL(widget.Channels, &args)+`) OR er.video_id IS NOT NULL)`)
	} else if widget.Type != "saved" && widget.Type != "continue" {
		where = append(where, `(EXISTS (SELECT 1 FROM channel_follows f WHERE f.channel_id = v.channel_id) OR er.video_id IS NOT NULL)`)
	}
	if starredOnly {
		where = append(where, `(EXISTS (SELECT 1 FROM channel_stars s WHERE s.channel_id = v.channel_id) OR er.video_id IS NOT NULL)`)
	}
	if excludeMuted {
		where = append(where, momentOwnerUnmutedSQL("v"))
	}
	sortAt := `COALESCE(v.published_at,0)`
	switch widget.Type {
	case "continue":
		where = append(where, `wh.playback_position > 0 AND (COALESCE(wh.duration,0) = 0 OR wh.playback_position < wh.duration * 0.95)`)
		if widget.Order != "newest" {
			sortAt = `COALESCE(wh.updated_at_ms,0)`
		}
	case "saved":
		where = append(where, `b.video_id IS NOT NULL`)
		if widget.Order != "newest" {
			sortAt = `COALESCE(b.bookmarked_at,0)`
		}
	case "moments":
		where = append(where, `(`+platform+` IN ('instagram','tiktok') OR (v.owner_kind = 'youtube_video' AND (v.duration BETWEEN 1 AND 90 OR json_extract(NULLIF(v.metadata_json,''),'$.height') > json_extract(NULLIF(v.metadata_json,''),'$.width') * 1.3)))`)
	case "latest":
		where = append(where, `COALESCE(v.media_kind,'video') NOT IN ('image','slideshow')`)
	}
	order := `home_sort_at_ms DESC, v.video_id DESC`
	channelName := `COALESCE(NULLIF(cp.display_name,''),NULLIF(c.name,''),NULLIF(cp.handle,''),v.channel_id)`
	if widget.Order == "account" {
		order = channelName + ` COLLATE NOCASE, ` + order
	}
	if len(widget.ContentTypes) > 0 {
		var content []string
		for _, kind := range widget.ContentTypes {
			switch kind {
			case "story":
				content = append(content, `v.source_kind = 'story'`)
			case "video":
				content = append(content, `(COALESCE(NULLIF(v.media_kind,''),'video') = 'video' AND COALESCE(v.source_kind,'') != 'story')`)
			case "image", "slideshow":
				content = append(content, `(v.media_kind = '`+kind+`' AND COALESCE(v.source_kind,'') != 'story')`)
			}
		}
		if len(content) == 0 {
			return nil, nil
		}
		where = append(where, `(`+strings.Join(content, ` OR `)+`)`)
	}
	if widget.Type != "saved" && widget.Type != "continue" {
		where = append(where, `(COALESCE(v.source_kind,'') != 'story' OR (v.published_at >= ? AND `+validStoryVideoSQL("v", "c")+`))`)
		args = append(args, db.StoryCutoffMs(time.Now().UnixMilli()))
	}
	args = append(args, widget.Count)
	rows, err := db.reader().Query(with+`SELECT v.id,v.video_id,v.channel_id,v.owner_kind,COALESCE(v.title,''),COALESCE(v.description,''),
		COALESCE(v.duration,0),v.published_at,v.downloaded_at,CASE WHEN `+videoFullyWatchedSQL("v")+` THEN 1 ELSE 0 END,
		COALESCE(v.is_temp,0),COALESCE(v.is_pinned,0),COALESCE(v.metadata_json,''),COALESCE(v.media_kind,''),COALESCE(v.slide_count,0),COALESCE(v.source_kind,''),
		`+channelName+`,`+platform+`,
		EXISTS(SELECT 1 FROM channel_stars s WHERE s.channel_id = v.channel_id),EXISTS(SELECT 1 FROM channel_follows f WHERE f.channel_id = v.channel_id),
		b.category_id,COALESCE(wh.playback_position,0),v.dearrow_title,v.dearrow_title_casual,v.dearrow_checked_at,
		`+sortAt+` AS home_sort_at_ms,b.custom_title,
		COALESCE(er.reposter_channel_id,''),COALESCE(er.reposter_handle,''),COALESCE(er.reposter_display_name,''),COALESCE(er.repost_count,0)
		FROM videos v LEFT JOIN channels c ON c.channel_id = v.channel_id LEFT JOIN channel_profiles cp ON cp.channel_id = v.channel_id
		LEFT JOIN bookmarks b ON b.video_id = v.video_id LEFT JOIN watch_history wh ON wh.video_id = v.video_id
		LEFT JOIN eligible_reposts er ON er.video_id = v.video_id AND er.source_rank = 1
		WHERE `+strings.Join(where, ` AND `)+` ORDER BY `+order+` LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var videos []HomeVideo
	for rows.Next() {
		var v model.Video
		var published, downloaded, category sql.NullInt64
		var sortAt int64
		var customTitle sql.NullString
		if err := rows.Scan(&v.ID, &v.VideoID, &v.ChannelID, &v.OwnerKind, &v.Title, &v.Description, &v.Duration, &published, &downloaded, &v.Watched, &v.IsTemp, &v.IsPinned, &v.MetadataJSON,
			&v.MediaKind, &v.MediaSlideCount, &v.SourceKind, &v.ChannelName, &v.Platform, &v.IsStarred, &v.IsSubscribed, &category, &v.PlaybackPosition, &v.DearrowTitle, &v.DearrowTitleCasual, &v.DearrowCheckedAtMs, &sortAt, &customTitle,
			&v.ReposterChannelID, &v.ReposterHandle, &v.ReposterDisplayName, &v.RepostCount); err != nil {
			return nil, err
		}
		v.PublishedAt = millisToTimePtr(published)
		if t := millisToTimePtr(downloaded); t != nil {
			v.DownloadedAt = *t
		}
		if category.Valid {
			v.BookmarkCategoryID = &category.Int64
		}
		v.RepostIntroduced = v.ReposterChannelID != ""
		v.EnrichForCard()
		v.EffectiveMomentAtMs = sortAt
		v.IsShortForm = v.IsShortForm || v.Platform == "instagram" || v.Platform == "tiktok"
		if widget.Type == "saved" && customTitle.Valid && customTitle.String != "" {
			v.Title = customTitle.String
			v.DearrowTitle, v.DearrowTitleCasual = nil, nil
		}
		videos = append(videos, HomeVideo{Video: v, SortAtMs: sortAt})
	}
	return videos, rows.Err()
}

func homeFeedCandidatesSQL(widget home.Widget, args *[]any) string {
	if widget.Type == "saved" {
		return `home_feed_candidates AS (
			SELECT fi.tweet_id FROM bookmarks b
			CROSS JOIN feed_items fi ON fi.tweet_id = b.video_id
			WHERE b.video_id IS NOT NULL
			UNION
			SELECT fi.tweet_id FROM bookmarks b
			CROSS JOIN feed_items fi ON fi.canonical_tweet_id = b.video_id
			WHERE b.video_id IS NOT NULL
			  AND fi.canonical_tweet_id IS NOT NULL AND fi.canonical_tweet_id != ''
		), `
	}
	channels := `SELECT f.channel_id FROM channel_follows f`
	if len(widget.Channels) > 0 {
		values := make([]string, len(widget.Channels))
		for i, channelID := range widget.Channels {
			values[i] = "(?)"
			*args = append(*args, channelID)
		}
		channels = `VALUES ` + strings.Join(values, ",")
	}
	scope := homeSourceScopeSQL(widget, "selected.channel_id", args)
	return `home_selected_channels(channel_id) AS (` + channels + `),
		home_source_channels AS (
			SELECT selected.channel_id FROM home_selected_channels selected WHERE ` + scope + `
		), home_feed_candidates AS (
			SELECT fi.tweet_id FROM home_source_channels source
			CROSS JOIN feed_items fi ON fi.channel_id = source.channel_id
			UNION
			SELECT fi.tweet_id FROM home_source_channels source
			CROSS JOIN feed_items fi ON fi.source_channel_id = source.channel_id
			UNION
			SELECT fi.tweet_id FROM home_source_channels source
			CROSS JOIN feed_items fi ON fi.reposter_channel_id = source.channel_id
			WHERE fi.reposter_channel_id IS NOT NULL AND fi.reposter_channel_id != ''
			UNION
			SELECT fi.tweet_id FROM home_source_channels source
			CROSS JOIN retweet_sources rs ON rs.retweeter_channel_id = source.channel_id
			CROSS JOIN feed_items fi ON fi.content_hash = rs.content_hash
			WHERE fi.content_hash IS NOT NULL AND fi.content_hash != ''
		), `
}

func (db *DB) GetHomeFeedItems(widget home.Widget) ([]model.FeedItem, error) {
	if !widget.IncludesPlatform("twitter") || widget.Type == "live" || widget.Type == "moments" || widget.Type == "latest" || widget.Type == "continue" {
		return nil, nil
	}
	if widget.Type == "account" && len(widget.Channels) == 0 {
		return nil, nil
	}
	var args []any
	candidates := homeFeedCandidatesSQL(widget, &args)
	where := []string{feedPrimaryItemPredicate("fi")}
	if widget.Type != "saved" {
		where = append(where, retweetFilterClause("fi"))
	}
	starredOnly := widget.Type == "starred" || widget.StarredOnly
	excludeMuted := widget.Type != "saved" && widget.Type != "account"
	author := `1 = 1`
	if len(widget.Channels) > 0 {
		author = `fi.channel_id IN (` + homeValuesSQL(widget.Channels, &args) + `)`
	} else if widget.Type != "saved" {
		author = `EXISTS (SELECT 1 FROM channel_follows f WHERE f.channel_id = fi.channel_id)`
	}
	if starredOnly {
		author += ` AND EXISTS (SELECT 1 FROM channel_stars s WHERE s.channel_id = fi.channel_id)`
	}
	sourceWhere := []string{`COALESCE(source_settings.include_reposts,1) != 0`, homeSourceScopeSQL(widget, "source.channel_id", &args)}
	if excludeMuted {
		where = append(where, `NOT EXISTS (SELECT 1 FROM muted_channels m WHERE m.channel_id = fi.channel_id)`)
	}
	where = append(where, `( (`+author+`) OR EXISTS (
		SELECT 1 FROM (
			SELECT fi.source_channel_id AS channel_id UNION SELECT fi.reposter_channel_id
			UNION SELECT r.retweeter_channel_id FROM retweet_sources r WHERE r.content_hash = NULLIF(fi.content_hash,'')
		) source
		LEFT JOIN channel_settings source_settings ON source_settings.channel_id = source.channel_id
		WHERE `+strings.Join(sourceWhere, ` AND `)+`))`)
	order := `fi.published_at DESC,fi.tweet_id DESC`
	if widget.Type == "saved" {
		where = append(where, `EXISTS(SELECT 1 FROM bookmarks b WHERE b.video_id = fi.tweet_id OR b.video_id = fi.canonical_tweet_id)`)
		if widget.Order != "newest" {
			order = `(SELECT MAX(b.bookmarked_at) FROM bookmarks b WHERE b.video_id = fi.tweet_id OR b.video_id = fi.canonical_tweet_id) DESC,fi.tweet_id DESC`
		}
	}
	if widget.Order == "account" {
		order = `COALESCE(NULLIF(fi.author_display_name,''),'@' || NULLIF(fi.author_handle,''),fi.channel_id) COLLATE NOCASE,` + order
	}
	projection := `fi.tweet_id, fi.canonical_tweet_id, fi.published_at, fi.channel_id`
	profileJoin := ""
	if widget.Order == "account" {
		projection += `, COALESCE(author_profile.display_name,'') AS author_display_name, COALESCE(author_profile.handle,'') AS author_handle`
		profileJoin = `LEFT JOIN channel_profiles author_profile ON author_profile.channel_id = fi.channel_id AND author_profile.tombstone = 0`
	}
	if len(widget.ContentTypes) > 0 && !widget.IncludesContent("post") {
		var content []string
		if widget.IncludesContent("image") {
			content = append(content, `(json_array_length(NULLIF(fi.media_json,'')) = 1 AND json_extract(fi.media_json,'$[0].type') = 'photo')`)
		}
		if widget.IncludesContent("slideshow") {
			content = append(content, `json_array_length(NULLIF(fi.media_json,'')) > 1`)
		}
		if widget.IncludesContent("video") {
			content = append(content, `(json_array_length(NULLIF(fi.media_json,'')) = 1 AND json_extract(fi.media_json,'$[0].type') IN ('video','gif'))`)
		}
		if len(content) == 0 {
			return nil, nil
		}
		where = append(where, `(`+strings.Join(content, ` OR `)+`)`)
	}
	args = append(args, widget.Count)
	rows, err := db.reader().Query(`WITH `+candidates+`eligible AS (
		SELECT `+projection+`, ROW_NUMBER() OVER(PARTITION BY COALESCE(NULLIF(fi.canonical_tweet_id,''),
			CASE WHEN EXISTS (SELECT 1 FROM feed_items wrapper WHERE wrapper.canonical_tweet_id IS NOT NULL AND wrapper.canonical_tweet_id != '' AND wrapper.canonical_tweet_id = fi.tweet_id AND wrapper.tweet_id != fi.tweet_id) THEN fi.tweet_id END,
			NULLIF(fi.content_hash,''),fi.tweet_id)
			ORDER BY CASE WHEN `+homeFeedHasContentSQL("fi")+` THEN 0 ELSE 1 END, COALESCE(fi.is_retweet,0), fi.fetched_at DESC,fi.tweet_id) AS home_row
		FROM home_feed_candidates candidate
		CROSS JOIN feed_items fi ON fi.tweet_id = candidate.tweet_id
		`+profileJoin+`
		WHERE `+strings.Join(where, ` AND `)+`), home_feed_page AS (
		SELECT fi.tweet_id FROM eligible fi WHERE fi.home_row = 1 ORDER BY `+order+` LIMIT ?)
		SELECT `+feedItemSelectSQL("fi")+` FROM home_feed_page page
		CROSS JOIN feed_items_resolved fi ON fi.tweet_id = page.tweet_id ORDER BY `+order, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items, err := scanFeedItems(rows)
	if err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var hashes []string
	for _, item := range items {
		if item.ContentHash != "" {
			hashes = append(hashes, item.ContentHash)
		}
	}
	hashes = uniqueStrings(hashes)
	if len(hashes) == 0 {
		return items, nil
	}
	byHash := make(map[string][]model.RetweeterInfo, len(hashes))
	for _, batch := range stringChunks(hashes, 400) {
		var sourceArgs []any
		sourceFilter := `rs.content_hash IN (` + homeValuesSQL(batch, &sourceArgs) + `)
			AND COALESCE(rs_settings.include_reposts,1) != 0 AND ` + homeSourceScopeSQL(widget, "rs.retweeter_channel_id", &sourceArgs)
		sources, err := db.reader().Query(`SELECT rs.content_hash, rs.retweeter_channel_id,
			COALESCE(rs.retweeter_handle,''),COALESCE(rs.retweeter_display_name,'')
			FROM retweet_sources_resolved rs
			LEFT JOIN channel_settings rs_settings ON rs_settings.channel_id = rs.retweeter_channel_id
			WHERE `+sourceFilter+` ORDER BY rs.published_at DESC, rs.retweeter_channel_id`, sourceArgs...)
		if err != nil {
			return nil, err
		}
		for sources.Next() {
			var hash string
			var source model.RetweeterInfo
			if err := sources.Scan(&hash, &source.ChannelID, &source.Handle, &source.DisplayName); err != nil {
				_ = sources.Close()
				return nil, err
			}
			source.AvatarURL = "/api/media/avatar/" + source.ChannelID
			byHash[hash] = append(byHash[hash], source)
		}
		if err := sources.Err(); err != nil {
			_ = sources.Close()
			return nil, err
		}
		if err := sources.Close(); err != nil {
			return nil, err
		}
	}
	for i := range items {
		items[i].Retweeters = byHash[items[i].ContentHash]
	}
	return items, nil
}
