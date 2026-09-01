package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const instagramGalleryDLTimeout = 90 * time.Second

var instagramSourceSuffixes = []string{"reels", "posts"}
var instagramHandleRe = regexp.MustCompile(`^[a-z0-9._]{1,64}$`)

func normalizeInstagramHandle(raw string) string {
	handle := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
	if !instagramHandleRe.MatchString(handle) {
		return ""
	}
	return handle
}

type InstagramProfile struct {
	Handle      string
	DisplayName string
	Bio         string
	Website     string
	Followers   int
	Following   int
	Verified    bool
	AvatarURL   string
}

// InstagramChannel fetches recent Instagram posts and reels through gallery-dl
// without downloading media.
func (g *GalleryDLWrapper) InstagramChannel(ctx context.Context, handle string, limit int, cookiesFile string, cookiesBrowser ...string) (SourceSnapshot, error) {
	handle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if handle == "" {
		return SourceSnapshot{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	authAttempts := instagramCookieAuthAttempts(cookiesFile, optionalCookieBrowser(cookiesBrowser))
	snapshot := SourceSnapshot{Windows: make([]SourceWindow, 0, len(instagramSourceSuffixes))}
	var firstErr error
	for _, suffix := range instagramSourceSuffixes {
		rawURL := "https://www.instagram.com/" + handle + "/" + suffix + "/"
		window := SourceWindow{Component: suffix}
		var partialRefs []VideoRef
		var firstAttemptErr error
		authSucceeded := false
		for _, auth := range authAttempts {
			cookieRefs, cookieErr := g.instagramDump(ctx, rawURL, limit, auth, handle)
			if cookieErr == nil {
				authSucceeded = true
				window.Refs = cookieRefs
				if len(cookieRefs) > 0 {
					break
				}
				continue
			}
			partialRefs = append(partialRefs, cookieRefs...)
			if firstAttemptErr == nil {
				firstAttemptErr = cookieErr
			}
		}
		window.Complete = authSucceeded && len(window.Refs) > 0
		if !window.Complete {
			window.Refs = mergeSourceRefs(partialRefs, limit)
			if !authSucceeded && firstErr == nil {
				firstErr = firstAttemptErr
			}
		}
		snapshot.Windows = append(snapshot.Windows, window)
	}
	if firstErr != nil {
		return snapshot, firstErr
	}
	return snapshot, nil
}

// InstagramTagged fetches recent posts where handle was tagged. The returned
// refs keep the original post owner in ChannelID and use repost-source fields
// to record the followed tagged account that introduced the post.
func (g *GalleryDLWrapper) InstagramTagged(ctx context.Context, handle string, limit int, cookiesFile string, cookiesBrowser ...string) ([]VideoRef, error) {
	handle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if handle == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	authAttempts := instagramCookieAuthAttempts(cookiesFile, optionalCookieBrowser(cookiesBrowser))
	rawURL := "https://www.instagram.com/" + handle + "/tagged/"
	var refs []VideoRef
	var err error
	authSucceeded := false
	for _, auth := range authAttempts {
		cookieOutput, cookieErr := g.instagramTaggedDumpOutput(ctx, rawURL, limit, auth.File, auth.Browser)
		cookieRefs := ParseInstagramTaggedDumpForHandle(cookieOutput, handle)
		if cookieErr == nil {
			authSucceeded = true
			err, refs = nil, cookieRefs
			if len(cookieRefs) > 0 {
				break
			}
			continue
		}
		if err == nil {
			err = cookieErr
		}
	}
	if authSucceeded && len(refs) == 0 {
		err = nil
	}
	if err != nil {
		return refs, err
	}
	return mergeSourceRefs(refs, limit), nil
}

func (g *GalleryDLWrapper) InstagramProfile(ctx context.Context, handle string, cookiesFile string, cookiesBrowser ...string) (*InstagramProfile, error) {
	handle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if handle == "" {
		return nil, nil
	}
	var firstErr error
	anySuccess := false
	cookieAttempts := instagramProfileCookieAttempts(cookiesFile, optionalCookieBrowser(cookiesBrowser))
	rawURL := "https://www.instagram.com/" + handle + "/info/"
	for _, cookies := range cookieAttempts {
		output, err := g.instagramProfileDumpOutput(ctx, rawURL, cookies.File, cookies.Browser)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		anySuccess = true
		if profile := ParseInstagramProfileDump(output, handle); profile != nil {
			return profile, nil
		}
	}
	if anySuccess {
		return nil, fmt.Errorf("gallery-dl Instagram profile returned no matching metadata for %s", handle)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, nil
}

func instagramProfileScore(profile InstagramProfile, fallbackHandle string) int {
	score := 0
	if profile.Handle != "" {
		score++
	}
	if profile.DisplayName != "" && profile.DisplayName != fallbackHandle {
		score += 2
	}
	if instagramAvatarURLUsable(profile.AvatarURL) {
		score += 8
	}
	if profile.Bio != "" || profile.Website != "" {
		score += 2
	}
	if profile.Followers > 0 || profile.Following > 0 || profile.Verified {
		score += 2
	}
	return score
}

func instagramProfileCookieAttempts(cookiesFile, cookiesBrowser string) []CookieSet {
	if strings.TrimSpace(cookiesFile) == "" {
		if strings.TrimSpace(cookiesBrowser) == "" {
			return []CookieSet{{}}
		}
		return []CookieSet{{Browser: strings.TrimSpace(cookiesBrowser)}}
	}
	out := []CookieSet{{File: strings.TrimSpace(cookiesFile)}}
	if strings.TrimSpace(cookiesBrowser) != "" {
		out = append(out, CookieSet{Browser: strings.TrimSpace(cookiesBrowser)})
	}
	return out
}

func instagramCookieAuthAttempts(cookiesFile, cookiesBrowser string) []CookieSet {
	var out []CookieSet
	if strings.TrimSpace(cookiesFile) != "" {
		out = append(out, CookieSet{File: strings.TrimSpace(cookiesFile)})
	}
	if strings.TrimSpace(cookiesBrowser) != "" {
		out = append(out, CookieSet{Browser: strings.TrimSpace(cookiesBrowser)})
	}
	if len(out) == 0 {
		return []CookieSet{{}}
	}
	return out
}

func optionalCookieBrowser(cookiesBrowser []string) string {
	if len(cookiesBrowser) == 0 {
		return ""
	}
	return strings.TrimSpace(cookiesBrowser[0])
}

func (g *GalleryDLWrapper) instagramDump(ctx context.Context, rawURL string, limit int, cookies CookieSet, sourceHandle string) ([]VideoRef, error) {
	output, err := g.instagramDumpOutput(ctx, rawURL, limit, cookies.File, cookies.Browser)
	return ParseInstagramChannelDumpForHandle(output, sourceHandle), err
}

func (g *GalleryDLWrapper) instagramDumpOutput(ctx context.Context, rawURL string, limit int, cookiesFile string, cookiesBrowser ...string) ([]byte, error) {
	browser := optionalCookieBrowser(cookiesBrowser)
	args := instagramDumpArgs(limit, cookiesFile, rawURL, browser)
	result := g.Run(ctx, "instagram.dump", "instagram", rawURL, args, cookiesFile, CommandOptions{Timeout: instagramGalleryDLTimeout}, browser)
	output := result.CombinedOutput()
	err := result.Err
	if err != nil {
		if errors.Is(result.Err, context.DeadlineExceeded) {
			return output, fmt.Errorf("gallery-dl Instagram timed out after %s for %s", instagramGalleryDLTimeout, rawURL)
		}
		return output, fmt.Errorf("gallery-dl Instagram: %w: %s", err, RedactText(string(output)))
	}
	return output, nil
}

func (g *GalleryDLWrapper) instagramProfileDumpOutput(ctx context.Context, rawURL, cookiesFile string, cookiesBrowser ...string) ([]byte, error) {
	browser := optionalCookieBrowser(cookiesBrowser)
	args := instagramProfileDumpArgs(cookiesFile, rawURL, browser)
	result := g.Run(ctx, "instagram.profile", "instagram", rawURL, args, cookiesFile, CommandOptions{Timeout: instagramGalleryDLTimeout}, browser)
	output := result.CombinedOutput()
	if result.Err != nil {
		if errors.Is(result.Err, context.DeadlineExceeded) {
			return output, fmt.Errorf("gallery-dl Instagram profile timed out after %s for %s", instagramGalleryDLTimeout, rawURL)
		}
		return output, fmt.Errorf("gallery-dl Instagram profile: %w: %s", result.Err, RedactText(string(output)))
	}
	return output, nil
}

func (g *GalleryDLWrapper) instagramTaggedDumpOutput(ctx context.Context, rawURL string, limit int, cookiesFile string, cookiesBrowser ...string) ([]byte, error) {
	browser := optionalCookieBrowser(cookiesBrowser)
	args := instagramTaggedArgs(limit, cookiesFile, rawURL, browser)
	result := g.Run(ctx, "instagram.tagged", "instagram", rawURL, args, cookiesFile, CommandOptions{Timeout: instagramGalleryDLTimeout}, browser)
	output := result.CombinedOutput()
	err := result.Err
	if err != nil {
		if errors.Is(result.Err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("gallery-dl Instagram tagged timed out after %s for %s", instagramGalleryDLTimeout, rawURL)
		}
		return nil, fmt.Errorf("gallery-dl Instagram tagged: %w: %s", err, RedactText(string(output)))
	}
	return output, nil
}

func instagramDumpArgs(limit int, cookiesFile, rawURL string, cookiesBrowser ...string) []string {
	if limit <= 0 {
		limit = 20
	}
	args := []string{
		"--dump-json",
		"--simulate",
		"--range", "1-" + strconv.Itoa(limit),
	}
	args = appendCookieAuthArgs(args, cookiesFile, optionalCookieBrowser(cookiesBrowser))
	args = append(args, rawURL)
	return args
}

func instagramTaggedArgs(limit int, cookiesFile, rawURL string, cookiesBrowser ...string) []string {
	return instagramDumpArgs(limit, cookiesFile, rawURL, cookiesBrowser...)
}

func instagramProfileDumpArgs(cookiesFile, rawURL string, cookiesBrowser ...string) []string {
	args := []string{
		"--dump-json",
		"--simulate",
		"--option", "extractor.instagram.user-strategy=info",
	}
	args = appendCookieAuthArgs(args, cookiesFile, optionalCookieBrowser(cookiesBrowser))
	return append(args, rawURL)
}

func ParseInstagramChannelDump(output []byte) []VideoRef {
	return ParseInstagramChannelDumpForHandle(output, "")
}

func ParseInstagramChannelDumpForHandle(output []byte, sourceHandle string) []VideoRef {
	sourceHandle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(sourceHandle), "@"))
	seen := map[string]struct{}{}
	var refs []VideoRef
	for _, payload := range galleryDLJSONPayloads(output) {
		for _, obj := range instagramSourceObjects(payload) {
			ref := instagramVideoRefFromGalleryDLObject(obj, sourceHandle)
			if ref.VideoID == "" {
				continue
			}
			if _, ok := seen[ref.VideoID]; ok {
				continue
			}
			seen[ref.VideoID] = struct{}{}
			refs = append(refs, ref)
		}
	}
	return refs
}

func ParseInstagramTaggedDumpForHandle(output []byte, taggedHandle string) []VideoRef {
	taggedHandle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(taggedHandle), "@"))
	if taggedHandle == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var refs []VideoRef
	for _, payload := range galleryDLJSONPayloads(output) {
		for _, obj := range instagramSourceObjects(payload) {
			ref := instagramVideoRefFromGalleryDLObject(obj, "")
			if ref.VideoID == "" || ref.ChannelID == "" {
				continue
			}
			reposterHandle := instagramTaggedHandleFromObject(obj, taggedHandle)
			if reposterHandle == "" {
				continue
			}
			if _, ok := seen[ref.VideoID]; ok {
				continue
			}
			ref.IsRepost = true
			ref.ReposterHandle = reposterHandle
			ref.ReposterChannelID = "instagram_" + reposterHandle
			reposterProfile := instagramTaggedProfileFromObject(obj, reposterHandle)
			ref.ReposterDisplayName = reposterProfile.DisplayName
			ref.ReposterAvatarURL = reposterProfile.AvatarURL
			ref.RepostedAtMs = firstMillis(obj, "tagged_at", "reposted_at")
			seen[ref.VideoID] = struct{}{}
			refs = append(refs, ref)
		}
	}
	return refs
}

