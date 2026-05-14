package web

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/components"
	"github.com/screwys/igloo/internal/db"
)

// ── Android sync helpers ──────────────────────────────────────────────────────

// Mirrors the step types actually registered in
// android/app/src/main/java/com/screwy/igloo/sync/SyncRegistry.kt —
// must match exactly so the dashboard's N/total never drifts. Update both
// when Android adds/removes a step.
var androidSyncSteps = []string{
	"purge_deleted", "outbox_drain", "feed_sync", "feed_changes",
	"video_metadata", "channel_metadata", "bookmarks_aliases",
	"ranked_feed", "youtube_videos", "youtube_comments",
	"manifest_subscriptions", "manifest_liked", "manifest_bookmarked",
	"cache_health_pre", "cache_health", "shorts_media", "feed_media",
	"subtitles", "avatars", "sponsorblock", "preview_sprites",
	"prune", "stats_upload",
}

var skipOnLowMemory = map[string]bool{
	"youtube_videos": true, "shorts_media": true,
	"feed_media": true, "subtitles": true,
	"avatars": true, "sponsorblock": true,
}

func formatAgo(seconds float64) string {
	s := int(seconds)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds ago", s)
	case s < 3600:
		return fmt.Sprintf("%dm ago", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh %dm ago", s/3600, (s%3600)/60)
	default:
		return fmt.Sprintf("%dd ago", s/86400)
	}
}

// parseAndroidTimestamp handles "2026-04-01 13:44:13", "2026-04-01 13:44:13,430", and RFC3339.
func parseAndroidTimestamp(ts string) (time.Time, error) {
	clean := ts
	if i := strings.IndexByte(clean, ','); i > 10 {
		clean = clean[:i]
	} else if i := strings.IndexByte(clean, '.'); i > 10 {
		clean = clean[:i]
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", clean, time.Local); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, strings.Replace(ts, "Z", "+00:00", 1)); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp: %q", ts)
}

type syncStepEntry struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMs any    `json:"duration_ms"`
}

type androidClientLogEntry struct {
	TimestampMs   int64             `json:"timestamp_ms"`
	ReceivedAtMs  int64             `json:"received_at_ms"`
	Level         string            `json:"level"`
	Event         string            `json:"event"`
	Fields        map[string]string `json:"fields"`
	RawFields     map[string]any    `json:"-"`
	TimestampTime time.Time         `json:"-"`
}

type androidSyncSummary struct {
	Started     time.Time
	Completed   time.Time
	CompletedAt bool
	Steps       []syncStepEntry
	Footer      string
	Available   bool
}

