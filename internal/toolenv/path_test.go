package toolenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAugmentPATHPrefersUserPackageManagerDirs(t *testing.T) {
	home := filepath.Join(string(os.PathSeparator), "home", "me")
	brew := filepath.Join(string(os.PathSeparator), "brew")
	exists := func(path string) bool {
		switch path {
		case filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".deno", "bin"),
			filepath.Join(brew, "bin"),
			filepath.Join(brew, "sbin"),
			"/usr/bin",
			"/bin":
			return true
		default:
			return false
		}
	}

	got := AugmentPATH("/usr/bin:/bin", home, brew, exists)
	parts := strings.Split(got, string(os.PathListSeparator))
	wantPrefix := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".deno", "bin"),
		filepath.Join(brew, "bin"),
		filepath.Join(brew, "sbin"),
	}
	for i, want := range wantPrefix {
		if parts[i] != want {
			t.Fatalf("PATH part %d = %q, want %q (full PATH %q)", i, parts[i], want, got)
		}
	}
}

func TestAugmentPATHDeduplicatesExistingDirs(t *testing.T) {
	home := filepath.Join(string(os.PathSeparator), "home", "me")
	localBin := filepath.Join(home, ".local", "bin")
	exists := func(path string) bool {
		return path == localBin || path == "/usr/bin"
	}

	got := AugmentPATH(localBin+":/usr/bin:"+localBin, home, "", exists)
	if count := strings.Count(got, localBin); count != 1 {
		t.Fatalf("local bin count = %d, want 1 in %q", count, got)
	}
	if got != localBin+":/usr/bin" {
		t.Fatalf("PATH = %q, want %q", got, localBin+":/usr/bin")
	}
}
