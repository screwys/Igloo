package fetchprofile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/screwys/igloo/internal/download"
)

const ttTimeout = 30 * time.Second

// FetchTikTok invokes gallery-dl against the user's /avatar route. gallery-dl
// writes the canonical profile page it already fetched so Igloo can retain the
// sibling userInfo stats that the avatar projection omits.
func FetchTikTok(ctx context.Context, handle string) (*Profile, error) {
	h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if h == "" {
		return nil, ErrNotFound
	}
	cmdCtx, cancel := context.WithTimeout(ctx, ttTimeout)
	defer cancel()
	pageDir, err := os.MkdirTemp("", "igloo-tiktok-profile-")
	if err != nil {
		return nil, fmt.Errorf("create TikTok profile workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(pageDir) }()
	url := "https://www.tiktok.com/@" + h + "/avatar"
	result := download.CommandRunner{}.Run(cmdCtx, "gallery-dl", []string{"--dump-json", "--write-pages", url}, download.CommandOptions{WorkingDir: pageDir})
	out := result.CombinedOutput()
	if result.Err != nil {
		if isTikTokNotFound(out) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("gallery-dl: %w", result.Err)
	}
	profile, err := parseTikTokAvatar(h, result.Stdout)
	if err != nil {
		return nil, err
	}
	if err := validateTikTokProfileIdentity(h, profile); err != nil {
		return nil, err
	}
	pageProfile, err := readTikTokProfilePage(pageDir, h)
	if err != nil {
		return nil, err
	}
	if pageProfile != nil {
		return pageProfile, nil
	}
	return profile, nil
}

func isTikTokNotFound(stderr []byte) bool {
	s := strings.ToLower(string(stderr))
	return strings.Contains(s, "not found") || strings.Contains(s, "no such user")
}

// parseTikTokAvatar walks the gallery-dl --dump-json output looking for the
// user-detail dict. The array contains tuples shaped
// [code, url-or-dict, metadata]; the user-detail we want is the dict entry
// where "type" == "avatar".
func parseTikTokAvatar(handle string, out []byte) (*Profile, error) {
	var user map[string]any
	decoder := json.NewDecoder(bytes.NewReader(out))
	for user == nil {
		var raw []any
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode: %w", err)
		}
		for _, item := range raw {
			arr, ok := item.([]any)
			if !ok || len(arr) < 2 {
				continue
			}
			d, ok := arr[1].(map[string]any)
			if !ok {
				continue
			}
			if t, _ := d["type"].(string); t == "avatar" {
				user = d
				break
			}
		}
	}
	if user == nil {
		return nil, ErrNotFound
	}
	return tikTokProfileFromUser(handle, user, nil), nil
}

func tikTokProfileFromUser(handle string, user, stats map[string]any) *Profile {
	p := &Profile{
		ChannelID: "tiktok_" + handle,
		Platform:  "tiktok",
		Handle:    strOf(user, "uniqueId"),
		Bio:       strOf(user, "signature"),
		AvatarURL: strOf(user, "avatarLarger"),
		Verified:  boolOf(user, "verified"),
	}
	if p.Handle == "" {
		p.Handle = handle
	}
	p.DisplayName = displayNameOrHandle(strOf(user, "nickname"), p.Handle)
	if bl, ok := user["bioLink"].(map[string]any); ok {
		p.Website = normalizeURL(strOf(bl, "link"))
	}
	if p.AvatarURL == "" {
		p.AvatarURL = strOf(user, "avatarMedium")
	}
	if p.AvatarURL == "" {
		p.AvatarURL = strOf(user, "avatarThumb")
	}
	p.Followers = intOf(stats, "followerCount")
	p.Following = intOf(stats, "followingCount")
	return p
}

func readTikTokProfilePage(dir, handle string) (*Profile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read TikTok profile workspace: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".txt" {
			continue
		}
		page, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read TikTok profile page: %w", err)
		}
		profile, err := parseTikTokProfilePage(handle, page)
		if err != nil {
			continue
		}
		if err := validateTikTokProfileIdentity(handle, profile); err != nil {
			return nil, err
		}
		return profile, nil
	}
	return nil, nil
}

func parseTikTokProfilePage(handle string, page []byte) (*Profile, error) {
	const marker = `<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">`
	start := bytes.Index(page, []byte(marker))
	if start < 0 {
		return nil, ErrNotFound
	}
	start += len(marker)
	end := bytes.Index(page[start:], []byte("</script>"))
	if end < 0 {
		return nil, fmt.Errorf("decode TikTok profile page: rehydration script is incomplete")
	}
	var root map[string]any
	if err := json.Unmarshal(page[start:start+end], &root); err != nil {
		return nil, fmt.Errorf("decode TikTok profile page: %w", err)
	}
	scope, _ := root["__DEFAULT_SCOPE__"].(map[string]any)
	detail, _ := scope["webapp.user-detail"].(map[string]any)
	userInfo, _ := detail["userInfo"].(map[string]any)
	user, _ := userInfo["user"].(map[string]any)
	if user == nil {
		return nil, ErrNotFound
	}
	stats, _ := userInfo["stats"].(map[string]any)
	if len(stats) == 0 {
		stats, _ = userInfo["statsV2"].(map[string]any)
	}
	return tikTokProfileFromUser(handle, user, stats), nil
}

func validateTikTokProfileIdentity(requested string, profile *Profile) error {
	if profile == nil {
		return ErrNotFound
	}
	returned := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(profile.Handle), "@"))
	if returned == "" || returned != requested {
		return fmt.Errorf("%w: requested @%s returned @%s", ErrIdentityMismatch, requested, returned)
	}
	return nil
}

func strOf(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func displayNameOrHandle(name, handle string) string {
	for _, r := range name {
		if unicode.IsGraphic(r) && !unicode.IsSpace(r) {
			return name
		}
	}
	return handle
}

func boolOf(m map[string]any, k string) bool {
	if v, ok := m[k].(bool); ok {
		return v
	}
	return false
}

func intOf(m map[string]any, k string) int {
	if m == nil {
		return 0
	}
	switch value := m[k].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		n, _ := strconv.Atoi(value.String())
		return n
	case string:
		n, _ := strconv.Atoi(value)
		return n
	default:
		return 0
	}
}
