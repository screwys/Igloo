package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ── Android dashboard ─────────────────────────────────────────────────────────

var allowedAndroidTags = map[string]bool{
	"FeedSync": true, "FeedRepo": true, "FeedVM": true,
	"ChannelFeedVM": true, "VideoRepo": true, "VideosVM": true,
	"MediaCache": true, "AuthVM": true, "AuthRepo": true,
	"CrashReport": true, "StatsLogger": true,
}

const androidLegacyLogMaxBodyBytes int64 = 256 << 10

func hasAllowedTag(line string) bool {
	for tag := range allowedAndroidTags {
		if strings.Contains(line, tag) {
			return true
		}
	}
	return false
}

func (s *Server) handleAndroidLog(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Line string `json:"line"`
	}
	if err := decodeLimitedJSON(w, r, androidLegacyLogMaxBodyBytes, &body); err != nil {
		if requestBodyTooLarge(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"success": false, "error": requestBodyTooLargeMessage})
			return
		}
		writeJSON(w, 400, map[string]any{"success": false, "error": "invalid JSON"})
		return
	}
	line := strings.TrimSpace(body.Line)
	if line == "" {
		writeJSON(w, 400, map[string]any{"success": false, "error": "empty line"})
		return
	}

	path := filepath.Join(s.cfg.DataDir, "logs", "android", "android.log")
	_ = appendToFile(path, []string{line})

	evt := parseLogLine(line)
	android.mu.Lock()
	android.eventBuffer = append(android.eventBuffer, evt)
	if len(android.eventBuffer) > 500 {
		android.eventBuffer = android.eventBuffer[len(android.eventBuffer)-500:]
	}
	android.mu.Unlock()

	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) handleAndroidBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceID       string            `json:"device_id"`
		DeviceInfo     string            `json:"device_info"`
		AndroidVersion string            `json:"android_version"`
		Logs           []androidLogEvent `json:"logs"`
	}
	if err := decodeLimitedJSON(w, r, androidLegacyLogMaxBodyBytes, &body); err != nil {
		if requestBodyTooLarge(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"success": false, "error": requestBodyTooLargeMessage})
			return
		}
		writeJSON(w, 400, map[string]any{"success": false, "error": "invalid JSON"})
		return
	}

	// Filter by allowed tags, cap at 200.
	var filtered []androidLogEvent
	var lines []string
	for _, e := range body.Logs {
		if hasAllowedTag(e.Tag) || hasAllowedTag(e.Message) {
			filtered = append(filtered, e)
			lines = append(lines, fmt.Sprintf("%s [%s] [%s] %s", e.Timestamp, e.Level, e.Tag, e.Message))
		}
		if len(filtered) >= 200 {
			break
		}
	}

	if len(lines) > 0 {
		path := filepath.Join(s.cfg.DataDir, "logs", "android", "android.log")
		_ = appendToFile(path, lines)

		android.mu.Lock()
		android.eventBuffer = append(android.eventBuffer, filtered...)
		if len(android.eventBuffer) > 500 {
			android.eventBuffer = android.eventBuffer[len(android.eventBuffer)-500:]
		}
		android.mu.Unlock()

		// Emit highlight to main activity ring when a sync cycle completes
		for _, e := range filtered {
			if e.Tag == "FeedSync" && strings.HasPrefix(e.Message, "=== Sync complete") {
				s.workers.Emit("android", "Android sync complete", "done")
				break
			}
		}
	}

	writeJSON(w, 200, map[string]any{"success": true, "accepted": len(filtered)})
}

