package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/screwys/igloo/internal/model"
)

// GetFeedItemByTweetID fetches a single feed_items row by tweet_id, including
// ghost rows. Returns (nil, nil) if not found. Used by the reply resolver to
// detect whether a parent already exists in DB before fetching from fxtwitter,
// and by the thread API.
func (db *DB) GetFeedItemByTweetID(tweetID string) (*model.FeedItem, error) {
	f, err := scanFeedItem(db.conn.QueryRow(`
		SELECT `+feedItemSelectSQL("feed_items")+`
		FROM feed_items_resolved AS feed_items
		WHERE tweet_id = ?
	`, tweetID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetFeedItemByTweetID: %w", err)
	}
	return &f, nil
}

// UpsertGhostFeedItem stores a single feed_items row with is_ghost=1. The row
// represents a parent tweet fetched from fxtwitter to maintain thread continuity
// — the user does not follow this account, so we don't want it polluting feed
// listings, but we need it joinable via reply_to_status.
//
// If a row with the same tweet_id already exists and is NOT a ghost (i.e., we
// follow this account and ingested it normally), the ON CONFLICT clause keeps
// is_ghost=0 — the real row wins.
func (db *DB) UpsertGhostFeedItem(item model.FeedItem) error {
	item.IsGhost = true
	_, err := db.UpsertFeedItems([]model.FeedItem{item})
	return err
}

// ListThreadQuotes returns posts quoting the selected tweet without treating
// them as replies or making context-only rows visible in the main feed.
func (db *DB) ListThreadQuotes(tweetID string, limit int) ([]model.FeedItem, error) {
	tweetID, err := db.ResolveFeedStateID(tweetID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	rows, err := db.reader().Query(`
		SELECT `+feedItemSelectSQL("feed_items")+`
		FROM feed_items_resolved AS feed_items
		WHERE quote_tweet_id = ? AND quote_tweet_id != '' AND tweet_id != ?
		ORDER BY published_at DESC, tweet_id DESC
		LIMIT ?`, tweetID, tweetID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanFeedItems(rows)
}

// UpdateReplyToStatus sets reply_to_status on an existing feed_items row.
// Idempotent — calling multiple times with the same value is a no-op.
func (db *DB) UpdateReplyToStatus(tweetID, parentTweetID string) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`UPDATE feed_items
			 SET reply_to_status = ?, is_reply = CASE WHEN ? = '' THEN 0 ELSE 1 END
			 WHERE tweet_id = ?`,
			parentTweetID, parentTweetID, tweetID,
		)
		return err
	})
}

// GetThreadChain returns the conversation chain rooted at tweetID's earliest
// known ancestor and ending at tweetID, ordered root → leaf. If a parent in
// the chain is missing from the DB, the chain stops at the first orphan
// (the leaf row is always returned, even with no ancestors).
// Reposts use their original once it is stored; until then the captured repost
// remains available as the thread's content.
func (db *DB) GetThreadChain(tweetID string) ([]model.FeedItem, error) {
	chains, err := db.GetThreadChains([]string{tweetID})
	if err != nil {
		return nil, err
	}
	return chains[tweetID], nil
}

