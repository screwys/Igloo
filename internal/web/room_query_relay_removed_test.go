package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/auth"
)

func TestRoomQueryRelayEndpointsAreRemoved(t *testing.T) {
	srv := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerLogsAPIRoutes(mux)
	handler := chain(mux, srv.enforceAuth, srv.csrfProtect)

	sessionID, err := srv.db.CreateAuthSession("alice")
	if err != nil {
		t.Fatalf("CreateAuthSession: %v", err)
	}
	issuedAtMs := time.Now().UnixMilli()
	token := auth.SignAccessToken(srv.cfg.SecretKey, "alice", "admin", nil, sessionID, issuedAtMs)

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/logs/android/room-query", `{"query":"SELECT * FROM auth_tokens"}`},
		{http.MethodGet, "/api/logs/android/room-query/check", ""},
		{http.MethodPost, "/api/logs/android/room-query/result", `{"rows":[["secret"]]}`},
		{http.MethodGet, "/api/logs/android/room-query/result", ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.RemoteAddr = "203.0.113.10:4321"
			req.Host = "igloo.example.test"
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated status = %d, want 401, body = %s", rec.Code, rec.Body.String())
			}

			req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.RemoteAddr = "203.0.113.10:4321"
			req.Host = "igloo.example.test"
			req.Header.Set("Authorization", "Bearer "+token)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec = httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("authenticated status = %d, want 404, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}
