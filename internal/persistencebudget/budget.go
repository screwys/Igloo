package persistencebudget

import "fmt"

const (
	DerivedCacheBytesWarning int64 = 512 << 20
	DerivedCacheRowsWarning  int64 = 1_000_000
	QueueBytesWarning        int64 = 256 << 20
	QueueRowsWarning         int64 = 100_000
	DiagnosticBytesWarning   int64 = 256 << 20
	DiagnosticRowsWarning    int64 = 500_000
)

type LifecycleGroup struct {
	Lifecycle string
	Tables    int
	Rows      int64
	Bytes     int64
}

type Warning struct {
	Severity  string `json:"severity"`
	Code      string `json:"code"`
	Lifecycle string `json:"lifecycle"`
	Message   string `json:"message"`
}

func Evaluate(groups []LifecycleGroup) []Warning {
	var warnings []Warning
	for _, group := range groups {
		switch group.Lifecycle {
		case "unclassified":
			if group.Tables > 0 {
				warnings = append(warnings, Warning{
					Severity:  "warn",
					Code:      "unclassified_tables",
					Lifecycle: group.Lifecycle,
					Message:   fmt.Sprintf("%d table(s) are missing lifecycle ownership", group.Tables),
				})
			}
		case "derived_cache":
			if group.Bytes > DerivedCacheBytesWarning {
				warnings = append(warnings, Warning{
					Severity:  "warn",
					Code:      "derived_cache_size",
					Lifecycle: group.Lifecycle,
					Message:   fmt.Sprintf("derived cache uses %s; verify bounded prune/rebuild behavior", formatBytes(group.Bytes)),
				})
			}
			if group.Rows > DerivedCacheRowsWarning {
				warnings = append(warnings, Warning{
					Severity:  "warn",
					Code:      "derived_cache_rows",
					Lifecycle: group.Lifecycle,
					Message:   fmt.Sprintf("derived cache has %d rows; verify generation/cache retention", group.Rows),
				})
			}
		case "queue":
			if group.Bytes > QueueBytesWarning {
				warnings = append(warnings, Warning{
					Severity:  "warn",
					Code:      "queue_size",
					Lifecycle: group.Lifecycle,
					Message:   fmt.Sprintf("queue tables use %s; inspect stuck or retrying work", formatBytes(group.Bytes)),
				})
			}
			if group.Rows > QueueRowsWarning {
				warnings = append(warnings, Warning{
					Severity:  "warn",
					Code:      "queue_rows",
					Lifecycle: group.Lifecycle,
					Message:   fmt.Sprintf("queue tables have %d rows; inspect stuck or retrying work", group.Rows),
				})
			}
		case "diagnostic":
			if group.Bytes > DiagnosticBytesWarning {
				warnings = append(warnings, Warning{
					Severity:  "warn",
					Code:      "diagnostic_size",
					Lifecycle: group.Lifecycle,
					Message:   fmt.Sprintf("diagnostic tables use %s; inspect retention", formatBytes(group.Bytes)),
				})
			}
			if group.Rows > DiagnosticRowsWarning {
				warnings = append(warnings, Warning{
					Severity:  "warn",
					Code:      "diagnostic_rows",
					Lifecycle: group.Lifecycle,
					Message:   fmt.Sprintf("diagnostic tables have %d rows; inspect retention", group.Rows),
				})
			}
		}
	}
	return warnings
}

func formatBytes(bytes int64) string {
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