// GetThreadChains walks all seed chains together and hydrates shared ancestors
// once. Missing rows may still resolve to their latest captured quote.
func (db *DB) GetThreadChains(tweetIDs []string) (map[string][]model.FeedItem, error) {
	out := make(map[string][]model.FeedItem, len(tweetIDs))
	if len(tweetIDs) == 0 {
		return out, nil
	}
	seeds := make(map[string]string, len(tweetIDs))
	for _, id := range tweetIDs {
		seeds[id] = strings.TrimSpace(id)
		out[id] = []model.FeedItem{}
	}
	encoded, err := json.Marshal(seeds)
	if err != nil {
		return nil, err
	}
	seedRows, err := db.reader().Query(`
		SELECT seeds.key, COALESCE(item.canonical_url, '')
		FROM json_each(?) seeds
		LEFT JOIN feed_items item
		  ON item.tweet_id = CAST(seeds.value AS TEXT) AND seeds.value != ''
	`, string(encoded))
	if err != nil {
		return nil, fmt.Errorf("GetThreadChains seeds: %w", err)
	}
	defer func() { _ = seedRows.Close() }()
	for seedRows.Next() {
		var id, canonicalURL string
		if err := seedRows.Scan(&id, &canonicalURL); err != nil {
			return nil, err
		}
		if canonicalID := model.TwitterStatusIDFromURL(canonicalURL); canonicalID != "" {
			seeds[id] = canonicalID
		}
	}
	if err := seedRows.Err(); err != nil {
		return nil, err
	}
	if err := seedRows.Close(); err != nil {
		return nil, err
	}
	encoded, err = json.Marshal(seeds)
	if err != nil {
		return nil, err
	}
	chainRows, err := db.reader().Query(`
		WITH RECURSIVE
		seeds(seed_id, state_id) AS MATERIALIZED (
			SELECT key, CAST(value AS TEXT) FROM json_each(?)
		),
		chain(seed_id, tweet_id, depth) AS (
			SELECT seeds.seed_id, COALESCE(item.tweet_id, seeds.seed_id), 0
			FROM seeds
			LEFT JOIN feed_items item ON item.tweet_id = seeds.state_id
			UNION ALL
			SELECT c.seed_id, fi.reply_to_status, c.depth + 1
			FROM chain c
			JOIN feed_items fi ON fi.tweet_id = c.tweet_id
			WHERE fi.reply_to_status IS NOT NULL
			  AND fi.reply_to_status != ''
			  AND c.depth < 50
		)
		SELECT seed_id, tweet_id FROM chain
		WHERE tweet_id IS NOT NULL AND tweet_id != ''
		ORDER BY seed_id, depth DESC
	`, string(encoded))
	if err != nil {
		return nil, fmt.Errorf("GetThreadChains query: %w", err)
	}
	defer func() { _ = chainRows.Close() }()
	idsBySeed := make(map[string][]string, len(seeds))
	uniqueIDs := make(map[string]bool)
	var ids []string
	for chainRows.Next() {
		var seedID, id string
		if err := chainRows.Scan(&seedID, &id); err != nil {
			return nil, err
		}
		idsBySeed[seedID] = append(idsBySeed[seedID], id)
		if !uniqueIDs[id] {
			uniqueIDs[id] = true
			ids = append(ids, id)
		}
	}
	if err := chainRows.Err(); err != nil {
		return nil, err
	}
	if err := chainRows.Close(); err != nil {
		return nil, err
	}
	itemsByID, err := db.GetFeedItemsForTweetIDs(ids)
	if err != nil {
		return nil, err
	}
	var missingIDs []string
	for _, id := range ids {
		if _, ok := itemsByID[id]; !ok {
			missingIDs = append(missingIDs, id)
		}
	}
	if len(missingIDs) > 0 {
		encoded, err := json.Marshal(missingIDs)
		if err != nil {
			return nil, err
		}
		quoteRows, err := db.reader().Query(`
			SELECT `+feedItemSelectSQL("feed_items")+`
			FROM json_each(?) missing
			JOIN feed_items_resolved AS feed_items ON feed_items.tweet_id = (
				SELECT tweet_id FROM feed_items
				WHERE quote_tweet_id = CAST(missing.value AS TEXT)
				ORDER BY fetched_at DESC, tweet_id DESC
				LIMIT 1
			)
		`, string(encoded))
		if err != nil {
			return nil, fmt.Errorf("GetThreadChains quotes: %w", err)
		}
		defer func() { _ = quoteRows.Close() }()
		quotes, err := scanFeedItems(quoteRows)
		if err != nil {
			return nil, err
		}
		for _, quotedBy := range quotes {
			id := quotedBy.QuoteTweetID
			quote := model.FeedItem{
				TweetID: id, CanonicalTweetID: id,
				ChannelID:         quotedBy.QuoteChannelID,
				AuthorHandle:      quotedBy.QuoteAuthorHandle,
				AuthorDisplayName: quotedBy.QuoteAuthorDisplayName,
				AuthorAvatarURL:   quotedBy.QuoteAuthorAvatarURL,
				BodyText:          quotedBy.QuoteBodyText, ArticleTitle: quotedBy.QuoteArticleTitle,
				PollJSON: quotedBy.QuotePollJSON, CommunityNote: quotedBy.QuoteCommunityNote,
				Lang: quotedBy.QuoteLang, MediaJSON: quotedBy.QuoteMediaJSON,
				PublishedAt: quotedBy.QuotePublishedAt, FetchedAt: quotedBy.FetchedAt,
				IsGhost: true,
			}
			if quote.AuthorHandle != "" {
				quote.CanonicalURL = fmt.Sprintf("https://x.com/%s/status/%s", quote.AuthorHandle, id)
			}
			quote.ParseMedia()
			itemsByID[id] = quote
		}
	}
	for seedID, chainIDs := range idsBySeed {
		for _, id := range chainIDs {
			if item, ok := itemsByID[id]; ok {
				out[seedID] = append(out[seedID], item)
			}
		}
	}
	return out, nil
}

