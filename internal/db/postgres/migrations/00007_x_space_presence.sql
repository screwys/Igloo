-- +goose Up
CREATE TABLE x_space_presence (
    channel_id TEXT PRIMARY KEY REFERENCES channels(channel_id) ON DELETE CASCADE,
    space_id TEXT NOT NULL,
    observed_at_ms BIGINT NOT NULL
);

CREATE INDEX x_space_presence_space_id ON x_space_presence(space_id);

CREATE VIEW x_followed_broadcasts AS
SELECT DISTINCT ON (b.broadcast_id) b.broadcast_id, cf.channel_id, b.url, b.title, b.thumbnail_url,
    b.live_status, b.viewer_count, b.observed_at_ms
FROM x_broadcasts b
JOIN channel_follows cf ON cf.channel_id=b.channel_id OR EXISTS (
    SELECT 1 FROM x_space_presence p
    WHERE p.channel_id=cf.channel_id AND b.url='https://x.com/i/spaces/' || p.space_id
)
WHERE b.live_status='is_live'
ORDER BY b.broadcast_id,cf.channel_id=b.channel_id DESC,
    EXISTS (SELECT 1 FROM channel_stars cs WHERE cs.channel_id=cf.channel_id) DESC,cf.channel_id;

-- +goose Down
DROP VIEW x_followed_broadcasts;
DROP TABLE x_space_presence;
