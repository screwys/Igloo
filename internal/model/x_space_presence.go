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

type XSpaceParticipant struct {
	ChannelID   string `json:"channel_id"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Role        string `json:"role"`
}
