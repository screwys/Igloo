package model

type XLiveAccount struct {
	ChannelID string
	UserID    string
}

type XSpacePresence struct {
	UserID      string `json:"user_id"`
	SpaceID     string `json:"space_id"`
	Title       string `json:"title"`
	ViewerCount int64  `json:"viewer_count"`
}
