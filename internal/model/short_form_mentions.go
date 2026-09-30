package model

import (
	"regexp"
	"strings"
)

type ShortFormMentionSpan struct {
	Start  int
	End    int
	Handle string
}

var (
	shortFormMentionPattern = regexp.MustCompile(`@[A-Za-z0-9_](?:[A-Za-z0-9_.]{0,30}[A-Za-z0-9_])?`)
	mentionLinkPattern      = regexp.MustCompile(`(?s)<a\b[^>]*>.*?</a>|https?://[^\s<>"']+`)
)

// LinkableShortFormMentions finds caption mentions outside links and emails.
func LinkableShortFormMentions(text string) []ShortFormMentionSpan {
	candidates := shortFormMentionPattern.FindAllStringIndex(text, -1)
	if len(candidates) == 0 {
		return nil
	}
	links := mentionLinkPattern.FindAllStringIndex(text, -1)
	linkIndex := 0
	var mentions []ShortFormMentionSpan
	for _, candidate := range candidates {
		start, end := candidate[0], candidate[1]
		for linkIndex < len(links) && links[linkIndex][1] <= start {
			linkIndex++
		}
		if linkIndex < len(links) && start >= links[linkIndex][0] && start < links[linkIndex][1] {
			continue
		}
		if start > 0 && isTwitterMentionWordByte(text[start-1]) {
			continue
		}
		if end < len(text) && (text[end] == '@' || twitterEmailTLD.MatchString(text[end:])) {
			continue
		}
		mentions = append(mentions, ShortFormMentionSpan{
			Start: start, End: end, Handle: strings.ToLower(text[start+1 : end]),
		})
	}
	return mentions
}
