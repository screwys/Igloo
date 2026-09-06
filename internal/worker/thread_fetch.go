package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/screwys/igloo/internal/fxtwitter"
	"github.com/screwys/igloo/internal/model"
	"github.com/screwys/igloo/internal/xfeed"
)

const threadFetchBudget = 30 * time.Second

// RefreshThread captures the available conversation and one page of quoting
// posts. Manual refresh is independent of the automatic account selection.
func (m *Manager) RefreshThread(ctx context.Context, tweetID string) (int, error) {
	if !xfeed.ValidTweetID(tweetID) {
		return 0, fmt.Errorf("invalid tweet ID")
	}
	sourceID := tweetID
	tweetID, err := m.db.ResolveFeedStateID(sourceID)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, threadFetchBudget)
	defer cancel()
	fx := fxtwitter.NewClient()
	if m.replyResolver != nil && m.replyResolver.fx != nil {
		fx = m.replyResolver.fx
	}
	conversation, err := fx.FetchConversation(ctx, tweetID)
	if err != nil {
		return 0, err
	}
	if conversation.Status.ID != tweetID {
		return 0, fmt.Errorf("conversation returned a different post")
	}
	// A repost can be the only local copy. Preserve its captured content and
	// media on the original owner before enriching that owner with context.
	if sourceID != tweetID {
		if _, err := m.db.ResolveFeedStateIDForWrite(sourceID); err != nil {
			return 0, err
		}
	}
	seen := make(map[string]bool)
	store := func(tweets []*fxtwitter.Tweet, quotedID string) (int, error) {
		items := make([]model.FeedItem, 0, len(tweets))
		for _, tweet := range tweets {
			if tweet == nil || seen[tweet.ID] || !xfeed.ValidTweetID(tweet.ID) || !xfeed.ValidHandle(tweet.AuthorHandle) {
				continue
			}
			item := tweetToGhostFeedItem(tweet)
			if quotedID != "" {
				if item.QuoteTweetID != "" && item.QuoteTweetID != quotedID {
					continue
				}
				item.QuoteTweetID = quotedID
			}
			seen[tweet.ID] = true
			items = append(items, item)
		}
		result, err := m.upsertFeedItemsBatch(items)
		if err != nil {
			return 0, err
		}
		if err := m.reconcileXMediaRetentionChanges(result.XMediaRetentionChanges); err != nil {
			return result.Processed, err
		}
		m.KickMediaWork()
		return result.Processed, nil
	}
	conversation.Thread = append(conversation.Thread, conversation.Status)
	conversation.Thread = append(conversation.Thread, conversation.Replies...)
	fetched, err := store(conversation.Thread, "")
	if err != nil {
		return fetched, err
	}
	quotes, err := fx.FetchQuotingPosts(ctx, tweetID)
	if err != nil {
		return fetched, err
	}
	count, err := store(quotes, tweetID)
	return fetched + count, err
}

func (m *Manager) newAutomaticThreadRoots(channelID string, items []model.FeedItem) ([]string, error) {
	mode, err := m.db.GetSetting("x_thread_fetch_mode", "starred")
	if err != nil || mode == "never" {
		return nil, err
	}
	if mode != "always" {
		starred, err := m.db.GetStarredChannelIDs()
		if err != nil || !starred[channelID] {
			return nil, err
		}
	}
	var roots []model.FeedItem
	var ids []string
	for _, item := range items {
		if !item.IsReply && !item.IsRetweet && !item.IsGhost && xfeed.ValidTweetID(item.TweetID) {
			roots = append(roots, item)
			ids = append(ids, item.TweetID)
		}
	}
	existing, err := m.db.GetFeedItemsForTweetIDs(ids)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(roots, func(i, j int) bool {
		if len(roots[i].TweetID) != len(roots[j].TweetID) {
			return len(roots[i].TweetID) > len(roots[j].TweetID)
		}
		return roots[i].TweetID > roots[j].TweetID
	})
	limit := m.db.IntSetting("x_thread_auto_post_limit")
	if limit < 1 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}
	ids = ids[:0]
	for _, item := range roots {
		if _, found := existing[item.TweetID]; !found {
			ids = append(ids, item.TweetID)
			existing[item.TweetID] = item
			if len(ids) == limit {
				break
			}
		}
	}
	return ids, nil
}

func (m *Manager) fetchAutomaticThreads(ctx context.Context, roots []string) {
	ctx, cancel := context.WithTimeout(ctx, threadFetchBudget)
	defer cancel()
	for _, tweetID := range roots {
		if ctx.Err() != nil {
			break
		}
		if _, err := m.RefreshThread(ctx, tweetID); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("[x_ingest] thread context %s: %v", tweetID, err)
		}
	}
}
