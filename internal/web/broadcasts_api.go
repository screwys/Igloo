package web

import (
	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/model"
	"log/slog"
	"net/http"
)

func (s *Server) handleBroadcasts(w http.ResponseWriter, r *http.Request) {
	p := s.pageProps(w, r)
	broadcasts := []model.YouTubeBroadcast{}
	if p.PlatformsContain("youtube") && p.BroadcastsEnabled {
		var err error
		broadcasts, err = s.db.ListYouTubeBroadcasts(db.YouTubeBroadcastQuery{Limit: -1, Order: "live"})
		if err != nil {
			slog.Error("load broadcasts", "err", err)
			http.Error(w, "Could not load broadcasts", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"broadcasts": broadcasts, "broadcasts_enabled": p.BroadcastsEnabled, "include_reposts": s.db.MomentsIncludeRepostsEnabled(), "include_tagged": s.db.InstagramIncludeTaggedEnabled()})
}
