package fxtwitter

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/screwys/igloo/internal/model"
)

// The legacy status API uses a handle string; v2 thread endpoints return the
// parent handle and status together. Decode both into the same stored identity.
type rawReplyTarget struct {
	ScreenName string `json:"screen_name"`
	Status     string `json:"status"`
}

func (r *rawReplyTarget) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &r.ScreenName)
	}
	type target rawReplyTarget
	return json.Unmarshal(data, (*target)(r))
}

func capturePoll(poll *model.Poll, captured time.Time) string {
	if poll == nil || len(poll.Choices) == 0 {
		return ""
	}
	poll.CapturedAt = captured.UnixMilli()
	if end, err := time.Parse(time.RFC3339, poll.EndsAt); err == nil {
		poll.EndsAt = end.UTC().Format(time.RFC3339)
	} else {
		poll.EndsAt = ""
	}
	encoded, err := json.Marshal(poll)
	if err != nil {
		return ""
	}
	return string(encoded)
}

type rawCommunityNote struct {
	Text   string `json:"text"`
	Facets []struct {
		Type        string `json:"type"`
		Indices     []int  `json:"indices"`
		Replacement string `json:"replacement"`
	} `json:"facets"`
	Entities []struct {
		FromIndex int `json:"fromIndex"`
		ToIndex   int `json:"toIndex"`
		Ref       struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"ref"`
	} `json:"entities"`
}

func communityNoteText(note *rawCommunityNote) string {
	if note == nil {
		return ""
	}
	type sourceLink struct {
		start, end int
		url        string
	}
	links := make([]sourceLink, 0, len(note.Facets)+len(note.Entities))
	for _, facet := range note.Facets {
		if facet.Type == "url" && len(facet.Indices) == 2 {
			links = append(links, sourceLink{facet.Indices[0], facet.Indices[1], facet.Replacement})
		}
	}
	if len(note.Facets) == 0 {
		for _, entity := range note.Entities {
			if entity.Ref.Type == "TimelineUrl" {
				links = append(links, sourceLink{entity.FromIndex, entity.ToIndex, entity.Ref.URL})
			}
		}
	}
	sort.SliceStable(links, func(i, j int) bool { return links[i].start > links[j].start })
	text := note.Text
	for _, link := range links {
		if !strings.HasPrefix(link.url, "https://") && !strings.HasPrefix(link.url, "http://") {
			continue
		}
		encoded := utf16.Encode([]rune(text))
		if link.start < 0 || link.end < link.start || link.end > len(encoded) {
			continue
		}
		label := string(utf16.Decode(encoded[link.start:link.end]))
		replacement := link.url
		if label != "" && !strings.HasPrefix(label, "http://") && !strings.HasPrefix(label, "https://") {
			replacement = label + " (" + link.url + ")"
		}
		text = string(utf16.Decode(encoded[:link.start])) + replacement + string(utf16.Decode(encoded[link.end:]))
	}
	return strings.TrimSpace(text)
}
