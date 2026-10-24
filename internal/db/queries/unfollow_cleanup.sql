-- name: EnqueueUnfollowCleanup :exec
INSERT INTO unfollow_cleanup_jobs (channel_id, requested_at_ms)
VALUES ($1, $2)
ON CONFLICT (channel_id) DO UPDATE SET requested_at_ms = excluded.requested_at_ms;

-- name: NextUnfollowCleanup :one
SELECT channel_id, requested_at_ms FROM unfollow_cleanup_jobs
ORDER BY requested_at_ms, channel_id LIMIT 1;

-- name: UnfollowCleanupJobIsCurrent :one
SELECT EXISTS(SELECT 1 FROM unfollow_cleanup_jobs WHERE channel_id = $1 AND requested_at_ms = $2);

-- name: UnfollowCleanupChannelIsFollowed :one
SELECT EXISTS(SELECT 1 FROM channel_follows WHERE channel_id = $1);

-- name: UnfollowCleanupPlatform :one
SELECT COALESCE(platform, '')::text FROM channels WHERE channel_id = $1;

-- name: UnfollowCleanupRevision :one
SELECT revision FROM unfollow_cleanup_state WHERE id = 1;

-- name: LockUnfollowCleanupRevision :one
SELECT revision FROM unfollow_cleanup_state WHERE id = 1 FOR UPDATE;

-- name: QueueUnfollowCleanupItem :exec
INSERT INTO unfollow_cleanup_items (channel_id, owner_kind, owner_id)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: UnfollowVideoCleanupItems :many
SELECT video_id FROM video_desires WHERE source_channel_id = $1
UNION
SELECT owner_id FROM unfollow_cleanup_items WHERE channel_id = $1 AND owner_kind = 'video';

-- name: QueueUnfollowTweetFiles :exec
INSERT INTO unfollow_cleanup_files (file_path)
SELECT DISTINCT current.file_path FROM assets a
JOIN media_objects current ON current.object_id = a.object_id
WHERE a.owner_kind = 'tweet' AND a.owner_id IN (SELECT jsonb_array_elements_text(sqlc.arg(owner_ids)::jsonb))
  AND current.published_revision > 0 AND current.file_path != ''
ON CONFLICT DO NOTHING;

-- name: QueueUnfollowVideoFiles :exec
INSERT INTO unfollow_cleanup_files (file_path)
SELECT DISTINCT current.file_path FROM assets a
JOIN videos v ON v.video_id = a.owner_id AND v.owner_kind = a.owner_kind
JOIN media_objects current ON current.object_id = a.object_id
WHERE v.video_id IN (SELECT jsonb_array_elements_text(sqlc.arg(owner_ids)::jsonb))
  AND current.published_revision > 0 AND current.file_path != ''
ON CONFLICT DO NOTHING;

-- name: DeleteUnfollowVideoAssets :exec
DELETE FROM assets a USING videos v
WHERE v.video_id = a.owner_id AND v.owner_kind = a.owner_kind
  AND v.video_id IN (SELECT jsonb_array_elements_text(sqlc.arg(owner_ids)::jsonb));

-- name: RetireUnfollowReposts :execrows
DELETE FROM retweet_sources WHERE ctid IN (
    SELECT r.ctid FROM retweet_sources r WHERE r.retweeter_channel_id = $1 LIMIT $2
);

-- name: RetireUnfollowVideoDesires :execrows
DELETE FROM video_desires WHERE ctid IN (
    SELECT d.ctid FROM video_desires d WHERE d.source_channel_id = $1 LIMIT $2
);

-- name: RetireUnfollowVideoReposts :execrows
DELETE FROM video_repost_sources WHERE ctid IN (
    SELECT r.ctid FROM video_repost_sources r WHERE r.reposter_channel_id = $1 LIMIT $2
);

-- name: FinishUnfollowCleanupItems :exec
DELETE FROM unfollow_cleanup_items
WHERE channel_id = $1 AND owner_kind = $2 AND owner_id IN (SELECT jsonb_array_elements_text(sqlc.arg(owner_ids)::jsonb));

-- name: DiscardUnfollowCleanupItems :execrows
DELETE FROM unfollow_cleanup_items WHERE ctid IN (
    SELECT i.ctid FROM unfollow_cleanup_items i WHERE i.channel_id = $1 LIMIT $2
);

-- name: FinishUnfollowCleanup :exec
DELETE FROM unfollow_cleanup_jobs WHERE channel_id = $1 AND requested_at_ms = $2;

-- name: NextUnfollowCleanupFiles :many
SELECT file_path FROM unfollow_cleanup_files ORDER BY file_path LIMIT $1;

-- name: FinishUnfollowCleanupFile :exec
DELETE FROM unfollow_cleanup_files WHERE file_path = $1;
