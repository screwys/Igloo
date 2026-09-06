// Package fxtwitter is a thin client for the fxtwitter community API.
// Used by the avatar worker (for avatar_url) and the profile worker (for
// everything else). Both callers hit api.fxtwitter.com on their own
// cadences — this package has no shared state beyond a reusable Client.
package fxtwitter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/screwys/igloo/internal/model"
)

const DefaultBaseURL = "https://api.fxtwitter.com"

// ErrNotFound is returned when fxtwitter returns 404 or an empty body, which is
// its observed behavior for handles that don't exist.
var ErrNotFound = errors.New("fxtwitter: user not found")

// User mirrors the subset of fxtwitter's JSON we use.
type User struct {
	ID             string
	ScreenName     string
	Name           string
	Description    string
	Location       string
	AccountRegion  string
	AccountDetails model.AccountDetails
	Website        string
	AvatarURL      string
	BannerURL      string
	Followers      int
	Following      int
	Tweets         int
	MediaCount     int
	Likes          int
	Verified       bool
	VerifiedType   string
	Protected      bool
	Joined         time.Time
}

// Tweet mirrors the subset of fxtwitter's /status/<id> JSON we use.
type Tweet struct {
	ID                string
	AuthorHandle      string
	AuthorDisplayName string
	AuthorAvatarURL   string
	Text              string
	ArticleTitle      string
	PollJSON          string
	CommunityNote     string
	Lang              string
	ReplyToHandle     string // "" if not a reply
	ReplyToStatus     string // "" if not a reply
	CreatedAt         time.Time
	MediaJSON         string // serialized []model.MediaRef, "" if no media
	MentionHandles    []string
	Quote             *Tweet
}

// Client wraps HTTP + base URL for easy testing.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Timeout time.Duration
}

// NewClient returns a Client with the production base URL and a 10 s timeout.
func NewClient() *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		HTTP:    http.DefaultClient,
		Timeout: 10 * time.Second,
	}
}

// FetchUser queries fxtwitter for the given handle.
func (c *Client) FetchUser(ctx context.Context, handle string) (*User, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	url := c.BaseURL + "/2/profile/" + strings.TrimPrefix(handle, "@") + "?about_account=1"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fxtwitter request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fxtwitter status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, ErrNotFound
	}

	var raw struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		User    *struct {
			ID           string `json:"id"`
			ScreenName   string `json:"screen_name"`
			Name         string `json:"name"`
			Description  string `json:"description"`
			Location     string `json:"location"`
			AboutAccount struct {
				BasedIn          string `json:"based_in"`
				Source           string `json:"source"`
				LocationAccurate *bool  `json:"location_accurate"`
				UsernameChanges  *struct {
					Count *int `json:"count"`
				} `json:"username_changes"`
			} `json:"about_account"`
			Website      any    `json:"website"`
			AvatarURL    string `json:"avatar_url"`
			BannerURL    string `json:"banner_url"`
			Followers    int    `json:"followers"`
			Following    int    `json:"following"`
			Tweets       int    `json:"statuses"`
			MediaCount   int    `json:"media_count"`
			Likes        int    `json:"likes"`
			Joined       string `json:"joined"`
			Protected    bool   `json:"protected"`
			Verification struct {
				Verified   bool   `json:"verified"`
				Type       string `json:"type"`
				VerifiedAt string `json:"verified_at"`
			} `json:"verification"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if raw.User == nil {
		return nil, ErrNotFound
	}

	u := &User{
		ID:            raw.User.ID,
		ScreenName:    raw.User.ScreenName,
		Name:          raw.User.Name,
		Description:   raw.User.Description,
		Location:      raw.User.Location,
		AccountRegion: strings.TrimSpace(raw.User.AboutAccount.BasedIn),
		AvatarURL:     raw.User.AvatarURL,
		BannerURL:     raw.User.BannerURL,
		Followers:     raw.User.Followers,
		Following:     raw.User.Following,
		Tweets:        raw.User.Tweets,
		MediaCount:    raw.User.MediaCount,
		Likes:         raw.User.Likes,
		Verified:      raw.User.Verification.Verified,
		VerifiedType:  raw.User.Verification.Type,
		Protected:     raw.User.Protected,
	}
	u.Website = websiteFromAny(raw.User.Website)
	u.AccountDetails = model.AccountDetails{
		Source:           strings.TrimSpace(raw.User.AboutAccount.Source),
		LocationAccurate: raw.User.AboutAccount.LocationAccurate,
		CreatedAt:        accountDate(raw.User.Joined),
		UserID:           raw.User.ID,
		VerifiedAt:       accountDate(raw.User.Verification.VerifiedAt),
		Verification:     raw.User.Verification.Type,
	}
	if raw.User.AboutAccount.UsernameChanges != nil {
		u.AccountDetails.UsernameChanges = raw.User.AboutAccount.UsernameChanges.Count
	}
	if u.AccountDetails.CreatedAt != "" {
		u.Joined, _ = time.Parse(time.RFC3339, u.AccountDetails.CreatedAt)
	}
	return u, nil
}

func accountDate(raw string) string {
	for _, layout := range []string{time.RFC3339, "Mon Jan 02 15:04:05 -0700 2006"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func websiteFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		for _, key := range []string{"url", "display_url"} {
			if s, ok := v[key].(string); ok {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

// FetchTweet queries fxtwitter for a single tweet by handle + ID. Returns
// ErrNotFound on 404 / empty body. The handle is required by the fxtwitter
// URL shape but does not need to exactly match the tweet's author — fxtwitter
// resolves the canonical author from the ID.
func (c *Client) FetchTweet(ctx context.Context, handle, tweetID string) (*Tweet, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	url := c.BaseURL + "/" + strings.TrimPrefix(handle, "@") + "/status/" + tweetID
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fxtwitter request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fxtwitter status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, ErrNotFound
	}

	var raw struct {
		Code    int       `json:"code"`
		Message string    `json:"message"`
		Tweet   *rawTweet `json:"tweet"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if raw.Tweet == nil {
		return nil, ErrNotFound
	}

	return tweetFromRaw(raw.Tweet), nil
}

