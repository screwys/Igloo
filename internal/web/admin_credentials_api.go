package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/screwys/igloo/internal/auth"
)

// ── Change Credentials ──────────────────────────────────────────────────────

func (s *Server) handleChangeCredentials(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	isHTMX := r.Header.Get("HX-Request") != ""

	if user == nil {
		if isHTMX {
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<span class="status-msg error">Not authenticated</span>`)
		} else {
			writeJSON(w, 401, map[string]any{"error": "not authenticated"})
		}
		return
	}

	var currentPassword, newUsername, newPassword, confirmPassword string
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/x-www-form-urlencoded") || strings.HasPrefix(ct, "multipart/form-data") {
		_ = r.ParseForm()
		currentPassword = r.FormValue("current_password")
		newUsername = r.FormValue("new_username")
		newPassword = r.FormValue("new_password")
		confirmPassword = r.FormValue("new_password_confirm")
	} else {
		var body struct {
			CurrentPassword string `json:"current_password"`
			NewUsername     string `json:"new_username"`
			NewPassword     string `json:"new_password"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			if requestBodyTooLarge(err) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": requestBodyTooLargeMessage})
				return
			}
			writeJSON(w, 400, map[string]any{"error": "invalid request"})
			return
		}
		currentPassword = body.CurrentPassword
		newUsername = body.NewUsername
		newPassword = body.NewPassword
	}

	credErr := func(msg string) {
		if isHTMX {
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(w, `<span class="status-msg error">%s</span>`, template.HTMLEscapeString(msg))
		} else {
			writeJSON(w, 400, map[string]any{"error": msg})
		}
	}

	if currentPassword == "" {
		credErr("Current password is required")
		return
	}
	if newPassword != "" && confirmPassword != "" && newPassword != confirmPassword {
		credErr("Passwords do not match")
		return
	}

	auth.LockUsers()
	defer auth.UnlockUsers()

	users, err := auth.LoadUsers(s.cfg.AuthUsersPath)
	if err != nil {
		credErr("Could not load users")
		return
	}

	rec, exists := users[user.Username]
	if !exists {
		credErr("User not found")
		return
	}

	if !auth.VerifyPassword(currentPassword, rec.Password) {
		credErr("Current password incorrect")
		return
	}

	usernameChanged := false
	finalUsername := user.Username

	if newUsername != "" && newUsername != user.Username {
		if len(newUsername) < 3 {
			credErr("Username must be at least 3 characters")
			return
		}
		if _, taken := users[newUsername]; taken {
			credErr("Username already taken")
			return
		}
		delete(users, user.Username)
		users[newUsername] = rec
		finalUsername = newUsername
		usernameChanged = true
	}

	if newPassword != "" {
		if len(newPassword) < 6 {
			credErr("Password must be at least 6 characters")
			return
		}
		updated := users[finalUsername]
		updated.Password = auth.HashPassword(newPassword)
		users[finalUsername] = updated
	}

	if err := auth.SaveUsers(s.cfg.AuthUsersPath, users); err != nil {
		credErr("Could not save users")
		return
	}
	auth.InvalidateCache()

	if isHTMX {
		w.Header().Set("Content-Type", "text/html")
		msg := "Credentials updated!"
		if usernameChanged {
			msg += " Reloading..."
			w.Header().Set("HX-Refresh", "true")
		}
		_, _ = fmt.Fprintf(w, `<span class="status-msg success">%s</span>`, template.HTMLEscapeString(msg))
	} else {
		writeJSON(w, 200, map[string]any{
			"success":          true,
			"message":          "Credentials updated successfully",
			"username_changed": usernameChanged,
			"new_username":     finalUsername,
		})
	}
}