func ParseInstagramProfileDump(output []byte, fallbackHandle string) *InstagramProfile {
	fallbackHandle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(fallbackHandle), "@"))
	for _, payload := range galleryDLJSONPayloads(output) {
		for _, obj := range instagramSourceObjects(payload) {
			profile := instagramProfileFromGalleryDLObject(obj, fallbackHandle)
			if profile.Handle != "" {
				return &profile
			}
		}
	}
	return nil
}

func instagramSourceObjects(value any) []map[string]any {
	switch v := value.(type) {
	case []any:
		if len(v) >= 2 {
			if obj, ok := v[1].(map[string]any); ok {
				return []map[string]any{obj}
			}
		}
		if len(v) >= 3 {
			if obj, ok := v[2].(map[string]any); ok {
				return []map[string]any{obj}
			}
		}
		var out []map[string]any
		for _, item := range v {
			out = append(out, instagramSourceObjects(item)...)
		}
		return out
	case map[string]any:
		return []map[string]any{v}
	default:
		return nil
	}
}

func instagramProfileFromGalleryDLObject(obj map[string]any, fallbackHandle string) InstagramProfile {
	fallbackHandle = normalizeInstagramHandle(fallbackHandle)
	handle := normalizeInstagramHandle(firstDirectExactString(obj, "username", "owner_username", "uploader_id"))
	if handle == "" || (fallbackHandle != "" && handle != fallbackHandle) {
		return InstagramProfile{}
	}
	displayName := firstDirectExactString(obj, "fullname", "full_name", "name")
	if displayName == "" {
		displayName = handle
	}
	return InstagramProfile{
		Handle:      handle,
		DisplayName: displayName,
		Bio:         firstDirectExactString(obj, "biography", "bio"),
		Website:     firstDirectExactString(obj, "external_url", "website"),
		Followers:   firstDirectInt(obj, "edge_followed_by", "followers", "follower_count"),
		Following:   firstDirectInt(obj, "edge_follow", "following", "following_count"),
		Verified:    firstDirectBool(obj, "is_verified", "verified"),
		AvatarURL:   firstDirectExactString(obj, "profile_pic_url_hd", "profile_pic_url", "avatar_url", "profile_image_url"),
	}
}

