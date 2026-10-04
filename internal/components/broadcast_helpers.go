package components

import (
	"github.com/screwys/igloo/internal/model"
	"net/url"
)

func broadcastHref(p PageProps, broadcast model.YouTubeBroadcast) string {
	if p.UserRole != "admin" || broadcast.LiveStatus == "is_upcoming" {
		return "https://www.youtube.com/watch?v=" + url.QueryEscape(broadcast.VideoID)
	}
	href := "/temp/watch?v=" + url.QueryEscape(broadcast.VideoID)
	if broadcast.LiveStatus == "is_live" || broadcast.LiveStatus == "post_live" {
		href += "&mode=stream"
	}
	return href
}
