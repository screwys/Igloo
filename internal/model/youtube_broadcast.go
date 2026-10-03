package model

// YouTubeBroadcast is a recent /streams entry observed without downloading it.
type YouTubeBroadcast struct {
	VideoID             string `json:"video_id"`
	ChannelID           string `json:"channel_id"`
	Title               string `json:"title"`
	ThumbnailURL        string `json:"thumbnail_url"`
	LiveStatus          string `json:"live_status"`
	PublishedAtMs       int64  `json:"published_at_ms"`
	StartsAtMs          int64  `json:"starts_at_ms"`
	ConcurrentViewCount *int64 `json:"concurrent_view_count,omitempty"`
	ObservedAtMs        int64  `json:"observed_at_ms"`
	SourceRank          int    `json:"source_rank"`
	Availability        string `json:"-"`
}
