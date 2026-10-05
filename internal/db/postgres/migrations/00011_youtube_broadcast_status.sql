-- +goose Up
UPDATE youtube_broadcasts b
SET live_status = v.status, observed_at_ms = floor(extract(epoch FROM statement_timestamp()) * 1000)::bigint
FROM (
    SELECT video_id, channel_id, NULLIF(CASE WHEN metadata_json IS JSON OBJECT
        THEN metadata_json::jsonb->>'live_status' END, '') AS status
    FROM videos WHERE owner_kind = 'youtube_video'
) v
WHERE b.video_id = v.video_id AND b.channel_id = v.channel_id
  AND v.status IS NOT NULL AND b.live_status IS DISTINCT FROM v.status;

-- +goose Down
SELECT 1;
