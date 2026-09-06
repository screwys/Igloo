package fxtwitter

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/model"
)

func TestTweetCapturesPollResultsAndCommunityNoteLinks(t *testing.T) {
	for _, source := range []string{
		`"entities":[{"fromIndex":7,"toIndex":13,"ref":{"type":"TimelineUrl","url":"https://example.test/evidence"}}]`,
		`"facets":[{"type":"url","indices":[7,13],"replacement":"https://example.test/evidence"}]`,
	} {
		var raw rawTweet
		if err := json.Unmarshal([]byte(`{"id":"100","poll":{"choices":[{"label":"First","count":3,"percentage":75},{"label":"Second","count":1,"percentage":25}],"total_votes":4,"ends_at":"2026-09-07T12:00:00Z"},"community_note":{"text":"🧊 See source.",`+source+`},"quote":{"id":"200","community_note":{"text":"Quoted context"}}}`), &raw); err != nil {
			t.Fatal(err)
		}
		before := time.Now().UnixMilli()
		tweet := tweetFromRaw(&raw)
		poll := model.ParsePoll(tweet.PollJSON)
		if poll == nil || len(poll.Choices) != 2 || poll.TotalVotes != 4 || poll.Choices[0].Count != 3 || poll.CapturedAt < before {
			t.Fatalf("poll snapshot = %+v", poll)
		}
		if tweet.CommunityNote != "🧊 See source (https://example.test/evidence)." || tweet.Quote.CommunityNote != "Quoted context" {
			t.Fatalf("notes lost source or quote: %+v", tweet)
		}
	}
}

func TestV2StatusPreservesReplyAndSnapshotContext(t *testing.T) {
	var raw rawTweet
	if err := json.Unmarshal([]byte(`{"type":"status","id":"100","author":{"screen_name":"sample_author"},"created_at":"2026-09-06T12:00:00Z","replying_to":{"screen_name":"parent_author","status":"200"},"quote":{"type":"tombstone","id":"300"}}`), &raw); err != nil {
		t.Fatal(err)
	}
	tweet := tweetFromRaw(&raw)
	if tweet.ReplyToHandle != "parent_author" || tweet.ReplyToStatus != "200" || tweet.CreatedAt.IsZero() || tweet.Quote != nil {
		t.Fatalf("v2 status = %+v", tweet)
	}
}
