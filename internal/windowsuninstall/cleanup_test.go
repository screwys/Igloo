package windowsuninstall

import (
	"os"
	"path/filepath"
	"testing"
)

func mark(t *testing.T, root, marker string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, marker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupKeepsAllFilesByDefault(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	mark(t, data, ".igloo-state-root")
	if err := Cleanup(KeepFiles, Roots{Data: data}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(data); err != nil {
		t.Fatalf("kept data missing: %v", err)
	}
}

func TestCleanupRemovesDataButPreservesNestedMedia(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	media := filepath.Join(data, "media")
	mark(t, data, ".igloo-state-root")
	mark(t, media, ".igloo-media-root")
	if err := os.WriteFile(filepath.Join(data, "igloo.db"), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, "video.mp4"), []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(RemoveData, Roots{Data: data, Media: media}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, "igloo.db")); !os.IsNotExist(err) {
		t.Fatalf("database survived removal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(media, "video.mp4")); err != nil {
		t.Fatalf("nested media was removed: %v", err)
	}
}

func TestCleanupRemoveEverythingRequiresEveryMarkerBeforeDeleting(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "data")
	media := filepath.Join(base, "media")
	config := filepath.Join(base, "config")
	mark(t, data, ".igloo-state-root")
	mark(t, media, ".igloo-media-root")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(RemoveEverything, Roots{Data: data, Media: media, Config: config}); err == nil {
		t.Fatal("cleanup accepted an unmarked config root")
	}
	if _, err := os.Stat(data); err != nil {
		t.Fatalf("preflight failure removed data: %v", err)
	}
}

func TestCleanupRemoveEverythingDeletesOwnedRoots(t *testing.T) {
	base := t.TempDir()
	roots := Roots{Data: filepath.Join(base, "data"), Media: filepath.Join(base, "media"), Config: filepath.Join(base, "config")}
	mark(t, roots.Data, ".igloo-state-root")
	mark(t, roots.Media, ".igloo-media-root")
	mark(t, roots.Config, ".igloo-config-root")
	if err := Cleanup(RemoveEverything, roots); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{roots.Data, roots.Media, roots.Config} {
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatalf("owned root %q survived: %v", root, err)
		}
	}
}
