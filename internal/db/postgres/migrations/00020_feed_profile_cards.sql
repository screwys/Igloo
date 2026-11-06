-- +goose Up
-- Which X profiles show a post, decided once for web and Android. A post row
-- appears on the profiles of the accounts that wrote it, posted it from their
-- timeline, are quoted in it, or are tagged in its photos.
-- Index builds run functions with a restricted search_path, so functions
-- that call other Igloo functions keep the schema they were created in.
-- +goose StatementBegin
CREATE FUNCTION igloo_feed_profile_channels(
    channel_id TEXT, source_channel_id TEXT, quote_channel_id TEXT, media_json TEXT
) RETURNS TEXT[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE SET search_path FROM CURRENT AS $$
    SELECT COALESCE(array_agg(DISTINCT c ORDER BY c), '{}')
    FROM unnest(
        ARRAY[channel_id, source_channel_id, quote_channel_id]
        || CASE WHEN media_json LIKE '%"tagged"%' THEN igloo_media_tagged_channels(media_json) ELSE '{}'::TEXT[] END
    ) c
    WHERE c <> ''
$$;
-- +goose StatementEnd

-- Rows of the same post collapse to one card per profile; the lowest rank
-- wins: the profile's own repost, then the original, then other reposts.
-- +goose StatementBegin
CREATE FUNCTION igloo_feed_profile_rank(is_retweet BIGINT, source_channel_id TEXT, profile_channel TEXT) RETURNS INTEGER
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT CASE
        WHEN COALESCE(is_retweet, 0) = 1 AND source_channel_id = profile_channel THEN 0
        WHEN COALESCE(is_retweet, 0) = 0 THEN 1
        ELSE 2
    END
$$;
-- +goose StatementEnd

-- The profiles and ranks of one row as JSON, synced to Android.
-- +goose StatementBegin
CREATE FUNCTION igloo_feed_profile_cards(
    is_retweet BIGINT, channel_id TEXT, source_channel_id TEXT, quote_channel_id TEXT, media_json TEXT
) RETURNS TEXT
LANGUAGE sql IMMUTABLE PARALLEL SAFE SET search_path FROM CURRENT AS $$
    SELECT COALESCE(jsonb_agg(
        jsonb_build_object('channel_id', c, 'rank', igloo_feed_profile_rank(is_retweet, source_channel_id, c))
        ORDER BY c
    ), '[]'::jsonb)::TEXT
    FROM unnest(igloo_feed_profile_channels(channel_id, source_channel_id, quote_channel_id, media_json)) c
$$;
-- +goose StatementEnd

CREATE INDEX idx_feed_items_profile_channels ON feed_items
USING GIN (igloo_feed_profile_channels(channel_id, source_channel_id, quote_channel_id, media_json));

-- +goose Down
DROP INDEX idx_feed_items_profile_channels;
DROP FUNCTION igloo_feed_profile_cards(BIGINT, TEXT, TEXT, TEXT, TEXT);
DROP FUNCTION igloo_feed_profile_rank(BIGINT, TEXT, TEXT);
DROP FUNCTION igloo_feed_profile_channels(TEXT, TEXT, TEXT, TEXT);
