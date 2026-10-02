package web

import (
	"bufio"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/components"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

// readLastLines reads up to n lines from the end of a file.
// It reads at most 64KB from the end to avoid loading large files.
func readLastLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	const chunkSize = 64 * 1024
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()

	offset := size - chunkSize
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// If we seeked into the middle, first line may be partial — drop it.
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

var noisePatterns = []string{
	"Transfer-Encoding",
	"PreviewWorker",
	"backfill",
	"preview_debug",
}

func filterNoise(lines []string) []string {
	out := lines[:0]
	for _, l := range lines {
		noisy := false
		for _, p := range noisePatterns {
			if strings.Contains(l, p) {
				noisy = true
				break
			}
		}
		if !noisy {
			out = append(out, l)
		}
	}
	return out
}

// logPathForType resolves a recognized log type to its file path.
func (s *Server) logPathForType(logType string) (string, bool) {
	switch logType {
	case "server", "api", "download", "scheduler", "x_ingest", "error":
		return filepath.Join(s.cfg.Storage.StateRoot(), "logs", "server", logType+".log"), true
	case "android":
		return filepath.Join(s.cfg.Storage.StateRoot(), "logs", "android", "android.log"), true
	case "android-stats":
		return filepath.Join(s.cfg.Storage.StateRoot(), "logs", "android", "stats.jsonl"), true
	default:
		return "", false
	}
}

// knownLogTypes lists all recognized log file names (for summary).
var knownLogTypes = []string{
	"server", "api", "download", "scheduler", "x_ingest", "error",
	"android", "android-stats",
}

// ── Server logs ───────────────────────────────────────────────────────────────

func (s *Server) handleLogsServer(w http.ResponseWriter, r *http.Request) {
	logType := r.URL.Query().Get("type")
	if logType == "" {
		logType = "server"
	}
	nStr := r.URL.Query().Get("lines")
	n, err := strconv.Atoi(nStr)
	if err != nil || n <= 0 {
		n = 100
	}
	filterNoisyParam := r.URL.Query().Get("filter_noise")

	path, ok := s.logPathForType(logType)
	if !ok {
		writeJSON(w, 400, map[string]any{"success": false, "error": "unknown log type"})
		return
	}
	lines, err := readLastLines(path, n)
	if err != nil {
		if os.IsNotExist(err) {
			if r.URL.Query().Get("fmt") == "html" {
				filter := r.URL.Query().Get("raw_filter")
				if filter == "" {
					filter = "all"
				}
				w.Header().Set("Content-Type", "text/html")
				_ = components.ServerRawLog(s.pageProps(w, r), components.ServerRawLogData{Filter: filter}).Render(r.Context(), w)
				return
			}
			writeJSON(w, 200, map[string]any{"success": true, "content": "", "type": logType})
			return
		}
		slog.Error("readLastLines", "path", path, "err", err)
		writeJSON(w, 500, map[string]any{"error": "could not read log"})
		return
	}

	if filterNoisyParam == "1" {
		lines = filterNoise(lines)
	}
	if lines == nil {
		lines = []string{}
	}

	if r.URL.Query().Get("fmt") == "html" {
		filter := r.URL.Query().Get("raw_filter")
		if filter == "" {
			filter = "all"
		}
		d := components.ServerRawLogData{Filter: filter}
		for _, line := range lines {
			level := ""
			switch {
			case strings.Contains(line, "[ERROR]"):
				level = "ERROR"
			case strings.Contains(line, "[WARNING]"):
				level = "WARNING"
			case strings.Contains(line, "[INFO]"):
				level = "INFO"
			}
			d.Lines = append(d.Lines, components.ServerRawLogLine{Text: line, Level: level})
		}
		w.Header().Set("Content-Type", "text/html")
		_ = components.ServerRawLog(s.pageProps(w, r), d).Render(r.Context(), w)
		return
	}

	writeJSON(w, 200, map[string]any{
		"success": true,
		"content": strings.Join(lines, "\n"),
		"type":    logType,
	})
}

func (s *Server) handleLogsSummary(w http.ResponseWriter, r *http.Request) {
	type fileSummary struct {
		Name       string `json:"name"`
		Exists     bool   `json:"exists"`
		Size       int64  `json:"size"`
		ModifiedMs int64  `json:"modified_ms"`
	}
	var summary []fileSummary
	for _, t := range knownLogTypes {
		var path string
		switch t {
		case "android":
			path = filepath.Join(s.cfg.Storage.StateRoot(), "logs", "android", "android.log")
		case "android-stats":
			path = filepath.Join(s.cfg.Storage.StateRoot(), "logs", "android", "stats.jsonl")
		default:
			path = filepath.Join(s.cfg.Storage.StateRoot(), "logs", "server", t+".log")
		}
		fs := fileSummary{Name: t}
		if fi, err := os.Stat(path); err == nil {
			fs.Exists = true
			fs.Size = fi.Size()
			fs.ModifiedMs = fi.ModTime().UnixMilli()
		}
		summary = append(summary, fs)
	}
	writeJSON(w, 200, map[string]any{"files": summary})
}

func (s *Server) handleLogsCleanup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Days int `json:"days"`
	}
	body.Days = 30
	if err := decodeJSON(w, r, &body); err != nil && requestBodyTooLarge(err) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": requestBodyTooLargeMessage})
		return
	}
	if body.Days <= 0 {
		body.Days = 30
	}

	cutoff := time.Now().Add(-time.Duration(body.Days) * 24 * time.Hour)
	logsDir := filepath.Join(s.cfg.Storage.StateRoot(), "logs")

	var deleted int
	var freedBytes int64

	err := filepath.Walk(logsDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil //nolint:nilerr // Keep cleaning other logs when an entry cannot be read.
		}
		if fi.ModTime().Before(cutoff) {
			freedBytes += fi.Size()
			if rmErr := os.Remove(path); rmErr == nil {
				deleted++
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		slog.Error("logs cleanup walk", "err", err)
	}

	writeJSON(w, 200, map[string]any{
		"deleted":     deleted,
		"freed_bytes": freedBytes,
	})
}

func (s *Server) handleLogsMerged(w http.ResponseWriter, r *http.Request) {
	const perFile = 200
	paths := []string{
		filepath.Join(s.cfg.Storage.StateRoot(), "logs", "server", "server.log"),
		filepath.Join(s.cfg.Storage.StateRoot(), "logs", "server", "download.log"),
	}

	var all []string
	for _, p := range paths {
		lines, err := readLastLines(p, perFile)
		if err == nil {
			all = append(all, lines...)
		}
	}

	// Sort descending (ISO-prefixed lines sort lexicographically).
	sort.Slice(all, func(i, j int) bool { return all[i] > all[j] })

	if all == nil {
		all = []string{}
	}
	writeJSON(w, 200, map[string]any{"lines": all, "count": len(all)})
}
