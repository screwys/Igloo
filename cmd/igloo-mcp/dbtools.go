package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	serverDB   *sql.DB
	serverDBMu sync.Mutex
)

func getDBPath() string {
	if d := os.Getenv("IGLOO_DATA_DIR"); d != "" {
		return filepath.Join(d, "igloo.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "igloo", "igloo.db")
}

func getServerDB() (*sql.DB, error) {
	serverDBMu.Lock()
	defer serverDBMu.Unlock()
	if serverDB != nil {
		if err := serverDB.Ping(); err == nil {
			return serverDB, nil
		}
		_ = serverDB.Close()
		serverDB = nil
	}
	dbPath := getDBPath()
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)", dbPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping %s: %w", dbPath, err)
	}
	serverDB = conn
	return serverDB, nil
}

// isSafeSQL checks that the query is read-only.
func isSafeSQL(q string) bool {
	trimmed := strings.TrimSpace(strings.ToUpper(q))
	if strings.HasPrefix(trimmed, "SELECT") || strings.HasPrefix(trimmed, "PRAGMA") ||
		strings.HasPrefix(trimmed, "EXPLAIN") || strings.HasPrefix(trimmed, "WITH") {
		return true
	}
	return false
}

const maxRows = 200

// serverQuery executes a read-only SQL query against the server DB.
func serverQuery(query string) (string, error) {
	if !isSafeSQL(query) {
		return "", fmt.Errorf("only SELECT, PRAGMA, EXPLAIN, and WITH queries are allowed")
	}
	conn, err := getServerDB()
	if err != nil {
		return "", err
	}
	rows, err := conn.Query(query)
	if err != nil {
		return "", fmt.Errorf("query: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	cols, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("columns: %w", err)
	}
	if len(cols) == 0 {
		return "Query returned no columns", nil
	}

	// Collect rows
	var allRows [][]string
	scanDest := make([]any, len(cols))
	scanPtrs := make([]any, len(cols))
	for i := range scanDest {
		scanPtrs[i] = &scanDest[i]
	}
	for rows.Next() && len(allRows) < maxRows {
		if err := rows.Scan(scanPtrs...); err != nil {
			return "", fmt.Errorf("scan: %w", err)
		}
		row := make([]string, len(cols))
		for i, v := range scanDest {
			if v == nil {
				row[i] = "NULL"
			} else {
				row[i] = fmt.Sprint(v)
			}
		}
		allRows = append(allRows, row)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate query: %w", err)
	}

	// Compute column widths
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = len(c)
	}
	for _, row := range allRows {
		for i, v := range row {
			if len(v) > widths[i] {
				widths[i] = len(v)
			}
			if widths[i] > 60 {
				widths[i] = 60
			}
		}
	}

	// Format output
	var sb strings.Builder
	// Header
	for i, c := range cols {
		if i > 0 {
			sb.WriteString(" | ")
		}
		fmt.Fprintf(&sb, "%-*s", widths[i], c)
	}
	sb.WriteByte('\n')
	// Separator
	for i := range cols {
		if i > 0 {
			sb.WriteString("-+-")
		}
		sb.WriteString(strings.Repeat("-", widths[i]))
	}
	sb.WriteByte('\n')
	// Rows
	for _, row := range allRows {
		for i, v := range row {
			if i > 0 {
				sb.WriteString(" | ")
			}
			display := v
			if len(display) > 60 {
				display = display[:57] + "..."
			}
			fmt.Fprintf(&sb, "%-*s", widths[i], display)
		}
		sb.WriteByte('\n')
	}

	truncated := ""
	if len(allRows) == maxRows {
		truncated = fmt.Sprintf("\n(limited to %d rows)", maxRows)
	}
	return fmt.Sprintf("%d rows%s\n%s", len(allRows), truncated, sb.String()), nil
}

