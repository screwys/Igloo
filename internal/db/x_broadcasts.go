package db

import (
	"database/sql"
	"errors"

	"github.com/screwys/igloo/internal/model"
)

const seedSavedXBroadcastSourcesSQL = `INSERT INTO x_broadcast_sources (url, channel_id, published_at_ms)
	SELECT DISTINCT ON (url) url, channel_id, COALESCE(published_at,0) FROM (
		SELECT 'https://x.com/i/' || matched[1] || '/' || matched[2] AS url, content.channel_id,content.published_at
		FROM (
			SELECT channel_id, body_text AS body,published_at FROM feed_items
			UNION ALL
			SELECT quote_channel_id, quote_body_text,published_at FROM feed_items
		) content
		CROSS JOIN LATERAL regexp_matches(COALESCE(content.body,''),
			'https?://(?:www\.)?(?:x|twitter)\.com/i/(broadcasts|events|spaces)/([a-z0-9_]+)', 'gi') AS matched
		WHERE content.channel_id LIKE 'twitter_%'
	) links
	ORDER BY url, published_at DESC NULLS LAST, channel_id
	ON CONFLICT (url) DO NOTHING`

func seedXBroadcastSourcesTx(tx *sql.Tx, item model.FeedItem) error {
	var publishedAtMs int64
	if item.PublishedAt != nil {
		publishedAtMs = item.PublishedAt.UnixMilli()
	}
	for _, content := range []struct {
		channelID string
		body      string
	}{{item.ChannelID, item.BodyText}, {item.QuoteChannelID, item.QuoteBodyText}} {
		if content.channelID == "" {
			continue
		}
		for _, rawURL := range model.XBroadcastURLs(content.body) {
			if _, err := tx.Exec(`INSERT INTO x_broadcast_sources (url,channel_id,published_at_ms)
				VALUES ($1,$2,$3) ON CONFLICT (url) DO UPDATE SET
				published_at_ms=excluded.published_at_ms
				WHERE x_broadcast_sources.published_at_ms<excluded.published_at_ms`, rawURL, content.channelID, publishedAtMs); err != nil {
				return err
			}
		}
	}
	return nil
}

// XBroadcastSources returns saved links that still need live status checks.
func (db *DB) XBroadcastSources(active bool) ([]string, error) {
	rows, err := db.reader().Query(`SELECT s.url FROM x_broadcast_sources s
		LEFT JOIN x_broadcasts b ON b.broadcast_id=s.broadcast_id
		WHERE (($1 AND b.live_status IN ('is_live','is_upcoming'))
			OR (NOT $1 AND (b.broadcast_id IS NULL OR b.live_status='')
				AND s.checked_at_ms < (extract(epoch FROM now())*1000)::bigint-3600000))
		AND (EXISTS (SELECT 1 FROM channel_follows cf
			WHERE cf.channel_id=s.channel_id OR cf.channel_id=b.channel_id)
			OR EXISTS (SELECT 1 FROM x_space_presence p JOIN channel_follows cf ON cf.channel_id=p.channel_id
				WHERE s.url='https://x.com/i/spaces/' || p.space_id))
		ORDER BY CASE b.live_status WHEN 'is_live' THEN 0 WHEN 'is_upcoming' THEN 1 ELSE 2 END,
		s.published_at_ms DESC,s.checked_at_ms,s.url`, active)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var sources []string
	for rows.Next() {
		var rawURL string
		if err := rows.Scan(&rawURL); err != nil {
			return nil, err
		}
		sources = append(sources, rawURL)
	}
	return sources, rows.Err()
}

