package releasehygiene

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

type finding struct {
	path   string
	line   int
	value  string
	reason string
}

type urlRule struct {
	platform string
	kind     string
	re       *regexp.Regexp
	skip     map[string]bool
}

var (
	syntheticTokens = []string{"sample", "example", "test", "fixture", "demo"}

	allowMarker = "igloo-hygiene: allow-social-fixture"

	// This gates the current legacy backlog without hiding new additions. Remove
	// or shrink it as older fixtures move to sample_* names.
	knownSocialFixtureDebtFingerprint = "2fe7de2f8679891d52df808a40ea07756759ba9613ee80b29de5e9d8f0c2bca5"
	knownSocialFixtureDebtFindings    = 1434

	rawIdentityRe = regexp.MustCompile(`(?i)\b(ChannelID|channel_id|channelId|ReposterChannelID|reposter_channel_id|reposterChannelId|ownerId|owner_id|SourceID|source_id|sourceId|SourceHandle|source_handle|sourceHandle|AuthorHandle|author_handle|authorHandle|QuoteAuthorHandle|quote_author_handle|RetweetedByHandle|retweeted_by_handle|ReposterHandle|reposter_handle|account_handles|accountHandles|TweetID|tweet_id|tweetId|VideoID|video_id|videoId|Handle|handle)"?\s*[:=]\s*["']@?([A-Za-z0-9_.@,-]+)["']`)

	identityContextRe = regexp.MustCompile(`(?i)(ChannelID|channel_id|channelId|ownerId|owner_id|SourceID|source_id|sourceId|VideoID|video_id|videoId|TweetID|tweet_id|tweetId|AuthorHandle|author_handle|authorHandle|QuoteAuthorHandle|quote_author_handle|RetweetedByHandle|retweeted_by_handle|ReposterChannelID|reposter_channel_id|ReposterHandle|reposter_handle|SourceHandle|source_handle|sourceHandle|profile_url|profileUrl|canonical_url|canonicalUrl|tweetUrl|TweetURL|url|URL|Handle|handle|account_handles|accountHandles|/channels/|/api/media/(?:avatar|banner)/)`)

	urlRules = []urlRule{
		{
			platform: "twitter",
			kind:     "X/Twitter handle URL",
			re:       regexp.MustCompile(`https?://(?:www\.)?(?:x|twitter)\.com/([A-Za-z0-9_.-]+)`),
			skip: map[string]bool{
				"i": true, "intent": true, "share": true, "search": true, "home": true, "settings": true,
			},
		},
		{
			platform: "tiktok",
			kind:     "TikTok profile URL",
			re:       regexp.MustCompile(`https?://(?:www\.)?tiktok\.com/@([A-Za-z0-9_.-]+)`),
		},
		{
			platform: "tiktok",
			kind:     "TikTok video ID URL",
			re:       regexp.MustCompile(`https?://(?:www\.)?tiktok\.com/@[A-Za-z0-9_.-]+/(?:video|photo)/([A-Za-z0-9_-]+)`),
		},
		{
			platform: "instagram",
			kind:     "Instagram story handle URL",
			re:       regexp.MustCompile(`https?://(?:www\.)?instagram\.com/stories/([A-Za-z0-9_.-]+)`),
		},
		{
			platform: "instagram",
			kind:     "Instagram profile URL",
			re:       regexp.MustCompile(`https?://(?:www\.)?instagram\.com/([A-Za-z0-9_.-]+)`),
			skip: map[string]bool{
				"p": true, "reel": true, "tv": true, "stories": true, "explore": true, "accounts": true, "direct": true,
			},
		},
		{
			platform: "instagram",
			kind:     "Instagram post ID URL",
			re:       regexp.MustCompile(`https?://(?:www\.)?instagram\.com/(?:p|reel|tv)/([A-Za-z0-9_-]+)`),
		},
		{
			platform: "youtube",
			kind:     "YouTube channel ID URL",
			re:       regexp.MustCompile(`https?://(?:www\.)?youtube\.com/channel/([A-Za-z0-9_-]+)`),
		},
		{
			platform: "youtube",
			kind:     "YouTube handle URL",
			re:       regexp.MustCompile(`https?://(?:www\.)?youtube\.com/@([A-Za-z0-9_.-]+)`),
		},
		{
			platform: "youtube",
			kind:     "YouTube video ID URL",
			re:       regexp.MustCompile(`https?://(?:www\.)?youtube\.com/watch\?v=([A-Za-z0-9_-]+)`),
		},
	}
)