// GetThreadTree returns the earliest known ancestor for tweetID followed by
// every stored descendant reply. Items are ordered as a pre-order reply tree:
// root, first direct reply and its descendants, then the next direct reply.
func (db *DB) GetThreadTree(tweetID string) ([]model.FeedItem, error) {
	chain, err := db.GetThreadChain(tweetID)
	if err != nil {
		return nil, err
	}
	if len(chain) == 0 {
		return nil, nil
	}
	rootID := chain[0].TweetID

	const q = `
		WITH RECURSIVE subtree(tweet_id, parent_id, depth, published_at) AS (
			SELECT ?, '', 0, 0
			UNION ALL
			SELECT child.tweet_id, child.reply_to_status, subtree.depth + 1, COALESCE(child.published_at, 0)
			FROM feed_items child
			JOIN subtree ON child.reply_to_status = subtree.tweet_id
			WHERE child.reply_to_status IS NOT NULL
			  AND child.reply_to_status != ''
			  AND subtree.depth < 50
		)
		SELECT tweet_id, COALESCE(parent_id, ''), depth, published_at
		FROM subtree
		WHERE tweet_id IS NOT NULL AND tweet_id != ''`

	rows, err := db.conn.Query(q, rootID)
	if err != nil {
		return nil, fmt.Errorf("GetThreadTree query: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	type node struct {
		tweetID     string
		parentID    string
		depth       int
		publishedAt int64
	}
	nodes := make(map[string]node)
	children := make(map[string][]node)
	var ids []string
	for rows.Next() {
		var n node
		if err := rows.Scan(&n.tweetID, &n.parentID, &n.depth, &n.publishedAt); err != nil {
			return nil, err
		}
		if _, exists := nodes[n.tweetID]; exists {
			continue
		}
		nodes[n.tweetID] = n
		ids = append(ids, n.tweetID)
		if n.parentID != "" {
			children[n.parentID] = append(children[n.parentID], n)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	itemsByID, err := db.GetFeedItemsForTweetIDs(ids)
	if err != nil {
		return nil, err
	}
	itemsByID[rootID] = chain[0]
	for parentID := range children {
		sort.Slice(children[parentID], func(i, j int) bool {
			left := children[parentID][i]
			right := children[parentID][j]
			if left.publishedAt != right.publishedAt {
				return left.publishedAt < right.publishedAt
			}
			return left.tweetID < right.tweetID
		})
	}

	out := make([]model.FeedItem, 0, len(ids))
	visited := make(map[string]bool, len(ids))
	var walk func(string)
	walk = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		n, ok := nodes[id]
		if !ok {
			return
		}
		item, ok := itemsByID[id]
		if !ok {
			return
		}
		item.ThreadDepth = n.depth
		out = append(out, item)
		for _, child := range children[id] {
			walk(child.tweetID)
		}
	}
	walk(rootID)
	return out, nil
}

type ThreadSummary struct {
	PostCount   int
	PeopleCount int
}

// GetThreadSummaries returns full-tree totals for each seed tweet. Reply roots
// are resolved once, then sibling branches are traversed through the indexed
// reply_to_status relationship.
func (db *DB) GetThreadSummaries(tweetIDs []string) (map[string]ThreadSummary, error) {
	if len(tweetIDs) == 0 {
		return map[string]ThreadSummary{}, nil
	}
	encoded, err := json.Marshal(tweetIDs)
	if err != nil {
		return nil, err
	}
	rows, err := db.reader().Query(`
		WITH RECURSIVE
		candidate_ids(seed_id) AS MATERIALIZED (
			SELECT DISTINCT TRIM(CAST(value AS TEXT))
			FROM json_each(?)
			WHERE TRIM(CAST(value AS TEXT)) != ''
		),
		up(seed_id, tweet_id, reply_to_status, depth) AS (
			SELECT candidate_ids.seed_id, item.tweet_id,
			       COALESCE(item.reply_to_status, ''), 0
			FROM candidate_ids
			JOIN feed_items item ON item.tweet_id = candidate_ids.seed_id
			UNION ALL
			SELECT up.seed_id, parent.tweet_id,
			       COALESCE(parent.reply_to_status, ''), up.depth + 1
			FROM up
			JOIN feed_items parent ON parent.tweet_id = up.reply_to_status
			WHERE up.reply_to_status != ''
			  AND up.depth < 50
		),
		root_depths AS (
			SELECT seed_id, MAX(depth) AS max_depth
			FROM up
			GROUP BY seed_id
		),
		root_seeds AS (
			SELECT up.seed_id, up.tweet_id AS root_id
			FROM up
			JOIN root_depths
			  ON root_depths.seed_id = up.seed_id
			 AND root_depths.max_depth = up.depth
		),
		unique_roots(root_id) AS (
			SELECT DISTINCT root_id FROM root_seeds
		),
		subtree(root_id, tweet_id, depth) AS (
			SELECT root_id, root_id, 0 FROM unique_roots
			UNION ALL
			SELECT subtree.root_id, child.tweet_id, subtree.depth + 1
			FROM subtree
			JOIN feed_items child ON child.reply_to_status = subtree.tweet_id
			WHERE child.reply_to_status IS NOT NULL
			  AND child.reply_to_status != ''
			  AND subtree.depth < 50
		),
		summaries AS (
			SELECT subtree.root_id, COUNT(*) AS post_count,
			       COUNT(DISTINCT NULLIF(item.channel_id, '')) AS people_count
			FROM subtree
			JOIN feed_items item ON item.tweet_id = subtree.tweet_id
			GROUP BY subtree.root_id
		)
		SELECT root_seeds.seed_id, summaries.post_count, summaries.people_count
		FROM root_seeds
		JOIN summaries ON summaries.root_id = root_seeds.root_id
		ORDER BY root_seeds.seed_id
	`, string(encoded))
	if err != nil {
		return nil, fmt.Errorf("get thread summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]ThreadSummary, len(tweetIDs))
	for rows.Next() {
		var seedID string
		var summary ThreadSummary
		if err := rows.Scan(
			&seedID,
			&summary.PostCount,
			&summary.PeopleCount,
		); err != nil {
			return nil, err
		}
		out[seedID] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type IncompleteReplyChain struct {
	SeedTweetID string
	Item        model.FeedItem
}

func (db *DB) ListIncompleteReplyChainsContext(ctx context.Context, tweetIDs []string) ([]IncompleteReplyChain, error) {
	if len(tweetIDs) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(tweetIDs)
	if err != nil {
		return nil, err
	}
	rows, err := db.reader().QueryContext(ctx, `
		WITH RECURSIVE
		candidate_ids(seed_id, priority) AS MATERIALIZED (
			SELECT TRIM(CAST(value AS TEXT)), CAST(key AS INTEGER)
			FROM json_each(?)
			WHERE TRIM(CAST(value AS TEXT)) != ''
		),
		chain(seed_id, tweet_id, priority, depth, path) AS (
			SELECT seed_id, seed_id, priority, 0, ',' || seed_id || ','
			FROM candidate_ids
			UNION ALL
			SELECT chain.seed_id, parent.tweet_id, chain.priority, chain.depth + 1,
			       chain.path || parent.tweet_id || ','
			FROM chain
			JOIN feed_items current ON current.tweet_id = chain.tweet_id
			JOIN feed_items parent ON parent.tweet_id = current.reply_to_status
			WHERE COALESCE(current.reply_to_status, '') != ''
			  AND chain.depth < 50
			  AND INSTR(chain.path, ',' || parent.tweet_id || ',') = 0
		)
		SELECT chain.seed_id, current.tweet_id,
		       COALESCE(current.author_handle, ''),
		       COALESCE(current.reply_to_handle, ''),
		       COALESCE(current.reply_to_status, '')
		FROM chain
		JOIN feed_items_resolved current ON current.tweet_id = chain.tweet_id
		LEFT JOIN feed_items parent ON parent.tweet_id = current.reply_to_status
		WHERE COALESCE(current.is_reply, 0) = 1
		  AND (COALESCE(current.reply_to_status, '') = '' OR parent.tweet_id IS NULL)
		ORDER BY chain.priority, chain.depth DESC
	`, string(encoded))
	if err != nil {
		return nil, fmt.Errorf("list incomplete reply chains: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []IncompleteReplyChain
	for rows.Next() {
		var row IncompleteReplyChain
		if err := rows.Scan(
			&row.SeedTweetID,
			&row.Item.TweetID,
			&row.Item.AuthorHandle,
			&row.Item.ReplyToHandle,
			&row.Item.ReplyToStatus,
		); err != nil {
			return nil, err
		}
		row.Item.IsReply = true
		out = append(out, row)
	}
	return out, rows.Err()
}
