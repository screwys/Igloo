package web

import (
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/screwys/igloo/internal/auth"
	"github.com/screwys/igloo/internal/components"
)

// ── Users ─────────────────────────────────────────────────────────────────────

type userResponse struct {
	Username  string   `json:"username"`
	Role      string   `json:"role"`
	Platforms []string `json:"platforms"`
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	if r.Header.Get("HX-Request") != "" {
		// Form request: return create or edit form
		if form := r.URL.Query().Get("form"); form != "" {
			if form == "edit" {
				username := r.URL.Query().Get("user")
				users := auth.GetCachedUsers()
				if rec, ok := users[username]; ok {
					u := components.UserDisplay{Username: username, Role: rec.Role, Platforms: s.effectivePlatforms(rec.Platforms)}
					_ = components.UserForm(s.pageProps(w, r), "edit", &u, s.platformChoices()).Render(r.Context(), w)
					return
				}
			}
			_ = components.UserForm(s.pageProps(w, r), "create", nil, s.platformChoices()).Render(r.Context(), w)
			return
		}
		// Default: return full users panel
		s.renderUsersPanelHTML(w, r)
		return
	}

	users := auth.GetCachedUsers()
	result := make([]userResponse, 0, len(users))
	for username, rec := range users {
		result = append(result, userResponse{
			Username:  username,
			Role:      rec.Role,
			Platforms: s.effectivePlatforms(rec.Platforms),
		})
	}
	writeJSON(w, 200, map[string]any{"users": result})
}