func (db *DB) FollowedXLiveAccounts() ([]model.XLiveAccount, error) {
	rows, err := db.reader().Query(`SELECT c.channel_id, COALESCE(cp.account_details_json,'')
		FROM channels c JOIN channel_follows cf ON cf.channel_id=c.channel_id
		LEFT JOIN channel_profiles cp ON cp.channel_id=c.channel_id
		WHERE c.platform='twitter' ORDER BY c.channel_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	accounts := []model.XLiveAccount{}
	for rows.Next() {
		var account model.XLiveAccount
		var details string
		if err := rows.Scan(&account.ChannelID, &details); err != nil {
			return nil, err
		}
		account.UserID = model.ParseAccountDetails(details).UserID
		if account.UserID != "" {
			accounts = append(accounts, account)
		}
	}
	return accounts, rows.Err()
}

func (db *DB) ReplaceXSpacePresence(accounts []model.XLiveAccount, presence []model.XSpacePresence, observedAtMs int64) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		channelsByUser := make(map[string][]string, len(accounts))
		for _, account := range accounts {
			channelsByUser[account.UserID] = append(channelsByUser[account.UserID], account.ChannelID)
			if _, err := tx.Exec(`DELETE FROM x_space_presence WHERE channel_id=$1`, account.ChannelID); err != nil {
				return err
			}
		}
		for _, room := range presence {
			if _, err := tx.Exec(`UPDATE x_broadcasts SET live_status='is_live',
				title=CASE WHEN $2!='' THEN $2 ELSE title END,viewer_count=$3,observed_at_ms=$4
				WHERE url=$1`, "https://x.com/i/spaces/"+room.SpaceID, room.Title, room.ViewerCount, observedAtMs); err != nil {
				return err
			}
			for _, channelID := range channelsByUser[room.UserID] {
				if _, err := tx.Exec(`INSERT INTO x_space_presence (channel_id,space_id,observed_at_ms)
					VALUES ($1,$2,$3) ON CONFLICT (channel_id) DO UPDATE SET
					space_id=excluded.space_id,observed_at_ms=excluded.observed_at_ms`,
					channelID, room.SpaceID, observedAtMs); err != nil {
					return err
				}
				if _, err := tx.Exec(`INSERT INTO x_broadcast_sources (url,channel_id,published_at_ms)
					VALUES ($1,$2,$3) ON CONFLICT (url) DO UPDATE SET
					channel_id=excluded.channel_id,published_at_ms=excluded.published_at_ms,
					checked_at_ms=CASE WHEN x_broadcast_sources.broadcast_id IS NULL THEN 0
						ELSE x_broadcast_sources.checked_at_ms END`,
					"https://x.com/i/spaces/"+room.SpaceID, channelID, observedAtMs); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (db *DB) RecordXBroadcastCheck(rawURL string, checkedAtMs int64) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE x_broadcast_sources SET checked_at_ms=$2 WHERE url=$1`, rawURL, checkedAtMs)
		return err
	})
}

func (db *DB) XBroadcastSourceChannelID(rawURL string) (string, error) {
	var channelID string
	err := db.reader().QueryRow(`SELECT channel_id FROM x_broadcast_sources WHERE url=$1`, rawURL).Scan(&channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return channelID, err
}

func (db *DB) ObserveXBroadcast(sourceURL string, broadcast model.XBroadcast) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO x_broadcasts
			(broadcast_id,channel_id,url,title,thumbnail_url,live_status,viewer_count,observed_at_ms)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (broadcast_id) DO UPDATE SET channel_id=excluded.channel_id,url=excluded.url,
			title=excluded.title,thumbnail_url=excluded.thumbnail_url,live_status=excluded.live_status,
			viewer_count=excluded.viewer_count,observed_at_ms=excluded.observed_at_ms`,
			broadcast.BroadcastID, broadcast.ChannelID, broadcast.URL, broadcast.Title, broadcast.ThumbnailURL,
			broadcast.LiveStatus, broadcast.ViewerCount, broadcast.ObservedAtMs); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO x_broadcast_sources (url,channel_id,broadcast_id,checked_at_ms)
			VALUES ($1,$2,$3,$4) ON CONFLICT (url) DO UPDATE SET
			broadcast_id=excluded.broadcast_id,checked_at_ms=excluded.checked_at_ms`,
			sourceURL, broadcast.ChannelID, broadcast.BroadcastID, broadcast.ObservedAtMs)
		return err
	})
}

const xBroadcastProjection = `SELECT b.broadcast_id,b.channel_id,b.url,b.title,b.thumbnail_url,
	b.live_status,b.viewer_count,b.observed_at_ms,
	COALESCE(NULLIF(cp.handle,''),NULLIF(c.source_id,''),substr(c.channel_id,9)),
	COALESCE(NULLIF(cp.display_name,''),NULLIF(c.name,''),c.channel_id)
	FROM `

const xBroadcastJoins = ` b JOIN channels c ON c.channel_id=b.channel_id
	LEFT JOIN channel_profiles cp ON cp.channel_id=b.channel_id `

func scanXBroadcast(row interface{ Scan(...any) error }) (model.XBroadcast, error) {
	var broadcast model.XBroadcast
	err := row.Scan(&broadcast.BroadcastID, &broadcast.ChannelID, &broadcast.URL, &broadcast.Title,
		&broadcast.ThumbnailURL, &broadcast.LiveStatus, &broadcast.ViewerCount, &broadcast.ObservedAtMs,
		&broadcast.Handle, &broadcast.DisplayName)
	broadcast.AvatarURL = "/api/media/avatar/" + broadcast.ChannelID
	return broadcast, err
}

func (db *DB) ListXBroadcasts() ([]model.XBroadcast, error) {
	rows, err := db.reader().Query(xBroadcastProjection + `x_followed_broadcasts` + xBroadcastJoins +
		`ORDER BY b.observed_at_ms DESC,b.broadcast_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	broadcasts := []model.XBroadcast{}
	for rows.Next() {
		broadcast, err := scanXBroadcast(rows)
		if err != nil {
			return nil, err
		}
		broadcasts = append(broadcasts, broadcast)
	}
	return broadcasts, rows.Err()
}

func (db *DB) GetXBroadcast(broadcastID string) (*model.XBroadcast, error) {
	broadcast, err := scanXBroadcast(db.reader().QueryRow(xBroadcastProjection+`x_broadcasts`+xBroadcastJoins+`WHERE b.broadcast_id=$1`, broadcastID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &broadcast, nil
}
