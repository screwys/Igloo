// Package persistenceaudit reports database storage usage and budgets.
package persistenceaudit

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/screwys/igloo/internal/config"
	igloodb "github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/persistencebudget"
)

type options struct {
	DBPath string
	JSON   bool
	Top    int
}

type Report struct {
	DBPath       string                      `json:"db_path"`
	DBBytes      int64                       `json:"db_bytes"`
	WALBytes     int64                       `json:"wal_bytes,omitempty"`
	WALAvailable bool                        `json:"wal_available"`
	WALScope     string                      `json:"wal_scope"`
	BlockSize    int64                       `json:"block_size"`
	Groups       []LifecycleGroup            `json:"groups"`
	Warnings     []persistencebudget.Warning `json:"warnings,omitempty"`
	Unclassified []TableReport               `json:"unclassified,omitempty"`
}

type LifecycleGroup struct {
	Lifecycle string        `json:"lifecycle"`
	Tables    int           `json:"tables"`
	Rows      int64         `json:"rows"`
	Bytes     int64         `json:"bytes"`
	TopTables []TableReport `json:"top_tables"`
}

type TableReport struct {
	Name      string `json:"name"`
	Lifecycle string `json:"lifecycle"`
	Rows      int64  `json:"rows"`
	Bytes     int64  `json:"bytes"`
}

func parseOptions(args []string) (options, error) {
	opts := options{Top: 5}
	fs := flag.NewFlagSet("persistence-audit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DBPath, "db", "", "Igloo state directory; defaults to configured state directory")
	fs.BoolVar(&opts.JSON, "json", false, "print JSON output")
	fs.IntVar(&opts.Top, "top", opts.Top, "number of top tables to print per lifecycle")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if opts.Top < 0 {
		return options{}, fmt.Errorf("top must be non-negative")
	}
	return opts, nil
}

func Run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseOptions(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "persistence audit: %v\n", err)
		return 2
	}

	dbPath := strings.TrimSpace(opts.DBPath)
	if dbPath == "" {
		cfg := config.Load()
		if cfg.ConfigError != nil {
			_, _ = fmt.Fprintf(stderr, "persistence audit: invalid configuration: %v\n", cfg.ConfigError)
			return 1
		}
		dbPath = cfg.Storage.StateRoot()
	}

	report, err := ReadReport(dbPath, opts.Top)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "persistence audit: %v\n", err)
		return 1
	}
	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "persistence audit: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	_, _ = fmt.Fprint(stdout, formatText(report))
	return 0
}

func ReadReport(stateRoot string, top int) (Report, error) {
	stateRoot = filepath.Clean(stateRoot)
	store, err := igloodb.OpenReadOnlyAtStateRoot(stateRoot)
	if err != nil {
		return Report{}, fmt.Errorf("open readonly db: %w", err)
	}
	defer func() { _ = store.Close() }()
	storage, err := store.DatabaseStorage()
	if err != nil {
		return Report{}, err
	}
	report := Report{DBPath: stateRoot, DBBytes: storage.DatabaseBytes, BlockSize: storage.BlockSize, WALScope: "cluster", WALAvailable: storage.ClusterWALBytes != nil}
	if storage.ClusterWALBytes != nil {
		report.WALBytes = *storage.ClusterWALBytes
	}
	if err := store.WithRead(func(conn *sql.DB) error {
		tables, err := userTables(conn)
		if err != nil {
			return err
		}
		bytesByTable, err := tableStorageBytes(conn)
		if err != nil {
			return err
		}
		report.Groups, report.Unclassified, err = lifecycleGroups(conn, tables, bytesByTable, top)
		return err
	}); err != nil {
		return Report{}, err
	}
	report.Warnings = persistencebudget.Evaluate(budgetGroups(report.Groups))
	return report, nil
}

