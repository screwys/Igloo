-- +goose Up

-- +goose StatementBegin
CREATE FUNCTION igloo_sync_insert_feed_peers() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
    hashes TEXT[];
BEGIN
    IF NOT EXISTS (SELECT 1 FROM inserted_rows) THEN RETURN NULL; END IF;
    PERFORM 1 FROM android_sync_clock WHERE id = 1 FOR UPDATE;

    IF TG_TABLE_NAME = 'feed_items' THEN
        SELECT array_agg(DISTINCT content_hash) INTO hashes
        FROM (
            SELECT content_hash FROM inserted_rows
            UNION ALL
            SELECT target.content_hash
            FROM inserted_rows inserted
            JOIN feed_items target ON target.tweet_id = inserted.quote_tweet_id
            WHERE inserted.quote_tweet_id <> ''
        ) affected
        WHERE content_hash IS NOT NULL AND content_hash <> '';
    ELSIF TG_TABLE_NAME = 'retweet_sources' THEN
        SELECT array_agg(DISTINCT content_hash) INTO hashes
        FROM inserted_rows
        WHERE content_hash IS NOT NULL AND content_hash <> '';
    ELSIF TG_TABLE_NAME = 'feed_likes' THEN
        SELECT array_agg(DISTINCT target.content_hash) INTO hashes
        FROM inserted_rows inserted
        JOIN feed_items target ON target.tweet_id = inserted.tweet_id
        WHERE inserted.tweet_id <> '' AND target.content_hash IS NOT NULL AND target.content_hash <> '';
    ELSE
        SELECT array_agg(DISTINCT target.content_hash) INTO hashes
        FROM inserted_rows inserted
        JOIN feed_items target ON target.tweet_id = inserted.video_id
        WHERE inserted.video_id <> '' AND target.content_hash IS NOT NULL AND target.content_hash <> '';
    END IF;
    IF hashes IS NULL THEN RETURN NULL; END IF;

    WITH peers AS MATERIALIZED (
        SELECT tweet_id,
               row_number() OVER (ORDER BY tweet_id COLLATE "C") AS position,
               count(*) OVER () AS peer_count
        FROM feed_items
        WHERE content_hash = ANY(hashes)
    ), clock AS (
        UPDATE android_sync_clock SET revision = revision + (SELECT count(*) FROM peers) WHERE id = 1
        RETURNING revision
    )
    INSERT INTO android_sync_heads (owner_kind, owner_id, revision)
    SELECT 'feed', peers.tweet_id, clock.revision - peers.peer_count + peers.position
    FROM peers CROSS JOIN clock
    ON CONFLICT (owner_kind, owner_id) DO UPDATE SET revision = EXCLUDED.revision;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER igloo_sync_feed_peers ON feed_items;
CREATE TRIGGER igloo_sync_feed_peers AFTER DELETE OR UPDATE OF content_hash, published_at ON feed_items
FOR EACH ROW EXECUTE FUNCTION igloo_sync_feed_peers('content_hash', 'direct', 'content_hash,published_at');
DROP TRIGGER igloo_sync_quote_peers ON feed_items;
CREATE TRIGGER igloo_sync_quote_peers AFTER DELETE OR UPDATE OF quote_tweet_id, published_at ON feed_items
FOR EACH ROW EXECUTE FUNCTION igloo_sync_feed_peers('quote_tweet_id', 'lookup', 'quote_tweet_id,published_at');
DROP TRIGGER igloo_sync_retweet_peers ON retweet_sources;
CREATE TRIGGER igloo_sync_retweet_peers AFTER DELETE OR UPDATE OF content_hash, retweeter_channel_id, tweet_id, published_at ON retweet_sources
FOR EACH ROW EXECUTE FUNCTION igloo_sync_feed_peers('content_hash', 'direct', 'content_hash,retweeter_channel_id,tweet_id,published_at');
DROP TRIGGER igloo_sync_likes_peers ON feed_likes;
CREATE TRIGGER igloo_sync_likes_peers AFTER DELETE OR UPDATE OF tweet_id ON feed_likes
FOR EACH ROW EXECUTE FUNCTION igloo_sync_feed_peers('tweet_id', 'lookup', 'tweet_id');
DROP TRIGGER igloo_sync_bookmarks_peers ON bookmarks;
CREATE TRIGGER igloo_sync_bookmarks_peers AFTER DELETE OR UPDATE OF video_id ON bookmarks
FOR EACH ROW EXECUTE FUNCTION igloo_sync_feed_peers('video_id', 'lookup', 'video_id');

