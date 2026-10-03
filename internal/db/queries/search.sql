-- name: SearchChannels :many
SELECT c.channel_id, CAST(COALESCE(NULLIF(cp.display_name,''),NULLIF(c.name,''),c.name) AS TEXT) AS name,
       CAST(COALESCE(c.source_id,'') AS TEXT) AS source_id, CAST(COALESCE(c.platform,'youtube') AS TEXT) AS platform,
       CASE WHEN cs.channel_id IS NOT NULL THEN 1::BIGINT ELSE 0::BIGINT END AS is_starred,
       CAST(COALESCE(cp.handle,'') AS TEXT) AS handle, CAST(COALESCE(cp.display_name,'') AS TEXT) AS display_name
FROM channels c JOIN channel_follows cf ON cf.channel_id=c.channel_id
LEFT JOIN channel_stars cs ON cs.channel_id=c.channel_id
LEFT JOIN channel_profiles cp ON cp.channel_id=c.channel_id AND cp.tombstone=0
WHERE c.search_document @@ igloo_search_query(sqlc.arg(search)::TEXT)
ORDER BY ts_rank(c.search_document,igloo_search_query(sqlc.arg(search)::TEXT)) DESC, c.channel_id
LIMIT sqlc.arg(page_limit)::BIGINT;

-- name: SearchVideos :many
SELECT v.video_id, CAST(COALESCE(v.title,'') AS TEXT) AS title,
       CAST(COALESCE(cp.display_name,'') AS TEXT) AS channel_name,
       v.channel_id, CAST(COALESCE(c.platform,'youtube') AS TEXT) AS platform,
       v.published_at, CAST(COALESCE(v.is_temp,0) AS BIGINT) AS is_temp,
       v.dearrow_title, v.dearrow_title_casual
FROM videos v LEFT JOIN channels c ON c.channel_id=v.channel_id
LEFT JOIN channel_profiles cp ON cp.channel_id=v.channel_id
WHERE v.search_document @@ igloo_search_query(sqlc.arg(search)::TEXT)
ORDER BY ts_rank(v.search_document,igloo_search_query(sqlc.arg(search)::TEXT)) DESC,v.video_id
LIMIT sqlc.arg(page_limit)::BIGINT;

-- name: SearchFeedItems :many
WITH body_matches AS (
 SELECT tweet_id,published_at FROM feed_items
 WHERE body_text ILIKE sqlc.arg(pattern)::TEXT COLLATE "C" ESCAPE ''
 ORDER BY published_at DESC,tweet_id DESC LIMIT sqlc.arg(page_limit)::BIGINT
), author_matches AS (
 SELECT fi.tweet_id,fi.published_at FROM feed_items fi
 JOIN channel_profiles cp ON cp.channel_id=fi.channel_id AND cp.tombstone=0
 WHERE cp.handle ILIKE sqlc.arg(pattern)::TEXT COLLATE "C" ESCAPE '' OR cp.display_name ILIKE sqlc.arg(pattern)::TEXT COLLATE "C" ESCAPE ''
 ORDER BY fi.published_at DESC,fi.tweet_id DESC LIMIT sqlc.arg(page_limit)::BIGINT
), matching AS (
 SELECT * FROM body_matches UNION SELECT * FROM author_matches
)
SELECT fi.tweet_id AS tweet_id,
       COALESCE(fi.source_handle,'') AS source_handle,
       COALESCE(fi.author_handle,'') AS author_handle,
       COALESCE(fi.author_display_name,'') AS author_display_name,
       CAST(COALESCE(fi.author_avatar_url,'') AS TEXT) AS author_avatar_url,
       COALESCE(fi.body_text,'') AS body_text,
       COALESCE(fi.article_title,'') AS article_title,
       COALESCE(fi.poll_json,'') AS poll_json,
       COALESCE(fi.community_note,'') AS community_note,
       COALESCE(fi.lang,'') AS lang,
       COALESCE(fi.is_retweet,0) AS is_retweet,
       COALESCE(fi.retweeted_by_handle,'') AS retweeted_by_handle,
       COALESCE(fi.retweeted_by_display_name,'') AS retweeted_by_display_name,
       COALESCE(fi.quote_tweet_id,'') AS quote_tweet_id,
       COALESCE(fi.quote_author_handle,'') AS quote_author_handle,
       COALESCE(fi.quote_author_display_name,'') AS quote_author_display_name,
       CAST(COALESCE(fi.quote_author_avatar_url,'') AS TEXT) AS quote_author_avatar_url,
       COALESCE(fi.quote_body_text,'') AS quote_body_text,
       COALESCE(fi.quote_article_title,'') AS quote_article_title,
       COALESCE(fi.quote_poll_json,'') AS quote_poll_json,
       COALESCE(fi.quote_community_note,'') AS quote_community_note,
       COALESCE(fi.quote_lang,'') AS quote_lang,
       COALESCE(fi.quote_media_json,'') AS quote_media_json,
       COALESCE(fi.media_json,'') AS media_json,
       COALESCE(fi.canonical_url,'') AS canonical_url,
       COALESCE(fi.reply_to_handle,'') AS reply_to_handle,
       COALESCE(fi.reply_to_status,'') AS reply_to_status,
       COALESCE(fi.is_reply,0) AS is_reply,
       COALESCE(fi.is_ghost,0) AS is_ghost,
       fi.quote_published_at AS quote_published_at,
       COALESCE(fi.views,0) AS views,
       COALESCE(fi.likes,0) AS likes,
       COALESCE(fi.retweets,0) AS retweets,
       fi.published_at AS published_at,
       fi.fetched_at AS fetched_at,
       COALESCE(fi.content_hash,'') AS content_hash,
       COALESCE(fi.canonical_tweet_id,'') AS canonical_tweet_id,
       COALESCE(fi.source_channel_id,'') AS source_channel_id,
       COALESCE(fi.channel_id,'') AS channel_id,
       COALESCE(fi.quote_channel_id,'') AS quote_channel_id,
       COALESCE(fi.reply_channel_id,'') AS reply_channel_id,
       COALESCE(fi.reposter_channel_id,'') AS reposter_channel_id
FROM matching matched JOIN feed_items_resolved fi ON fi.tweet_id=matched.tweet_id
ORDER BY fi.published_at DESC,fi.tweet_id DESC LIMIT sqlc.arg(page_limit)::BIGINT;
