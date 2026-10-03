package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	igloodb "github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/persistencebudget"
	"github.com/screwys/igloo/internal/storage"
)

func doctorStatus() (string, error) {
	conn, err := getServerDB()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("=== Igloo Doctor ===\n\n")
	writeDoctorStorageLayout(&sb)
	serverDBMu.Lock()
	store := serverStore
	serverDBMu.Unlock()
	writeDoctorDatabaseStorage(&sb, store)
	writeDoctorDBStat(&sb, conn)
	writeDoctorPersistenceLifecycle(&sb, conn)
	writeDoctorAndroidSync(&sb, conn)
	writeDoctorQueues(&sb, conn)
	writeDoctorProfileReadiness(&sb, conn)
	writeDoctorAssetInventory(&sb, conn)
	writeDoctorDownloaderFailures(&sb, conn)
	writeDoctorAndroidSyncClientFailures(&sb)
	writeDoctorRecentErrors(&sb)
	return strings.TrimRight(sb.String(), "\n"), nil
}

func writeDoctorStorageLayout(sb *strings.Builder) {
	stateRoot := getStateRoot()
	configuredMediaRoot := strings.TrimSpace(os.Getenv("IGLOO_MEDIA_DIR"))
	layout, err := storage.New(stateRoot, configuredMediaRoot)
	sb.WriteString("Storage layout:\n")
	if err != nil {
		fmt.Fprintf(sb, "  invalid: %v\n\n", err)
		return
	}
	mode := "co-located"
	if filepath.Clean(layout.MediaRoot()) != filepath.Join(filepath.Clean(layout.StateRoot()), "media") {
		mode = "external"
	}
	fmt.Fprintf(sb, "  state_root: %s\n", layout.StateRoot())
	fmt.Fprintf(sb, "  media_root: %s (%s)\n", layout.MediaRoot(), mode)
	if _, err := layout.Path("media/.doctor-readiness"); err != nil {
		fmt.Fprintf(sb, "  media_ready: false (%v)\n\n", err)
		return
	}
	sb.WriteString("  media_ready: true\n\n")
}

func writeDoctorDatabaseStorage(sb *strings.Builder, store *igloodb.DB) {
	sb.WriteString("PostgreSQL storage:\n")
	storage, err := store.DatabaseStorage()
	if err != nil {
		fmt.Fprintf(sb, "  unavailable: %v\n\n", err)
		return
	}
	fmt.Fprintf(sb, "  database_size: %s\n", formatSize(storage.DatabaseBytes))
	fmt.Fprintf(sb, "  block_size: %s\n", formatSize(storage.BlockSize))
	if storage.ClusterWALBytes == nil {
		sb.WriteString("  cluster_wal_size: unavailable\n\n")
	} else {
		fmt.Fprintf(sb, "  cluster_wal_size: %s\n\n", formatSize(*storage.ClusterWALBytes))
	}
}

func writeDoctorDBStat(sb *strings.Builder, conn *sql.DB) {
	sb.WriteString("Top PostgreSQL tables/indexes:\n")
	rows, err := conn.Query(`
		SELECT c.relname, pg_relation_size(c.oid) AS bytes
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'i', 'p')
		ORDER BY bytes DESC
		LIMIT 10
	`)
	if err != nil {
		fmt.Fprintf(sb, "  unavailable: %v\n\n", err)
		return
	}
	defer func() {
		_ = rows.Close()
	}()
	wrote := false
	for rows.Next() {
		var name string
		var bytes int64
		if err := rows.Scan(&name, &bytes); err != nil {
			fmt.Fprintf(sb, "  unavailable: %v\n\n", err)
			return
		}
		wrote = true
		fmt.Fprintf(sb, "  %-32s %s\n", name, formatSize(bytes))
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintf(sb, "  unavailable: %v\n\n", err)
		return
	}
	if !wrote {
		sb.WriteString("  none\n")
	}
	sb.WriteString("\n")
}

type doctorPersistenceTable struct {
	name  string
	rows  int64
	bytes int64
}

type doctorPersistenceLifecycle struct {
	name   string
	tables []doctorPersistenceTable
	rows   int64
	bytes  int64
}

