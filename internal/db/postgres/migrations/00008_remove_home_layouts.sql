-- +goose Up
DROP TABLE home_layouts;
UPDATE settings SET value = 'videos' WHERE key = 'starting_page' AND value = 'home';

-- +goose Down
CREATE TABLE home_layouts (
    username TEXT PRIMARY KEY,
    layout_json TEXT NOT NULL,
    updated_at_ms BIGINT NOT NULL
);
