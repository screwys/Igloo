package home

import (
	"fmt"
	"slices"
)

type Layout struct {
	Columns int      `json:"columns"`
	Spacing string   `json:"spacing"`
	Widgets []Widget `json:"widgets"`
}

type Widget struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Title        string   `json:"title"`
	Size         string   `json:"size"`
	Layout       string   `json:"layout"`
	Style        string   `json:"style"`
	Count        int      `json:"count"`
	Platforms    []string `json:"platforms"`
	Channels     []string `json:"channels"`
	ContentTypes []string `json:"content_types"`
	LiveStates   []string `json:"live_states"`
	ShowHeader   bool     `json:"show_header"`
	ShowMedia    bool     `json:"show_media"`
	ShowText     bool     `json:"show_text"`
	StarredOnly  bool     `json:"starred_only"`
	Order        string   `json:"order"`
}

type Kind struct {
	Type    string   `json:"type"`
	Label   string   `json:"label"`
	Icon    string   `json:"icon"`
	Layouts []string `json:"layouts"`
}

var Kinds = []Kind{
	{"continue", "Continue watching", "History", []string{"feature", "cards", "list"}},
	{"live", "Live", "PlayCircle", []string{"feature", "cards", "list"}},
	{"starred", "Starred accounts", "Star", []string{"editorial", "cards", "list", "lanes"}},
	{"moments", "Moments", "PlayCircle", []string{"portraits", "cards", "list"}},
	{"saved", "Saved", "Bookmark", []string{"cards", "list"}},
	{"account", "From an account", "Person", []string{"editorial", "cards", "list"}},
	{"latest", "Latest videos", "VideoLibrary", []string{"cards", "list"}},
}

func NewWidget(kind, id string) Widget {
	for _, k := range Kinds {
		if k.Type == kind {
			order := "newest"
			if kind == "continue" || kind == "saved" {
				order = "recent"
			}
			if kind == "live" {
				order = "live"
			}
			return Widget{ID: id, Type: kind, Size: "medium", Layout: k.Layouts[0], Style: "surface", Count: 4,
				ShowHeader: true, ShowMedia: true, ShowText: true, StarredOnly: kind == "starred" || kind == "live",
				LiveStates: []string{"is_live", "is_upcoming"}, Order: order}
		}
	}
	return Widget{}
}

func DefaultLayout() Layout {
	layout := Layout{Columns: 3, Spacing: "comfortable", Widgets: []Widget{}}
	for _, kind := range []string{"continue", "live", "starred", "moments", "saved"} {
		widget := NewWidget(kind, kind)
		widget.Size = "small"
		if kind == "continue" {
			widget.Size, widget.Count = "large", 3
		}
		layout.Widgets = append(layout.Widgets, widget)
	}
	return layout
}

func (l Layout) Validate() error {
	if l.Columns < 1 || l.Columns > 6 {
		return fmt.Errorf("invalid columns")
	}
	if !slices.Contains([]string{"comfortable", "compact"}, l.Spacing) {
		return fmt.Errorf("invalid spacing")
	}
	ids := make(map[string]bool, len(l.Widgets))
	for _, w := range l.Widgets {
		if w.ID == "" || ids[w.ID] {
			return fmt.Errorf("widget IDs must be unique")
		}
		ids[w.ID] = true
		kind := slices.IndexFunc(Kinds, func(k Kind) bool { return k.Type == w.Type })
		if kind < 0 || !slices.Contains(Kinds[kind].Layouts, w.Layout) {
			return fmt.Errorf("invalid widget type or layout")
		}
		if !slices.Contains([]string{"small", "medium", "large", "full"}, w.Size) {
			return fmt.Errorf("invalid widget size")
		}
		if !slices.Contains([]string{"surface", "open", "accent"}, w.Style) {
			return fmt.Errorf("invalid widget style")
		}
		if w.Count < 1 {
			return fmt.Errorf("item count must be positive")
		}
		if !slices.Contains([]string{"recent", "newest", "account", "live"}, w.Order) {
			return fmt.Errorf("invalid item order")
		}
		for _, p := range w.Platforms {
			if !slices.Contains([]string{"youtube", "twitter", "tiktok", "instagram"}, p) {
				return fmt.Errorf("invalid platform")
			}
		}
		for _, c := range w.ContentTypes {
			if !slices.Contains([]string{"post", "video", "image", "slideshow", "story"}, c) {
				return fmt.Errorf("invalid content type")
			}
		}
		for _, s := range w.LiveStates {
			if !slices.Contains([]string{"is_live", "is_upcoming", "was_live", "post_live"}, s) {
				return fmt.Errorf("invalid broadcast state")
			}
		}
	}
	return nil
}

func (w Widget) IncludesPlatform(platform string) bool {
	return len(w.Platforms) == 0 || slices.Contains(w.Platforms, platform)
}

func (w Widget) IncludesContent(kind string) bool {
	return len(w.ContentTypes) == 0 || slices.Contains(w.ContentTypes, kind)
}
