-- +goose Up
ALTER TABLE feed_seen ADD COLUMN channel_id TEXT, ADD COLUMN source_channel_id TEXT;

-- +goose StatementBegin
CREATE FUNCTION igloo_rebuild_feed_seen_roles() RETURNS VOID
LANGUAGE SQL AS $$
    UPDATE feed_seen seen
    SET (channel_id, source_channel_id) = (
        SELECT NULLIF(item.channel_id, ''), NULLIF(item.source_channel_id, '')
        FROM feed_items item
        WHERE item.tweet_id = seen.tweet_id AND COALESCE(item.is_ghost, 0) = 0
    );
$$;
-- +goose StatementEnd
SELECT igloo_rebuild_feed_seen_roles();

CREATE INDEX idx_feed_seen_channel_at ON feed_seen(channel_id, seen_at DESC)
WHERE channel_id IS NOT NULL AND channel_id != '';
CREATE INDEX idx_feed_seen_source_channel_at ON feed_seen(source_channel_id, seen_at DESC)
WHERE source_channel_id IS NOT NULL AND source_channel_id != '';

-- +goose StatementBegin
CREATE FUNCTION igloo_capture_feed_seen_roles() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    SELECT NULLIF(item.channel_id, ''), NULLIF(item.source_channel_id, '')
    INTO NEW.channel_id, NEW.source_channel_id
    FROM feed_items item
    WHERE item.tweet_id = NEW.tweet_id AND COALESCE(item.is_ghost, 0) = 0;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER igloo_capture_feed_seen_roles BEFORE INSERT OR UPDATE OF tweet_id ON feed_seen
FOR EACH ROW EXECUTE FUNCTION igloo_capture_feed_seen_roles();

-- +goose StatementBegin
CREATE FUNCTION igloo_update_feed_seen_roles() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
    author_id TEXT;
    source_id TEXT;
BEGIN
    IF TG_OP = 'UPDATE' AND
       (OLD.tweet_id, OLD.channel_id, OLD.source_channel_id, OLD.is_ghost)
       IS NOT DISTINCT FROM
       (NEW.tweet_id, NEW.channel_id, NEW.source_channel_id, NEW.is_ghost) THEN
        RETURN NULL;
    END IF;
    IF TG_OP = 'DELETE' OR (TG_OP = 'UPDATE' AND OLD.tweet_id IS DISTINCT FROM NEW.tweet_id) THEN
        UPDATE feed_seen SET channel_id = NULL, source_channel_id = NULL
        WHERE tweet_id = OLD.tweet_id AND (channel_id IS NOT NULL OR source_channel_id IS NOT NULL);
    END IF;
    IF TG_OP != 'DELETE' THEN
        IF COALESCE(NEW.is_ghost, 0) = 0 THEN
            author_id := NULLIF(NEW.channel_id, '');
            source_id := NULLIF(NEW.source_channel_id, '');
        END IF;
        UPDATE feed_seen SET channel_id = author_id, source_channel_id = source_id
        WHERE tweet_id = NEW.tweet_id AND
              (channel_id, source_channel_id) IS DISTINCT FROM (author_id, source_id);
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER igloo_update_feed_seen_roles
AFTER INSERT OR DELETE OR UPDATE OF tweet_id, channel_id, source_channel_id, is_ghost ON feed_items
FOR EACH ROW EXECUTE FUNCTION igloo_update_feed_seen_roles();

-- +goose Down
DROP TRIGGER igloo_update_feed_seen_roles ON feed_items;
DROP TRIGGER igloo_capture_feed_seen_roles ON feed_seen;
DROP FUNCTION igloo_update_feed_seen_roles();
DROP FUNCTION igloo_capture_feed_seen_roles();
DROP FUNCTION igloo_rebuild_feed_seen_roles();
DROP INDEX idx_feed_seen_channel_at;
DROP INDEX idx_feed_seen_source_channel_at;
ALTER TABLE feed_seen DROP COLUMN channel_id, DROP COLUMN source_channel_id;