func instagramProfileHasData(profile InstagramProfile) bool {
	return profile.DisplayName != "" ||
		profile.Bio != "" ||
		profile.Website != "" ||
		profile.Followers > 0 ||
		profile.Following > 0 ||
		profile.Verified ||
		profile.AvatarURL != ""
}

func instagramVideoRefFromGalleryDLObject(obj map[string]any, sourceHandle string) VideoRef {
	kind := strings.ToLower(firstString(obj, "type", "subcategory"))
	if kind == "story" || strings.Contains(kind, "stories") {
		return VideoRef{}
	}
	shortcode := firstString(obj, "post_shortcode", "shortcode")
	if shortcode == "" {
		shortcode = instagramShortcodeFromURL(firstString(obj, "post_url", "url", "webpage_url"))
	}
	if shortcode == "" {
		return VideoRef{}
	}
	prefix := "post"
	if kind == "reel" || strings.Contains(kind, "reel") {
		prefix = "reel"
	}
	handle := normalizeInstagramHandle(firstString(obj, "username", "owner_username", "uploader_id"))
	displayName := firstString(obj, "fullname", "full_name", "name")
	avatarURL := firstExactString(obj, "profile_pic_url_hd", "profile_pic_url", "avatar_url", "profile_image_url")
	if handle != "" {
		nested := instagramNestedProfileForHandle(obj, handle)
		if nested.DisplayName != "" && displayName == "" {
			displayName = nested.DisplayName
		}
		if preferInstagramAvatarURL(nested.AvatarURL, avatarURL) {
			avatarURL = nested.AvatarURL
		}
	}
	if sourceHandle != "" {
		if coauthorDisplayName, ok := instagramCoauthorDisplayName(obj, sourceHandle); ok || handle == "" {
			nested := instagramNestedProfileForHandle(obj, sourceHandle)
			handle = sourceHandle
			if nested.DisplayName != "" {
				displayName = nested.DisplayName
			} else if coauthorDisplayName != "" {
				displayName = coauthorDisplayName
			} else if displayName == "" {
				displayName = sourceHandle
			}
			avatarURL = nested.AvatarURL
		}
	}
	title := firstString(obj, "title", "description", "caption")
	if title == "" {
		title = "Instagram " + prefix
	}
	ref := VideoRef{
		VideoID:           "instagram_" + prefix + "_" + shortcode,
		Title:             title,
		URL:               firstString(obj, "post_url", "url", "webpage_url"),
		ChannelID:         "instagram_" + handle,
		AuthorHandle:      handle,
		AuthorDisplayName: displayName,
		AuthorAvatarURL:   avatarURL,
		PublishedAtMs:     firstMillis(obj, "timestamp", "date", "post_date"),
	}
	if ref.PublishedAtMs == 0 {
		if t := firstTime(obj, "date", "post_date"); t != nil {
			ref.PublishedAtMs = t.UnixMilli()
		}
	}
	if ref.URL == "" {
		if prefix == "reel" {
			ref.URL = "https://www.instagram.com/reel/" + shortcode + "/"
		} else {
			ref.URL = "https://www.instagram.com/p/" + shortcode + "/"
		}
	}
	if ref.ChannelID == "instagram_" {
		ref.ChannelID = ""
	}
	return ref
}

