-- +goose Up
-- X cookie files resting after a rate limit or a failed session. Repeated
-- session failures drop a file until it holds a different login session.
-- session is a hash of the file's auth_token, never the token itself.
CREATE TABLE x_cookie_state (
    label TEXT PRIMARY KEY,
    session TEXT NOT NULL DEFAULT '',
    fail_count INTEGER NOT NULL DEFAULT 0,
    retry_at_ms BIGINT NOT NULL DEFAULT 0,
    dropped BOOLEAN NOT NULL DEFAULT FALSE,
    last_error TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE x_cookie_state;
