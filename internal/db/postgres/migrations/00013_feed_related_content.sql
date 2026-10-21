-- +goose Up
CREATE INDEX idx_feed_items_related_content ON feed_items ((CASE
    WHEN NULLIF(TRIM(COALESCE(quote_tweet_id, '')), '') IS NOT NULL
        THEN 'tweet:' || LOWER(TRIM(quote_tweet_id))
    WHEN NULLIF(TRIM(COALESCE(canonical_tweet_id, '')), '') IS NOT NULL
        THEN 'tweet:' || LOWER(TRIM(canonical_tweet_id))
    ELSE 'tweet:' || LOWER(TRIM(tweet_id))
END));
DROP INDEX idx_feed_items_seen_cover;

-- +goose Down
DROP INDEX idx_feed_items_related_content;
CREATE INDEX idx_feed_items_seen_cover ON feed_items(tweet_id, quote_tweet_id, canonical_tweet_id, channel_id, source_channel_id, is_ghost);