func TestSocialFixtureIdentitiesUseSampleNames(t *testing.T) {
	root := repoRoot(t)
	paths := gitTrackedFiles(t, root)

	var findings []finding
	for _, path := range paths {
		if skipPath(path) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if bytes.Contains(data, []byte{0}) || !utf8.Valid(data) {
			continue
		}
		findings = append(findings, scanFile(path, string(data))...)
	}
	if len(findings) == 0 {
		return
	}
	fingerprint := socialFixtureDebtFingerprint(findings)
	if fingerprint == knownSocialFixtureDebtFingerprint && len(findings) == knownSocialFixtureDebtFindings {
		t.Logf("known legacy social fixture debt remains: %d findings, fingerprint %s; new generic social identities must use sample_* names", len(findings), fingerprint)
		return
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].path != findings[j].path {
			return findings[i].path < findings[j].path
		}
		if findings[i].line != findings[j].line {
			return findings[i].line < findings[j].line
		}
		return findings[i].value < findings[j].value
	})

	var b strings.Builder
	fmt.Fprintf(&b, "social fixture identities must use obvious sample/test names; prefer sample_* values or add %q with a reason for intentional exceptions\n", allowMarker)
	fmt.Fprintf(&b, "current findings: %d, fingerprint: %s\n", len(findings), fingerprint)
	for i, f := range findings {
		if i >= 200 {
			fmt.Fprintf(&b, "... and %d more\n", len(findings)-i)
			break
		}
		fmt.Fprintf(&b, "%s:%d: %s (%s)\n", f.path, f.line, f.value, f.reason)
	}
	t.Fatal(b.String())
}

func TestSocialFixtureScannerExamples(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantValues []string
	}{
		{
			name: "x channel fixture needs sample marker",
			content: `
if err := srv.db.AddChannel(model.Channel{
	ChannelID: "twitter__me_moe",
	SourceID:  "_me_moe",
	Name:      "_me_moe",
	URL:       "https://x.com/_me_moe",
	Platform:  "twitter",
}); err != nil {
	t.Fatal(err)
}`,
			wantValues: []string{"_me_moe", "twitter__me_moe"},
		},
		{
			name: "generic author and tweet IDs still need sample marker",
			content: `
items := []model.FeedItem{
	{
		TweetID:      "tweet-kagi-rate-limited-older",
		AuthorHandle: "author_older",
		BodyText:     "hello",
		Lang:         "en",
	},
}`,
			wantValues: []string{"author_older", "tweet-kagi-rate-limited-older"},
		},
		{
			name: "sample fixtures are accepted",
			content: `
item := model.FeedItem{
	TweetID:      "sample_tweet_kagi_rate_limited_older",
	AuthorHandle: "sample_author_older",
	BodyText:     "hello",
	Lang:         "en",
}
channel := model.Channel{
	ChannelID: "twitter__sample_handle",
	SourceID:  "_sample_handle",
	Name:      "_sample_handle",
	URL:       "https://x.com/_sample_handle",
	Platform:  "twitter",
}`,
		},
		{
			name: "dynamic and structural urls are accepted",
			content: `
url := "https://x.com/" + handle
url = fmt.Sprintf("https://x.com/%s", handle)
match := "https://x.com/i/lists/*"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := scanFile("example_test.go", tt.content)
			gotValues := uniqueFindingValues(findings)
			if !sameStrings(gotValues, tt.wantValues) {
				t.Fatalf("values = %v, want %v; findings: %+v", gotValues, tt.wantValues, findings)
			}
		})
	}
}

func scanFile(path, content string) []finding {
	lines := strings.Split(content, "\n")
	var findings []finding
	for i, line := range lines {
		if lineAllowed(line) {
			continue
		}
		for _, rule := range urlRules {
			for _, match := range rule.re.FindAllStringSubmatch(line, -1) {
				if len(match) < 2 {
					continue
				}
				value := strings.Trim(match[1], `"'`)
				if rule.skip[strings.ToLower(value)] {
					continue
				}
				if !syntheticIdentity(rule.platform, value) {
					findings = append(findings, finding{path: path, line: i + 1, value: value, reason: rule.kind})
				}
			}
		}
		if isFixturePath(path) && hasIdentityContext(lines, i) {
			for _, match := range rawIdentityRe.FindAllStringSubmatch(line, -1) {
				if len(match) < 3 {
					continue
				}
				field := match[1]
				for _, value := range splitRawIdentityValues(match[2]) {
					if !looksConcreteIdentity(value) {
						continue
					}
					if !shouldCheckRawIdentity(field, value) {
						continue
					}
					if !syntheticIdentity("", value) {
						findings = append(findings, finding{path: path, line: i + 1, value: value, reason: "social handle/source literal"})
					}
				}
			}
		}
	}
	return findings
}

