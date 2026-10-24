-- name: UnusedChannelMediaOwners :many
WITH shown(channel_id) AS (
    SELECT channel_id FROM channel_follows
    UNION SELECT channel_id FROM channel_stars
    UNION SELECT channel_id FROM muted_channels
    UNION SELECT channel_id FROM channel_settings
    UNION SELECT channel_id FROM feed_items
    UNION SELECT quote_channel_id FROM feed_items
    UNION SELECT reply_channel_id FROM feed_items
    UNION SELECT reposter_channel_id FROM feed_items
    UNION SELECT source_channel_id FROM feed_items
    UNION SELECT retweeter_channel_id FROM retweet_sources
    UNION SELECT channel_id FROM videos
    UNION SELECT reposter_channel_id FROM video_repost_sources
    UNION SELECT source_channel_id FROM video_desires
    UNION SELECT owner_channel_id FROM download_queue
    UNION SELECT channel_id FROM x_broadcasts
    UNION SELECT channel_id FROM x_broadcast_sources
    UNION SELECT channel_id FROM x_followed_broadcasts
    UNION SELECT channel_id FROM x_space_presence
    UNION SELECT channel_id FROM youtube_broadcasts
    UNION SELECT channel_id FROM tiktok_lives
    UNION SELECT c.value ->> 'channel_id' FROM youtube_recommendations r,
        jsonb_array_elements(CASE WHEN jsonb_typeof(r.candidates_json::jsonb) = 'array' THEN r.candidates_json::jsonb ELSE '[]'::jsonb END) c
    UNION SELECT c.value ->> 'channel_id' FROM discover_generation d,
        jsonb_array_elements(CASE WHEN jsonb_typeof(d.candidates_json::jsonb) = 'array' THEN d.candidates_json::jsonb ELSE '[]'::jsonb END) c
)
SELECT DISTINCT a.owner_id FROM assets a
WHERE a.owner_kind = 'channel' AND a.asset_kind IN ('avatar', 'banner')
  AND NOT EXISTS (SELECT 1 FROM shown s WHERE s.channel_id = a.owner_id);

-- name: UnusedCommentAuthorMediaOwners :many
SELECT a.owner_id FROM assets a
WHERE a.owner_kind = 'comment_author' AND a.asset_kind = 'avatar'
  AND NOT EXISTS (SELECT 1 FROM video_comments c WHERE c.author_id = substr(a.owner_id, 9));

-- name: DeleteUnusedChannelMedia :many
DELETE FROM assets a
WHERE a.owner_kind = 'channel' AND a.asset_kind IN ('avatar', 'banner')
  AND a.owner_id IN (SELECT jsonb_array_elements_text(sqlc.arg(channel_ids)::jsonb))
  AND a.required_reason NOT IN ('bookmark', 'like', 'manual')
  AND NOT EXISTS (SELECT 1 FROM channel_profiles cp WHERE cp.channel_id = a.owner_id
                  AND GREATEST(cp.observed_at_ms, cp.fetched_at) >= sqlc.arg(seen_after_ms))
  AND NOT EXISTS (SELECT 1 FROM channel_follows f WHERE f.channel_id = a.owner_id)
  AND NOT EXISTS (SELECT 1 FROM channel_stars s WHERE s.channel_id = a.owner_id)
  AND NOT EXISTS (SELECT 1 FROM muted_channels m WHERE m.channel_id = a.owner_id)
  AND NOT EXISTS (SELECT 1 FROM channel_settings cs WHERE cs.channel_id = a.owner_id)
  AND NOT EXISTS (SELECT 1 FROM profile_jobs pj WHERE pj.channel_id = a.owner_id
                  AND pj.requested_revision > pj.completed_revision)
RETURNING a.owner_id, a.object_id, a.desired_object_id;

-- name: DeleteUnusedCommentAuthorMedia :many
DELETE FROM assets a
WHERE a.owner_kind = 'comment_author' AND a.asset_kind = 'avatar'
  AND a.owner_id IN (SELECT jsonb_array_elements_text(sqlc.arg(author_ids)::jsonb))
  AND a.required_reason NOT IN ('bookmark', 'like', 'manual')
  AND NOT EXISTS (SELECT 1 FROM video_comments c WHERE c.author_id = substr(a.owner_id, 9))
RETURNING a.owner_id, a.object_id, a.desired_object_id;

-- name: QueueIdentityMediaFiles :exec
INSERT INTO unfollow_cleanup_files (file_path)
SELECT DISTINCT file_path FROM media_objects
WHERE object_id IN (SELECT jsonb_array_elements_text(sqlc.arg(object_ids)::jsonb)) AND file_path != ''
ON CONFLICT DO NOTHING;

-- name: DeleteIdentityMediaObjects :exec
DELETE FROM media_objects m
WHERE m.object_id IN (SELECT jsonb_array_elements_text(sqlc.arg(object_ids)::jsonb))
  AND NOT EXISTS (SELECT 1 FROM assets a WHERE a.object_id = m.object_id OR a.desired_object_id = m.object_id);

-- name: DeleteIdentityProfileJobs :exec
DELETE FROM profile_jobs pj
WHERE pj.channel_id IN (SELECT jsonb_array_elements_text(sqlc.arg(channel_ids)::jsonb))
  AND pj.requested_revision <= pj.completed_revision;
