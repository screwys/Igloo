package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchSuggestIncludesChannelDisplayHandle(t *testing.T) {
	srv := newTestServer(t)
	const channelID = "youtube_UCopaque123"

	if err := srv.db.ExecRaw(
		`INSERT INTO channels (channel_id, source_id, name, platform) VALUES (?, ?, ?, ?)`,
		channelID, "UCopaque123", "Sample Tube", "youtube",
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	if err := srv.db.ExecRaw(
		`INSERT INTO channel_profiles (channel_id, platform, handle, display_name) VALUES (?, ?, ?, ?)`,
		channelID, "youtube", "sampletube", "Sample Tube",
	); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	if err := srv.db.ExecRaw(
		`INSERT INTO channel_follows (channel_id, followed_at) VALUES (?, 1)`,
		channelID,
	); err != nil {
		t.Fatalf("insert follow: %v", err)
	}

	rr := httptest.NewRecorder()
	srv.handleSearchSuggest(rr, httptest.NewRequest("GET", "/api/search/suggest?q=Sample&channel_limit=5&video_limit=1", nil))
	if rr.Code != 200 {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var body struct {
		Channels []struct {
			ChannelID string `json:"channel_id"`
			Name      string `json:"name"`
			Handle    string `json:"handle"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Channels) != 1 {
		t.Fatalf("channels = %+v, want one result", body.Channels)
	}
	ch := body.Channels[0]
	if ch.ChannelID != channelID || ch.Name != "Sample Tube" || ch.Handle != "sampletube" {
		t.Fatalf("channel result = %+v, want display name plus profile handle", ch)
	}
}

func TestSearchSuggestExcludesUnfollowedChannelsAcrossPlatforms(t *testing.T) {
	srv := newTestServer(t)
	for _, platform := range []string{"twitter", "tiktok", "instagram"} {
		followedID := platform + "_sample_followed"
		unfollowedID := platform + "_sample_unfollowed"
		for _, channelID := range []string{followedID, unfollowedID} {
			if err := srv.db.ExecRaw(
				`INSERT INTO channels (channel_id, source_id, name, platform) VALUES (?, ?, ?, ?)`,
				channelID, channelID, "Shared Search Name", platform,
			); err != nil {
				t.Fatalf("insert %s channel: %v", platform, err)
			}
		}
		if err := srv.db.ExecRaw(
			`INSERT INTO channel_follows (channel_id, followed_at) VALUES (?, 1)`,
			followedID,
		); err != nil {
			t.Fatalf("follow %s channel: %v", platform, err)
		}
	}

	rr := httptest.NewRecorder()
	srv.handleSearchSuggest(rr, httptest.NewRequest("GET", "/api/search/suggest?q=Shared&channel_limit=30&video_limit=1", nil))
	if rr.Code != 200 {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var body struct {
		Channels []struct {
			ChannelID string `json:"channel_id"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Channels) != 3 {
		t.Fatalf("channels = %+v, want one followed result per platform", body.Channels)
	}
	for _, channel := range body.Channels {
		if strings.HasSuffix(channel.ChannelID, "_unfollowed") {
			t.Fatalf("unfollowed channel appeared in suggestions: %s", channel.ChannelID)
		}
	}
}
