package web

import "testing"

func TestParseYouTubeSearchOutputRestoresIndependentAvatar(t *testing.T) {
	results := parseYouTubeSearchOutput([]byte(`{"id":"sample_video","title":"Sample Video","channel":"Sample Search Channel","channel_id":"UCsample_search","channel_url":"https://www.youtube.com/channel/UCsample_search","uploader_id":"@sample_search","duration":42}`))
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	result := results[0]
	if result["ChannelID"] != "youtube_UCsample_search" {
		t.Fatalf("channel id = %#v", result["ChannelID"])
	}
	if result["AvatarURL"] != "https://unavatar.io/youtube/sample_search" {
		t.Fatalf("avatar url = %#v", result["AvatarURL"])
	}
}

func TestYouTubeSearchChannelsProjectsDiscoveryIdentity(t *testing.T) {
	channels := youtubeSearchChannels([]map[string]any{
		{
			"ChannelID":     "youtube_UCsample_search",
			"ChannelName":   "Sample Search Channel",
			"ChannelURL":    "https://www.youtube.com/channel/UCsample_search",
			"ChannelHandle": "@sample_search",
		},
		{"ChannelID": ""},
	})
	if len(channels) != 1 {
		t.Fatalf("channels = %d, want 1", len(channels))
	}
	channel := channels[0]
	if channel.ChannelID != "youtube_UCsample_search" ||
		channel.SourceID != "UCsample_search" ||
		channel.Name != "Sample Search Channel" ||
		channel.DisplayName != "Sample Search Channel" ||
		channel.Handle != "@sample_search" ||
		channel.Platform != "youtube" {
		t.Fatalf("unexpected channel: %+v", channel)
	}
}
