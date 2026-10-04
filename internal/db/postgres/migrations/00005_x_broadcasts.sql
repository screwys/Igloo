-- +goose Up
CREATE TABLE x_broadcasts (
    broadcast_id TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES channels(channel_id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    thumbnail_url TEXT NOT NULL DEFAULT '',
    live_status TEXT NOT NULL DEFAULT '',
    viewer_count BIGINT NOT NULL DEFAULT 0,
    observed_at_ms BIGINT NOT NULL
);

CREATE TABLE x_broadcast_sources (
    url TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL,
    broadcast_id TEXT REFERENCES x_broadcasts(broadcast_id) ON DELETE SET NULL,
    checked_at_ms BIGINT NOT NULL DEFAULT 0
);

INSERT INTO x_broadcast_sources (url, channel_id)
SELECT DISTINCT ON (url) url, channel_id FROM (
    SELECT 'https://x.com/i/' || matched[1] || '/' || matched[2] AS url, content.channel_id
    FROM (
        SELECT channel_id, body_text AS body FROM feed_items
        UNION ALL
        SELECT quote_channel_id, quote_body_text FROM feed_items
    ) content
    CROSS JOIN LATERAL regexp_matches(COALESCE(content.body,''),
        'https?://(?:www\.)?(?:x|twitter)\.com/i/(broadcasts|events|spaces)/([a-z0-9_]+)', 'gi') AS matched
    WHERE content.channel_id LIKE 'twitter_%'
) links
ORDER BY url, channel_id
ON CONFLICT (url) DO NOTHING;

-- +goose Down
DROP TABLE x_broadcast_sources;
DROP TABLE x_broadcasts;
