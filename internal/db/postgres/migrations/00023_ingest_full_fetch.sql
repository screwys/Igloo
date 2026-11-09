-- +goose Up
-- When ingest last read an X account's replies and reposts timelines, which
-- it does only every few passes. Unix seconds, like the other ingest times.
ALTER TABLE ingest_state ADD COLUMN full_fetch_at DOUBLE PRECISION NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE ingest_state DROP COLUMN full_fetch_at;
