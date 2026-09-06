package fxtwitter

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/screwys/igloo/internal/model"
)

type rawArticle struct {
	Title   string `json:"title"`
	Content struct {
		Blocks []struct {
			Text         string `json:"text"`
			EntityRanges []struct {
				Key    int `json:"key"`
				Offset int `json:"offset"`
				Length int `json:"length"`
			} `json:"entityRanges"`
			Data struct {
				URLs []struct {
					Text string `json:"text"`
				} `json:"urls"`
			} `json:"data"`
		} `json:"blocks"`
		EntityMap []struct {
			Key   string `json:"key"`
			Value struct {
				Type string `json:"type"`
				Data struct {
					Markdown string `json:"markdown"`
					TweetID  string `json:"tweetId"`
				} `json:"data"`
			} `json:"value"`
		} `json:"entityMap"`
	} `json:"content"`
	CoverMedia    rawArticleMedia   `json:"cover_media"`
	MediaEntities []rawArticleMedia `json:"media_entities"`
}

type rawArticleMedia struct {
	Info struct {
		ImageURL     string `json:"original_img_url"`
		Width        int    `json:"original_img_width"`
		Height       int    `json:"original_img_height"`
		ThumbnailURL string `json:"media_url_https"`
		AltText      string `json:"ext_alt_text"`
		PreviewImage struct {
			URL    string `json:"original_img_url"`
			Width  int    `json:"original_img_width"`
			Height int    `json:"original_img_height"`
		} `json:"preview_image"`
		Variants     []rawArticleVideoVariant `json:"variants"`
		OriginalInfo struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"original_info"`
		VideoInfo struct {
			Variants []rawArticleVideoVariant `json:"variants"`
		} `json:"video_info"`
	} `json:"media_info"`
}

type rawArticleVideoVariant struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Bitrate     int    `json:"bitrate"`
	BitRate     int    `json:"bit_rate"`
}

func applyArticle(tweet *Tweet, article *rawArticle) {
	if article == nil {
		return
	}
	var paragraphs []string
	for _, block := range article.Content.Blocks {
		text := block.Text
		sort.SliceStable(block.EntityRanges, func(i, j int) bool { return block.EntityRanges[i].Offset > block.EntityRanges[j].Offset })
		for _, ref := range block.EntityRanges {
			for _, entity := range article.Content.EntityMap {
				if entity.Key != strconv.Itoa(ref.Key) {
					continue
				}
				replacement := ""
				switch entity.Value.Type {
				case "MARKDOWN":
					replacement = entity.Value.Data.Markdown
				case "TWEET":
					if entity.Value.Data.TweetID != "" {
						replacement = "https://x.com/i/status/" + entity.Value.Data.TweetID
					}
				case "MEDIA":
				default:
					continue
				}
				encoded := utf16.Encode([]rune(text))
				if ref.Offset >= 0 && ref.Length >= 0 && ref.Offset <= len(encoded) && ref.Length <= len(encoded)-ref.Offset {
					text = string(utf16.Decode(encoded[:ref.Offset])) + replacement + string(utf16.Decode(encoded[ref.Offset+ref.Length:]))
				}
			}
		}
		for _, link := range block.Data.URLs {
			if (strings.HasPrefix(link.Text, "https://") || strings.HasPrefix(link.Text, "http://")) && !strings.Contains(text, link.Text) {
				text += " (" + link.Text + ")"
			}
		}
		if text = strings.TrimSpace(text); text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	if len(paragraphs) > 0 {
		tweet.ArticleTitle = strings.TrimSpace(article.Title)
		tweet.Text = strings.Join(paragraphs, "\n\n")
	}
	var media []model.MediaRef
	_ = json.Unmarshal([]byte(tweet.MediaJSON), &media)
	appendMedia := func(raw rawArticleMedia) {
		info := raw.Info
		ref := model.MediaRef{URL: info.ImageURL, Type: "photo", Width: info.Width, Height: info.Height, AltText: info.AltText}
		bitrate := -1
		variants := info.Variants
		if len(variants) == 0 {
			variants = info.VideoInfo.Variants
		}
		for _, variant := range variants {
			rate := max(variant.Bitrate, variant.BitRate)
			if (variant.ContentType == "video/mp4" || strings.Contains(variant.URL, ".mp4")) && variant.URL != "" && rate > bitrate {
				bitrate = rate
				ref = model.MediaRef{URL: variant.URL, Type: "video", ThumbnailURL: info.ThumbnailURL, Width: info.OriginalInfo.Width, Height: info.OriginalInfo.Height, AltText: info.AltText}
				if info.PreviewImage.URL != "" {
					ref.ThumbnailURL, ref.Width, ref.Height = info.PreviewImage.URL, info.PreviewImage.Width, info.PreviewImage.Height
				}
			}
		}
		if ref.URL != "" {
			media = append(media, ref)
		}
	}
	appendMedia(article.CoverMedia)
	for _, raw := range article.MediaEntities {
		appendMedia(raw)
	}
	if len(media) > 0 {
		encoded, _ := json.Marshal(media)
		tweet.MediaJSON = string(encoded)
	}
}
