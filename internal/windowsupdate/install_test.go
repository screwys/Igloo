package windowsupdate

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractUpdateZipRejectsTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "update.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../outside.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("unsafe")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "payload")
	if err := extractUpdateZip(archive, destination); err == nil {
		t.Fatal("archive traversal was accepted")
	}
}

func TestExtractUpdateZipWritesRegularFiles(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "update.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("body{}")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "payload")
	if err := extractUpdateZip(archive, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "body{}" {
		t.Fatalf("extracted payload = %q", got)
	}
}
