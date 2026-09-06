package model

import (
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// AccountDetails contains only account metadata supplied by the platform.
// Pointers distinguish an explicit false or zero from an unknown value.
type AccountDetails struct {
	Source           string `json:"source,omitempty"`
	LocationAccurate *bool  `json:"location_accurate,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
	UserID           string `json:"user_id,omitempty"`
	VerifiedAt       string `json:"verified_at,omitempty"`
	UsernameChanges  *int   `json:"username_changes,omitempty"`
	Verification     string `json:"verification,omitempty"`
}

func ParseAccountDetails(raw string) AccountDetails {
	var details AccountDetails
	_ = json.Unmarshal([]byte(raw), &details)
	return details
}

var accountRegionCodes = func() map[string]string {
	codes := make(map[string]string)
	for _, region := range language.Supported.Regions() {
		if region.IsCountry() {
			code := region.String()
			if name := display.English.Regions().Name(region); name != "" {
				codes[strings.ToLower(code)] = code
				codes[strings.ToLower(name)] = code
			}
		}
	}
	codes["turkey"], codes["türkiye"] = "TR", "TR"
	codes["united states of america"] = "US"
	codes["korea"] = "KR"
	return codes
}()

// AccountRegionFlag only maps reported countries. Broad regions and unknown
// labels do not identify a country and have no country flag.
func AccountRegionFlag(region string) string {
	code := accountRegionCodes[strings.ToLower(strings.TrimSpace(region))]
	if len(code) != 2 {
		return ""
	}
	return string([]rune{0x1F1E6 + rune(code[0]-'A'), 0x1F1E6 + rune(code[1]-'A')})
}

// ChannelProfile is the unified profile record for a channel across all
// platforms. Fields that don't apply to a given platform are zero-value
// (e.g., Followers is 0 for TikTok, VerifiedType is "" for YouTube).
type ChannelProfile struct {
	ChannelID          string // 'twitter_alice' | 'youtube_UC...' | 'tiktok_bob'
	Platform           string // 'twitter' | 'youtube' | 'tiktok'
	Handle             string // display handle (lowercase twitter handle; tiktok uniqueId; youtube @handle if known)
	DisplayName        string
	Bio                string
	Website            string
	Followers          int // 0 when unavailable for platform
	Following          int // 0 when unavailable
	Verified           bool
	VerifiedType       string // twitter only: individual/business/government
	Protected          bool   // twitter only
	AccountRegion      string // X-reported account region, not the profile location
	AccountDetailsJSON string
	AvatarURL          string // source URL (change detection)
	BannerURL          string // source URL; "" when platform has no banner
	ObservedAt         *time.Time
	FetchedAt          *time.Time
	Tombstone          bool
	StoryState         string
	StoryCount         int
	StoryUnseenCount   int
	StoryFirstVideoID  string
}

// ProfileJob is the durable fetch request for one channel identity. A request
// remains pending while RequestedRevision is newer than CompletedRevision.
type ProfileJob struct {
	ChannelID         string
	RequestedRevision int64
	CompletedRevision int64
	RequestedAt       time.Time
	LeaseOwner        string
	LeaseUntil        *time.Time
	Attempts          int
	NextAttemptAt     *time.Time
	LastError         string
	UpdatedAt         time.Time
}