// listDBTables returns all user tables with row counts.
func listDBTables() (string, error) {
	conn, err := getServerDB()
	if err != nil {
		return "", err
	}
	rows, err := conn.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = rows.Close()
	}()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", fmt.Errorf("scan table name: %w", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate tables: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%-30s %s\n", "Table", "Rows")
	sb.WriteString(strings.Repeat("-", 42) + "\n")
	for _, t := range tables {
		var count int
		_ = conn.QueryRow("SELECT COUNT(*) FROM `" + t + "`").Scan(&count)
		fmt.Fprintf(&sb, "%-30s %d\n", t, count)
	}
	fmt.Fprintf(&sb, "\nTotal: %d tables", len(tables))
	return sb.String(), nil
}

// dbSchema returns schema info for a specific table or all tables.
func dbSchema(tableName string) (string, error) {
	conn, err := getServerDB()
	if err != nil {
		return "", err
	}

	if tableName != "" {
		return singleTableSchema(conn, tableName)
	}

	// All tables
	rows, err := conn.Query(`SELECT name, sql FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = rows.Close()
	}()

	var sb strings.Builder
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			return "", fmt.Errorf("scan table schema: %w", err)
		}
		fmt.Fprintf(&sb, "%s\n\n", ddl)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate table schemas: %w", err)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

func singleTableSchema(conn *sql.DB, table string) (string, error) {
	// Get CREATE TABLE
	var ddl string
	err := conn.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl)
	if err != nil {
		return "", fmt.Errorf("table '%s' not found", table)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n\n", ddl)

	// Column info
	rows, err := conn.Query(fmt.Sprintf("PRAGMA table_info(`%s`)", table))
	if err == nil {
		defer func() {
			_ = rows.Close()
		}()
		fmt.Fprintf(&sb, "%-4s %-25s %-15s %-8s %-15s %s\n", "#", "Name", "Type", "NotNull", "Default", "PK")
		sb.WriteString(strings.Repeat("-", 75) + "\n")
		for rows.Next() {
			var cid int
			var name, typ string
			var notNull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
				return "", fmt.Errorf("scan columns for %s: %w", table, err)
			}
			def := ""
			if dflt.Valid {
				def = dflt.String
			}
			pkStr := ""
			if pk > 0 {
				pkStr = fmt.Sprintf("PK(%d)", pk)
			}
			nn := ""
			if notNull == 1 {
				nn = "NOT NULL"
			}
			fmt.Fprintf(&sb, "%-4d %-25s %-15s %-8s %-15s %s\n", cid, name, typ, nn, def, pkStr)
		}
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("iterate columns for %s: %w", table, err)
		}
	}

	// Indexes
	idxRows, err := conn.Query(fmt.Sprintf("PRAGMA index_list(`%s`)", table))
	if err == nil {
		defer func() {
			_ = idxRows.Close()
		}()
		var indexes []string
		for idxRows.Next() {
			var seq int
			var name, origin string
			var unique, partial int
			if err := idxRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
				return "", fmt.Errorf("scan indexes for %s: %w", table, err)
			}
			u := ""
			if unique == 1 {
				u = " UNIQUE"
			}
			indexes = append(indexes, fmt.Sprintf("  %s%s (%s)", name, u, origin))
		}
		if err := idxRows.Err(); err != nil {
			return "", fmt.Errorf("iterate indexes for %s: %w", table, err)
		}
		if len(indexes) > 0 {
			sb.WriteString("\nIndexes:\n")
			for _, idx := range indexes {
				sb.WriteString(idx + "\n")
			}
		}
	}

	// Row count
	var count int
	_ = conn.QueryRow("SELECT COUNT(*) FROM `" + table + "`").Scan(&count)
	fmt.Fprintf(&sb, "\nRow count: %d", count)

	// Sample (5 rows)
	if count > 0 {
		sample, err := conn.Query(fmt.Sprintf("SELECT * FROM `%s` LIMIT 5", table))
		if err == nil {
			defer func() {
				_ = sample.Close()
			}()
			cols, err := sample.Columns()
			if err != nil {
				return "", fmt.Errorf("sample columns for %s: %w", table, err)
			}
			sb.WriteString("\n\nSample (5 rows):\n")
			for i, c := range cols {
				if i > 0 {
					sb.WriteString(" | ")
				}
				sb.WriteString(c)
			}
			sb.WriteByte('\n')
			scanDest := make([]any, len(cols))
			scanPtrs := make([]any, len(cols))
			for i := range scanDest {
				scanPtrs[i] = &scanDest[i]
			}
			for sample.Next() {
				if err := sample.Scan(scanPtrs...); err != nil {
					return "", fmt.Errorf("scan sample for %s: %w", table, err)
				}
				for i, v := range scanDest {
					if i > 0 {
						sb.WriteString(" | ")
					}
					s := "NULL"
					if v != nil {
						s = fmt.Sprint(v)
						if len(s) > 50 {
							s = s[:47] + "..."
						}
					}
					sb.WriteString(s)
				}
				sb.WriteByte('\n')
			}
			if err := sample.Err(); err != nil {
				return "", fmt.Errorf("iterate sample for %s: %w", table, err)
			}
		}
	}

	return sb.String(), nil
}

// dbSummary returns a quick overview: table counts, recent data timestamps, queue states.
func dbSummary() (string, error) {
	conn, err := getServerDB()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("=== Server Database Summary ===\n\n")

	// Table row counts
	rows, err := conn.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = rows.Close()
	}()

	type tableInfo struct {
		name  string
		count int
	}
	var tables []tableInfo
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", fmt.Errorf("scan table name: %w", err)
		}
		var count int
		_ = conn.QueryRow("SELECT COUNT(*) FROM `" + name + "`").Scan(&count)
		tables = append(tables, tableInfo{name, count})
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate tables: %w", err)
	}

	sort.Slice(tables, func(i, j int) bool { return tables[i].count > tables[j].count })
	sb.WriteString("Tables by row count:\n")
	for _, t := range tables {
		fmt.Fprintf(&sb, "  %-30s %d\n", t.name, t.count)
	}

	// Queue statuses
	sb.WriteString("\nQueue statuses:\n")
	queueQueries := map[string]string{
		"media_objects":  `SELECT job_state, COUNT(*) FROM media_objects GROUP BY job_state ORDER BY COUNT(*) DESC`,
		"download_queue": `SELECT status, COUNT(*) FROM download_queue GROUP BY status ORDER BY COUNT(*) DESC`,
	}
	for table, q := range queueQueries {
		qrows, err := conn.Query(q)
		if err != nil {
			continue
		}
		var parts []string
		for qrows.Next() {
			var status string
			var count int
			if err := qrows.Scan(&status, &count); err != nil {
				_ = qrows.Close()
				return "", fmt.Errorf("scan queue statuses for %s: %w", table, err)
			}
			parts = append(parts, fmt.Sprintf("%s=%d", status, count))
		}
		err = qrows.Err()
		_ = qrows.Close()
		if err != nil {
			return "", fmt.Errorf("iterate queue statuses for %s: %w", table, err)
		}
		if len(parts) > 0 {
			fmt.Fprintf(&sb, "  %-20s %s\n", table+":", strings.Join(parts, ", "))
		}
	}

	// Recent activity timestamps
	sb.WriteString("\nRecent activity:\n")
	timestamps := []struct {
		label, query string
	}{
		{"Latest feed item", `SELECT published_at FROM feed_items ORDER BY published_at DESC LIMIT 1`},
		{"Latest video", `SELECT downloaded_at FROM videos ORDER BY downloaded_at DESC LIMIT 1`},
		{"Latest ingest", `SELECT datetime(last_success_at, 'unixepoch') FROM ingest_state ORDER BY last_success_at DESC LIMIT 1`},
		{"Latest asset update", `SELECT updated_at_ms FROM media_objects ORDER BY updated_at_ms DESC LIMIT 1`},
	}
	for _, ts := range timestamps {
		var val sql.NullString
		_ = conn.QueryRow(ts.query).Scan(&val)
		v := "none"
		if val.Valid && val.String != "" {
			v = val.String
		}
		fmt.Fprintf(&sb, "  %-25s %s\n", ts.label+":", v)
	}

	return sb.String(), nil
}