func instagramNestedProfileForHandle(obj map[string]any, handle string) InstagramProfile {
	handle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if handle == "" {
		return InstagramProfile{}
	}
	bestScore := -1
	var best InstagramProfile
	for _, nestedKey := range []string{"user", "owner", "author", "audio_user"} {
		nested, ok := obj[nestedKey].(map[string]any)
		if !ok {
			continue
		}
		username := strings.ToLower(strings.TrimPrefix(firstExactString(nested, "username", "owner_username", "uploader_id"), "@"))
		if username != handle {
			continue
		}
		profile := InstagramProfile{
			Handle:      username,
			DisplayName: firstExactString(nested, "full_name", "fullname", "name"),
			Bio:         firstExactString(nested, "biography", "bio"),
			Website:     firstExactString(nested, "external_url", "website"),
			AvatarURL:   firstExactString(nested, "profile_pic_url_hd", "profile_pic_url", "avatar_url", "profile_image_url"),
			Followers:   firstInt(nested, "edge_followed_by", "followers", "follower_count", "count_followed"),
			Following:   firstInt(nested, "edge_follow", "following", "following_count", "count_follow"),
			Verified:    firstBool(nested, "is_verified", "verified"),
		}
		if !instagramProfileHasData(profile) {
			continue
		}
		score := instagramProfileScore(profile, handle)
		if score > bestScore {
			best = profile
			bestScore = score
		}
	}
	return best
}