func socialFixtureDebtFingerprint(findings []finding) string {
	counts := make(map[string]int, len(findings))
	for _, f := range findings {
		counts[f.path+"\x00"+f.value+"\x00"+f.reason]++
	}

	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s\x00%d\n", key, counts[key])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%x", sum[:])
}

func uniqueFindingValues(findings []finding) []string {
	seen := make(map[string]bool, len(findings))
	for _, finding := range findings {
		seen[finding.value] = true
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func gitTrackedFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	parts := bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		paths = append(paths, string(part))
	}
	return paths
}

func skipPath(path string) bool {
	base := filepath.Base(path)
	if path == "internal/releasehygiene/social_fixture_test.go" ||
		strings.HasPrefix(path, "static/dist/") ||
		strings.HasPrefix(path, "android/.gradle/") ||
		strings.HasPrefix(path, "android/app/build/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".mp4", ".webm", ".mp3", ".ogg", ".ico", ".jar", ".keystore", ".db", ".sqlite":
		return true
	}
	return false
}

func isFixturePath(path string) bool {
	base := filepath.Base(path)
	return strings.Contains(path, "/testdata/") ||
		strings.Contains(path, "/src/test/") ||
		strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, ".test.mjs") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.kt") ||
		strings.HasPrefix(path, "locales/") ||
		strings.HasPrefix(path, "docs/")
}

func lineAllowed(line string) bool {
	idx := strings.Index(line, allowMarker)
	if idx < 0 {
		return false
	}
	return strings.TrimSpace(line[idx+len(allowMarker):]) != ""
}

func hasIdentityContext(lines []string, idx int) bool {
	if identityContextRe.MatchString(lines[idx]) {
		return true
	}
	start := max(0, idx-4)
	end := min(len(lines), idx+5)
	for _, line := range lines[start:end] {
		if strings.Contains(line, "Platform:") ||
			strings.Contains(line, `"platform"`) ||
			strings.Contains(line, "platform =") ||
			strings.Contains(line, "channel_id") ||
			strings.Contains(line, "ChannelID") ||
			strings.Contains(line, "channelId") {
			if strings.Contains(line, "twitter") ||
				strings.Contains(line, "tiktok") ||
				strings.Contains(line, "instagram") ||
				strings.Contains(line, "youtube") ||
				strings.Contains(line, "x.com") {
				return true
			}
		}
	}
	return false
}

func splitRawIdentityValues(value string) []string {
	value = strings.Trim(value, `"'`)
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '@'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, ` "'`)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func looksConcreteIdentity(value string) bool {
	if value == "" {
		return false
	}
	if strings.ContainsAny(value, "${}%+*/<>") {
		return false
	}
	lower := strings.ToLower(value)
	if lower == "user" || lower == "username" || lower == "handle" || lower == "channel" {
		return true
	}
	if len(value) == 1 {
		return false
	}
	return regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`).MatchString(value)
}

func shouldCheckRawIdentity(field, value string) bool {
	field = strings.ToLower(field)
	value = strings.ToLower(strings.Trim(value, ` "'@`))
	switch field {
	case "tweetid", "tweet_id", "videoid", "video_id":
		return looksSocialPostID(value)
	default:
		return true
	}
}

func looksSocialPostID(value string) bool {
	if strings.HasPrefix(value, "twitter_") ||
		strings.HasPrefix(value, "tiktok_") ||
		strings.HasPrefix(value, "instagram_") ||
		strings.HasPrefix(value, "youtube_") {
		return true
	}
	if strings.Contains(value, "tweet") || strings.Contains(value, "post") || strings.Contains(value, "reel") {
		return true
	}
	if regexp.MustCompile(`^[0-9]{8,}$`).MatchString(value) {
		return true
	}
	return false
}

func syntheticIdentity(platform, value string) bool {
	normalized := strings.ToLower(strings.Trim(value, ` "'@`))
	if normalized == "" {
		return true
	}
	for _, token := range syntheticTokens {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	if platform == "tiktok" && regexp.MustCompile(`^900[0-9]{15,}$`).MatchString(normalized) {
		return true
	}
	if platform == "youtube" && strings.HasPrefix(normalized, "ucexample") {
		return true
	}
	return false
}
