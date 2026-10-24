-- +goose Up
-- Comments belong to their video. Earlier video deletes left them behind.
-- Android dropped these with their videos, so the per-row head trigger is
-- skipped; it would bump the sync clock once per comment in one transaction.
ALTER TABLE video_comments DISABLE TRIGGER igloo_sync_head_video_comments;
DELETE FROM video_comments c WHERE NOT EXISTS (SELECT 1 FROM videos v WHERE v.video_id = c.video_id);
ALTER TABLE video_comments ENABLE TRIGGER igloo_sync_head_video_comments;
ALTER TABLE video_comments ADD CONSTRAINT video_comments_video_id_fkey
    FOREIGN KEY (video_id) REFERENCES videos(video_id) ON DELETE CASCADE;
-- Identity media pruning checks whether a comment author still has comments.
CREATE INDEX idx_video_comments_author ON video_comments(author_id);

-- +goose Down
DROP INDEX idx_video_comments_author;
ALTER TABLE video_comments DROP CONSTRAINT video_comments_video_id_fkey;
