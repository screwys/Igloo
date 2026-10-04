package web

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/screwys/igloo/internal/components"
	"github.com/screwys/igloo/internal/model"
)

var channelIDRe = regexp.MustCompile(`^(twitter|youtube|tiktok|instagram)_[A-Za-z0-9_@.\-]+$`)

func (s *Server) registerProfileAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/profile-card/{channelID}", s.handleProfileCard)
}

// Profile discovery and refresh belong to the durable job queue, never hover.
func (s *Server) handleProfileCard(w http.ResponseWriter, r *http.Request) {
	channelID := canonicalProfileChannelID(r.PathValue("channelID"))
	if !channelIDRe.MatchString(channelID) {
		http.Error(w, "invalid channel_id", http.StatusBadRequest)
		return
	}

	p, err := s.db.GetChannelProfile(channelID)
	if err != nil {
		http.Error(w, "profile lookup failed", http.StatusInternalServerError)
		return
	}
	if p != nil && p.Tombstone {
		http.NotFound(w, r)
		return
	}
	p = profileForChannelID(p, channelID)

	s.writeProfileCard(w, r, s.profileForPresentation(p), s.isChannelFollowed(channelID))
}

func (s *Server) isChannelFollowed(channelID string) bool {
	return s.db.IsChannelFollowed(channelID)
}

func (s *Server) isChannelStarred(channelID string) bool {
	return s.db.IsChannelStarred(channelID)
}

func profileForChannelID(p *model.ChannelProfile, channelID string) *model.ChannelProfile {
	if p == nil {
		platform, handle := channelIDPlatformHandle(channelID)
		return &model.ChannelProfile{ChannelID: channelID, Platform: platform, Handle: handle}
	}
	return p
}

// profileForPresentation keeps source metadata intact in the database while
// exposing banner readiness from the canonical asset inventory to templates.
func (s *Server) profileForPresentation(p *model.ChannelProfile) *model.ChannelProfile {
	if p == nil {
		return nil
	}
	cp := *p
	if !s.db.BoolSetting("x_account_region_enabled") {
		cp.AccountRegion = ""
		cp.AccountDetailsJSON = ""
	}
	if s.resolveBannerPath(cp.ChannelID) == "" {
		cp.BannerURL = ""
	}
	if cp.Platform == "tiktok" {
		if live, err := s.db.GetTikTokLive(cp.ChannelID); err == nil && live != nil {
			cp.LiveRoomID = live.RoomID
			cp.StoryState = model.StoryStateNew
			cp.StoryFirstVideoID = "tiktok_live_" + live.RoomID
		}
	}
	return &cp
}

func canonicalProfileChannelID(channelID string) string {
	channelID = strings.TrimSpace(channelID)
	lower := strings.ToLower(channelID)
	switch {
	case strings.HasPrefix(lower, "twitter_"),
		strings.HasPrefix(lower, "tiktok_"),
		strings.HasPrefix(lower, "instagram_"):
		return lower
	default:
		// YouTube channel IDs are case-sensitive.
		return channelID
	}
}

func platformOfChannelID(channelID string) string {
	platform, _, ok := strings.Cut(channelID, "_")
	if !ok {
		return ""
	}
	switch platform {
	case "twitter", "youtube", "tiktok", "instagram":
		return platform
	default:
		return ""
	}
}

func (s *Server) writeProfileCard(w http.ResponseWriter, r *http.Request, p *model.ChannelProfile, isFollowing bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = components.ProfileCard(s.pageProps(w, r), p, "hover", isFollowing, s.isChannelStarred(p.ChannelID)).Render(r.Context(), w)
}
