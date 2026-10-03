package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	igloodb "github.com/screwys/igloo/internal/db"
)

var (
	serverDB    *sql.DB
	serverStore *igloodb.DB
	serverDBMu  sync.Mutex
)

func getStateRoot() string {
	if d := os.Getenv("IGLOO_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "igloo")
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
	store, err := igloodb.OpenReadOnlyAtStateRoot(getStateRoot())
	if err != nil {
		return nil, fmt.Errorf("open server database: %w", err)
	}
	if err := store.WithRead(func(conn *sql.DB) error { serverDB = conn; return nil }); err != nil {
		_ = store.Close()
		return nil, err
	}
	serverStore = store
	return serverDB, nil
}

// isSafeSQL checks that the query is read-only.
func isSafeSQL(q string) bool {
	trimmed := strings.TrimSpace(strings.ToUpper(q))
	if strings.HasPrefix(trimmed, "SELECT") || strings.HasPrefix(trimmed, "SHOW") ||
		strings.HasPrefix(trimmed, "EXPLAIN") || strings.HasPrefix(trimmed, "WITH") {
		return true
	}
	return false
}

const maxRows = 200

// serverQuery executes a read-only SQL query against the server DB.
func serverQuery(query string) (string, error) {
	if !isSafeSQL(query) {
		return "", fmt.Errorf("only SELECT, SHOW, EXPLAIN, and WITH queries are allowed")
	}
	conn, err := getServerDB()
	if err != nil {
		return "", err
	}
	tx, err := conn.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", fmt.Errorf("begin read-only query: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.Query(query)
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
	rows, err := conn.Query(`SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname = 'public' ORDER BY tablename`)
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
		if err := conn.QueryRow("SELECT COUNT(*) FROM public." + quoteSQLIdentifier(t)).Scan(&count); err != nil {
			return "", fmt.Errorf("count table %s: %w", t, err)
		}
		fmt.Fprintf(&sb, "%-30s %d\n", t, count)
	}
	fmt.Fprintf(&sb, "\nTotal: %d tables", len(tables))
	return sb.String(), nil
}

// dbSchema returns table definitions, indexes and exact row counts.
func dbSchema(tableName string) (string, error) {
	conn, err := getServerDB()
	if err != nil {
		return "", err
	}
	if tableName != "" {
		return singleTableSchema(conn, tableName)
	}
	rows, err := conn.Query("SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname = 'public' ORDER BY tablename")
	if err != nil {
		return "", err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			_ = rows.Close()
			return "", err
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return "", err
	}
	if err := rows.Close(); err != nil {
		return "", err
	}
	var definitions []string
	for _, table := range tables {
		definition, err := singleTableSchema(conn, table)
		if err != nil {
			return "", err
		}
		definitions = append(definitions, definition)
	}
	return strings.Join(definitions, "\n\n"), nil
}

func singleTableSchema(conn *sql.DB, table string) (string, error) {
	rows, err := conn.Query(`
		SELECT a.attname, format_type(a.atttypid, a.atttypmod), a.attnotnull,
		       COALESCE(pg_get_expr(d.adbin, d.adrelid), ''), a.attidentity::text
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE n.nspname = 'public' AND c.relname = $1
		  AND c.relkind IN ('r','p') AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum
	`, table)
	if err != nil {
		return "", fmt.Errorf("columns for %s: %w", table, err)
	}
	var columns []string
	for rows.Next() {
		var name, typ, defaultValue, identity string
		var notNull bool
		if err := rows.Scan(&name, &typ, &notNull, &defaultValue, &identity); err != nil {
			_ = rows.Close()
			return "", err
		}
		definition := "  " + quoteSQLIdentifier(name) + " " + typ
		switch identity {
		case "a":
			definition += " GENERATED ALWAYS AS IDENTITY"
		case "d":
			definition += " GENERATED BY DEFAULT AS IDENTITY"
		}
		if defaultValue != "" {
			definition += " DEFAULT " + defaultValue
		}
		if notNull {
			definition += " NOT NULL"
		}
		columns = append(columns, definition)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return "", err
	}
	if err := rows.Close(); err != nil {
		return "", err
	}
	if len(columns) == 0 {
		return "", fmt.Errorf("table %q not found", table)
	}
	constraints, err := conn.Query(`
		SELECT con.conname, pg_get_constraintdef(con.oid)
		FROM pg_catalog.pg_constraint con
		JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname = $1
		ORDER BY con.conname
	`, table)
	if err != nil {
		return "", err
	}
	for constraints.Next() {
		var name, definition string
		if err := constraints.Scan(&name, &definition); err != nil {
			_ = constraints.Close()
			return "", err
		}
		columns = append(columns, "  CONSTRAINT "+quoteSQLIdentifier(name)+" "+definition)
	}
	if err := constraints.Err(); err != nil {
		_ = constraints.Close()
		return "", err
	}
	if err := constraints.Close(); err != nil {
		return "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "CREATE TABLE public.%s (\n%s\n);\n", quoteSQLIdentifier(table), strings.Join(columns, ",\n"))
	indexes, err := conn.Query("SELECT indexdef FROM pg_catalog.pg_indexes WHERE schemaname = 'public' AND tablename = $1 ORDER BY indexname", table)
	if err != nil {
		return "", err
	}
	for indexes.Next() {
		var definition string
		if err := indexes.Scan(&definition); err != nil {
			_ = indexes.Close()
			return "", err
		}
		fmt.Fprintf(&sb, "\n%s;\n", definition)
	}
	if err := indexes.Err(); err != nil {
		_ = indexes.Close()
		return "", err
	}
	if err := indexes.Close(); err != nil {
		return "", err
	}
	var count int64
	if err := conn.QueryRow("SELECT COUNT(*) FROM public." + quoteSQLIdentifier(table)).Scan(&count); err != nil {
		return "", fmt.Errorf("count table %s: %w", table, err)
	}
	fmt.Fprintf(&sb, "\nRow count: %d", count)
	return sb.String(), nil
}

func quoteSQLIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
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
	rows, err := conn.Query(`SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname = 'public' ORDER BY tablename`)
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
		if err := conn.QueryRow("SELECT COUNT(*) FROM public." + quoteSQLIdentifier(name)).Scan(&count); err != nil {
			return "", fmt.Errorf("count table %s: %w", name, err)
		}
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
			return "", fmt.Errorf("queue statuses for %s: %w", table, err)
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
		{"Latest ingest", `SELECT (to_timestamp(last_success_at) AT TIME ZONE 'UTC')::text FROM ingest_state ORDER BY last_success_at DESC LIMIT 1`},
		{"Latest asset update", `SELECT updated_at_ms FROM media_objects ORDER BY updated_at_ms DESC LIMIT 1`},
	}
	for _, ts := range timestamps {
		var val sql.NullString
		if err := conn.QueryRow(ts.query).Scan(&val); err != nil && err != sql.ErrNoRows {
			return "", fmt.Errorf("%s: %w", ts.label, err)
		}
		v := "none"
		if val.Valid && val.String != "" {
			v = val.String
		}
		fmt.Fprintf(&sb, "  %-25s %s\n", ts.label+":", v)
	}

	return sb.String(), nil
}
