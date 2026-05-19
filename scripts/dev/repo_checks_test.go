package dev

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDriftCheckUsesPinnedTemplGenerator(t *testing.T) {
	root := repoRoot(t)
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "bin")
	home := filepath.Join(tmp, "home")
	distDir := filepath.Join(root, "static/js/dist")
	distBackupRoot := filepath.Join(root, "tmp", "repo-checks", filepath.Base(tmp))
	distBackup := filepath.Join(distBackupRoot, "static-js-dist-backup")
	restoreDist := false
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(distBackupRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(distDir); err == nil {
		restoreDist = true
		if err := os.Rename(distDir, distBackup); err != nil {
			t.Fatalf("backup static/js/dist: %v", err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat static/js/dist: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(distDir); err != nil {
			t.Errorf("remove generated mock dist: %v", err)
		}
		if restoreDist {
			if err := os.MkdirAll(filepath.Dir(distDir), 0o755); err != nil {
				t.Errorf("recreate static/js parent: %v", err)
				return
			}
			if err := os.Rename(distBackup, distDir); err != nil {
				t.Errorf("restore static/js/dist: %v", err)
			}
		}
		if err := os.RemoveAll(distBackupRoot); err != nil {
			t.Errorf("remove dist backup dir: %v", err)
		}
	})

	writeExecutable(t, filepath.Join(bin, "templ"), `#!/usr/bin/env bash
echo "templ $*" >>"$DRIFT_TEST_LOG"
if [[ "${1:-}" == "version" ]]; then
  echo "v0.0.0"
  exit 0
fi
echo "drift-check used PATH templ instead of the pinned generator" >&2
exit 42
`)
	writeExecutable(t, filepath.Join(bin, "go"), `#!/usr/bin/env bash
echo "go $*" >>"$DRIFT_TEST_LOG"
case "$*" in
  "run github.com/a-h/templ/cmd/templ@v0.3.1020 generate") exit 0 ;;
  "run ./cmd/igloo-assets")
    mkdir -p static/js/dist
    for asset in feed.js feed.js.map shorts.js shorts.js.map player.js player.js.map; do
      printf 'mock bundle\n' >"static/js/dist/$asset"
    done
    exit 0
    ;;
esac
echo "unexpected go invocation: $*" >&2
exit 43
`)
	writeExecutable(t, filepath.Join(bin, "git"), `#!/usr/bin/env bash
echo "git $*" >>"$DRIFT_TEST_LOG"
if [[ "$*" == "diff --exit-code -- internal/components static/js static/css" ]]; then
  exit 0
fi
exec /usr/bin/git "$@"
`)

	logPath := filepath.Join(tmp, "drift.log")
	cmd := exec.Command(filepath.Join(root, "scripts/dev/drift-check.sh"))
	cmd.Dir = root
	cmd.Env = []string{
		"DRIFT_TEST_LOG=" + logPath,
		"HOME=" + home,
		"PATH=" + bin + ":/usr/bin:/bin",
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("drift-check.sh failed: %v\n%s", err, output)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logData)
	if strings.Contains(log, "\ntempl generate") || strings.HasPrefix(log, "templ generate") {
		t.Fatalf("drift-check executed templ from PATH:\n%s", log)
	}
	if !strings.Contains(log, "go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate\n") {
		t.Fatalf("drift-check did not invoke the pinned templ generator:\n%s", log)
	}
}

func TestGitHubActionsWorkflowDependenciesAreSHAPinned(t *testing.T) {
	workflowsDir := filepath.Join(repoRoot(t), ".github", "workflows")
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		t.Fatal(err)
	}

	usesPattern := regexp.MustCompile(`^\s*uses:\s+([^#\s]+)@([^#\s]+)`)
	shaPattern := regexp.MustCompile(`^[0-9a-f]{40}$`)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".yml" && filepath.Ext(name) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(workflowsDir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			match := usesPattern.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			if !shaPattern.MatchString(match[2]) {
				t.Errorf("%s has mutable action reference: %s@%s", name, match[1], match[2])
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(cwd, "../.."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return root
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
