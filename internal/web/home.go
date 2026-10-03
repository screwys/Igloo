package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/screwys/igloo/internal/components"
	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/feed"
	"github.com/screwys/igloo/internal/home"
	"github.com/screwys/igloo/internal/model"
)

func (s *Server) registerHomeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /home", s.handleHomePage)
	mux.HandleFunc("GET /api/home/widgets", s.handleHomeWidgets)
	mux.HandleFunc("POST /api/home/widgets", s.handleHomeWidgets)
	mux.HandleFunc("GET /api/home/layout", s.handleHomeLayout)
	mux.HandleFunc("PUT /api/home/layout", s.handleSaveHomeLayout)
	mux.HandleFunc("GET /api/home/broadcasts", s.handleHomeBroadcasts)
}

func (s *Server) homePageProps(w http.ResponseWriter, r *http.Request) components.PageProps {
	p := s.pageProps(w, r)
	p.PageTitle = components.L(p, "nav_home", "Home")
	p.ActiveNav = "home"
	p.PageStyles = []string{"css/home.css"}
	p.PageScripts = []string{"js/dist/feed.js", "js/dist/shorts.js", "js/home.js"}
	return p
}

func (s *Server) handleHomePage(w http.ResponseWriter, r *http.Request) {
	p := s.homePageProps(w, r)
	layout, err := s.db.GetHomeLayout(p.Username)
	if err != nil {
		s.homeError(w, err)
		return
	}
	data, err := s.buildHomeData(p, layout)
	if err != nil {
		s.homeError(w, err)
		return
	}
	p.Sidebar = s.buildSidebarContext(r, data.Channels)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := components.HomePage(p, data).Render(r.Context(), w); err != nil {
		slog.Error("render Home", "err", err)
	}
}

func (s *Server) handleHomeWidgets(w http.ResponseWriter, r *http.Request) {
	p := s.homePageProps(w, r)
	var layout home.Layout
	var err error
	if r.Method == http.MethodPost {
		if err = decodeJSON(w, r, &layout); err != nil {
			writeJSON(w, 400, map[string]any{"error": "invalid layout"})
			return
		}
		if err = layout.Validate(); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
	} else {
		layout, err = s.db.GetHomeLayout(p.Username)
	}
	if err != nil {
		s.homeError(w, err)
		return
	}
	data, err := s.buildHomeData(p, layout)
	if err != nil {
		s.homeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := components.HomeWidgets(p, data).Render(r.Context(), w); err != nil {
		slog.Error("render Home widgets", "err", err)
	}
}

func (s *Server) handleHomeLayout(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		writeJSON(w, 401, map[string]any{"error": "unauthorized"})
		return
	}
	layout, err := s.db.GetHomeLayout(user.Username)
	if err != nil {
		s.homeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(layout); err != nil {
		slog.Error("encode Home layout", "err", err)
	}
}

func (s *Server) handleSaveHomeLayout(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		writeJSON(w, 401, map[string]any{"error": "unauthorized"})
		return
	}
	var layout home.Layout
	if err := decodeJSON(w, r, &layout); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid layout"})
		return
	}
	if err := layout.Validate(); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	if err := s.db.SetHomeLayout(user.Username, layout); err != nil {
		s.homeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) handleHomeBroadcasts(w http.ResponseWriter, r *http.Request) {
	p := s.pageProps(w, r)
	broadcasts := []model.YouTubeBroadcast{}
	if p.PlatformsContain("youtube") && p.BroadcastsEnabled {
		var err error
		broadcasts, err = s.db.ListYouTubeBroadcasts(db.YouTubeBroadcastQuery{Limit: -1, Order: "live"})
		if err != nil {
			s.homeError(w, err)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"broadcasts": broadcasts, "broadcasts_enabled": p.BroadcastsEnabled, "include_reposts": s.db.MomentsIncludeRepostsEnabled(), "include_tagged": s.db.InstagramIncludeTaggedEnabled()})
}

func (s *Server) homeError(w http.ResponseWriter, err error) {
	slog.Error("Home data", "err", err)
	writeJSON(w, 500, map[string]any{"error": "Could not load Home"})
}

func homeQueryWidget(p components.PageProps, widget home.Widget) (home.Widget, bool) {
	query := widget
	query.Platforms = []string{}
	for _, platform := range []string{"youtube", "twitter", "instagram", "tiktok"} {
		if platform == "twitter" && (widget.Type == "continue" || widget.Type == "latest") {
			continue
		}
		if p.PlatformsContain(platform) && widget.IncludesPlatform(platform) {
			query.Platforms = append(query.Platforms, platform)
		}
	}
	return query, len(query.Platforms) > 0
}

