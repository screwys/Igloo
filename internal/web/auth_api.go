package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/screwys/igloo/internal/auth"
	"github.com/screwys/igloo/internal/components"
	"github.com/screwys/igloo/internal/db"
)

func (s *Server) registerAuthAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/refresh", s.handleAuthRefresh)
	mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("GET /api/auth/sessions", s.handleAuthSessions)
	mux.HandleFunc("DELETE /api/auth/sessions/{sessionID}", s.handleRevokeAuthSession)
}

// handleAuthLogin opens a new session and issues a paired access +
// refresh token (#16, #17). Login endpoint is in the enforceAuth
// allowlist so it runs without a pre-existing Bearer token.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		ClientName string `json:"client_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, 400, "invalid_body", "invalid JSON")
		return
	}

	users := auth.GetCachedUsers()
	if len(users) == 0 {
		writeJSONError(w, 503, "setup_required", "no users are configured")
		return
	}
	rec, ok := users[body.Username]
	if !ok {
		slog.Warn("auth: unknown user", "username", body.Username, "known_users", len(users))
		writeJSONError(w, 401, "invalid_credentials", "invalid credentials")
		return
	}
	if !auth.VerifyPassword(body.Password, rec.Password) {
		slog.Warn("auth: password mismatch", "username", body.Username)
		writeJSONError(w, 401, "invalid_credentials", "invalid credentials")
		return
	}

	clientName := strings.TrimSpace(body.ClientName)
	if clientName == "" {
		clientName = "App"
	}
	if len(clientName) > 100 {
		writeJSONError(w, 400, "invalid_client_name", "client name too long")
		return
	}
	sessionID, err := s.db.CreateAuthSession(body.Username)
	if err != nil {
		slog.Error("CreateAuthSession", "err", err)
		writeJSONError(w, 500, "session_create_failed", "could not create session")
		return
	}
	if err := s.db.SetAuthSessionClient(sessionID, clientName, 0); err != nil {
		writeJSONError(w, 500, "session_create_failed", "could not save session")
		return
	}
	tokenID, issuedAtMs, refreshExpiresAtMs, err := s.db.CreateRefreshToken(sessionID, auth.RefreshTokenTTL)
	if err != nil {
		slog.Error("CreateRefreshToken", "err", err)
		writeJSONError(w, 500, "session_create_failed", "could not issue refresh token")
		return
	}

	platforms := s.effectivePlatforms(rec.Platforms)
	accessToken := auth.SignAccessToken(s.cfg.SecretKey, body.Username, rec.Role, platforms, sessionID, issuedAtMs)
	refreshToken := auth.SignRefreshToken(s.cfg.SecretKey, body.Username, rec.Role, platforms, sessionID, tokenID, issuedAtMs, refreshExpiresAtMs)

	s.workers.Emit("auth", fmt.Sprintf("Login: %s", body.Username), "info")
	writeJSON(w, 200, map[string]any{
		"access_token":          accessToken,
		"refresh_token":         refreshToken,
		"access_expires_at_ms":  issuedAtMs + auth.AccessTokenTTL.Milliseconds(),
		"refresh_expires_at_ms": refreshExpiresAtMs,
		"username":              body.Username,
		"role":                  rec.Role,
		"platforms":             platforms,
		"is_admin":              rec.Role == "admin",
	})
}

// handleAuthRefresh rotates a token, allowing overlapping requests from shared logins.
func (s *Server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, 400, "invalid_body", "invalid JSON")
		return
	}

	claims, err := auth.VerifyRefreshToken(s.cfg.SecretKey, body.RefreshToken)
	if err != nil {
		writeJSONError(w, 401, refreshErrorCode(err), err.Error())
		return
	}

	token, err := s.db.RotateRefreshToken(claims.TokenID, auth.RefreshTokenTTL)
	if err != nil {
		if errors.Is(err, db.ErrRefreshTokenUnknown) || errors.Is(err, db.ErrRefreshTokenExpired) || errors.Is(err, db.ErrRefreshTokenConsumed) || errors.Is(err, db.ErrSessionRevoked) {
			writeJSONError(w, 401, refreshErrorCode(err), err.Error())
		} else {
			slog.Error("RotateRefreshToken", "err", err)
			writeJSONError(w, 500, "session_create_failed", "could not rotate refresh token")
		}
		return
	}
	tokenID, issuedAtMs, refreshExpiresAtMs := token.TokenID, token.IssuedAtMs, token.ExpiresAtMs

	platforms := s.effectivePlatforms(claims.Platforms)
	accessToken := auth.SignAccessToken(s.cfg.SecretKey, claims.Username, claims.Role, platforms, claims.SessionID, issuedAtMs)
	refreshToken := auth.SignRefreshToken(s.cfg.SecretKey, claims.Username, claims.Role, platforms, claims.SessionID, tokenID, issuedAtMs, refreshExpiresAtMs)

	writeJSON(w, 200, map[string]any{
		"access_token":          accessToken,
		"refresh_token":         refreshToken,
		"access_expires_at_ms":  issuedAtMs + auth.AccessTokenTTL.Milliseconds(),
		"refresh_expires_at_ms": refreshExpiresAtMs,
	})
}

