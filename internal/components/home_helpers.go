package components

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/screwys/igloo/internal/home"
	"github.com/screwys/igloo/internal/model"
)

func homeJSON(value any) string {
	b, _ := json.Marshal(value)
	return string(b)
}

func homeWidgetDefaults() []home.Widget {
	widgets := make([]home.Widget, 0, len(home.Kinds))
	for _, kind := range home.Kinds {
		widgets = append(widgets, home.NewWidget(kind.Type, kind.Type))
	}
	return widgets
}

func homeWidgetLabel(p PageProps, kind string) string {
	switch kind {
	case "continue":
		return L(p, "home_continue", "Continue watching")
	case "live":
		return L(p, "home_live", "Live")
	case "starred":
		return L(p, "home_starred", "Starred accounts")
	case "moments":
		return L(p, "nav_moments", "Moments")
	case "saved":
		return L(p, "home_saved", "Saved")
	case "account":
		return L(p, "home_account", "From an account")
	default:
		return L(p, "home_latest", "Latest videos")
	}
}

func homeWidgetTitle(p PageProps, widget home.Widget) string {
	if widget.Title != "" {
		return widget.Title
	}
	return homeWidgetLabel(p, widget.Type)
}

func homeWidgetIcon(kind string) string {
	if kind == "continue" {
		return "PlayCircle"
	}
	for _, k := range home.Kinds {
		if k.Type == kind {
			return k.Icon
		}
	}
	return "VideoLibrary"
}

func homeGridStyle(layout home.Layout) string {
	return fmt.Sprintf("--home-columns:%d;--home-tablet-columns:%d;", layout.Columns*4, min(layout.Columns*4, 8))
}

func homeWidgetSpan(columns int, size string) int {
	full := columns * 4
	if full < 4 {
		full = 12
	}
	switch size {
	case "full":
		return full
	case "large":
		if columns == 2 {
			return full
		}
		return max(4, (full*2+2)/3)
	case "medium":
		return max(4, full/2)
	default:
		return 4
	}
}

func homeWidgetStyle(columns int, widget home.Widget) string {
	return fmt.Sprintf("--home-span:%d;", homeWidgetSpan(columns, widget.Size))
}

type homeAccountLane struct {
	ChannelID string
	Name      string
	AvatarURL string
	Items     []HomeEntry
}

func homeAccountLanes(items []HomeEntry) []homeAccountLane {
	lanes := make([]homeAccountLane, 0)
	indices := make(map[string]int)
	for _, item := range items {
		channelID, name, avatar := item.Video.ChannelID, videoChannelName(item.Video), item.Video.AvatarURL
		if item.Feed != nil {
			channelID, name, avatar = item.Feed.ChannelID, feedAuthorLabel(*item.Feed), item.Feed.AuthorAvatarURL
		}
		index, exists := indices[channelID]
		if !exists {
			index = len(lanes)
			indices[channelID] = index
			lanes = append(lanes, homeAccountLane{ChannelID: channelID, Name: name, AvatarURL: avatar})
		}
		lanes[index].Items = append(lanes[index].Items, item)
	}
	return lanes
}

type homeMediaPreview struct {
	Kind         string
	URL          string
	StreamURL    string
	PosterURL    string
	PlaybackKind string
	AltText      string
}

func homeFeedPreviews(refs []model.MediaRef, slides []string, stream, preview string) []homeMediaPreview {
	items := make([]homeMediaPreview, 0, len(refs))
	for index, ref := range refs {
		item := homeMediaPreview{Kind: "image", URL: feedMediaURLAt(slides, index), AltText: ref.AltText}
		if feedMediaRefIsVideo(ref) {
			item.Kind, item.PlaybackKind = "video", feedMediaPlaybackKind(ref)
			item.StreamURL = item.URL
			if len(refs) == 1 && stream != "" {
				item.StreamURL = stream
			}
			item.PosterURL = ref.ThumbnailURL
			if item.PosterURL == "" && index == 0 {
				item.PosterURL = preview
			}
		} else {
			item.PosterURL = item.URL
		}
		if item.URL != "" || item.StreamURL != "" {
			items = append(items, item)
		}
	}
	return items
}

func homeBroadcastChannel(data HomePageData, broadcast model.YouTubeBroadcast) model.Channel {
	for _, channel := range data.Channels {
		if channel.ChannelID == broadcast.ChannelID {
			return channel
		}
	}
	return model.Channel{ChannelID: broadcast.ChannelID, Platform: "youtube"}
}

func homeBroadcastLabel(p PageProps, status string) string {
	switch status {
	case "is_live":
		return L(p, "home_live", "Live")
	case "is_upcoming":
		return L(p, "home_upcoming", "Upcoming")
	case "post_live":
		return L(p, "home_processing", "Processing")
	default:
		return L(p, "home_replay", "Replay")
	}
}

func homeBroadcastTime(broadcast model.YouTubeBroadcast) *time.Time {
	ms := broadcast.PublishedAtMs
	if broadcast.LiveStatus == "is_upcoming" && broadcast.StartsAtMs > 0 {
		ms = broadcast.StartsAtMs
	}
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms)
	return &t
}

func homeBroadcastHref(p PageProps, broadcast model.YouTubeBroadcast) string {
	if p.UserRole != "admin" || broadcast.LiveStatus == "is_upcoming" {
		return "https://www.youtube.com/watch?v=" + url.QueryEscape(broadcast.VideoID)
	}
	href := "/temp/watch?v=" + url.QueryEscape(broadcast.VideoID)
	if broadcast.LiveStatus == "is_live" || broadcast.LiveStatus == "post_live" {
		href += "&mode=stream"
	}
	return href
}

func homeViewerCount(broadcast model.YouTubeBroadcast) string {
	if broadcast.ConcurrentViewCount == nil {
		return ""
	}
	return strconv.FormatInt(*broadcast.ConcurrentViewCount, 10)
}
