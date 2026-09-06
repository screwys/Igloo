package model

import (
	"encoding/json"
	"time"
)

// Poll records a results snapshot, not live voting state.
type Poll struct {
	Choices    []PollChoice `json:"choices"`
	TotalVotes int64        `json:"total_votes"`
	EndsAt     string       `json:"ends_at"`
	CapturedAt int64        `json:"captured_at"`
}

type PollChoice struct {
	Label      string  `json:"label"`
	Count      int64   `json:"count"`
	Percentage float64 `json:"percentage"`
}

func ParsePoll(raw string) *Poll {
	var poll Poll
	if raw == "" || json.Unmarshal([]byte(raw), &poll) != nil || len(poll.Choices) == 0 {
		return nil
	}
	return &poll
}

func (p Poll) ClosedAtCapture() bool {
	ends, err := time.Parse(time.RFC3339, p.EndsAt)
	return err == nil && p.CapturedAt > 0 && p.CapturedAt >= ends.UnixMilli()
}
