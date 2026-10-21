-- +goose Up
CREATE TABLE feed_state_account_scores (
    handle TEXT PRIMARY KEY,
    score DOUBLE PRECISION NOT NULL
);
INSERT INTO settings (key, value)
SELECT 'feed_rank_snapshot_reranked_at_ms', COALESCE(MAX(computed_at), 0)::TEXT
FROM feed_rank_snapshot
ON CONFLICT(key) DO NOTHING;

-- +goose Down
DROP TABLE feed_state_account_scores;
DELETE FROM settings WHERE key IN (
    'feed_state_account_scores_refreshed_at_ms',
    'feed_rank_snapshot_reranked_at_ms'
);
