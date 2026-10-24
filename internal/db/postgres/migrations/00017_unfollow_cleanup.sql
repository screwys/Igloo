-- +goose Up
CREATE TABLE unfollow_cleanup_jobs (
    channel_id TEXT PRIMARY KEY,
    requested_at_ms BIGINT NOT NULL
);

CREATE TABLE unfollow_cleanup_items (
    channel_id TEXT NOT NULL,
    owner_kind TEXT NOT NULL CHECK (owner_kind IN ('tweet', 'video')),
    owner_id TEXT NOT NULL,
    PRIMARY KEY (channel_id, owner_kind, owner_id)
);

CREATE TABLE unfollow_cleanup_files (
    file_path TEXT PRIMARY KEY
);

CREATE TABLE unfollow_cleanup_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    revision BIGINT NOT NULL
);
INSERT INTO unfollow_cleanup_state VALUES (1, 0);

-- Planning reads a snapshot without holding the application write lock.
-- Only changes to content ownership or dependencies invalidate that snapshot.
-- +goose StatementBegin
CREATE FUNCTION igloo_invalidate_unfollow_cleanup() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE unfollow_cleanup_state SET revision = revision + 1 WHERE id = 1;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER igloo_cleanup_feed AFTER INSERT OR DELETE OR UPDATE OF
    tweet_id, source_channel_id, channel_id, reposter_channel_id, is_retweet,
    content_hash, canonical_tweet_id, quote_tweet_id, reply_to_status ON feed_items
FOR EACH STATEMENT EXECUTE FUNCTION igloo_invalidate_unfollow_cleanup();
CREATE TRIGGER igloo_cleanup_follow AFTER INSERT OR DELETE OR UPDATE OF channel_id ON channel_follows
FOR EACH STATEMENT EXECUTE FUNCTION igloo_invalidate_unfollow_cleanup();
CREATE TRIGGER igloo_cleanup_like AFTER INSERT OR DELETE OR UPDATE OF tweet_id ON feed_likes
FOR EACH STATEMENT EXECUTE FUNCTION igloo_invalidate_unfollow_cleanup();
CREATE TRIGGER igloo_cleanup_bookmark AFTER INSERT OR DELETE OR UPDATE OF video_id ON bookmarks
FOR EACH STATEMENT EXECUTE FUNCTION igloo_invalidate_unfollow_cleanup();
CREATE TRIGGER igloo_cleanup_pin AFTER INSERT OR DELETE OR UPDATE OF video_id, is_pinned ON videos
FOR EACH STATEMENT EXECUTE FUNCTION igloo_invalidate_unfollow_cleanup();
CREATE TRIGGER igloo_cleanup_repost AFTER INSERT OR DELETE OR UPDATE ON retweet_sources
FOR EACH STATEMENT EXECUTE FUNCTION igloo_invalidate_unfollow_cleanup();
CREATE TRIGGER igloo_cleanup_source AFTER INSERT OR DELETE OR UPDATE OF source_id, enabled ON feed_sources
FOR EACH STATEMENT EXECUTE FUNCTION igloo_invalidate_unfollow_cleanup();
CREATE TRIGGER igloo_cleanup_attribution AFTER INSERT OR DELETE OR UPDATE ON feed_item_sources
FOR EACH STATEMENT EXECUTE FUNCTION igloo_invalidate_unfollow_cleanup();

-- +goose Down
DROP TRIGGER igloo_cleanup_attribution ON feed_item_sources;
DROP TRIGGER igloo_cleanup_source ON feed_sources;
DROP TRIGGER igloo_cleanup_repost ON retweet_sources;
DROP TRIGGER igloo_cleanup_pin ON videos;
DROP TRIGGER igloo_cleanup_bookmark ON bookmarks;
DROP TRIGGER igloo_cleanup_like ON feed_likes;
DROP TRIGGER igloo_cleanup_follow ON channel_follows;
DROP TRIGGER igloo_cleanup_feed ON feed_items;
DROP FUNCTION igloo_invalidate_unfollow_cleanup();
DROP TABLE unfollow_cleanup_state;
DROP TABLE unfollow_cleanup_files;
DROP TABLE unfollow_cleanup_items;
DROP TABLE unfollow_cleanup_jobs;
