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

func TestShuffledDiscoverVideosPreservesPoolWithoutMutatingInput(t *testing.T) {
	videos := []model.DiscoveryVideo{
		{VideoID: "one"}, {VideoID: "two"}, {VideoID: "three"}, {VideoID: "four"}, {VideoID: "five"},
	}
	got := shuffledDiscoverVideos(videos, 42)
	if len(got) != len(videos) || videos[0].VideoID != "one" {
		t.Fatalf("shuffle mutated input or changed size: input=%+v got=%+v", videos, got)
	}
	if got[0].VideoID == "one" && got[1].VideoID == "two" && got[2].VideoID == "three" {
		t.Fatalf("fixed-seed shuffle retained source order: %+v", got)
	}
	seen := map[string]bool{}
	for _, video := range got {
		seen[video.VideoID] = true
	}
	for _, video := range videos {
		if !seen[video.VideoID] {
			t.Fatalf("shuffle dropped %s: %+v", video.VideoID, got)
		}
	}
}
