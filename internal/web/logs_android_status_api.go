package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/components"
	"github.com/screwys/igloo/internal/db"
)

func (s *Server) handleAndroidStatus(w http.ResponseWriter, r *http.Request) {
	android.mu.Lock()
	buf := make([]androidLogEvent, len(android.eventBuffer))
	copy(buf, android.eventBuffer)
	roomResult := android.roomResult
	pending := android.forceSyncFlag
	android.mu.Unlock()

	// Bootstrap from log file if buffer is empty
	if len(buf) == 0 {
		path := filepath.Join(s.cfg.DataDir, "logs", "android", "android.log")
		if lines, err := readLastLines(path, 200); err == nil {
			for _, line := range lines {
				buf = append(buf, parseLogLine(line))
			}
		}
	}

	android.mu.Lock()
	cacheHealth := android.cacheHealth
	if cacheHealth == nil {
		cacheHealth = loadCacheHealthFromDisk(s.cfg.DataDir)
		android.cacheHealth = cacheHealth
	}
	android.mu.Unlock()

	clientEntries := readAndroidClientLogEntries(s.cfg.DataDir, 500)
	if len(clientEntries) > 0 {
		buf = structuredEventsToLogEvents(clientEntries)
	}

	latestGeneration, genErr := s.db.GetLatestAndroidSyncGeneration()
	if genErr != nil {
		slog.Warn("android dashboard latest generation failed", "err", genErr)
	}
	latestHealth, healthErr := s.db.GetLatestAndroidSyncHealthReport()
	if healthErr != nil {
		slog.Warn("android dashboard latest health failed", "err", healthErr)
	}
	latestGenerationID := ""
	if latestGeneration != nil {
		latestGenerationID = latestGeneration.GenerationID
	}
	if latestHealth != nil && latestHealth.GenerationID != "" {
		latestGenerationID = latestHealth.GenerationID
	}
	healthReportedAtMs := int64(0)
	if latestHealth != nil {
		healthReportedAtMs = latestHealth.ReportedAtMs
	}

	lastSync, syncSteps := parseSyncCycle(buf)
	structuredSync := parseStructuredSync(clientEntries, cacheHealth, latestGenerationID, latestGeneration != nil, healthReportedAtMs)
	if structuredSync.Available {
		syncSteps = structuredSync.Steps
	}
	if !structuredSync.Started.IsZero() {
		lastSync = map[string]any{
			"started": structuredSync.Started.Format(time.RFC3339),
		}
		if structuredSync.CompletedAt {
			lastSync["completed"] = structuredSync.Completed.Format(time.RFC3339)
			lastSync["ago"] = formatAgo(time.Since(structuredSync.Completed).Seconds())
			lastSync["elapsed"] = formatDuration(structuredSync.Completed.Sub(structuredSync.Started))
		}
	}

	// Compute elapsed/ago for lastSync
	if lastSync != nil {
		if completedStr, ok := lastSync["completed"].(string); ok && completedStr != "" {
			if completedT, err := parseAndroidTimestamp(completedStr); err == nil {
				if startedStr, ok := lastSync["started"].(string); ok && startedStr != "" {
					if startedT, err := parseAndroidTimestamp(startedStr); err == nil {
						lastSync["elapsed"] = fmt.Sprintf("%.1fs", completedT.Sub(startedT).Seconds())
					}
				}
				lastSync["ago"] = formatAgo(time.Since(completedT).Seconds())
			}
		}
		if _, ok := lastSync["elapsed"]; !ok {
			lastSync["elapsed"] = "?"
		}
		if _, ok := lastSync["ago"]; !ok {
			lastSync["ago"] = "?"
		}
	}
	if latestHealth != nil {
		completed := time.UnixMilli(latestHealth.ReportedAtMs)
		lastSync = map[string]any{
			"completed": completed.Format(time.RFC3339),
			"ago":       formatAgo(time.Since(completed).Seconds()),
		}
		if structuredSync.CompletedAt && !structuredSync.Started.IsZero() {
			lastSync["started"] = structuredSync.Started.Format(time.RFC3339)
			lastSync["elapsed"] = formatDuration(structuredSync.Completed.Sub(structuredSync.Started))
		} else if duration := androidSyncGenerationDuration(clientEntries, latestHealth.GenerationID); duration > 0 {
			lastSync["elapsed"] = formatDuration(duration)
		}
	} else if lastSync == nil && latestGeneration != nil {
		generated := time.UnixMilli(latestGeneration.CreatedAtMs)
		lastSync = map[string]any{
			"completed": generated.Format(time.RFC3339),
			"ago":       "generation ready",
		}
	}

	user := userFromContext(r.Context())
	username := ""
	if user != nil {
		username = user.Username
	}
	retention := cacheHealthSettings(cacheHealth)
	if latestGeneration != nil {
		if genRetention, ok := androidRetentionSettingsFromGeneration(latestGeneration.Retention); ok {
			retention = genRetention
		}
	}
	if latestHealth != nil && latestHealth.HasRetention {
		retention = latestHealth.Retention
	}
	var expectations db.AndroidDashboardExpectations
	var expErr error
	if latestGeneration == nil {
		expectations, expErr = s.db.GetAndroidDashboardExpectations(username, retention, time.Now().UnixMilli())
		if expErr != nil {
			slog.Warn("android dashboard expectations failed", "err", expErr)
		}
	}
	feedItemsTotal := 0
	feedItemsMedia := 0
	if latestGeneration != nil {
		feedItemsTotal = latestGeneration.ContentCounts["feed_items"]
		feedItemsMedia = androidGenerationFeedAssetCount(latestGeneration.AssetCounts)
	} else {
		feedItemsTotal = expectations.FeedItems
		feedItemsMedia = expectations.FeedMedia
		if feedItemsTotal == 0 {
			feedItemsTotal = parseAndroidFeedItemCounts(buf)
		}
	}
	feedItems := map[string]any{"total": feedItemsTotal, "with_media": feedItemsMedia}

	stepsCompleted := 0
	for _, step := range syncSteps {
		if step.Status == "done" {
			stepsCompleted++
		}
	}

	// Activity: newest first, cap 50
	activityCap := min(50, len(buf))
	activity := make([]androidLogEvent, activityCap)
	for i := range activityCap {
		activity[i] = buf[len(buf)-1-i]
	}

	// Errors: deduplicated; warnings: up to 20
	type errorEntry struct {
		Tag       string `json:"tag"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
		FirstSeen string `json:"first_seen"`
		Count     int    `json:"count"`
	}
	type errKey struct{ tag, msg string }
	var errors []errorEntry
	errorKeys := make(map[errKey]int)
	var warnings []map[string]any
	warningCount := 0

	for i := len(buf) - 1; i >= 0; i-- {
		e := buf[i]
		switch strings.ToUpper(e.Level) {
		case "ERROR":
			msgKey := e.Message
			if len(msgKey) > 80 {
				msgKey = msgKey[:80]
			}
			k := errKey{e.Tag, msgKey}
			if idx, exists := errorKeys[k]; exists {
				errors[idx].Count++
			} else {
				errorKeys[k] = len(errors)
				errors = append(errors, errorEntry{
					Tag: e.Tag, Message: e.Message,
					Timestamp: e.Timestamp, FirstSeen: e.Timestamp, Count: 1,
				})
			}
		case "WARN", "WARNING":
			warningCount++
			if len(warnings) < 20 {
				warnings = append(warnings, map[string]any{
					"tag": e.Tag, "message": e.Message, "timestamp": e.Timestamp,
				})
			}
		}
	}

	if errors == nil {
		errors = []errorEntry{}
	}
	if warnings == nil {
		warnings = []map[string]any{}
	}

	if r.URL.Query().Get("fmt") == "html" {
		filter := r.URL.Query().Get("filter")
		if filter == "" {
			filter = "all"
		}
		generationItems, generationAssets, generationReady, generationMissing := 0, 0, 0, 0
		if latestGeneration != nil {
			generationItems = latestGeneration.ItemCount
			generationAssets = latestGeneration.AssetCount
			generationReady = latestGeneration.ReadyAssetCount
			generationMissing = latestGeneration.ServerMissingAssetCount
		}
		deviceVerified, devicePending, deviceFailed, deviceMissing, deviceTotal := 0, 0, 0, 0, 0
		deviceBytes := ""
		if latestHealth != nil {
			deviceVerified = latestHealth.VerifiedAssets
			devicePending = latestHealth.PendingAssets
			deviceFailed = latestHealth.FailedAssets
			deviceMissing = latestHealth.MissingAssets
			deviceTotal = latestHealth.TotalAssets
			deviceBytes = formatAndroidBytes(latestHealth.VerifiedBytes)
		} else if latestGeneration != nil {
			deviceTotal = latestGeneration.AssetCount
		}
		d := components.AndroidDashboardData{
			StepsCompleted:    stepsCompleted,
			StepsTotal:        len(syncSteps),
			FeedItemsTotal:    feedItemsTotal,
			FeedItemsMedia:    feedItemsMedia,
			GenerationItems:   generationItems,
			GenerationAssets:  generationAssets,
			GenerationReady:   generationReady,
			GenerationMissing: generationMissing,
			DevicePercent:     androidPercent(deviceVerified, deviceTotal),
			DeviceVerified:    deviceVerified,
			DevicePending:     devicePending,
			DeviceFailed:      deviceFailed,
			DeviceMissing:     deviceMissing,
			DeviceTotal:       deviceTotal,
			DeviceBytes:       deviceBytes,
			ErrorCount:        len(errors),
			WarningCount:      warningCount,
			LogFilter:         filter,
			ForceSyncPending:  pending,
		}
		if lastSync != nil {
			d.SyncAgo, _ = lastSync["ago"].(string)
			d.SyncDuration, _ = lastSync["elapsed"].(string)
			if completedStr, ok := lastSync["completed"].(string); ok && completedStr != "" {
				if t, err := parseAndroidTimestamp(completedStr); err == nil {
					d.SyncCompletedHMS = t.Local().Format("15:04:05")
				}
			}
			if startedStr, ok := lastSync["started"].(string); ok && startedStr != "" {
				startedHMS, endedHMS := "", ""
				if t, err := parseAndroidTimestamp(startedStr); err == nil {
					startedHMS = t.Local().Format("15:04:05")
				}
				if completedStr, ok := lastSync["completed"].(string); ok && completedStr != "" {
					if t, err := parseAndroidTimestamp(completedStr); err == nil {
						endedHMS = t.Local().Format("15:04:05")
					}
				}
				if startedHMS != "" && endedHMS != "" {
					d.PipelineFooter = "Sync started " + startedHMS + " \u00b7 completed " + endedHMS + " \u00b7 " + d.SyncDuration + " total"
				}
			}
		}
		if structuredSync.Footer != "" {
			d.PipelineFooter = structuredSync.Footer
		}
		for _, step := range syncSteps {
			ps := components.AndroidPipelineStep{Name: step.Name, Status: step.Status}
			if step.DurationMs != nil {
				if ms, ok := step.DurationMs.(int); ok {
					if ms >= 1000 {
						ps.Duration = fmt.Sprintf("%.1fs", float64(ms)/1000)
					} else {
						ps.Duration = fmt.Sprintf("%dms", ms)
					}
				} else if ms, ok := step.DurationMs.(float64); ok {
					if ms >= 1000 {
						ps.Duration = fmt.Sprintf("%.1fs", ms/1000)
					} else {
						ps.Duration = fmt.Sprintf("%.0fms", ms)
					}
				} else if ms, ok := step.DurationMs.(int64); ok {
					if ms >= 1000 {
						ps.Duration = fmt.Sprintf("%.1fs", float64(ms)/1000)
					} else {
						ps.Duration = fmt.Sprintf("%dms", ms)
					}
				}
			}
			d.Pipeline = append(d.Pipeline, ps)
		}
		d.CacheHealth = androidAssetHealthRows(latestHealth, latestGeneration != nil, generationReady, generationAssets)
		// Activity with dedup
		prevKey := ""
		for _, e := range activity {
			tsSec := e.Timestamp
			if len(tsSec) > 19 {
				tsSec = tsSec[:19]
			}
			key := tsSec + "|" + e.Tag + "|" + e.Message
			if key == prevKey {
				continue
			}
			prevKey = key
			levelCSS := "log-lvl-info"
			switch strings.ToUpper(e.Level) {
			case "ERROR":
				levelCSS = "log-lvl-err"
			case "WARN", "WARNING":
				levelCSS = "log-lvl-warn"
			}
			tsDisp := e.Timestamp
			if t, err := parseAndroidTimestamp(e.Timestamp); err == nil {
				tsDisp = t.Local().Format("15:04:05")
			}
			d.Activity = append(d.Activity, components.AndroidLogEntry{
				Timestamp: tsDisp, Tag: e.Tag, Message: e.Message, LevelCSS: levelCSS,
			})
		}
		for _, e := range errors {
			tsDisp := e.Timestamp
			if t, err := parseAndroidTimestamp(e.Timestamp); err == nil {
				tsDisp = t.Local().Format("15:04:05")
			}
			firstDisp := ""
			if e.FirstSeen != "" && e.FirstSeen != e.Timestamp {
				if t, err := parseAndroidTimestamp(e.FirstSeen); err == nil {
					firstDisp = t.Local().Format("15:04:05")
				}
			}
			d.Errors = append(d.Errors, components.AndroidErrorEntry{
				Tag: e.Tag, Message: e.Message, Timestamp: tsDisp, FirstSeen: firstDisp, Count: e.Count,
			})
		}
		for _, w := range warnings {
			tag, _ := w["tag"].(string)
			msg, _ := w["message"].(string)
			d.Warnings = append(d.Warnings, components.AndroidWarningEntry{Tag: tag, Message: msg})
		}
		if roomResult != nil {
			rr := components.AndroidRoomResult{}
			if m, ok := roomResult.(map[string]any); ok {
				if s, ok := m["error"].(string); ok {
					rr.Error = s
				}
				if s, ok := m["query"].(string); ok {
					rr.Query = s
				}
				if n, ok := m["row_count"].(int); ok {
					rr.RowCount = n
				} else if n, ok := m["row_count"].(float64); ok {
					rr.RowCount = int(n)
				}
				if cols, ok := m["columns"].([]string); ok {
					rr.Columns = cols
				} else if colsAny, ok := m["columns"].([]any); ok {
					for _, c := range colsAny {
						if s, ok := c.(string); ok {
							rr.Columns = append(rr.Columns, s)
						}
					}
				}
				if rows, ok := m["rows"].([]any); ok {
					for _, row := range rows {
						if ra, ok := row.([]any); ok {
							var rowStr []string
							for _, v := range ra {
								if v == nil {
									rowStr = append(rowStr, "")
								} else {
									rowStr = append(rowStr, fmt.Sprintf("%v", v))
								}
							}
							rr.Rows = append(rr.Rows, rowStr)
						}
					}
				}
			}
			d.RoomQuery = &rr
		}
		w.Header().Set("Content-Type", "text/html")
		_ = components.AndroidDashboard(s.pageProps(w, r), d).Render(r.Context(), w)
		return
	}

	writeJSON(w, 200, map[string]any{
		"last_sync":          lastSync,
		"sync_steps":         syncSteps,
		"feed_items":         feedItems,
		"steps_completed":    stepsCompleted,
		"steps_total":        len(syncSteps),
		"activity":           activity,
		"errors":             errors,
		"warnings":           warnings,
		"error_count":        len(errors),
		"warning_count":      warningCount,
		"force_sync_pending": pending,
		"cache_health":       cacheHealth,
		"cache_expected": map[string]any{
			"videos":  expectations.Videos,
			"moments": expectations.Moments,
			"feed":    expectations.FeedMedia,
			"avatars": expectations.Avatars,
		},
		"room_query": roomResult,
	})
}

func (s *Server) handleAndroidForceSync(w http.ResponseWriter, r *http.Request) {
	android.mu.Lock()
	android.forceSyncFlag = true
	android.mu.Unlock()
	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) handleAndroidForceSyncCheck(w http.ResponseWriter, r *http.Request) {
	android.mu.Lock()
	pending := android.forceSyncFlag
	android.forceSyncFlag = false
	android.mu.Unlock()
	writeJSON(w, 200, map[string]any{"force_sync": pending})
}

func (s *Server) handleAndroidFetch(w http.ResponseWriter, r *http.Request) {
	android.mu.Lock()
	android.fetchRequested = true
	android.mu.Unlock()
	writeJSON(w, 200, map[string]any{"success": true})
}
