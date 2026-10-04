package download

import (
	"github.com/screwys/igloo/internal/model"
	"net/url"
	"regexp"
	"strings"
)

var tiktokIDRe = regexp.MustCompile(`/(video|photo)/(\d+)`)

// IsTikTokURL reports whether u looks like a TikTok or tnktok URL.
func IsTikTokURL(u string) bool {
	host, _, ok := httpURLParts(u)
	return ok && hostMatches(host, "tiktok.com", "tnktok.com")
}

func TikTokLiveHandle(rawURL string) string {
	if !IsTikTokURL(rawURL) {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[1] != "live" || !strings.HasPrefix(parts[0], "@") {
		return ""
	}
	return model.NormalizeTikTokHandle(parts[0])
}

// extractTikTokID extracts the numeric post ID from a TikTok URL.
// Returns "" if not found.
func extractTikTokID(rawURL string) string {
	m := tiktokIDRe.FindStringSubmatch(rawURL)
	if len(m) < 3 {
		return ""
	}
	return m[2]
}
