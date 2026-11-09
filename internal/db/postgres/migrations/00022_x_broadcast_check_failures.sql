-- +goose Up
-- Failed live-status checks of a saved X broadcast or Space link in a row.
-- Each failure doubles the wait before the next check, so links deleted on X
-- stop taking turns from account fetches.
ALTER TABLE x_broadcast_sources ADD COLUMN check_failures INTEGER NOT NULL DEFAULT 0;

-- Unresolved links with a check time have been failing hourly for days.
UPDATE x_broadcast_sources s SET check_failures = 7
WHERE s.checked_at_ms > 0
  AND NOT EXISTS (SELECT 1 FROM x_broadcasts b WHERE b.broadcast_id = s.broadcast_id AND b.live_status <> '');

-- +goose Down
ALTER TABLE x_broadcast_sources DROP COLUMN check_failures;
