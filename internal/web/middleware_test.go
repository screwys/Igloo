package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/sessions"
	"github.com/screwys/igloo/internal/auth"
	"github.com/screwys/igloo/internal/config"
)

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestCSRFRejectsPostWithoutToken(t *testing.T) {
	s := &Server{cfg: &config.Config{SecretKey: "test-key"}}
	handler := s.csrfProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("POST", "/api/test", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 403 {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestCSRFAllowsGet(t *testing.T) {
	s := &Server{cfg: &config.Config{SecretKey: "test-key"}}
	handler := s.csrfProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/channels", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthRefreshBypassesExpiredBearerAndCSRF(t *testing.T) {
	srv := newTestServer(t)
	sessionID, err := srv.db.CreateAuthSession("alice")
	if err != nil {
		t.Fatalf("CreateAuthSession: %v", err)
	}
	tokenID, issuedAtMs, expiresAtMs, err := srv.db.CreateRefreshToken(sessionID, auth.RefreshTokenTTL)
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	refreshToken := auth.SignRefreshToken(srv.cfg.SecretKey, "alice", "admin", nil, sessionID, tokenID, issuedAtMs, expiresAtMs)
	expiredIssuedAtMs := time.Now().Add(-25 * time.Hour).UnixMilli()
	expiredAccessToken := auth.SignAccessToken(srv.cfg.SecretKey, "alice", "admin", nil, sessionID, expiredIssuedAtMs)

	mux := http.NewServeMux()
	srv.registerAuthAPIRoutes(mux)
	handler := chain(mux, srv.enforceAuth, srv.csrfProtect)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", strings.NewReader(`{"refresh_token":"`+refreshToken+`"}`))
	req.Header.Set("Authorization", "Bearer "+expiredAccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["access_token"] == "" || body["refresh_token"] == "" {
		t.Fatalf("refresh did not issue a new token pair: %v", body)
	}
}

func TestThemeAssetsBypassAuth(t *testing.T) {
	srv := newTestServer(t)
	mux := http.NewServeMux()
	srv.registerAdminAPIRoutes(mux)
	handler := chain(mux, srv.enforceAuth, srv.csrfProtect)

	for _, tc := range []struct {
		path        string
		contentType string
		body        string
	}{
		{"/api/theme.css", "text/css", "--bg-primary:"},
		{"/api/theme.json", "application/json", `"tokens":`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, tc.contentType) {
				t.Fatalf("Content-Type = %q, want prefix %q", got, tc.contentType)
			}
			if !strings.Contains(rec.Body.String(), tc.body) {
				t.Fatalf("body missing %q: %s", tc.body, rec.Body.String())
			}
		})
	}
}

func TestEnsureCSRFReturnsRandomFailure(t *testing.T) {
	oldReader := csrfRandomReader
	csrfRandomReader = errorReader{err: errors.New("random unavailable")}
	t.Cleanup(func() {
		csrfRandomReader = oldReader
	})

	s := &Server{store: sessions.NewCookieStore([]byte("test-key"))}
	req := httptest.NewRequest(http.MethodGet, "/channels", nil)
	rec := httptest.NewRecorder()
	sess, err := s.store.Get(req, "session")
	if err != nil {
		t.Fatalf("session: %v", err)
	}

	token, err := s.ensureCSRF(sess, rec, req)
	if err == nil {
		t.Fatalf("ensureCSRF succeeded with token %q", token)
	}
	if _, ok := sess.Values["csrf_token"]; ok {
		t.Fatal("csrf token was stored after random source failure")
	}
}
