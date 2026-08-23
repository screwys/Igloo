package worker

import (
	"testing"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/download"
	"github.com/screwys/igloo/internal/model"
)

func TestMergeYouTubeRecommendationsKeepsChannelShelfThenRelatedCreators(t *testing.T) {
	job := db.YouTubeRecommendationJob{
		AnchorVideoID: "sample_anchor", ChannelID: "youtube_UCanchor", ChannelName: "Anchor Channel",
		ChannelHandle: "@anchor", ChannelURL: "https://www.youtube.com/channel/UCanchor",
	}
	channelRefs := []download.VideoRef{
		{VideoID: "same_one", Title: "Same One"},
		{VideoID: "same_two", Title: "Same Two"},
		{VideoID: "same_three", Title: "Same Three"},
		{VideoID: "same_four", Title: "Same Four"},
		{VideoID: "same_five", Title: "Same Five"},
	}
	mix := []download.VideoRef{
		{VideoID: "same_one", ChannelID: "youtube_UCanchor"},
		{VideoID: "related_one", Title: "Related", ChannelID: "youtube_UCrelated", AuthorDisplayName: "Related Channel", AuthorHandle: "@related"},
		{VideoID: "missing_owner", Title: "Missing owner"},
	}
	got := mergeYouTubeRecommendations(job, channelRefs, mix, 10)
	if len(got) != 5 {
		t.Fatalf("recommendations = %+v", got)
	}
	for i := 0; i < 4; i++ {
		if got[i].Source != "channel" || got[i].ChannelID != job.ChannelID {
			t.Fatalf("channel recommendation %d = %+v", i, got[i])
		}
	}
	if got[4].VideoID != "related_one" || got[4].Source != "related" || got[4].ChannelID != "youtube_UCrelated" {
		t.Fatalf("related recommendation = %+v", got[4])
	}
}

func TestDiscoverPrefetchUsesOnlyRelatedCandidates(t *testing.T) {
	got := discoverPrefetchCandidates([]model.DiscoveryVideo{
		{VideoID: "same_channel", Source: "channel"},
		{VideoID: "related_creator", Source: "related"},
	})
	if len(got) != 1 || got[0].VideoID != "related_creator" {
		t.Fatalf("prefetch candidates = %+v", got)
	}
}