func (s *Server) renderUsersPanelHTML(w http.ResponseWriter, r *http.Request) {
	users := auth.GetCachedUsers()
	display := make([]components.UserDisplay, 0, len(users))
	for username, rec := range users {
		display = append(display, components.UserDisplay{
			Username:  username,
			Role:      rec.Role,
			Platforms: s.effectivePlatforms(rec.Platforms),
		})
	}
	_ = components.UsersPanel(s.pageProps(w, r), display).Render(r.Context(), w)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	isHTMX := r.Header.Get("HX-Request") != ""

	var username, password string
	var platforms []string

	// HTMX form: dispatch to create or update based on _method hidden field
	if isHTMX && r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		_ = r.ParseForm()
		method := r.FormValue("_method")
		if method == "PUT" {
			editUser := r.FormValue("_edit_user")
			if editUser != "" {
				s.doUpdateUser(w, r, editUser, r.FormValue("password"), r.Form["platforms"], isHTMX)
				return
			}
		}
		username = strings.TrimSpace(r.FormValue("username"))
		password = strings.TrimSpace(r.FormValue("password"))
		platforms = r.Form["platforms"]
	} else {
		var body struct {
			Username  string   `json:"username"`
			Password  string   `json:"password"`
			Platforms []string `json:"platforms"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			if requestBodyTooLarge(err) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": requestBodyTooLargeMessage})
				return
			}
			writeJSON(w, 400, map[string]any{"error": "invalid JSON"})
			return
		}
		username = body.Username
		password = body.Password
		platforms = body.Platforms
	}

	if username == "" || password == "" {
		if isHTMX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(422)
			_, _ = fmt.Fprint(w, `<span class="status-message error">Username and password required.</span>`)
			return
		}
		writeJSON(w, 400, map[string]any{"error": "username and password required"})
		return
	}
	platforms, platformErr := s.normalizeRequestedPlatforms(platforms)
	if platformErr != nil {
		if isHTMX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(422)
			_, _ = fmt.Fprintf(w, `<span class="status-message error">%s</span>`, template.HTMLEscapeString(platformErr.Error()))
			return
		}
		writeJSON(w, 422, map[string]any{"error": platformErr.Error()})
		return
	}

	auth.LockUsers()
	defer auth.UnlockUsers()

	users, err := auth.LoadUsers(s.cfg.AuthUsersPath)
	if err != nil {
		slog.Error("LoadUsers", "err", err)
		writeJSON(w, 500, map[string]any{"error": "load error"})
		return
	}
	if _, exists := users[username]; exists {
		if isHTMX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(409)
			_, _ = fmt.Fprint(w, `<span class="status-message error">User already exists.</span>`)
			return
		}
		writeJSON(w, 409, map[string]any{"error": "user already exists"})
		return
	}

	users[username] = auth.UserRecord{
		Password:  auth.HashPassword(password),
		Role:      "user",
		Platforms: platforms,
	}
	if err := auth.SaveUsers(s.cfg.AuthUsersPath, users); err != nil {
		slog.Error("SaveUsers", "err", err)
		writeJSON(w, 500, map[string]any{"error": "save error"})
		return
	}
	auth.InvalidateCache()

	if isHTMX {
		s.renderUsersPanelHTML(w, r)
		return
	}
	writeJSON(w, 201, map[string]any{"success": true, "username": username})
}

func (s *Server) doUpdateUser(w http.ResponseWriter, r *http.Request, username, password string, platforms []string, isHTMX bool) {
	auth.LockUsers()
	defer auth.UnlockUsers()

	users, err := auth.LoadUsers(s.cfg.AuthUsersPath)
	if err != nil {
		slog.Error("LoadUsers", "err", err)
		writeJSON(w, 500, map[string]any{"error": "load error"})
		return
	}
	rec, exists := users[username]
	if !exists {
		writeJSON(w, 404, map[string]any{"error": "user not found"})
		return
	}

	password = strings.TrimSpace(password)
	if password != "" {
		rec.Password = auth.HashPassword(password)
	}
	if platforms != nil {
		normalized, platformErr := s.normalizeRequestedPlatforms(platforms)
		if platformErr != nil {
			if isHTMX {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(422)
				_, _ = fmt.Fprintf(w, `<span class="status-message error">%s</span>`, template.HTMLEscapeString(platformErr.Error()))
				return
			}
			writeJSON(w, 422, map[string]any{"error": platformErr.Error()})
			return
		}
		platforms = normalized
		rec.Platforms = platforms
	}
	users[username] = rec

	if err := auth.SaveUsers(s.cfg.AuthUsersPath, users); err != nil {
		slog.Error("SaveUsers", "err", err)
		writeJSON(w, 500, map[string]any{"error": "save error"})
		return
	}
	auth.InvalidateCache()

	if isHTMX {
		s.renderUsersPanelHTML(w, r)
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	username := r.PathValue("username")

	var body struct {
		Password  string   `json:"password"`
		Platforms []string `json:"platforms"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		if requestBodyTooLarge(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": requestBodyTooLargeMessage})
			return
		}
		writeJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}

	s.doUpdateUser(w, r, username, body.Password, body.Platforms, r.Header.Get("HX-Request") != "")
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	username := r.PathValue("username")
	if username == "admin" {
		writeJSON(w, 403, map[string]any{"error": "cannot delete admin user"})
		return
	}

	auth.LockUsers()
	defer auth.UnlockUsers()

	users, err := auth.LoadUsers(s.cfg.AuthUsersPath)
	if err != nil {
		slog.Error("LoadUsers", "err", err)
		writeJSON(w, 500, map[string]any{"error": "load error"})
		return
	}
	if _, exists := users[username]; !exists {
		writeJSON(w, 404, map[string]any{"error": "user not found"})
		return
	}

	delete(users, username)
	if err := auth.SaveUsers(s.cfg.AuthUsersPath, users); err != nil {
		slog.Error("SaveUsers", "err", err)
		writeJSON(w, 500, map[string]any{"error": "save error"})
		return
	}
	auth.InvalidateCache()

	if r.Header.Get("HX-Request") != "" {
		s.renderUsersPanelHTML(w, r)
		return
	}
	writeJSON(w, 200, map[string]any{"success": true})
}
