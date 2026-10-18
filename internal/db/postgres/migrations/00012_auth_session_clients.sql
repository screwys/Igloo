-- +goose Up
ALTER TABLE auth_sessions ADD COLUMN client_name TEXT NOT NULL DEFAULT 'App';
ALTER TABLE auth_sessions ADD COLUMN expires_at_ms BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE auth_sessions DROP COLUMN expires_at_ms;
ALTER TABLE auth_sessions DROP COLUMN client_name;
