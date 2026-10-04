-- +goose Up
UPDATE channels
SET url = 'https://www.youtube.com/channel/' || substring(channel_id FROM 9),
    last_checked = 0
WHERE channel_id LIKE 'youtube_UC%'
  AND url ~* '^https?://((www|m|music)\.)?youtube\.com/(watch|shorts|embed)([/?#]|$)|^https?://(www\.)?youtu\.be/';

-- +goose Down
SELECT 1;