// handleAuthLogout revokes the session identified by the presented
// refresh token. Fire-and-forget — client logs out locally regardless
// of whether the call reaches the server (#17).
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, 400, "invalid_body", "invalid JSON")
		return
	}
	claims, err := auth.VerifyRefreshToken(s.cfg.SecretKey, body.RefreshToken)
	if err != nil {
		// Even if the token's bad, we tell the client OK — they logged out locally already.
		writeJSON(w, 200, map[string]any{})
		return
	}
	if err := s.db.RevokeAuthSession(claims.SessionID, "user_logout"); err != nil {
		slog.Error("RevokeAuthSession", "session", claims.SessionID, "err", err)
	}
	writeJSON(w, 200, map[string]any{})
}

// refreshErrorCode maps refresh-path verify + consume errors onto the
// #18 401 enum.
func refreshErrorCode(err error) string {
	switch {
	case errors.Is(err, auth.ErrTokenExpired):
		return "refresh_token_expired"
	case errors.Is(err, auth.ErrTokenLegacyShape):
		return "legacy_token_invalid"
	case errors.Is(err, auth.ErrTokenMalformed):
		return "refresh_token_invalid"
	case errors.Is(err, auth.ErrTokenWrongType):
		return "refresh_token_invalid"
	case errors.Is(err, db.ErrRefreshTokenExpired):
		return "refresh_token_expired"
	case errors.Is(err, db.ErrRefreshTokenConsumed):
		return "refresh_token_replayed"
	case errors.Is(err, db.ErrSessionRevoked):
		return "session_revoked"
	case errors.Is(err, db.ErrRefreshTokenUnknown):
		return "refresh_token_invalid"
	}
	return "refresh_token_invalid"
}

func authClientName(r *http.Request) string {
	agent := r.UserAgent()
	switch {
	case strings.Contains(agent, "Firefox/"):
		return "Firefox"
	case strings.Contains(agent, "Edg/"):
		return "Edge"
	case strings.Contains(agent, "Chrome/"):
		return "Chrome"
	case strings.Contains(agent, "Safari/"):
		return "Safari"
	case strings.Contains(strings.ToLower(agent), "okhttp"):
		return "Android"
	default:
		return "App"
	}
}

func (s *Server) handleAuthSessions(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		writeJSONError(w, 401, "unauthenticated", "Authentication required")
		return
	}
	sessions, err := s.db.ListAuthSessions(user.Username)
	if err != nil {
		slog.Error("ListAuthSessions", "err", err)
		writeJSONError(w, 500, "db_error", "could not list sessions")
		return
	}
	if r.Header.Get("HX-Request") != "" {
		if err := components.AuthSessions(s.pageProps(w, r), sessions, user.SessionID).Render(r.Context(), w); err != nil {
			slog.Error("render AuthSessions", "err", err)
		}
		return
	}
	writeJSON(w, 200, map[string]any{"sessions": sessions, "current_session_id": user.SessionID})
}

func (s *Server) handleRevokeAuthSession(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		writeJSONError(w, 401, "unauthenticated", "Authentication required")
		return
	}
	sessionID := r.PathValue("sessionID")
	found, err := s.db.RevokeOwnedAuthSession(sessionID, user.Username)
	if err != nil {
		writeJSONError(w, 500, "db_error", "could not revoke session")
		return
	}
	if !found {
		writeJSONError(w, 404, "not_found", "session not found")
		return
	}
	if r.Header.Get("HX-Request") != "" {
		if sessionID == user.SessionID {
			w.Header().Set("HX-Redirect", "/login")
			w.WriteHeader(200)
			return
		}
		s.handleAuthSessions(w, r)
		return
	}
	writeJSON(w, 200, map[string]any{})
}
