package model

import (
	"net/url"
	"regexp"
	"strings"
)

type XBroadcast struct {
	BroadcastID  string `json:"broadcast_id"`
	ChannelID    string `json:"channel_id"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	ThumbnailURL string `json:"thumbnail_url"`
	LiveStatus   string `json:"live_status"`
	ViewerCount  int64  `json:"viewer_count"`
	ObservedAtMs int64  `json:"observed_at_ms"`
	Handle       string `json:"handle"`
	DisplayName  string `json:"display_name"`
	AvatarURL    string `json:"avatar_url"`
}

var xBroadcastLink = regexp.MustCompile(`(?i)https?://(?:www\.)?(?:x|twitter)\.com/i/(?:broadcasts|events|spaces)/[a-z0-9_]+`)

func XBroadcastURLs(text string) []string {
	var links []string
	seen := make(map[string]bool)
	for _, raw := range xBroadcastLink.FindAllString(text, -1) {
		link := NormalizeXBroadcastURL(raw)
		if link != "" && !seen[link] {
			links = append(links, link)
			seen[link] = true
		}
	}
	return links
}

func NormalizeXBroadcastURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" && u.Scheme != "http" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "x.com" && host != "twitter.com" && host != "www.x.com" && host != "www.twitter.com" {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "i" || parts[2] == "" {
		return ""
	}
	if parts[1] != "broadcasts" && parts[1] != "events" && parts[1] != "spaces" {
		return ""
	}
	return "https://x.com/i/" + parts[1] + "/" + parts[2]
}
