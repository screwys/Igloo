package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleTranslateRoutesVideoCommentsByStoredIdentity(t *testing.T) {
	srv := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerTranslateAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/translate", strings.NewReader(`{
		"video_id":"sample_video",
		"comment_id":"missing_comment",
		"target_lang":"en"
	}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "video comment not found") {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
}

func TestHandleTranslateRequiresBothVideoCommentIdentifiers(t *testing.T) {
	srv := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerTranslateAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/translate", strings.NewReader(`{
		"video_id":"sample_video",
		"target_lang":"en"
	}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
