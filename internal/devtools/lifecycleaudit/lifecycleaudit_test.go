package lifecycleaudit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadReportClassifiesDestructiveSQL(t *testing.T) {
	root := t.TempDir()
	writeLifecycleAuditFixture(t, root, "internal/db/sample.go", `
package db

func sample() {
	_ = `+"`DELETE FROM feed_items WHERE tweet_id = ?`"+`
	_ = `+"`DELETE FROM custom_table WHERE id = ?`"+`
	_ = `+"`DROP TABLE IF EXISTS temp.sample_work`"+`
}
`)
	writeLifecycleAuditFixture(t, root, "internal/db/sample_test.go", `
package db

func sampleTest() {
	_ = `+"`DELETE FROM ignored_test_fixture WHERE id = ?`"+`
}
`)

	report, err := ReadReport(Options{Root: root})
	if err != nil {
		t.Fatalf("ReadReport: %v", err)
	}
	if len(report.Operations) != 3 {
		t.Fatalf("operations = %+v, want 3 non-test destructive operations", report.Operations)
	}
	if report.Operations[0].Table != "feed_items" || report.Operations[0].Lifecycle != "archive" {
		t.Fatalf("first operation = %+v, want archive feed_items", report.Operations[0])
	}
	if report.Operations[2].Table != "temp.sample_work" || report.Operations[2].Lifecycle != "temporary" {
		t.Fatalf("temp operation = %+v, want temporary", report.Operations[2])
	}
	if len(report.Warnings) != 1 || report.Warnings[0].Table != "custom_table" {
		t.Fatalf("warnings = %+v, want custom_table warning", report.Warnings)
	}
	if report.Summary["archive"] != 1 || report.Summary["temporary"] != 1 || report.Summary["unclassified"] != 1 {
		t.Fatalf("summary = %+v, want archive/temporary/unclassified counts", report.Summary)
	}
}

func TestRunStrictFailsOnUnclassifiedDestructiveSQL(t *testing.T) {
	root := t.TempDir()
	writeLifecycleAuditFixture(t, root, "internal/db/sample.go", `
package db

func sample() {
	_ = `+"`DELETE FROM custom_table WHERE id = ?`"+`
}
`)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"-root", root, "-strict"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("Run exit = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "unclassified") || !strings.Contains(stdout.String(), "custom_table") {
		t.Fatalf("strict output missing warning context:\n%s", stdout.String())
	}
}

func writeLifecycleAuditFixture(t *testing.T, root, relPath, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", relPath, err)
	}
}
