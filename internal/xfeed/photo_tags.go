package xfeed

import (
	"encoding/json"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/screwys/igloo/internal/model"
)

// readPhotoTags collects photo tags from the X API responses gallery-dl saved
// with --write-pages. gallery-dl's metadata omits them, so they are read from
// each media entry's features and keyed by the media file ID.
func readPhotoTags(dir string) map[string][]model.MediaTag {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	tags := make(map[string][]model.MediaTag)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var page any
		if json.Unmarshal(data, &page) != nil {
			continue
		}
		collectPhotoTags(page, tags)
	}
	return tags
}

func collectPhotoTags(value any, tags map[string][]model.MediaTag) {
	switch v := value.(type) {
	case map[string]any:
		if key := twitterMediaKey(stringFromAny(v["media_url_https"])); key != "" {
			if found := mediaFeatureTags(v["features"]); len(found) > 0 {
				tags[key] = found
			}
		}
		for _, child := range v {
			collectPhotoTags(child, tags)
		}
	case []any:
		for _, child := range v {
			collectPhotoTags(child, tags)
		}
	}
}

// mediaFeatureTags reads features.<size>.tags; X repeats the same list for
// every image size.
func mediaFeatureTags(features any) []model.MediaTag {
	sizes, _ := features.(map[string]any)
	var tags []model.MediaTag
	seen := make(map[string]bool)
	for _, size := range sizes {
		sizeMap, _ := size.(map[string]any)
		list, _ := sizeMap["tags"].([]any)
		for _, raw := range list {
			tag, _ := raw.(map[string]any)
			if tag == nil || (stringFromAny(tag["type"]) != "" && stringFromAny(tag["type"]) != "user") {
				continue
			}
			handle := NormalizeHandle(stringFromAny(tag["screen_name"]))
			if !ValidHandle(handle) || seen[handle] {
				continue
			}
			seen[handle] = true
			tags = append(tags, model.MediaTag{Handle: handle, Name: strings.TrimSpace(stringFromAny(tag["name"]))})
		}
	}
	return tags
}

// twitterMediaKey returns the file ID shared by the API's media URL
// (pbs.twimg.com/media/ID.jpg) and gallery-dl's (pbs.twimg.com/media/ID?format=jpg).
func twitterMediaKey(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.HasSuffix(u.Hostname(), "twimg.com") {
		return ""
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" {
		return ""
	}
	return strings.TrimSuffix(base, path.Ext(base))
}

func photoTagHandles(media []model.MediaRef) []string {
	var handles []string
	for _, ref := range media {
		for _, tag := range ref.Tagged {
			handles = append(handles, tag.Handle)
		}
	}
	return handles
}