func preferInstagramAvatarURL(candidate, current string) bool {
	if strings.TrimSpace(candidate) == "" {
		return false
	}
	if strings.TrimSpace(current) == "" {
		return true
	}
	return instagramAvatarURLUsable(candidate) && !instagramAvatarURLUsable(current)
}

func instagramAvatarURLUsable(raw string) bool {
	raw = strings.TrimSpace(raw)
	return raw != "" && !instagramAvatarURLExpiredAt(raw, time.Now())
}

func instagramAvatarURLExpiredAt(raw string, now time.Time) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	oe := parsed.Query().Get("oe")
	if oe == "" {
		return false
	}
	expiry, err := strconv.ParseInt(oe, 16, 64)
	if err != nil || expiry <= 0 {
		return false
	}
	return !time.Unix(expiry, 0).After(now)
}

func instagramCoauthorDisplayName(obj map[string]any, handle string) (string, bool) {
	handle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if handle == "" {
		return "", false
	}
	raw, ok := obj["coauthors"].([]any)
	if !ok {
		return "", false
	}
	for _, item := range raw {
		coauthor, ok := item.(map[string]any)
		if !ok {
			continue
		}
		username := strings.ToLower(strings.TrimPrefix(firstExactString(coauthor, "username"), "@"))
		if username != handle {
			continue
		}
		return firstExactString(coauthor, "full_name", "fullname", "name"), true
	}
	return "", false
}