func writeDoctorPersistenceLifecycle(sb *strings.Builder, conn *sql.DB) {
	sb.WriteString("Persistence lifecycle:\n")
	groups, err := doctorPersistenceLifecycles(conn)
	if err != nil {
		fmt.Fprintf(sb, "  unavailable: %v\n\n", err)
		return
	}
	for _, group := range groups {
		if len(group.tables) == 0 {
			continue
		}
		fmt.Fprintf(sb, "  %-18s tables=%d rows=%d size=%s\n", group.name+":", len(group.tables), group.rows, formatSize(group.bytes))
		sort.Slice(group.tables, func(i, j int) bool {
			if group.tables[i].bytes != group.tables[j].bytes {
				return group.tables[i].bytes > group.tables[j].bytes
			}
			if group.tables[i].rows != group.tables[j].rows {
				return group.tables[i].rows > group.tables[j].rows
			}
			return group.tables[i].name < group.tables[j].name
		})
		limit := len(group.tables)
		if limit > 3 {
			limit = 3
		}
		for _, table := range group.tables[:limit] {
			fmt.Fprintf(sb, "    %-30s rows=%d size=%s\n", table.name, table.rows, formatSize(table.bytes))
		}
	}
	warnings := persistencebudget.Evaluate(doctorBudgetGroups(groups))
	if len(warnings) > 0 {
		sb.WriteString("  warnings:\n")
		for _, warning := range warnings {
			fmt.Fprintf(sb, "    - %s %s/%s: %s\n", warning.Severity, warning.Lifecycle, warning.Code, warning.Message)
		}
	}
	sb.WriteString("\n")
}

func doctorBudgetGroups(groups []doctorPersistenceLifecycle) []persistencebudget.LifecycleGroup {
	out := make([]persistencebudget.LifecycleGroup, 0, len(groups))
	for _, group := range groups {
		out = append(out, persistencebudget.LifecycleGroup{
			Lifecycle: group.name,
			Tables:    len(group.tables),
			Rows:      group.rows,
			Bytes:     group.bytes,
		})
	}
	return out
}

func doctorPersistenceLifecycles(conn *sql.DB) ([]doctorPersistenceLifecycle, error) {
	tables, err := doctorUserTables(conn)
	if err != nil {
		return nil, err
	}
	bytesByTable, err := doctorTableStorageBytes(conn)
	if err != nil {
		return nil, err
	}

	order := []string{
		"archive",
		"maintained_state",
		"user_state",
		"queue",
		"derived_cache",
		"diagnostic",
		"security_state",
		"legacy_migration",
		"unclassified",
	}
	byLifecycle := make(map[string]*doctorPersistenceLifecycle, len(order))
	for _, name := range order {
		byLifecycle[name] = &doctorPersistenceLifecycle{name: name}
	}
	for _, table := range tables {
		lifecycle, ok := igloodb.SchemaTableLifecycle(table)
		if !ok {
			lifecycle = "unclassified"
		}
		group := byLifecycle[lifecycle]
		if group == nil {
			group = &doctorPersistenceLifecycle{name: lifecycle}
			byLifecycle[lifecycle] = group
			order = append(order, lifecycle)
		}
		rowCount, err := doctorTableRowCount(conn, table)
		if err != nil {
			return nil, err
		}
		bytes := bytesByTable[table]
		group.tables = append(group.tables, doctorPersistenceTable{
			name:  table,
			rows:  rowCount,
			bytes: bytes,
		})
		group.rows += rowCount
		group.bytes += bytes
	}

	groups := make([]doctorPersistenceLifecycle, 0, len(order))
	for _, name := range order {
		if group := byLifecycle[name]; group != nil {
			groups = append(groups, *group)
		}
	}
	return groups, nil
}

