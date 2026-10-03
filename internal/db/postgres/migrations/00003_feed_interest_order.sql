-- +goose Up

DROP INDEX idx_feed_items_algo;
CREATE INDEX idx_feed_items_algo ON feed_items(algo_interest DESC NULLS LAST, published_at DESC);

-- +goose Down

DROP INDEX idx_feed_items_algo;
CREATE INDEX idx_feed_items_algo ON feed_items(algo_interest DESC, published_at DESC);
