-- +goose Up
CREATE TABLE tiktok_lives (
    channel_id TEXT PRIMARY KEY REFERENCES channels(channel_id) ON DELETE CASCADE,
    room_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    thumbnail_url TEXT NOT NULL DEFAULT '',
    viewer_count BIGINT NOT NULL DEFAULT 0,
    observed_at_ms BIGINT NOT NULL
);

-- +goose Down
DROP TABLE tiktok_lives;