func doctorUserTables(conn *sql.DB) ([]string, error) {
	rows, err := conn.Query(`
		SELECT tablename
		FROM pg_catalog.pg_tables
		WHERE schemaname = 'public'
		ORDER BY tablename
	`)
	if err != nil {
		return nil, fmt.Errorf("query user tables: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, fmt.Errorf("scan user table: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user tables: %w", err)
	}
	return tables, nil
}

func doctorTableStorageBytes(conn *sql.DB) (map[string]int64, error) {
	rows, err := conn.Query(`
		SELECT c.relname, pg_total_relation_size(c.oid)
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
	`)
	if err != nil {
		return nil, fmt.Errorf("query table storage bytes: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make(map[string]int64)
	for rows.Next() {
		var table string
		var bytes int64
		if err := rows.Scan(&table, &bytes); err != nil {
			return nil, fmt.Errorf("scan table storage bytes: %w", err)
		}
		out[table] = bytes
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table storage bytes: %w", err)
	}
	return out, nil
}

func doctorTableRowCount(conn *sql.DB, table string) (int64, error) {
	var count int64
	query := fmt.Sprintf(`SELECT COUNT(*) FROM public.%s`, quoteSQLIdentifier(table))
	if err := conn.QueryRow(query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count table %s: %w", table, err)
	}
	return count, nil
}

func writeDoctorAndroidSync(sb *strings.Builder, conn *sql.DB) {
	sb.WriteString("Android sync:\n")
	var epoch string
	var revision, heads int64
	if err := conn.QueryRow(`
		SELECT epoch, revision, (SELECT COUNT(*) FROM android_sync_heads)
		FROM android_sync_clock WHERE id = 1
	`).Scan(&epoch, &revision, &heads); err != nil {
		fmt.Fprintf(sb, "  stream unavailable: %v\n\n", err)
		return
	}
	fmt.Fprintf(sb, "  stream: epoch=%s revision=%d compact_heads=%d\n", epoch, revision, heads)
	var total, ready, missing int64
	if err := conn.QueryRow(`
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE current.published_revision > 0 AND current.file_path != ''),
		       COUNT(*) FILTER (WHERE current.published_revision = 0 AND desired.job_state IN ('server_missing', 'permanent_missing'))
		FROM assets a
		JOIN media_objects current ON current.object_id = a.object_id
		JOIN media_objects desired ON desired.object_id = a.desired_object_id
		WHERE a.lifecycle_state != 'pruned'
	`).Scan(&total, &ready, &missing); err == nil {
		fmt.Fprintf(sb, "  canonical assets: total=%d ready=%d missing=%d\n", total, ready, missing)
	} else {
		fmt.Fprintf(sb, "  canonical assets unavailable: %v\n", err)
	}
	var cursor sql.NullString
	var reportedAt, verified, pending, deviceMissing sql.NullInt64
	if err := conn.QueryRow(`
		SELECT cursor, reported_at_ms, verified_assets, pending_assets, missing_assets
		FROM android_sync_health_reports
		ORDER BY reported_at_ms DESC, id DESC
		LIMIT 1
	`).Scan(&cursor, &reportedAt, &verified, &pending, &deviceMissing); err == nil && cursor.Valid {
		fmt.Fprintf(sb, "  device: cursor=%s reported=%s verified=%d pending=%d missing=%d\n",
			compactLong(cursor.String, 80), formatMillis(reportedAt.Int64), verified.Int64, pending.Int64, deviceMissing.Int64)
	}
	sb.WriteString("\n")
}

func writeDoctorQueues(sb *strings.Builder, conn *sql.DB) {
	sb.WriteString("Queue counts:\n")
	for _, table := range []string{"download_queue", "translation_jobs"} {
		parts, err := doctorStatusCounts(conn, table, "status", "")
		if err != nil {
			fmt.Fprintf(sb, "  %-18s unavailable: %v\n", table+":", err)
			continue
		}
		if len(parts) == 0 {
			parts = []string{"empty=0"}
		}
		fmt.Fprintf(sb, "  %-18s %s\n", table+":", strings.Join(parts, ", "))
	}
	parts, err := doctorAssetStatusCounts(conn, "")
	if err != nil {
		fmt.Fprintf(sb, "  %-18s unavailable: %v\n", "assets:", err)
	} else {
		if len(parts) == 0 {
			parts = []string{"empty=0"}
		}
		fmt.Fprintf(sb, "  %-18s %s\n", "assets:", strings.Join(parts, ", "))
	}
	sb.WriteString("\n")
}

func writeDoctorProfileReadiness(sb *strings.Builder, conn *sql.DB) {
	sb.WriteString("Profile/media readiness:\n")
	var profiles, tombstones int
	if err := conn.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN COALESCE(tombstone, 0) != 0 THEN 1 ELSE 0 END), 0)
		FROM channel_profiles
	`).Scan(&profiles, &tombstones); err != nil {
		fmt.Fprintf(sb, "  channel_profiles unavailable: %v\n", err)
	} else {
		fmt.Fprintf(sb, "  channel_profiles: total=%d tombstones=%d\n", profiles, tombstones)
	}
	var pendingJobs, leasedJobs, failedJobs int
	if err := conn.QueryRow(`
		SELECT
		  COALESCE(SUM(CASE WHEN completed_revision < requested_revision THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN lease_owner != '' THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN attempts > 0 AND last_error != '' THEN 1 ELSE 0 END), 0)
		FROM profile_jobs
	`).Scan(&pendingJobs, &leasedJobs, &failedJobs); err != nil {
		fmt.Fprintf(sb, "  profile_jobs unavailable: %v\n", err)
	} else {
		fmt.Fprintf(sb, "  profile_jobs: pending=%d leased=%d failed=%d\n", pendingJobs, leasedJobs, failedJobs)
	}

	for _, kind := range []string{"avatar", "banner"} {
		states, err := doctorAssetStatusCounts(conn, fmt.Sprintf("a.owner_kind = 'channel' AND a.asset_kind = '%s'", kind))
		if err != nil {
			fmt.Fprintf(sb, "  %s assets: unavailable: %v\n", kind, err)
			continue
		}
		if len(states) == 0 {
			states = []string{"empty=0"}
		}
		fmt.Fprintf(sb, "  %s assets: %s\n", kind, strings.Join(states, ", "))
	}
	sb.WriteString("\n")
}

func writeDoctorAssetInventory(sb *strings.Builder, conn *sql.DB) {
	sb.WriteString("Asset inventory:\n")
	parts, err := doctorAssetStatusCounts(conn, "")
	if err != nil {
		fmt.Fprintf(sb, "  inventory states: unavailable: %v\n", err)
	} else {
		if len(parts) == 0 {
			parts = []string{"empty=0"}
		}
		fmt.Fprintf(sb, "  inventory states: %s\n", strings.Join(parts, ", "))
	}
	activeLeases, expiredLeases, err := doctorAssetLeaseCounts(conn, time.Now().UnixMilli())
	if err != nil {
		fmt.Fprintf(sb, "  asset leases unavailable: %v\n", err)
	} else {
		fmt.Fprintf(sb, "  asset leases: active_downloading=%d expired_downloading=%d\n", activeLeases, expiredLeases)
	}
	for _, kind := range []string{
		"post_media", "post_audio", "video_stream", "post_thumbnail",
		"dearrow_thumbnail", "subtitle", "avatar", "banner",
		"preview_track_json", "preview_sprite",
	} {
		states, err := doctorAssetStatusCounts(conn, fmt.Sprintf("a.asset_kind = '%s'", kind))
		if err != nil {
			fmt.Fprintf(sb, "  %-20s unavailable: %v\n", kind+":", err)
			continue
		}
		if len(states) == 0 {
			states = []string{"empty=0"}
		}
		fmt.Fprintf(sb, "  %-20s %s\n", kind+":", strings.Join(states, ", "))
	}
	sb.WriteString("\n")
}

func doctorAssetLeaseCounts(conn *sql.DB, nowMs int64) (active int, expired int, err error) {
	err = conn.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN COALESCE(lease_until_ms, 0) > $1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN COALESCE(lease_until_ms, 0) > 0 AND lease_until_ms <= $2 THEN 1 ELSE 0 END), 0)
		FROM media_objects
		WHERE job_state = 'downloading'
	`, nowMs, nowMs).Scan(&active, &expired)
	return active, expired, err
}

