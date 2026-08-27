package web

import (
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestPlayerRelatedRecommendationsExcludesChannelShelfAndLimits(t *testing.T) {
	videos := []model.DiscoveryVideo{
		{VideoID: "same_source", ChannelID: "youtube_UCcurrent", Source: "channel"},
		{VideoID: "same_related", ChannelID: "youtube_UCcurrent", Source: "related"},
		{VideoID: "sample_current", Title: "Current Video", ChannelID: "youtube_UCother_zero", Source: "related"},
		{VideoID: "reupload", Title: "  current   video ", ChannelID: "youtube_UCother_reupload", Source: "related"},
		{VideoID: "related_one", ChannelID: "youtube_UCother_one", Source: "related"},
		{VideoID: "related_two", ChannelID: "youtube_UCother_two", Source: "related"},
	}
	got := playerRelatedRecommendations(videos, "sample_current", "Current Video", "youtube_UCcurrent", 1)
	if len(got) != 1 || got[0].VideoID != "related_one" {
		t.Fatalf("related recommendations = %+v", got)
	}
}

func TestPlayerMoreFromChannelExcludesFollowedCreator(t *testing.T) {
	srv := newTestServer(t)
	const channelID = "youtube_UCfollowed_sidebar"
	if err := srv.db.AddChannel(model.Channel{ChannelID: channelID, Name: "Followed Sidebar", Platform: "youtube", IsSubscribed: true}); err != nil {
		t.Fatal(err)
	}
	got := srv.playerMoreFromChannel(model.Video{VideoID: "sample_current", ChannelID: channelID}, 4)
	if len(got) != 0 {
		t.Fatalf("followed creator local shelf = %+v", got)
	}
}