func instagramTaggedHandleFromObject(obj map[string]any, fallbackHandle string) string {
	fallbackHandle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(fallbackHandle), "@"))
	candidates := []string{
		firstExactString(obj, "tagged_username", "tagged_user", "tagged_handle"),
		fallbackHandle,
	}
	for _, candidate := range candidates {
		handle := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(candidate), "@"))
		if handle != "" {
			return handle
		}
	}
	return ""
}

func instagramTaggedProfileFromObject(obj map[string]any, handle string) InstagramProfile {
	handle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if handle == "" {
		return InstagramProfile{}
	}
	raw, ok := obj["tagged_users"].([]any)
	if !ok {
		return InstagramProfile{}
	}
	for _, item := range raw {
		tagged, ok := item.(map[string]any)
		if !ok {
			continue
		}
		username := normalizeInstagramHandle(firstExactString(tagged, "username", "owner_username", "uploader_id"))
		if username != handle {
			continue
		}
		return InstagramProfile{
			Handle:      username,
			DisplayName: firstExactString(tagged, "full_name", "fullname", "name"),
			AvatarURL:   firstExactString(tagged, "profile_pic_url_hd", "profile_pic_url", "avatar_url", "profile_image_url"),
		}
	}
	return InstagramProfile{}
}

func firstExactString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := item[key]; ok {
			if s := stringFromAny(v); s != "" {
				return s
			}
		}
	}
	for _, nestedKey := range []string{"author", "user", "owner"} {
		if nested, ok := item[nestedKey].(map[string]any); ok {
			for _, key := range keys {
				if v, ok := nested[key]; ok {
					if s := stringFromAny(v); s != "" {
						return s
					}
				}
			}
		}
	}
	return ""
}

func firstDirectExactString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := item[key]; ok {
			if s := stringFromAny(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func firstDirectInt(item map[string]any, keys ...string) int {
	for _, key := range keys {
		if n := intFromAny(item[key]); n > 0 {
			return n
		}
		if nested, ok := item[key].(map[string]any); ok {
			if n := intFromAny(nested["count"]); n > 0 {
				return n
			}
		}
	}
	return 0
}

func firstInt(item map[string]any, keys ...string) int {
	for _, key := range keys {
		if n := intFromAny(item[key]); n > 0 {
			return n
		}
		if nested, ok := item[key].(map[string]any); ok {
			if n := intFromAny(nested["count"]); n > 0 {
				return n
			}
		}
	}
	for _, nestedKey := range []string{"author", "user", "owner"} {
		if nested, ok := item[nestedKey].(map[string]any); ok {
			for _, key := range keys {
				if n := intFromAny(nested[key]); n > 0 {
					return n
				}
				if nestedCount, ok := nested[key].(map[string]any); ok {
					if n := intFromAny(nestedCount["count"]); n > 0 {
						return n
					}
				}
			}
		}
	}
	return 0
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := strconv.Atoi(v.String())
		return n
	default:
		return 0
	}
}

func firstBool(item map[string]any, keys ...string) bool {
	for _, key := range keys {
		if b, ok := item[key].(bool); ok && b {
			return true
		}
	}
	for _, nestedKey := range []string{"author", "user", "owner"} {
		if nested, ok := item[nestedKey].(map[string]any); ok {
			for _, key := range keys {
				if b, ok := nested[key].(bool); ok && b {
					return true
				}
			}
		}
	}
	return false
}

func firstDirectBool(item map[string]any, keys ...string) bool {
	for _, key := range keys {
		if b, ok := item[key].(bool); ok && b {
			return true
		}
	}
	return false
}

func instagramShortcodeFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, part := range parts {
		if (part == "p" || part == "reel" || part == "tv") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func mergeSourceRefs(refs []VideoRef, limit int) []VideoRef {
	seen := map[string]struct{}{}
	out := make([]VideoRef, 0, len(refs))
	for _, ref := range refs {
		if ref.VideoID == "" {
			continue
		}
		if _, ok := seen[ref.VideoID]; ok {
			continue
		}
		seen[ref.VideoID] = struct{}{}
		out = append(out, ref)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