func writeDoctorDownloaderFailures(sb *strings.Builder, conn *sql.DB) {
	sb.WriteString("Downloader failures:\n")
	parts, err := doctorStatusCounts(conn, "downloader_operations", "error_kind", "status IN ('failed', 'error')")
	if err != nil {
		fmt.Fprintf(sb, "  unavailable: %v\n\n", err)
		return
	}
	if len(parts) == 0 {
		sb.WriteString("  none\n\n")
		return
	}
	fmt.Fprintf(sb, "  %s\n\n", strings.Join(parts, ", "))
}

func writeDoctorAndroidSyncClientFailures(sb *strings.Builder) {
	sb.WriteString("Android sync client failures:\n")
	metadataRetries, err := doctorAndroidSyncMetadataRetryCounts(60)
	if err != nil {
		fmt.Fprintf(sb, "  unavailable: %v\n\n", err)
		return
	}
	if len(metadataRetries) == 0 {
		sb.WriteString("  none\n\n")
		return
	}
	fmt.Fprintf(sb, "  metadata_retry %s\n", strings.Join(metadataRetries, ", "))
	sb.WriteString("\n")
}

func doctorAndroidSyncMetadataRetryCounts(minutes int) ([]string, error) {
	logsDir := getLogsDir()
	cutoff := time.Duration(minutes*2) * time.Minute
	metadataCounts := map[string]int{}
	type androidLogLine struct {
		Event  string         `json:"event"`
		Fields map[string]any `json:"fields"`
	}
	err := filepath.WalkDir(logsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil //nolint:nilerr // Keep scanning available Android logs if a path cannot be read.
		}
		rel, _ := filepath.Rel(logsDir, path)
		if !strings.Contains(rel, "android") {
			return nil
		}
		info, err := d.Info()
		if err != nil || time.Since(info.ModTime()) > cutoff {
			return nil //nolint:nilerr // Logs can disappear during rotation.
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil //nolint:nilerr // Skip unavailable logs and scan the remaining files.
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var entry androidLogLine
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}
			if entry.Event == "android_sync_metadata_retry" {
				label := doctorLogField(entry.Fields, "label")
				if label == "" {
					label = "unknown"
				}
				metadataCounts[maskSensitive(label)]++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return doctorSortedCountParts(metadataCounts), nil
}

func doctorLogField(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	value, ok := fields[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func doctorSortedCountParts(counts map[string]int) []string {
	parts := make([]string, 0, len(counts))
	for key, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", key, count))
	}
	sort.Strings(parts)
	return parts
}

func writeDoctorRecentErrors(sb *strings.Builder) {
	sb.WriteString("Recent high-signal log errors:\n")
	errors, err := recentErrors(60, "")
	if err != nil {
		fmt.Fprintf(sb, "  unavailable: %v\n", err)
		return
	}
	for _, line := range strings.Split(maskSensitive(errors), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintf(sb, "  %s\n", line)
	}
}

func doctorStatusCounts(conn *sql.DB, table, groupColumn, where string) ([]string, error) {
	query := fmt.Sprintf("SELECT COALESCE(NULLIF(%s, ''), 'unknown'), COUNT(*) FROM %s", groupColumn, table)
	if where != "" {
		query += " WHERE " + where
	}
	query += " GROUP BY 1"
	rows, err := conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	var parts []string
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		parts = append(parts, fmt.Sprintf("%s=%d", key, count))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(parts)
	return parts, nil
}

func doctorAssetStatusCounts(conn *sql.DB, where string) ([]string, error) {
	query := `
		SELECT CASE WHEN a.lifecycle_state = 'pruned' THEN 'pruned'
		            WHEN current.published_revision > 0 AND current.file_path != '' THEN 'ready'
		            ELSE desired.job_state END AS state,
		       COUNT(*)
		FROM assets a
		JOIN media_objects current ON current.object_id = a.object_id
		JOIN media_objects desired ON desired.object_id = a.desired_object_id`
	if where != "" {
		query += " WHERE " + where
	}
	query += " GROUP BY state"
	rows, err := conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var parts []string
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, err
		}
		parts = append(parts, fmt.Sprintf("%s=%d", state, count))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(parts)
	return parts, nil
}

var sensitiveMaskers = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\b(cookie|token|secret|password|passphrase|authorization|api[_-]?key)=([^ \t\r\n,;]+)`), "$1=***"},
	{regexp.MustCompile(`(?im)\b(authorization)\s*:\s*[^\r\n]+`), "$1: ***"},
	{regexp.MustCompile(`(?im)\b(set-cookie|cookie)\s*:\s*[^\r\n]+`), "$1: ***"},
	{regexp.MustCompile(`(?im)\b([A-Za-z0-9_-]*(?:token|secret|password|passphrase|api[_-]?key)[A-Za-z0-9_-]*)\s*:\s*([^ \t\r\n,;]+)`), "$1: ***"},
}

func maskSensitive(s string) string {
	for _, masker := range sensitiveMaskers {
		s = masker.re.ReplaceAllString(s, masker.repl)
	}
	return s
}