type rawTweet struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Text   string `json:"text"`
	Lang   string `json:"lang"`
	Author struct {
		ScreenName string `json:"screen_name"`
		Name       string `json:"name"`
		AvatarURL  string `json:"avatar_url"`
	} `json:"author"`
	ReplyingTo       rawReplyTarget `json:"replying_to"`
	ReplyingToStatus string         `json:"replying_to_status"`
	RawText          struct {
		Text             string `json:"text"`
		DisplayTextRange []int  `json:"display_text_range"`
		Facets           []struct {
			Type     string `json:"type"`
			Original string `json:"original"`
		} `json:"facets"`
	} `json:"raw_text"`
	CreatedAt        string            `json:"created_at"`
	CreatedTimestamp int64             `json:"created_timestamp"`
	Media            *rawMedia         `json:"media"`
	Quote            *rawTweet         `json:"quote"`
	Article          *rawArticle       `json:"article"`
	Poll             *model.Poll       `json:"poll"`
	CommunityNote    *rawCommunityNote `json:"community_note"`
}

type rawMedia struct {
	All []struct {
		Type   string `json:"type"`
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"all"`
}

func tweetFromRaw(raw *rawTweet) *Tweet {
	if raw == nil || raw.Type == "tombstone" {
		return nil
	}
	visibleText := visibleTweetText(raw.Text, raw.RawText.Text, raw.RawText.DisplayTextRange)
	mentionHandles := make([]string, 0, len(raw.RawText.Facets))
	for _, facet := range raw.RawText.Facets {
		if facet.Type == "mention" && strings.TrimSpace(facet.Original) != "" {
			mentionHandles = append(mentionHandles, strings.TrimPrefix(strings.TrimSpace(facet.Original), "@"))
		}
	}
	out := &Tweet{
		ID:                raw.ID,
		AuthorHandle:      raw.Author.ScreenName,
		AuthorDisplayName: raw.Author.Name,
		AuthorAvatarURL:   raw.Author.AvatarURL,
		Text:              visibleText,
		Lang:              raw.Lang,
		ReplyToHandle:     raw.ReplyingTo.ScreenName,
		ReplyToStatus:     raw.ReplyingToStatus,
		MentionHandles:    mentionHandles,
	}
	if out.ReplyToStatus == "" {
		out.ReplyToStatus = raw.ReplyingTo.Status
	}
	if t, err := time.Parse("Mon Jan 02 15:04:05 -0700 2006", raw.CreatedAt); err == nil {
		out.CreatedAt = t.UTC()
	} else if t, err := time.Parse(time.RFC3339, raw.CreatedAt); err == nil {
		out.CreatedAt = t.UTC()
	} else if raw.CreatedTimestamp > 0 {
		out.CreatedAt = time.Unix(raw.CreatedTimestamp, 0).UTC()
	}

	// Map media.all[] into the same JSON shape feed_items.media_json uses.
	if raw.Media != nil && len(raw.Media.All) > 0 {
		type mediaRef struct {
			URL    string `json:"url"`
			Type   string `json:"type"`
			Width  int    `json:"width,omitempty"`
			Height int    `json:"height,omitempty"`
		}
		refs := make([]mediaRef, 0, len(raw.Media.All))
		for _, m := range raw.Media.All {
			if m.URL == "" {
				continue
			}
			t := m.Type
			if t == "gif" {
				t = "video"
			}
			refs = append(refs, mediaRef{URL: m.URL, Type: t, Width: m.Width, Height: m.Height})
		}
		if len(refs) > 0 {
			b, _ := json.Marshal(refs)
			out.MediaJSON = string(b)
		}
	}
	applyArticle(out, raw.Article)
	out.PollJSON = capturePoll(raw.Poll, time.Now().UTC())
	out.CommunityNote = communityNoteText(raw.CommunityNote)
	out.Quote = tweetFromRaw(raw.Quote)

	return out
}

func visibleTweetText(text, rawText string, displayRange []int) string {
	if rawText == "" || len(displayRange) != 2 {
		return text
	}
	visible, ok := sliceUTF16(rawText, displayRange[0], displayRange[1])
	if !ok {
		return text
	}
	if text == rawText {
		return visible
	}
	hiddenPrefix, ok := sliceUTF16(rawText, 0, displayRange[0])
	if ok && hiddenPrefix != "" && strings.HasPrefix(text, hiddenPrefix) {
		return strings.TrimPrefix(text, hiddenPrefix)
	}
	return text
}

func sliceUTF16(value string, start, end int) (string, bool) {
	if value == "" || start < 0 || end < start {
		return "", false
	}
	encoded := utf16.Encode([]rune(value))
	if end > len(encoded) {
		return "", false
	}
	return string(utf16.Decode(encoded[start:end])), true
}

// UpgradeBannerURL appends the 1500x500 size suffix that twimg banner URLs
// accept. Empty in → empty out so callers can call unconditionally.
func UpgradeBannerURL(u string) string {
	if u == "" {
		return ""
	}
	return u + "/1500x500"
}
