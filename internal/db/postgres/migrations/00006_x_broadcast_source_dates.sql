-- +goose Up
ALTER TABLE x_broadcast_sources ADD COLUMN published_at_ms BIGINT NOT NULL DEFAULT 0;

UPDATE x_broadcast_sources s SET published_at_ms=links.published_at_ms
FROM (
    SELECT 'https://x.com/i/' || matched[1] || '/' || matched[2] AS url,
        max(COALESCE(content.published_at,0)) AS published_at_ms
    FROM (
        SELECT body_text AS body,published_at FROM feed_items
        UNION ALL
        SELECT quote_body_text,published_at FROM feed_items
    ) content
    CROSS JOIN LATERAL regexp_matches(COALESCE(content.body,''),
        'https?://(?:www\.)?(?:x|twitter)\.com/i/(broadcasts|events|spaces)/([a-z0-9_]+)', 'gi') AS matched
    GROUP BY 1
) links WHERE links.url=s.url;

-- +goose Down
ALTER TABLE x_broadcast_sources DROP COLUMN published_at_ms;