func parseSyncCycle(buf []androidLogEvent) (map[string]any, []syncStepEntry) {
	steps := make([]syncStepEntry, len(androidSyncSteps))
	for i, name := range androidSyncSteps {
		steps[i] = syncStepEntry{Name: name, Status: "pending", DurationMs: nil}
	}

	// Find all "=== Sync started" indices. The most recent completed cycle is
	// either the one before the last start (if >1 start), or the last start
	// if its last FeedSync event is >=2 minutes old.
	var starts []int
	for i, e := range buf {
		if e.Tag == "FeedSync" && strings.HasPrefix(e.Message, "=== Sync started") {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return nil, steps
	}

	var syncStart, syncEnd int
	if len(starts) >= 2 {
		// Previous cycle ended at the last FeedSync event before the final start.
		syncStart = starts[len(starts)-2]
		for i := starts[len(starts)-1] - 1; i > syncStart; i-- {
			if buf[i].Tag == "FeedSync" {
				syncEnd = i
				break
			}
		}
		if syncEnd == 0 {
			syncEnd = starts[len(starts)-1] - 1
		}
	} else {
		// Only one start: treat as complete if last FeedSync event is >=2min old.
		syncStart = starts[0]
		for i := len(buf) - 1; i > syncStart; i-- {
			if buf[i].Tag == "FeedSync" {
				syncEnd = i
				break
			}
		}
		if syncEnd == 0 {
			return nil, steps
		}
		if endT, err := parseAndroidTimestamp(buf[syncEnd].Timestamp); err == nil {
			if time.Since(endT) < 2*time.Minute {
				return nil, steps
			}
		}
	}

	stepIndex := make(map[string]int, len(androidSyncSteps))
	for i, name := range androidSyncSteps {
		stepIndex[name] = i
	}
	foundSteps := make(map[string]bool)
	for _, e := range buf[syncStart+1 : syncEnd+1] {
		msg := e.Message
		for _, name := range androidSyncSteps {
			if strings.HasPrefix(msg, name+": done") || strings.HasPrefix(msg, name+" done") {
				steps[stepIndex[name]].Status = "done"
				foundSteps[name] = true
				break
			} else if strings.HasPrefix(msg, name+" failed") || strings.HasPrefix(msg, name+": failed") {
				steps[stepIndex[name]].Status = "failed"
				foundSteps[name] = true
				break
			}
		}
	}
	for i, s := range steps {
		if !foundSteps[s.Name] && s.Status == "pending" && skipOnLowMemory[s.Name] {
			steps[i].Status = "skipped"
		}
	}

	return map[string]any{
		"started":   buf[syncStart].Timestamp,
		"completed": buf[syncEnd].Timestamp,
	}, steps
}

// parseAndroidFeedItemCounts scans for the latest [FeedVM] loadInitial line
// to extract subscription feed item count.
func parseAndroidFeedItemCounts(buf []androidLogEvent) (total int) {
	for i := len(buf) - 1; i >= 0; i-- {
		e := buf[i]
		if e.Tag == "FeedVM" && strings.HasPrefix(e.Message, "loadInitial scope=subscriptions") {
			// Extract count=NNN
			if idx := strings.Index(e.Message, "count="); idx >= 0 {
				rest := e.Message[idx+6:]
				for j := 0; j < len(rest); j++ {
					if rest[j] < '0' || rest[j] > '9' {
						rest = rest[:j]
						break
					}
				}
				if n, err := strconv.Atoi(rest); err == nil {
					return n
				}
			}
			return 0
		}
	}
	return 0
}

func readAndroidClientLogEntries(dataDir string, limit int) []androidClientLogEntry {
	path := filepath.Join(dataDir, "logs", "android", "server.log")
	lines, err := readLastLines(path, limit)
	if err != nil {
		return nil
	}
	entries := make([]androidClientLogEntry, 0, len(lines))
	for _, line := range lines {
		var raw struct {
			TimestampMs  int64          `json:"timestamp_ms"`
			ReceivedAtMs int64          `json:"received_at_ms"`
			Level        string         `json:"level"`
			Event        string         `json:"event"`
			Fields       map[string]any `json:"fields"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil || raw.Event == "" {
			continue
		}
		tsMs := raw.TimestampMs
		if tsMs <= 0 {
			tsMs = raw.ReceivedAtMs
		}
		fields := make(map[string]string, len(raw.Fields))
		for k, v := range raw.Fields {
			fields[k] = fmt.Sprintf("%v", v)
		}
		entries = append(entries, androidClientLogEntry{
			TimestampMs:   raw.TimestampMs,
			ReceivedAtMs:  raw.ReceivedAtMs,
			Level:         strings.ToLower(raw.Level),
			Event:         raw.Event,
			Fields:        fields,
			RawFields:     raw.Fields,
			TimestampTime: time.UnixMilli(tsMs),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].TimestampTime.Before(entries[j].TimestampTime)
	})
	return entries
}

func structuredEventsToLogEvents(entries []androidClientLogEntry) []androidLogEvent {
	out := make([]androidLogEvent, 0, len(entries))
	for _, e := range entries {
		level := strings.ToUpper(e.Level)
		if level == "" {
			level = "INFO"
		}
		if level == "INFO" && androidEventLooksWarning(e.Event) {
			level = "WARN"
		}
		if level == "INFO" && androidEventLooksError(e.Event) {
			level = "ERROR"
		}
		out = append(out, androidLogEvent{
			Timestamp: e.TimestampTime.Format(time.RFC3339),
			Level:     level,
			Tag:       e.Event,
			Message:   androidClientLogMessage(e),
		})
	}
	return out
}

func androidClientLogMessage(e androidClientLogEntry) string {
	if len(e.Fields) == 0 {
		return e.Event
	}
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, min(len(keys), 5))
	for _, k := range keys {
		if len(parts) >= 5 {
			break
		}
		v := e.Fields[k]
		if len(v) > 90 {
			v = v[:87] + "..."
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, " ")
}

func androidEventLooksError(event string) bool {
	event = strings.ToLower(event)
	return strings.Contains(event, "unhandled") ||
		strings.Contains(event, "all_parses_failed") ||
		strings.HasSuffix(event, "_error")
}

func androidEventLooksWarning(event string) bool {
	event = strings.ToLower(event)
	return strings.Contains(event, "failed") ||
		strings.Contains(event, "exception") ||
		strings.Contains(event, "stalled") ||
		strings.Contains(event, "skipped_offline") ||
		strings.Contains(event, "aborted_offline")
}

func parseStructuredSync(entries []androidClientLogEntry, cacheHealth map[string]any, preferredGenerationID string, generationReady bool, healthReportedAtMs int64) androidSyncSummary {
	if summary := parseAndroidMirrorSync(entries, preferredGenerationID, generationReady, healthReportedAtMs); summary.Available {
		return summary
	}
	return parseInboundStructuredSync(entries, cacheHealth)
}

func parseAndroidMirrorSync(entries []androidClientLogEntry, preferredGenerationID string, generationReady bool, healthReportedAtMs int64) androidSyncSummary {
	names := []string{"generation", "asset_manifest", "item_import", "asset_drain", "health_report", "cleanup", "sync_complete"}
	steps := make([]syncStepEntry, len(names))
	for i, name := range names {
		steps[i] = syncStepEntry{Name: name, Status: "pending"}
	}
	stepIndex := make(map[string]int, len(steps))
	for i, step := range steps {
		stepIndex[step.Name] = i
	}
	mark := func(name, status string) {
		if idx, ok := stepIndex[name]; ok {
			if steps[idx].Status == "failed" && status == "done" {
				return
			}
			steps[idx].Status = status
		}
	}
	duration := func(name string, ms int64) {
		if ms <= 0 {
			return
		}
		if idx, ok := stepIndex[name]; ok {
			steps[idx].DurationMs = ms
		}
	}

	targetID := preferredGenerationID
	if targetID == "" {
		for i := len(entries) - 1; i >= 0; i-- {
			if id := entries[i].Fields["generation_id"]; id != "" && strings.HasPrefix(entries[i].Event, "android_sync_") {
				targetID = id
				break
			}
		}
	}
	if targetID == "" && !generationReady && healthReportedAtMs <= 0 {
		return androidSyncSummary{Steps: steps}
	}

	start := time.Time{}
	completed := time.Time{}
	available := generationReady || healthReportedAtMs > 0 || targetID != ""
	if generationReady {
		mark("generation", "done")
	}
	if healthReportedAtMs > 0 {
		mark("health_report", "done")
		completed = time.UnixMilli(healthReportedAtMs)
	}

	for _, e := range entries {
		if !androidMirrorEventMatchesGeneration(e, targetID) {
			continue
		}
		switch e.Event {
		case "android_sync_generation_request":
			if targetID == "" && start.IsZero() {
				start = e.TimestampTime
			}
		case "android_sync_generation_start":
			available = true
			mark("generation", "done")
			if start.IsZero() {
				start = e.TimestampTime
			}
		case "android_sync_assets_imported", "android_sync_assets_import_skipped":
			available = true
			mark("asset_manifest", "done")
		case "android_sync_assets_marker_stalled":
			available = true
			mark("asset_manifest", "failed")
		case "android_sync_items_imported", "android_sync_items_import_skipped":
			available = true
			mark("item_import", "done")
		case "android_sync_items_marker_stalled":
			available = true
			mark("item_import", "failed")
		case "android_sync_asset_drain_done":
			available = true
			mark("asset_drain", "done")
		case "android_sync_health_reported":
			available = true
			if e.Fields["uploaded"] == "false" {
				mark("health_report", "failed")
			} else {
				mark("health_report", "done")
				if completed.IsZero() || e.TimestampTime.After(completed) {
					completed = e.TimestampTime
				}
			}
		case "android_sync_content_pruned", "android_sync_orphan_asset_files_pruned", "android_sync_generations_pruned":
			available = true
			mark("cleanup", "done")
		case "android_sync_generation_done":
			available = true
			mark("generation", "done")
			mark("cleanup", "done")
			mark("sync_complete", "done")
			duration("sync_complete", anyInt64(e.RawFields["duration_ms"]))
			if start.IsZero() {
				start = e.TimestampTime
			}
			if completed.IsZero() || e.TimestampTime.After(completed) {
				completed = e.TimestampTime
			}
		case "android_sync_unhandled":
			available = true
			mark("sync_complete", "failed")
		case "periodic_sync_drain_done":
			available = true
			if e.Fields["completed"] == "true" {
				mark("sync_complete", "done")
				if completed.IsZero() || e.TimestampTime.After(completed) {
					completed = e.TimestampTime
				}
			}
			duration("sync_complete", anyInt64(e.RawFields["elapsed_ms"]))
		}
	}

	summary := androidSyncSummary{Started: start, Steps: steps, Available: available}
	if !completed.IsZero() {
		summary.Completed = completed
		summary.CompletedAt = true
		if !start.IsZero() {
			summary.Footer = "Sync started " + start.Local().Format("15:04:05") + " · completed " + completed.Local().Format("15:04:05") + " · " + formatDuration(completed.Sub(start)) + " total"
		} else if targetID != "" {
			summary.Footer = "Latest health report " + completed.Local().Format("15:04:05") + " · " + shortAndroidGenerationID(targetID)
		}
	} else if targetID != "" {
		summary.Footer = "Latest generation " + shortAndroidGenerationID(targetID)
	}
	return summary
}

func androidMirrorEventMatchesGeneration(e androidClientLogEntry, generationID string) bool {
	if !strings.HasPrefix(e.Event, "android_sync_") && e.Event != "periodic_sync_drain_done" {
		return false
	}
	eventGenerationID := e.Fields["generation_id"]
	if generationID == "" {
		return true
	}
	return eventGenerationID == "" || eventGenerationID == generationID
}

func shortAndroidGenerationID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	const prefix = "android-sync-"
	if rest, ok := strings.CutPrefix(id, prefix); ok && len(rest) > 12 {
		return prefix + rest[:12]
	}
	if len(id) > 24 {
		return id[:24]
	}
	return id
}

func parseInboundStructuredSync(entries []androidClientLogEntry, cacheHealth map[string]any) androidSyncSummary {
	names := []string{"mutation_delta", "inbound_pass", "channels", "feed", "shorts", "youtube_videos", "media", "cache_health"}
	steps := make([]syncStepEntry, len(names))
	for i, name := range names {
		steps[i] = syncStepEntry{Name: name, Status: "pending"}
	}
	stepIndex := make(map[string]int, len(steps))
	for i, step := range steps {
		stepIndex[step.Name] = i
	}
	mark := func(name, status string) {
		if idx, ok := stepIndex[name]; ok {
			steps[idx].Status = status
		}
	}

	startIdx := -1
	doneIdx := -1
	for i, e := range entries {
		switch e.Event {
		case "inbound_pass_start":
			startIdx = i
			doneIdx = -1
		case "inbound_pass_done":
			if startIdx >= 0 {
				doneIdx = i
			}
		}
	}
	if startIdx < 0 {
		return androidSyncSummary{Steps: steps}
	}

	start := entries[startIdx].TimestampTime
	completed := time.Time{}
	if doneIdx >= startIdx {
		completed = entries[doneIdx].TimestampTime
	}
	endIdx := len(entries) - 1
	if doneIdx >= startIdx {
		endIdx = doneIdx
	}

	for _, e := range entries[startIdx : endIdx+1] {
		switch e.Event {
		case "mutation_delta_page_applied":
			mark("mutation_delta", "done")
		case "inbound_pass_start":
			mark("inbound_pass", "done")
		case "inbound_pass_done":
			mark("inbound_pass", "done")
		case "stream_page_applied":
			stream := e.Fields["stream"]
			if e.Fields["end_of_stream"] == "true" {
				mark(stream, "done")
			}
		case "stream_fetch_response_error", "stream_fetch_exception", "stream_marker_stalled":
			mark(e.Fields["stream"], "failed")
		case "media_reconciler_batch_done":
			mark("media", "done")
		case "manifest_sync_scope_failed", "manifest_sync_marker_stalled":
			mark("media", "failed")
		case "android_cache_health_reported":
			mark("cache_health", "done")
			if completed.IsZero() || e.TimestampTime.After(completed) {
				completed = e.TimestampTime
			}
		}
	}
	if t := cacheHealthGeneratedAt(cacheHealth); !t.IsZero() && t.After(start) {
		mark("cache_health", "done")
		if completed.IsZero() || t.After(completed) {
			completed = t
		}
	}

	summary := androidSyncSummary{Started: start, Steps: steps, Available: true}
	if !completed.IsZero() {
		summary.Completed = completed
		summary.CompletedAt = true
		summary.Footer = "Sync started " + start.Local().Format("15:04:05") + " · completed " + completed.Local().Format("15:04:05") + " · " + formatDuration(completed.Sub(start)) + " total"
	}
	return summary
}

func cacheHealthGeneratedAt(health map[string]any) time.Time {
	if health == nil {
		return time.Time{}
	}
	if ms := anyInt64(health["generated_at_ms"]); ms > 0 {
		return time.UnixMilli(ms)
	}
	return time.Time{}
}

func cacheHealthSettings(health map[string]any) db.AndroidRetentionSettings {
	settings := db.AndroidRetentionSettings{FeedDays: 7, YoutubeDays: 7, MomentsDays: 7, StoryHours: 48}
	if health == nil {
		return settings
	}
	ret, _ := health["retention"].(map[string]any)
	if v, ok := ret["feed_days"]; ok {
		settings.FeedDays = max(0, anyInt(v))
	}
	if v, ok := ret["youtube_days"]; ok {
		settings.YoutubeDays = max(0, anyInt(v))
	}
	if v, ok := ret["moments_days"]; ok {
		settings.MomentsDays = max(0, anyInt(v))
	}
	if v, ok := ret["story_hours"]; ok {
		settings.StoryHours = db.NormalizeStoriesWindowHours(anyInt(v))
	}
	return settings
}

func cacheHealthReportedCounts(health map[string]any) map[string]int {
	out := map[string]int{}
	if health == nil {
		return out
	}
	if counts, ok := health["counts"].(map[string]any); ok {
		for _, k := range []string{"videos", "moments", "feed", "avatars"} {
			out[k] = anyInt(counts[k])
		}
		return out
	}
	for _, k := range []string{"videos", "moments", "feed", "avatars"} {
		val, ok := health[k]
		if !ok {
			continue
		}
		if arr, ok := val.([]int); ok && len(arr) >= 1 {
			out[k] = arr[0]
			continue
		}
		if arr, ok := val.([]any); ok && len(arr) >= 1 {
			out[k] = anyInt(arr[0])
		}
	}
	return out
}

func anyInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

func anyInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	default:
		return 0
	}
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	minutes := int(d / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm %02ds", minutes, seconds)
}

func androidSyncGenerationDuration(entries []androidClientLogEntry, generationID string) time.Duration {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Event != "android_sync_generation_done" {
			continue
		}
		if generationID != "" && e.Fields["generation_id"] != generationID {
			continue
		}
		if ms := anyInt64(e.RawFields["duration_ms"]); ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 0
}

func androidRetentionSettingsFromGeneration(raw map[string]int) (db.AndroidRetentionSettings, bool) {
	if raw == nil {
		return db.AndroidRetentionSettings{}, false
	}
	storyHours := 48
	if v, ok := raw["story_hours"]; ok {
		storyHours = db.NormalizeStoriesWindowHours(v)
	}
	return db.AndroidRetentionSettings{
		FeedDays:    max(0, raw["feed_days"]),
		YoutubeDays: max(0, raw["youtube_days"]),
		MomentsDays: max(0, raw["moments_days"]),
		StoryHours:  storyHours,
	}, true
}

func androidGenerationFeedAssetCount(counts map[string]int) int {
	return counts["post_media"] + counts["post_thumbnail"]
}

func androidPercent(part, total int) int {
	if total <= 0 || part <= 0 {
		return 0
	}
	pct := part * 100 / total
	if pct > 100 {
		return 100
	}
	return pct
}

func formatAndroidBytes(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	case bytes > 0:
		return fmt.Sprintf("%d B", bytes)
	default:
		return ""
	}
}

func androidAssetHealthRows(report *db.AndroidSyncHealthReport, hasGeneration bool, readyAssets, totalAssets int) []components.AndroidCacheRow {
	if report == nil {
		if !hasGeneration || totalAssets <= 0 {
			return nil
		}
		return []components.AndroidCacheRow{
			androidHealthRow("Server ready", readyAssets, totalAssets, "an-cache-bar-good"),
			androidHealthRow("Server missing", max(0, totalAssets-readyAssets), totalAssets, "an-cache-bar-bad"),
		}
	}
	total := report.TotalAssets
	if total <= 0 {
		return nil
	}
	return []components.AndroidCacheRow{
		androidHealthRow("Verified", report.VerifiedAssets, total, "an-cache-bar-good"),
		androidHealthRow("Pending", report.PendingAssets, total, "an-cache-bar-ok"),
		androidHealthRow("Failed", report.FailedAssets, total, "an-cache-bar-bad"),
		androidHealthRow("Server missing", report.MissingAssets, total, "an-cache-bar-bad"),
	}
}

func androidHealthRow(label string, count, total int, barCSS string) components.AndroidCacheRow {
	return components.AndroidCacheRow{
		Label:   label,
		Cached:  count,
		Total:   total,
		Percent: androidPercent(count, total),
		BarCSS:  barCSS,
	}
}
