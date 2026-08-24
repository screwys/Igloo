package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/translate"
)

func (s *Server) registerTranslateAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/translate", s.handleTranslate)
}

func (s *Server) handleTranslate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TweetID    string `json:"tweet_id"`
		Field      string `json:"field"`
		VideoID    string `json:"video_id"`
		CommentID  string `json:"comment_id"`
		TargetLang string `json:"target_lang"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		if requestBodyTooLarge(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": requestBodyTooLargeMessage})
			return
		}
		writeJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	body.TweetID = strings.TrimSpace(body.TweetID)
	body.Field = strings.TrimSpace(body.Field)
	body.VideoID = strings.TrimSpace(body.VideoID)
	body.CommentID = strings.TrimSpace(body.CommentID)
	body.TargetLang = strings.ToLower(strings.TrimSpace(body.TargetLang))

	translateCtx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	var result *translate.Result
	var err error
	logID := body.TweetID
	logField := body.Field
	if body.VideoID != "" || body.CommentID != "" {
		if body.VideoID == "" || body.CommentID == "" || body.TargetLang == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "video_id, comment_id, and target_lang required"})
			return
		}
		logID = body.VideoID + ":" + body.CommentID
		logField = "comment"
		result, err = translate.CommentText(translateCtx, s.db, body.VideoID, body.CommentID, body.TargetLang)
	} else {
		if body.TweetID == "" || body.TargetLang == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tweet_id and target_lang required"})
			return
		}
		if body.Field != "body" && body.Field != "quote" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "field must be 'body' or 'quote'"})
			return
		}
		result, err = translate.FeedText(translateCtx, s.db, body.TweetID, body.Field, body.TargetLang)
	}
	if err != nil {
		s.writeTranslateError(w, logID, logField, err)
		return
	}

	resp := map[string]any{
		"translated_text": result.TranslatedText,
		"source_lang":     result.SourceLang,
		"target_lang":     result.TargetLang,
	}
	if result.Provider != "" && result.Provider != "cache" {
		resp["provider"] = result.Provider
	}
	writeJSON(w, 200, resp)
}

func (s *Server) writeTranslateError(w http.ResponseWriter, tweetID, field string, err error) {
	var already translate.AlreadyTargetLanguageError
	switch {
	case errors.As(err, &already):
		writeJSON(w, http.StatusOK, map[string]any{"error": "Already in target language", "source_lang": already.SourceLang})
	case errors.Is(err, translate.ErrUnsupportedField):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "field must be 'body' or 'quote'"})
	case errors.Is(err, translate.ErrNoText):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "no text to translate"})
	case errors.Is(err, translate.ErrFeedItemNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "feed item not found"})
	case errors.Is(err, translate.ErrCommentNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "video comment not found"})
	case errors.Is(err, translate.ErrNotConfigured):
		slog.Error("translate", "tweet_id", tweetID, "field", field, "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "translation provider not configured"})
	case errors.Is(err, translate.ErrProviderRateLimited):
		slog.Warn("translate provider limited", "tweet_id", tweetID, "field", field, "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "translation provider temporarily unavailable"})
	case errors.Is(err, translate.ErrTranslationFailed):
		slog.Error("translate", "tweet_id", tweetID, "field", field, "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "translation failed"})
	default:
		slog.Error("translate", "tweet_id", tweetID, "field", field, "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "translation failed"})
	}
}
