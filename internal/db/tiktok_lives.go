package db

import (
	"database/sql"
	"errors"

	"github.com/screwys/igloo/internal/model"
)

func (db *DB) ObserveTikTokLive(channelID string, live *model.TikTokLive) error {
	if live == nil {
		return db.WithWrite(func(tx *sql.Tx) error {
			_, err := tx.Exec(`DELETE FROM tiktok_lives WHERE channel_id = $1`, channelID)
			return err
		})
	}
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO tiktok_lives
		(channel_id, room_id, title, viewer_count, observed_at_ms)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT(channel_id) DO UPDATE SET room_id=excluded.room_id,
		title=excluded.title, viewer_count=excluded.viewer_count, observed_at_ms=excluded.observed_at_ms`,
			channelID, live.RoomID, live.Title, live.ViewerCount, live.ObservedAtMs)
		return err
	})
}

func (db *DB) FollowedTikTokHandles() ([]string, error) {
	rows, err := db.reader().Query(`SELECT COALESCE(NULLIF(cp.handle,''),NULLIF(c.source_id,''),substr(c.channel_id,8))
		FROM channels c JOIN channel_follows cf ON cf.channel_id=c.channel_id
		LEFT JOIN channel_profiles cp ON cp.channel_id=c.channel_id WHERE c.platform='tiktok'
		ORDER BY c.channel_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var handles []string
	for rows.Next() {
		var handle string
		if err := rows.Scan(&handle); err != nil {
			return nil, err
		}
		handles = append(handles, handle)
	}
	return handles, rows.Err()
}

const tiktokLiveProjection = `SELECT l.channel_id,l.room_id,
	COALESCE(NULLIF(cp.handle,''),NULLIF(c.source_id,''),substr(c.channel_id,8)),
	COALESCE(NULLIF(cp.display_name,''),NULLIF(c.name,''),c.channel_id),
	l.title,l.viewer_count,l.observed_at_ms
	FROM tiktok_lives l JOIN channels c ON c.channel_id=l.channel_id
	LEFT JOIN channel_profiles cp ON cp.channel_id=c.channel_id `

func scanTikTokLive(rows interface{ Scan(...any) error }) (model.TikTokLive, error) {
	var live model.TikTokLive
	err := rows.Scan(&live.ChannelID, &live.RoomID, &live.Handle, &live.DisplayName, &live.Title, &live.ViewerCount, &live.ObservedAtMs)
	live.AvatarURL = "/api/media/avatar/" + live.ChannelID
	return live, err
}

func (db *DB) ListTikTokLives() ([]model.TikTokLive, error) {
	rows, err := db.reader().Query(tiktokLiveProjection + `JOIN channel_follows cf ON cf.channel_id=l.channel_id ORDER BY l.observed_at_ms DESC,l.channel_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	lives := []model.TikTokLive{}
	for rows.Next() {
		live, err := scanTikTokLive(rows)
		if err != nil {
			return nil, err
		}
		lives = append(lives, live)
	}
	return lives, rows.Err()
}

func (db *DB) GetTikTokLive(channelID string) (*model.TikTokLive, error) {
	live, err := scanTikTokLive(db.reader().QueryRow(tiktokLiveProjection+`WHERE l.channel_id=$1`, channelID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &live, nil
}
