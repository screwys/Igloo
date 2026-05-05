package web

import "strings"

func isShortsChannelID(channelID string) bool {
	lower := strings.ToLower(strings.TrimSpace(channelID))
	return strings.HasPrefix(lower, "tiktok_") || strings.HasPrefix(lower, "instagram_")
}