CREATE TRIGGER igloo_sync_feed_insert_peers AFTER INSERT ON feed_items
REFERENCING NEW TABLE AS inserted_rows
FOR EACH STATEMENT EXECUTE FUNCTION igloo_sync_insert_feed_peers();
CREATE TRIGGER igloo_sync_retweet_insert_peers AFTER INSERT ON retweet_sources
REFERENCING NEW TABLE AS inserted_rows
FOR EACH STATEMENT EXECUTE FUNCTION igloo_sync_insert_feed_peers();
CREATE TRIGGER igloo_sync_likes_insert_peers AFTER INSERT ON feed_likes
REFERENCING NEW TABLE AS inserted_rows
FOR EACH STATEMENT EXECUTE FUNCTION igloo_sync_insert_feed_peers();
CREATE TRIGGER igloo_sync_bookmarks_insert_peers AFTER INSERT ON bookmarks
REFERENCING NEW TABLE AS inserted_rows
FOR EACH STATEMENT EXECUTE FUNCTION igloo_sync_insert_feed_peers();

-- +goose Down

DROP TRIGGER igloo_sync_feed_insert_peers ON feed_items;
DROP TRIGGER igloo_sync_retweet_insert_peers ON retweet_sources;
DROP TRIGGER igloo_sync_likes_insert_peers ON feed_likes;
DROP TRIGGER igloo_sync_bookmarks_insert_peers ON bookmarks;
DROP FUNCTION igloo_sync_insert_feed_peers();

DROP TRIGGER igloo_sync_feed_peers ON feed_items;
CREATE TRIGGER igloo_sync_feed_peers AFTER INSERT OR DELETE OR UPDATE OF content_hash, published_at ON feed_items
FOR EACH ROW EXECUTE FUNCTION igloo_sync_feed_peers('content_hash', 'direct', 'content_hash,published_at');
DROP TRIGGER igloo_sync_quote_peers ON feed_items;
CREATE TRIGGER igloo_sync_quote_peers AFTER INSERT OR DELETE OR UPDATE OF quote_tweet_id, published_at ON feed_items
FOR EACH ROW EXECUTE FUNCTION igloo_sync_feed_peers('quote_tweet_id', 'lookup', 'quote_tweet_id,published_at');
DROP TRIGGER igloo_sync_retweet_peers ON retweet_sources;
CREATE TRIGGER igloo_sync_retweet_peers AFTER INSERT OR DELETE OR UPDATE OF content_hash, retweeter_channel_id, tweet_id, published_at ON retweet_sources
FOR EACH ROW EXECUTE FUNCTION igloo_sync_feed_peers('content_hash', 'direct', 'content_hash,retweeter_channel_id,tweet_id,published_at');
DROP TRIGGER igloo_sync_likes_peers ON feed_likes;
CREATE TRIGGER igloo_sync_likes_peers AFTER INSERT OR DELETE OR UPDATE OF tweet_id ON feed_likes
FOR EACH ROW EXECUTE FUNCTION igloo_sync_feed_peers('tweet_id', 'lookup', 'tweet_id');
DROP TRIGGER igloo_sync_bookmarks_peers ON bookmarks;
CREATE TRIGGER igloo_sync_bookmarks_peers AFTER INSERT OR DELETE OR UPDATE OF video_id ON bookmarks
FOR EACH ROW EXECUTE FUNCTION igloo_sync_feed_peers('video_id', 'lookup', 'video_id');
