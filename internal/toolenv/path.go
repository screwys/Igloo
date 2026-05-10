package toolenv

import (
	"os"
	"path/filepath"
	"strings"
)

// ApplyCommonToolPaths makes subprocess lookup work from systemd services as
// well as interactive shells. User services commonly start with a small PATH,
// while yt-dlp/gallery-dl/deno may have been installed by Homebrew, Go, pipx,
// or a user-local installer.
func ApplyCommonToolPaths() string {
	home, _ := os.UserHomeDir()
	path := AugmentPATH(os.Getenv("PATH"), home, os.Getenv("HOMEBREW_PREFIX"), dirExists)
	_ = os.Setenv("PATH", path)
	return path
}

func AugmentPATH(path, home, brewPrefix string, exists func(string) bool) string {
	if exists == nil {
		exists = dirExists
	}
	candidates := commonToolDirs(home, brewPrefix)
	seen := make(map[string]struct{}, len(candidates)+8)
	var parts []string
	for _, dir := range candidates {
		if dir == "" || !exists(dir) {
			continue
		}
		clean := filepath.Clean(dir)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		parts = append(parts, clean)
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		clean := filepath.Clean(dir)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		parts = append(parts, clean)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func commonToolDirs(home, brewPrefix string) []string {
	var dirs []string
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".deno", "bin"),
		)
	}
	if brewPrefix != "" {
		dirs = append(dirs,
			filepath.Join(brewPrefix, "bin"),
			filepath.Join(brewPrefix, "sbin"),
		)
	}
	dirs = append(dirs,
		"/home/linuxbrew/.linuxbrew/bin",
		"/home/linuxbrew/.linuxbrew/sbin",
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/usr/bin",
		"/bin",
	)
	return dirs
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
