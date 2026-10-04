package web

import (
	"net/http"
	"net/url"

	"github.com/screwys/igloo/internal/db"
)

func (s *Server) queueStreamSave(w http.ResponseWriter, r *http.Request, videoID string, intent db.TempDownloadSaveIntent, fields map[string]any) bool {
	needed, err := s.db.StreamVideoNeedsCapture(videoID)
	if err != nil {
		writeJSONError(w, 500, "stream_save", "Could not prepare the video for saving")
		return true
	}
	if !needed {
		return false
	}
	if !requireAdmin(w, r) {
		return true
	}
	sourceURL, err := s.db.StreamVideoSourceURL(videoID)
	if err != nil {
		writeJSONError(w, 500, "stream_save", "Could not read the stream source")
		return true
	}
	if err := s.db.QueueStreamSave(videoID, intent); err != nil {
		writeJSONError(w, 500, "stream_save", "Could not queue the video for saving")
		return true
	}
	s.workers.KickTempDownloads()
	fields["success"], fields["pending"] = true, true
	fields["save_status_url"] = "/api/temp-download-status?url=" + url.QueryEscape(sourceURL)
	fields["save_result_url"] = "/api/videos/" + url.PathEscape(videoID) + "/saved-state"
	writeJSON(w, http.StatusAccepted, fields)
	return true
}

func (s *Server) handleYouTubeSavedState(w http.ResponseWriter, r *http.Request) {
	videoID := r.PathValue("videoID")
	bookmarked, categoryID, err := s.db.IsBookmarked(videoID)
	if err != nil {
		writeJSONError(w, 500, "saved_state", "Could not read saved video state")
		return
	}
	liked, err := s.db.GetFeedLikesForTweetIDs([]string{videoID})
	if err != nil {
		writeJSONError(w, 500, "saved_state", "Could not read saved video state")
		return
	}
	categoryName := ""
	if bookmarked {
		if category, found, err := s.resolveBookmarkCategory(categoryID); err == nil && found {
			categoryName = category.Name
		}
	}
	writeJSON(w, 200, map[string]any{"success": true, "bookmarked": bookmarked,
		"category_id": categoryID, "category_name": categoryName, "is_liked": liked[videoID]})
}
