package windowsuninstall

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Mode int

const (
	KeepFiles Mode = iota
	RemoveData
	RemoveEverything
)

type Roots struct {
	Data   string
	Media  string
	Config string
}

type ownedRoot struct {
	path   string
	marker string
}

func Cleanup(mode Mode, roots Roots) error {
	if mode == KeepFiles {
		return nil
	}
	if mode != RemoveData && mode != RemoveEverything {
		return fmt.Errorf("unsupported uninstall mode %d", mode)
	}

	selected := []ownedRoot{{path: roots.Data, marker: ".igloo-state-root"}}
	if mode == RemoveEverything {
		selected = append(selected,
			ownedRoot{path: roots.Media, marker: ".igloo-media-root"},
			ownedRoot{path: roots.Config, marker: ".igloo-config-root"},
		)
	}
	for i := range selected {
		path, err := validateOwnedRoot(selected[i])
		if err != nil {
			return err
		}
		selected[i].path = path
	}

	if mode == RemoveData {
		kept := containedRoots(selected[0].path, roots.Media, roots.Config)
		return removeRootPreserving(selected[0].path, kept)
	}

	sort.Slice(selected, func(i, j int) bool { return len(selected[i].path) > len(selected[j].path) })
	removed := make(map[string]struct{}, len(selected))
	for _, root := range selected {
		key := strings.ToLower(root.path)
		if _, ok := removed[key]; ok {
			continue
		}
		if err := os.RemoveAll(root.path); err != nil {
			return fmt.Errorf("remove %q: %w", root.path, err)
		}
		removed[key] = struct{}{}
	}
	return nil
}

func validateOwnedRoot(root ownedRoot) (string, error) {
	if strings.TrimSpace(root.path) == "" || !filepath.IsAbs(root.path) {
		return "", fmt.Errorf("refusing to remove non-absolute root %q", root.path)
	}
	path := filepath.Clean(root.path)
	if path == filepath.VolumeName(path)+string(filepath.Separator) {
		return "", fmt.Errorf("refusing to remove volume root %q", path)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing to remove unavailable or redirected root %q", path)
	}
	marker := filepath.Join(path, root.marker)
	info, err = os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to remove unowned root %q: marker %q is missing", path, root.marker)
	}
	return path, nil
}

func containedRoots(root string, candidates ...string) []string {
	var kept []string
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		rel, err := filepath.Rel(root, candidate)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			kept = append(kept, candidate)
		}
	}
	return kept
}

func removeRootPreserving(root string, kept []string) error {
	if len(kept) == 0 {
		if err := os.RemoveAll(root); err != nil {
			return fmt.Errorf("remove %q: %w", root, err)
		}
		return nil
	}
	return removeChildrenPreserving(root, kept)
}

func removeChildrenPreserving(dir string, kept []string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %q: %w", dir, err)
	}
	for _, entry := range entries {
		child := filepath.Join(dir, entry.Name())
		var descendants []string
		keepChild := false
		for _, keep := range kept {
			if equalPath(child, keep) {
				keepChild = true
				break
			}
			rel, relErr := filepath.Rel(child, keep)
			if relErr == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				descendants = append(descendants, keep)
			}
		}
		if keepChild {
			continue
		}
		if len(descendants) > 0 && entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			if err := removeChildrenPreserving(child, descendants); err != nil {
				return err
			}
			continue
		}
		if err := os.RemoveAll(child); err != nil {
			return fmt.Errorf("remove %q: %w", child, err)
		}
	}
	return nil
}

func equalPath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
