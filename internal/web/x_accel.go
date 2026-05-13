package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) serveDataFileViaXAccel(w http.ResponseWriter, r *http.Request, path, contentType, cacheControl string) bool {
	if !requestFromReverseProxy(r) {
		return false
	}
	redirect, ok := s.dataFileXAccelRedirect(path)
	if !ok {
		return false
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if cacheControl != "" {
		w.Header().Set("Cache-Control", cacheControl)
	}
	w.Header().Set("X-Accel-Redirect", redirect)
	w.WriteHeader(http.StatusOK)
	return true
}

func requestFromReverseProxy(r *http.Request) bool {
	return r.Header.Get("X-Forwarded-Proto") != "" || r.Header.Get("X-Real-IP") != ""
}

func (s *Server) dataFileXAccelRedirect(path string) (string, bool) {
	dataDir, err := filepath.Abs(s.cfg.DataDir)
	if err != nil {
		return "", false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(dataDir, absPath)
	if err != nil || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return "", false
	}
	return "/x-accel/igloo-data/" + escapeXAccelPath(filepath.ToSlash(rel)), true
}

func escapeXAccelPath(rel string) string {
	parts := strings.Split(rel, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