func (s *Server) buildHomeData(p components.PageProps, layout home.Layout) (components.HomePageData, error) {
	data := components.HomePageData{Layout: layout, Widgets: make([]components.HomeWidgetData, len(layout.Widgets)), Channels: []model.Channel{}}
	channels := map[string]model.Channel{}
	for _, channel := range s.enrichedChannels() {
		if p.PlatformsContain(channel.Platform) {
			data.Channels = append(data.Channels, channel)
			channels[channel.ChannelID] = channel
		}
	}
	var allFeed []model.FeedItem
	for i, widget := range layout.Widgets {
		section := &data.Widgets[i]
		section.Widget = widget
		query, allowed := homeQueryWidget(p, widget)
		if !allowed {
			continue
		}
		if widget.Type == "live" {
			if !p.BroadcastsEnabled || !query.IncludesPlatform("youtube") || !query.IncludesContent("video") {
				continue
			}
			broadcasts, err := s.db.ListYouTubeBroadcasts(db.YouTubeBroadcastQuery{ChannelIDs: query.Channels, StarredOnly: query.StarredOnly, States: query.LiveStates, Order: query.Order, Limit: query.Count})
			if err != nil {
				return data, err
			}
			for j := range broadcasts {
				b := &broadcasts[j]
				ch := channels[b.ChannelID]
				section.Items = append(section.Items, components.HomeEntry{Broadcast: b, Video: model.Video{VideoID: b.VideoID, ChannelID: b.ChannelID, ChannelName: components.ChannelDisplayName(ch), Platform: "youtube", AvatarURL: ch.AvatarURL, ThumbnailURL: b.ThumbnailURL, Title: b.Title}})
			}
			continue
		}
		videos, err := s.db.GetHomeVideos(query)
		if err != nil {
			return data, err
		}
		for _, video := range videos {
			if video.Video.Platform == "twitter" {
				video.Video.Watched = false
				video.Video.PlaybackPosition = 0
				video.Video.IsShortForm = true
			}
			section.Items = append(section.Items, components.HomeEntry{Video: video.Video, SortAtMs: video.SortAtMs})
		}
		items, err := s.db.GetHomeFeedItems(query)
		if err != nil {
			return data, err
		}
		for j := range items {
			section.Items = append(section.Items, components.HomeEntry{Feed: &items[j]})
			allFeed = append(allFeed, items[j])
		}
	}
	enriched := feed.EnrichFeedItemsPreserveRows(s.db, allFeed)
	feedByID := make(map[string]model.FeedItem, len(enriched))
	for _, item := range enriched {
		feedByID[item.TweetID] = item
	}
	var savedIDs []string
	for _, section := range data.Widgets {
		if section.Widget.Type != "saved" {
			continue
		}
		for _, entry := range section.Items {
			if entry.Feed != nil {
				savedIDs = append(savedIDs, entry.Feed.TweetID)
				if entry.Feed.CanonicalTweetID != "" {
					savedIDs = append(savedIDs, entry.Feed.CanonicalTweetID)
				}
			}
		}
	}
	bookmarks, err := s.db.GetBookmarksForVideoIDsRich(savedIDs)
	if err != nil {
		return data, err
	}
	for i := range data.Widgets {
		section := &data.Widgets[i]
		if section.Widget.Type == "live" {
			continue
		}
		for j := range section.Items {
			entry := &section.Items[j]
			if entry.Feed == nil {
				continue
			}
			item := feedByID[entry.Feed.TweetID]
			if len(entry.Feed.Retweeters) > 0 {
				item.Retweeters = entry.Feed.Retweeters
			}
			entry.Feed = &item
			ch := channels[item.ChannelID]
			ch.ChannelID = item.ChannelID
			entry.Video = feedItemToVideo(item, ch)
			entry.Video.Title = item.BodyText
			if item.PublishedAt != nil {
				entry.SortAtMs = item.PublishedAt.UnixMilli()
			}
			if section.Widget.Type == "saved" {
				bookmark, ok := bookmarks[item.TweetID]
				if !ok {
					bookmark, ok = bookmarks[item.CanonicalTweetID]
				}
				if ok {
					if section.Widget.Order != "newest" {
						entry.SortAtMs = bookmark.BookmarkedAtMs
					}
					if bookmark.CustomTitle != nil {
						entry.CustomTitle = *bookmark.CustomTitle
					}
				}
			}
		}
		sort.SliceStable(section.Items, func(a, b int) bool {
			left, right := section.Items[a], section.Items[b]
			if section.Widget.Order == "account" {
				leftName, rightName := strings.ToLower(left.Video.ChannelName), strings.ToLower(right.Video.ChannelName)
				if leftName != rightName {
					return leftName < rightName
				}
			}
			return left.SortAtMs > right.SortAtMs
		})
		if len(section.Items) > section.Widget.Count {
			section.Items = section.Items[:section.Widget.Count]
		}
	}
	return data, nil
}