func (s *Server) handleAndroidStats(w http.ResponseWriter, r *http.Request) {
	// Android sends {"device_id":"...","lines":["json_event_str",...]}
	var body struct {
		DeviceID string   `json:"device_id"`
		Lines    []string `json:"lines"`
	}
	if err := decodeLimitedJSON(w, r, androidLegacyLogMaxBodyBytes, &body); err != nil {
		if requestBodyTooLarge(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"success": false, "error": requestBodyTooLargeMessage})
			return
		}
		writeJSON(w, 400, map[string]any{"success": false, "error": "invalid JSON"})
		return
	}

	var lines []string
	for _, l := range body.Lines {
		l = strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", "").Replace(l))
		if l != "" {
			lines = append(lines, l)
		}
		if len(lines) >= 500 {
			break
		}
	}

	if len(lines) == 0 {
		writeJSON(w, 200, map[string]any{"success": true, "accepted": 0})
		return
	}

	statsPath := filepath.Join(s.cfg.DataDir, "logs", "android", "stats.jsonl")
	_ = os.MkdirAll(filepath.Dir(statsPath), 0o755)

	// Rotate if over 10MB before appending.
	if fi, err := os.Stat(statsPath); err == nil && fi.Size() > 10*1024*1024 {
		_ = os.Rename(statsPath, statsPath+".1")
	}
	_ = appendToFile(statsPath, lines)

	writeJSON(w, 200, map[string]any{"success": true, "accepted": len(lines)})
}

func (s *Server) handleAndroidDebugLog(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Lines []string `json:"lines"`
	}
	if err := decodeLimitedJSON(w, r, androidLegacyLogMaxBodyBytes, &body); err != nil {
		if requestBodyTooLarge(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"success": false, "error": requestBodyTooLargeMessage})
			return
		}
		writeJSON(w, 400, map[string]any{"success": false, "error": "lines required"})
		return
	}
	if len(body.Lines) == 0 {
		writeJSON(w, 400, map[string]any{"success": false, "error": "lines required"})
		return
	}

	debugPath := filepath.Join(s.cfg.DataDir, "logs", "android", "debug.log")
	_ = os.MkdirAll(filepath.Dir(debugPath), 0o755)

	if fi, err := os.Stat(debugPath); err == nil && fi.Size() > 10*1024*1024 {
		_ = os.Rename(debugPath, debugPath+".1")
	}

	_ = appendToFile(debugPath, body.Lines)

	writeJSON(w, 200, map[string]any{"success": true, "accepted": len(body.Lines)})
}

func (s *Server) handleAndroidCacheHealth(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := decodeLimitedJSON(w, r, androidLegacyLogMaxBodyBytes, &body); err != nil {
		if requestBodyTooLarge(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"success": false, "error": requestBodyTooLargeMessage})
			return
		}
		writeJSON(w, 400, map[string]any{"success": false, "error": "bad json"})
		return
	}

	// New Android reports verified-present counts under "counts" plus the
	// retention settings that shaped the local cache. Preserve legacy flat
	// category payloads as fallback, but intentionally drop thumbnails: the web
	// modal no longer treats generated thumbnails as a cache-health category.
	health := make(map[string]any, len(body))
	for k, v := range body {
		if k == "thumbnails" {
			continue
		}
		health[k] = v
	}
	if counts, ok := health["counts"].(map[string]any); ok {
		delete(counts, "thumbnails")
	}

	android.mu.Lock()
	android.cacheHealth = health
	android.mu.Unlock()

	p := filepath.Join(s.cfg.DataDir, "logs", "android", "cache_health.json")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if data, err := json.Marshal(health); err == nil {
		_ = os.WriteFile(p, data, 0o644)
	}

	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) updateAndroidCacheHealthRetention(retention map[string]any, reportedAtMs int64) {
	if len(retention) == 0 {
		return
	}
	health := loadCacheHealthFromDisk(s.cfg.DataDir)
	if health == nil {
		health = map[string]any{}
	}
	health["generated_at_ms"] = reportedAtMs
	health["retention"] = retention

	android.mu.Lock()
	android.cacheHealth = health
	android.mu.Unlock()

	p := filepath.Join(s.cfg.DataDir, "logs", "android", "cache_health.json")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if data, err := json.Marshal(health); err == nil {
		_ = os.WriteFile(p, data, 0o644)
	}
}

func loadCacheHealthFromDisk(dataDir string) map[string]any {
	p := filepath.Join(dataDir, "logs", "android", "cache_health.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var health map[string]any
	if json.Unmarshal(data, &health) != nil {
		return nil
	}
	return health
}
