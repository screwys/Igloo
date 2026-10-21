package feed

import (
	"math"
	"sort"
	"time"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/model"
)

const (
	recencyHalfLifeHours = 2.0
	seenDemotion         = 0.01
	replyPenalty         = 4.0
	freshBonusPeak       = 18.0
	freshBonusWindowH    = 6.0

	// Affinity scoring (matching Python's feed_ranking.py)
	affinityAccountHalfLifeMs  = 30 * 24 * 3600 * 1000 // 30 days
	affinityTokenHalfLifeMs    = 14 * 24 * 3600 * 1000 // 14 days
	affinityAccountAuthorCoeff = 4.0
	affinityAccountSourceCoeff = 2.0
	affinityMaxAccountBoost    = 20.0
	affinityMaxTokenBoost      = 10.0
	affinityMaxItemBoost       = 40.0
	affinityMediaBoost         = 3.0
	affinityRetweetPenalty     = -4.0
)

// AlgorithmicFeedEnabled reports whether algorithmic ranking is on for web feed
// paths. When false, callers sort chronologically via SortFeedItemsChronological.
func AlgorithmicFeedEnabled(database *db.DB) bool {
	v, _ := database.GetSetting("algorithmic_feed_enabled", "false")
	return v == "true" || v == "1"
}

// SortFeedItemsChronological sorts items by PublishedAt descending, falling back
// to TweetID for stability when timestamps are missing or equal.
func SortFeedItemsChronological(items []model.FeedItem) []model.FeedItem {
	sort.SliceStable(items, func(i, j int) bool {
		var ti, tj int64
		if items[i].PublishedAt != nil {
			ti = items[i].PublishedAt.UnixMilli()
		}
		if items[j].PublishedAt != nil {
			tj = items[j].PublishedAt.UnixMilli()
		}
		if ti != tj {
			return ti > tj
		}
		return items[i].TweetID > items[j].TweetID
	})
	return items
}

// RankFeedItems sorts feed items by combined score (interest + recency - seen demotion).
func RankFeedItems(items []model.FeedItem) []model.FeedItem {
	for i := range items {
		if items[i].AlgoInterestScore == 0 {
			items[i].AlgoInterestScore = ComputeAlgoInterest(db.ScoringItem{
				IsStarred: items[i].ChannelIsStarred, IsFollowed: items[i].ChannelIsFollowed,
				HasMedia: len(items[i].Media) > 0 || items[i].MediaJSON != "", IsRetweet: items[i].IsRetweet,
			}, ScoringContext{})
		}
	}

	// Compute effective scores (interest + freshness - seen demotion)
	// and store them so the client can sort by the full score.
	for i := range items {
		items[i].AlgoInterestScore = effectiveScore(&items[i])
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].AlgoInterestScore > items[j].AlgoInterestScore
	})
	return items
}

// deduplicateRetweets collapses retweets sharing the same ContentHash into one card.
// Original (non-retweet) tweets are always kept and never show retweeter info.
// Retweets of the same content collapse into one card with retweeter info.
func deduplicateRetweets(items []model.FeedItem) []model.FeedItem {
	seenRT := make(map[string]int) // content_hash → index in result (for retweets only)
	var result []model.FeedItem

	for i := range items {
		hash := items[i].ContentHash

		// No hash or quote tweets: always keep
		if hash == "" || items[i].QuoteTweetID != "" {
			result = append(result, items[i])
			continue
		}

		// Original tweets: always keep, never show retweeter badge
		if !items[i].IsRetweet {
			items[i].Retweeters = nil
			result = append(result, items[i])
			continue
		}

		// Retweet: collapse with other retweets of same content
		if idx, exists := seenRT[hash]; exists {
			if items[i].IsLiked {
				result[idx].IsLiked = true
			}
			if items[i].IsBookmarked {
				result[idx].IsBookmarked = true
			}
			continue
		}

		seenRT[hash] = len(result)
		result = append(result, items[i])
	}

	return result
}

func effectiveScore(item *model.FeedItem) float64 {
	var pubMs int64
	if item.PublishedAt != nil {
		pubMs = item.PublishedAt.UnixMilli()
	}
	score := combinedScore(item.AlgoInterestScore, pubMs)
	if item.IsReply {
		score -= replyPenalty
		if score < 0 {
			score = 0
		}
	}
	if item.IsSeen {
		score *= seenDemotion
	}
	return score
}

func combinedScore(interest float64, publishedAtMs int64) float64 {
	if publishedAtMs <= 0 {
		return interest
	}

	pubTime := time.UnixMilli(publishedAtMs)
	hours := time.Since(pubTime).Hours()
	if hours < 0 {
		hours = 0
	}

	decayed := interest * math.Pow(0.5, hours/recencyHalfLifeHours)

	freshBonus := 0.0
	if hours < freshBonusWindowH {
		freshBonus = freshBonusPeak * (1.0 - hours/freshBonusWindowH)
	}

	return decayed + freshBonus
}

func affinityDecayAt(score float64, lastEventMs int64, halfLifeMs int64, nowMs int64) float64 {
	if score <= 0 || lastEventMs <= 0 {
		return 0
	}
	ageMs := float64(nowMs - lastEventMs)
	if ageMs <= 0 {
		return score
	}
	return score * math.Pow(0.5, ageMs/float64(halfLifeMs))
}

// PersonalizeItems scores requested rows using the daily account cache and
// their current share, token and channel state.
func PersonalizeItems(database *db.DB, items []model.FeedItem) {
	inputs := make([]db.ScoringItem, len(items))
	for i := range items {
		item := &items[i]
		inputs[i] = db.ScoringItem{
			AuthorHandle: item.AuthorHandle,
			SourceHandle: item.SourceHandle,
			BodyText:     item.BodyText,
			HasMedia:     len(item.Media) > 0 || item.MediaJSON != "",
			IsRetweet:    item.IsRetweet,
			IsStarred:    item.ChannelIsStarred,
			IsFollowed:   item.ChannelIsFollowed,
		}
	}
	for i, score := range ScoreItems(database, inputs) {
		items[i].AlgoInterestScore = score
	}
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
