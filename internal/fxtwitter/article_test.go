package fxtwitter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestTweetArticlePreservesBodyAndEmbeddedMedia(t *testing.T) {
	var raw rawTweet
	err := json.Unmarshal([]byte(`{
		"id":"100","text":"Preview","author":{"screen_name":"sample_author"},
		"article":{
			"title":"Article title",
			"content":{"blocks":[{"text":"First paragraph"},{"text":"Second paragraph"},{"entityRanges":[{"key":0}]},{"text":"\ufffc","entityRanges":[{"key":1,"offset":0,"length":1}]}],"entityMap":[{"key":"0","value":{"type":"TWEET","data":{"tweetId":"200"}}},{"key":"1","value":{"type":"MARKDOWN","data":{"markdown":"sample code"}}}]},
			"cover_media":{"media_info":{"original_img_url":"https://pbs.twimg.com/media/cover.jpg","original_img_width":1200,"original_img_height":800}},
			"media_entities":[{"media_info":{"media_url_https":"https://pbs.twimg.com/media/preview.jpg","original_info":{"width":640,"height":360},"video_info":{"variants":[{"content_type":"application/x-mpegURL","url":"https://video.twimg.com/video.m3u8"},{"content_type":"video/mp4","bitrate":100,"url":"https://video.twimg.com/low.mp4"},{"content_type":"video/mp4","bitrate":500,"url":"https://video.twimg.com/high.mp4"}]}}}]
		},
		"quote":{"id":"300","article":{"title":"Quoted article","content":{"blocks":[{"text":"Quote article body"}]}}}
	}`), &raw)
	if err != nil {
		t.Fatal(err)
	}
	tweet := tweetFromRaw(&raw)
	if tweet.ArticleTitle != "Article title" || tweet.Text != "First paragraph\n\nSecond paragraph\n\nhttps://x.com/i/status/200\n\nsample code" {
		t.Fatalf("article = %+v", tweet)
	}
	var media []model.MediaRef
	if err := json.Unmarshal([]byte(tweet.MediaJSON), &media); err != nil {
		t.Fatal(err)
	}
	if len(media) != 2 || media[0].Type != "photo" || media[1].Type != "video" || !strings.HasSuffix(media[1].URL, "high.mp4") || media[1].Width != 640 {
		t.Fatalf("media = %+v", media)
	}
	if tweet.Quote.ArticleTitle != "Quoted article" || tweet.Quote.Text != "Quote article body" {
		t.Fatalf("quoted article = %+v", tweet.Quote)
	}
}

func TestArticleVideoUsesDirectVariantsAndPreview(t *testing.T) {
	var raw rawTweet
	if err := json.Unmarshal([]byte(`{"article":{"media_entities":[{"media_info":{"preview_image":{"original_img_url":"https://pbs.twimg.com/media/preview.jpg","original_img_width":1280,"original_img_height":720},"variants":[{"url":"https://video.twimg.com/article.m3u8"},{"url":"https://video.twimg.com/article.mp4","bit_rate":500}]}}]}}`), &raw); err != nil {
		t.Fatal(err)
	}
	var media []model.MediaRef
	if err := json.Unmarshal([]byte(tweetFromRaw(&raw).MediaJSON), &media); err != nil {
		t.Fatal(err)
	}
	if len(media) != 1 || media[0].Type != "video" || media[0].Width != 1280 || media[0].Height != 720 || media[0].ThumbnailURL == "" || !strings.HasSuffix(media[0].URL, ".mp4") {
		t.Fatalf("article video lost: %+v", media)
	}
}
