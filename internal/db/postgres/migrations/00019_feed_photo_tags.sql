-- +goose Up
-- Channel IDs of the accounts tagged in a post's photos, read from the
-- "tagged" lists that X capture stores on media_json entries.
-- +goose StatementBegin
CREATE FUNCTION igloo_media_tagged_channels(media TEXT) RETURNS TEXT[]
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT COALESCE(array_agg(DISTINCT 'twitter_' || handle), '{}')
    FROM (
        SELECT lower(ltrim(btrim(tag ->> 'handle'), '@')) AS handle
        FROM jsonb_array_elements(
            CASE WHEN pg_input_is_valid(media, 'jsonb') AND jsonb_typeof(media::jsonb) = 'array'
                 THEN media::jsonb ELSE '[]'::jsonb END
        ) AS ref,
        jsonb_array_elements(
            CASE WHEN jsonb_typeof(ref -> 'tagged') = 'array' THEN ref -> 'tagged' ELSE '[]'::jsonb END
        ) AS tag
    ) tags
    WHERE handle ~ '^[a-z0-9_]{1,15}$' AND handle NOT IN ('unknown', 'undefined')
$$;
-- +goose StatementEnd

CREATE INDEX idx_feed_items_tagged_channels ON feed_items
USING GIN (igloo_media_tagged_channels(media_json))
WHERE media_json LIKE '%"tagged"%';

-- Fallback captures (FxTwitter) carry no photo tags. When an update replaces
-- tagged media with untagged media, copy each photo's tags from the stored
-- entry with the same media file ID.
-- +goose StatementBegin
CREATE FUNCTION igloo_keep_media_tags(old_media TEXT, new_media TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT CASE
        WHEN COALESCE(old_media, '') NOT LIKE '%"tagged"%'
          OR COALESCE(new_media, '') LIKE '%"tagged"%'
          OR NOT pg_input_is_valid(new_media, 'jsonb')
          OR NOT pg_input_is_valid(old_media, 'jsonb')
          OR jsonb_typeof(new_media::jsonb) <> 'array'
          OR jsonb_typeof(old_media::jsonb) <> 'array'
        THEN new_media
        ELSE COALESCE((
            SELECT jsonb_agg(
                CASE WHEN jsonb_typeof(ref) = 'object' AND old_tags.tagged IS NOT NULL
                     THEN ref || jsonb_build_object('tagged', old_tags.tagged)
                     ELSE ref END
                ORDER BY position
            )::text
            FROM jsonb_array_elements(new_media::jsonb) WITH ORDINALITY AS refs(ref, position)
            LEFT JOIN LATERAL (
                SELECT old_ref -> 'tagged' AS tagged
                FROM jsonb_array_elements(old_media::jsonb) AS old_ref
                WHERE jsonb_typeof(old_ref -> 'tagged') = 'array'
                  AND substring(old_ref ->> 'url' FROM '/media/([^/.?]+)')
                      = substring(ref ->> 'url' FROM '/media/([^/.?]+)')
                LIMIT 1
            ) old_tags ON TRUE
        ), new_media)
    END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION igloo_feed_keep_media_tags() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    NEW.media_json := igloo_keep_media_tags(OLD.media_json, NEW.media_json);
    NEW.quote_media_json := igloo_keep_media_tags(OLD.quote_media_json, NEW.quote_media_json);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER igloo_feed_keep_media_tags BEFORE UPDATE OF media_json, quote_media_json ON feed_items
FOR EACH ROW EXECUTE FUNCTION igloo_feed_keep_media_tags();

-- +goose Down
DROP TRIGGER igloo_feed_keep_media_tags ON feed_items;
DROP FUNCTION igloo_feed_keep_media_tags();
DROP FUNCTION igloo_keep_media_tags(TEXT, TEXT);
DROP INDEX idx_feed_items_tagged_channels;
DROP FUNCTION igloo_media_tagged_channels(TEXT);
