-- +goose Up
UPDATE videos v
SET title = b.title
FROM youtube_broadcasts b
WHERE b.video_id = v.video_id AND b.channel_id = v.channel_id
  AND b.title <> ''
  AND left(v.title, length(b.title) + 1) = b.title || ' '
  AND substring(v.title FROM length(b.title) + 2) ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}$';

-- +goose Down
SELECT 1;
