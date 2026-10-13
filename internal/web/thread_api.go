package web

import (
	"errors"
	"net/http"
	"time"

	"github.com/screwys/igloo/internal/feed"
	"github.com/screwys/igloo/internal/fxtwitter"
	"github.com/screwys/igloo/internal/xfeed"
)

func (s *Server) registerThreadAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/thread/{tweet_id}", s.handleGetThread)
	mux.HandleFunc("POST /api/thread/{tweet_id}/refresh", s.handleRefreshThread)
}

func (s *Server) handleRefreshThread(w http.ResponseWriter, r *http.Request) {
	tweetID := r.PathValue("tweet_id")
	if !xfeed.ValidTweetID(tweetID) {
		writeJSONError(w, http.StatusBadRequest, "invalid_tweet_id", "A valid tweet ID is required")
		return
	}
	if s.workers == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "worker_unavailable", "Thread fetching is unavailable")
		return
	}
	if err := s.retryThreadMedia(tweetID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "thread_media_retry_failed", err.Error())
		return
	}
	fetched, err := s.workers.RefreshThread(r.Context(), tweetID)
	mediaErr := s.retryThreadMedia(tweetID)
	if err == nil {
		err = mediaErr
	}
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, fxtwitter.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSONEnvelope(w, status, map[string]any{
			"ok": false, "error_code": "thread_fetch_failed", "error_message": err.Error(), "fetched": fetched,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "fetched": fetched})
}

func (s *Server) retryThreadMedia(tweetID string) error {
	items, err := s.db.GetThreadTree(tweetID)
	if err != nil {
		return err
	}
	quotes, err := s.db.ListThreadQuotes(tweetID, 20)
	if err != nil {
		return err
	}
	ownerIDs := []string{tweetID}
	for _, item := range append(items, quotes...) {
		ownerIDs = append(ownerIDs, item.TweetID, item.QuoteTweetID)
	}
	if _, err := s.db.RequeueXContentAssets(ownerIDs, true, "manual", time.Now().UnixMilli()); err != nil {
		return err
	}
	s.workers.KickMediaWork()
	return nil
}

// handleGetThread returns the root tweet and all stored replies for a tweet.
//
// GET /api/thread/{tweet_id}
//
// Includes is_ghost rows so the UI can render parent context for tweets the
// user doesn't follow. Bounded at 50 levels by GetThreadTree's CTE depth cap.
//
// Response shape:
//
//	{ "success": true,
//	  "thread": [ <feed item>, ... ],   // root-first pre-order
//	  "root_id": "<root_tweet_id>",
//	  "leaf_id": "<requested_tweet_id>" }
func (s *Server) handleGetThread(w http.ResponseWriter, r *http.Request) {
	tweetID := r.PathValue("tweet_id")
	if tweetID == "" {
		writeJSONError(w, 400, "missing_tweet_id", "tweet_id required")
		return
	}

	items, err := s.db.GetThreadTree(tweetID)
	if err != nil {
		writeJSONError(w, 500, "thread_query_failed", err.Error())
		return
	}
	if len(items) == 0 {
		writeJSON(w, 404, map[string]any{"success": false, "error": "tweet not found"})
		return
	}

	quotes, err := s.db.ListThreadQuotes(tweetID, 20)
	if err != nil {
		writeJSONError(w, 500, "thread_query_failed", err.Error())
		return
	}
	threadCount := len(items)
	// Use the no-collapse variant so every row in the reply tree is returned.
	items = feed.EnrichFeedItemsPreserveRows(s.db, append(items, quotes...))

	var allIDs []string
	for _, item := range items {
		allIDs = append(allIDs, item.TweetID)
	}
	bookmarkInfo, _ := s.db.GetBookmarksForVideoIDsRich(allIDs)

	subscribeURLs := make(map[string]string, len(items))
	for _, item := range items {
		if item.ChannelID == "" {
			continue
		}
		if _, ok := subscribeURLs[item.ChannelID]; !ok {
			subscribeURLs[item.ChannelID] = s.db.ResolveSubscribeURL(item.ChannelID)
		}
	}

	jsonItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m := feedItemToJSON(item, bookmarkInfo, subscribeURLs)
		m["is_reply"] = item.IsReply
		m["is_ghost"] = item.IsGhost
		m["thread_depth"] = item.ThreadDepth
		jsonItems = append(jsonItems, m)
	}

	writeJSON(w, 200, map[string]any{
		"success": true,
		"thread":  jsonItems[:threadCount],
		"quotes":  jsonItems[threadCount:],
		"root_id": items[0].TweetID,
		"leaf_id": tweetID,
	})
}
