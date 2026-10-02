package components

import (
	"github.com/screwys/igloo/internal/home"
	"github.com/screwys/igloo/internal/model"
)

type HomeEntry struct {
	Video       model.Video
	Feed        *model.FeedItem
	Broadcast   *model.YouTubeBroadcast
	SortAtMs    int64
	CustomTitle string
}

type HomeWidgetData struct {
	Widget home.Widget
	Items  []HomeEntry
}

type HomePageData struct {
	Layout   home.Layout
	Widgets  []HomeWidgetData
	Channels []model.Channel
}
