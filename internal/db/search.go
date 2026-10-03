package db

import (
	"context"
	"database/sql"
	"strings"
	"time"

	dbquery "github.com/screwys/igloo/internal/db/query"
	"github.com/screwys/igloo/internal/model"
)

// SearchChannels searches followed channels by their stored names and profiles.
func (db *DB) SearchChannels(search string, limit int) ([]model.Channel, error) {
	if strings.TrimSpace(search) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.queries().SearchChannels(context.Background(), dbquery.SearchChannelsParams{Search: search, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	var channels []model.Channel
	for _, row := range rows {
		channels = append(channels, model.Channel{
			ChannelID: row.ChannelID, Name: row.Name, SourceID: row.SourceID, Platform: row.Platform,
			IsStarred: row.IsStarred != 0, Handle: row.Handle, DisplayName: row.DisplayName,
			AvatarURL: "/api/media/avatar/" + row.ChannelID,
		})
	}
	return channels, nil
}

// SearchVideosFast searches the stored video text and channel metadata.
func (db *DB) SearchVideosFast(search string, limit int) ([]model.Video, error) {
	if strings.TrimSpace(search) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	rows, err := db.queries().SearchVideos(context.Background(), dbquery.SearchVideosParams{Search: search, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	var videos []model.Video
	for _, row := range rows {
		video := model.Video{
			VideoID: row.VideoID, Title: row.Title, ChannelName: row.ChannelName, ChannelID: row.ChannelID, Platform: row.Platform,
			PublishedAt: millisToTimePtr(sql.NullInt64{Int64: row.PublishedAt, Valid: true}), IsTemp: row.IsTemp != 0,
			ThumbnailURL: "/api/media/thumbnail/" + row.VideoID,
		}
		if row.ChannelID != "" {
			video.AvatarURL = "/api/media/avatar/" + row.ChannelID
		}
		if row.DearrowTitle.Valid {
			video.DearrowTitle = &row.DearrowTitle.String
		}
		if row.DearrowTitleCasual.Valid {
			video.DearrowTitleCasual = &row.DearrowTitleCasual.String
		}
		videos = append(videos, video)
	}
	return videos, nil
}

// SearchFeedItems searches body text and author metadata using the same page order.
func (db *DB) SearchFeedItems(search string, limit int) ([]model.FeedItem, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.queries().SearchFeedItems(context.Background(), dbquery.SearchFeedItemsParams{Pattern: "%" + search + "%", PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	var items []model.FeedItem
	for _, row := range rows {
		item := model.FeedItem{
			TweetID: row.TweetID.String, SourceHandle: row.SourceHandle, AuthorHandle: row.AuthorHandle,
			AuthorDisplayName: row.AuthorDisplayName, AuthorAvatarURL: row.AuthorAvatarUrl,
			BodyText: row.BodyText, ArticleTitle: row.ArticleTitle, PollJSON: row.PollJson, CommunityNote: row.CommunityNote, Lang: row.Lang,
			IsRetweet: row.IsRetweet != 0, RetweetedByHandle: row.RetweetedByHandle, RetweetedByDisplayName: row.RetweetedByDisplayName,
			QuoteTweetID: row.QuoteTweetID, QuoteAuthorHandle: row.QuoteAuthorHandle, QuoteAuthorDisplayName: row.QuoteAuthorDisplayName,
			QuoteAuthorAvatarURL: row.QuoteAuthorAvatarUrl, QuoteBodyText: row.QuoteBodyText, QuoteArticleTitle: row.QuoteArticleTitle,
			QuotePollJSON: row.QuotePollJson, QuoteCommunityNote: row.QuoteCommunityNote, QuoteLang: row.QuoteLang,
			QuoteMediaJSON: row.QuoteMediaJson, MediaJSON: row.MediaJson, CanonicalURL: row.CanonicalUrl,
			ReplyToHandle: row.ReplyToHandle, ReplyToStatus: row.ReplyToStatus, IsReply: row.IsReply != 0, IsGhost: row.IsGhost != 0,
			QuotePublishedAt: millisToTimePtr(sql.NullInt64{Int64: row.QuotePublishedAt, Valid: true}),
			Views:            row.Views, Likes: row.Likes, Retweets: row.Retweets,
			PublishedAt: millisToTimePtr(sql.NullInt64{Int64: row.PublishedAt, Valid: true}),
			ContentHash: row.ContentHash, CanonicalTweetID: row.CanonicalTweetID,
			SourceChannelID: row.SourceChannelID, ChannelID: row.ChannelID, QuoteChannelID: row.QuoteChannelID,
			ReplyChannelID: row.ReplyChannelID, ReposterChannelID: row.ReposterChannelID,
		}
		if row.FetchedAt != 0 {
			item.FetchedAt = time.UnixMilli(row.FetchedAt)
		}
		item.ParseMedia()
		items = append(items, item)
	}
	return items, nil
}