func budgetGroups(groups []LifecycleGroup) []persistencebudget.LifecycleGroup {
	out := make([]persistencebudget.LifecycleGroup, 0, len(groups))
	for _, group := range groups {
		out = append(out, persistencebudget.LifecycleGroup{
			Lifecycle: group.Lifecycle,
			Tables:    group.Tables,
			Rows:      group.Rows,
			Bytes:     group.Bytes,
		})
	}
	return out
}

func lifecycleGroups(conn *sql.DB, tables []string, bytesByTable map[string]int64, top int) ([]LifecycleGroup, []TableReport, error) {
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
	byLifecycle := make(map[string][]TableReport, len(order))
	for _, table := range tables {
		lifecycle, ok := igloodb.SchemaTableLifecycle(table)
		if !ok {
			lifecycle = "unclassified"
		}
		count, err := tableRowCount(conn, table)
		if err != nil {
			return nil, nil, err
		}
		byLifecycle[lifecycle] = append(byLifecycle[lifecycle], TableReport{
			Name:      table,
			Lifecycle: lifecycle,
			Rows:      count,
			Bytes:     bytesByTable[table],
		})
	}

	var groups []LifecycleGroup
	var unclassified []TableReport
	for _, lifecycle := range order {
		reports := byLifecycle[lifecycle]
		if len(reports) == 0 {
			continue
		}
		sortTables(reports)
		group := LifecycleGroup{
			Lifecycle: lifecycle,
			Tables:    len(reports),
			TopTables: topTables(reports, top),
		}
		for _, table := range reports {
			group.Rows += table.Rows
			group.Bytes += table.Bytes
		}
		groups = append(groups, group)
		if lifecycle == "unclassified" {
			unclassified = append(unclassified, reports...)
		}
	}
	return groups, unclassified, nil
}

func userTables(conn *sql.DB) ([]string, error) {
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

func tableStorageBytes(conn *sql.DB) (map[string]int64, error) {
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

func tableRowCount(conn *sql.DB, table string) (int64, error) {
	var count int64
	query := fmt.Sprintf(`SELECT COUNT(*) FROM public.%s`, quoteSQLIdentifier(table))
	if err := conn.QueryRow(query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count table %s: %w", table, err)
	}
	return count, nil
}

func quoteSQLIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func topTables(tables []TableReport, limit int) []TableReport {
	if limit > len(tables) {
		limit = len(tables)
	}
	if limit <= 0 {
		return nil
	}
	out := make([]TableReport, limit)
	copy(out, tables[:limit])
	return out
}

func sortTables(tables []TableReport) {
	sort.Slice(tables, func(i, j int) bool {
		if tables[i].Bytes != tables[j].Bytes {
			return tables[i].Bytes > tables[j].Bytes
		}
		if tables[i].Rows != tables[j].Rows {
			return tables[i].Rows > tables[j].Rows
		}
		return tables[i].Name < tables[j].Name
	})
}

func formatText(report Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "db: %s\n", report.DBPath)
	fmt.Fprintf(&b, "db_size: %s\n", formatSize(report.DBBytes))
	if report.WALAvailable {
		fmt.Fprintf(&b, "cluster_wal_size: %s\n", formatSize(report.WALBytes))
	} else {
		b.WriteString("cluster_wal_size: unavailable\n")
	}
	fmt.Fprintf(&b, "block_size: %s\n", formatSize(report.BlockSize))
	b.WriteString("lifecycles:\n")
	for _, group := range report.Groups {
		fmt.Fprintf(&b, "  %-18s tables=%d rows=%d size=%s\n", group.Lifecycle+":", group.Tables, group.Rows, formatSize(group.Bytes))
		for _, table := range group.TopTables {
			fmt.Fprintf(&b, "    %-30s rows=%d size=%s\n", table.Name, table.Rows, formatSize(table.Bytes))
		}
	}
	if len(report.Warnings) > 0 {
		b.WriteString("warnings:\n")
		for _, warning := range report.Warnings {
			fmt.Fprintf(&b, "  - %s %s/%s: %s\n", warning.Severity, warning.Lifecycle, warning.Code, warning.Message)
		}
	}
	return b.String()
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
