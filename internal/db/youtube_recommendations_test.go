package db

import (
	"fmt"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/model"
)

func TestYouTubeRecommendationSnapshotAndDiscoverPrefetch(t *testing.T) {
	d := openWritableTestDB(t)
	const anchor = "sample_anchor_video"
	if err := d.AddChannel(model.Channel{
		ChannelID: "youtube_UCsample_anchor", SourceID: "UCsample_anchor",
		Name: "Sample Anchor", URL: "https://www.youtube.com/channel/UCsample_anchor", Platform: "youtube",
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertVideo(anchor, "youtube_UCsample_anchor", "youtube_video", "Anchor", "", 60, 1, "", "video", 0, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if err := d.QueueYouTubeRecommendations(anchor, now); err != nil {
		t.Fatal(err)
	}
	job, ok, err := d.ClaimYouTubeRecommendationJob(LeaseOptions{Owner: "sample-worker", NowMs: now, LeaseMs: time.Minute.Milliseconds(), Limit: 1})
	if err != nil || !ok {
		t.Fatalf("claim = %+v ok=%v err=%v", job, ok, err)
	}
	candidates := []model.DiscoveryVideo{
		{VideoID: "sample_related_one", Title: "Related One", ChannelID: "youtube_UCrelated_one", Duration: 7200, Source: "related", Rank: 0},
		{VideoID: "sample_related_two", Title: "Related Two", ChannelID: "youtube_UCrelated_two", Duration: 7201, Source: "related", Rank: 1},
		{VideoID: "sample_related_three", Title: "Related Three", ChannelID: "youtube_UCrelated_three", Source: "related", Rank: 2},
		{VideoID: "sample_channel_shelf", Title: "Channel Shelf", ChannelID: "youtube_UCsample_anchor", Source: "channel", Rank: 3},
	}
	if err := d.CompleteYouTubeRecommendationJob(job, candidates, now+1); err != nil {
		t.Fatal(err)
	}
	got, fresh, err := d.GetYouTubeRecommendations(anchor, 2)
	if err != nil || !fresh || len(got) != 2 || got[0].VideoID != "sample_related_one" || got[1].VideoID != "sample_related_three" {
		t.Fatalf("recommendations = %+v fresh=%v err=%v", got, fresh, err)
	}
	if err := d.ExecRaw(`INSERT INTO channel_follows (channel_id, followed_at) VALUES ('youtube_UCrelated_one', 1)`); err != nil {
		t.Fatal(err)
	}
	pool, err := d.ListYouTubeDiscoverVideos(10)
	if err != nil || len(pool) != 1 || pool[0].VideoID != "sample_related_three" {
		t.Fatalf("discover pool = %+v err=%v", pool, err)
	}
	added, err := d.EnqueueDiscoverTempDownloads(pool, 2)
	if err != nil || added != 1 {
		t.Fatalf("prefetch added=%d err=%v", added, err)
	}
	interactiveURL := "https://www.youtube.com/watch?v=sample_related_two"
	if _, err := d.EnqueueTempDownload(interactiveURL, "youtube"); err != nil {
		t.Fatal(err)
	}
	work, ok, err := d.ClaimTempDownloadWork("sample-temp", now+2, time.Minute)
	if err != nil || !ok || work.URL != interactiveURL || work.Origin != "interactive" {
		t.Fatalf("temp work = %+v ok=%v err=%v", work, ok, err)
	}
}

func TestInterleaveDiscoverBatchesCapsAnchorsAndCreators(t *testing.T) {
	makeBatch := func(prefix, channel string, count int) []model.DiscoveryVideo {
		batch := make([]model.DiscoveryVideo, count)
		for i := range batch {
			batch[i] = model.DiscoveryVideo{VideoID: fmt.Sprintf("%s_%d", prefix, i), ChannelID: channel}
		}
		return batch
	}
	batches := [][]model.DiscoveryVideo{
		makeBatch("topic", "youtube_UCtopic", 12),
		makeBatch("first", "youtube_UCfirst", 4),
		makeBatch("second", "youtube_UCsecond", 4),
	}
	got := interleaveDiscoverBatches(batches, 20, 6, 2)
	if len(got) != 6 {
		t.Fatalf("interleaved videos = %+v", got)
	}
	counts := map[string]int{}
	for _, candidate := range got {
		counts[candidate.ChannelID]++
	}
	for channelID, count := range counts {
		if count > 2 {
			t.Fatalf("channel %s count = %d", channelID, count)
		}
	}
	if got[0].VideoID != "topic_0" || got[1].VideoID != "first_0" || got[2].VideoID != "second_0" {
		t.Fatalf("batches were not round-robin: %+v", got)
	}
}

func TestQueueFollowedYouTubeChannelRecommendationsUsesOneRecentAnchorPerChannel(t *testing.T) {
	d := openWritableTestDB(t)
	channels := []model.Channel{
		{ChannelID: "youtube_UCfirst", Name: "First", Platform: "youtube", IsSubscribed: true},
		{ChannelID: "youtube_UCsecond", Name: "Second", Platform: "youtube", IsSubscribed: true},
		{ChannelID: "youtube_UCunfollowed", Name: "Unfollowed", Platform: "youtube"},
	}
	for _, channel := range channels {
		if err := d.AddChannel(channel); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		videoID, channelID string
		published          int64
	}{
		{"first_old", "youtube_UCfirst", 1},
		{"first_recent", "youtube_UCfirst", 2},
		{"second_recent", "youtube_UCsecond", 3},
		{"unfollowed_recent", "youtube_UCunfollowed", 4},
	} {
		if err := d.InsertVideo(row.videoID, row.channelID, "youtube_video", row.videoID, "", 60, row.published, "", "video", 0, false); err != nil {
			t.Fatal(err)
		}
		storeReadyAssetForTest(t, d, Asset{
			AssetID:   BuildAssetID("youtube", "youtube_video", row.videoID, "video_stream", 0),
			AssetKind: "video_stream", OwnerKind: "youtube_video", OwnerID: row.videoID,
			FilePath: "media/youtube/" + row.videoID + ".mp4", ContentType: "video/mp4",
		}, row.published)
	}

	queued, err := d.QueueFollowedYouTubeChannelRecommendations(10)
	if err != nil || queued != 2 {
		t.Fatalf("queued=%d err=%v", queued, err)
	}
	rows, err := d.conn.Query(`SELECT anchor_video_id FROM youtube_recommendations ORDER BY anchor_video_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var videoID string
		if err := rows.Scan(&videoID); err != nil {
			t.Fatal(err)
		}
		got = append(got, videoID)
	}
	want := []string{"first_recent", "second_recent"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("anchors=%v want=%v", got, want)
	}
}
