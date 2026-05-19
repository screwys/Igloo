package restore

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/config"
	igloodb "github.com/screwys/igloo/internal/db"
)

func buildTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		hdr := &tar.Header{
			Name:     name,
			Size:     int64(len(body)),
			Mode:     0o644,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("write body %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	byteFiles := make(map[string][]byte, len(files))
	for name, body := range files {
		byteFiles[name] = []byte(body)
	}
	return buildZipBytes(t, byteFiles)
}

func buildZipBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestStageTarballRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	confDir := t.TempDir()

	tarBytes := buildTarball(t, map[string]string{
		"igloo.db":               "fake-db-bytes",
		"config/auth_users.json": `{"alpha":"hash"}`,
		"config/cookies/x.txt":   "cookie-contents",
	})

	if err := StageTarball(bytes.NewReader(tarBytes), dataDir); err != nil {
		t.Fatalf("StageTarball: %v", err)
	}
	if !HasPending(dataDir) {
		t.Fatal("HasPending returned false after staging")
	}

	cfg := &config.Config{
		DataDir:      dataDir,
		ConfDir:      confDir,
		DatabasePath: filepath.Join(dataDir, "igloo.db"),
	}

	if err := os.WriteFile(cfg.DatabasePath, []byte("old-db"), 0o644); err != nil {
		t.Fatalf("seed old db: %v", err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "untouched.txt"), []byte("keep-me"), 0o644); err != nil {
		t.Fatalf("seed untouched: %v", err)
	}

	if err := ApplyPending(cfg); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}

	dbBytes, err := os.ReadFile(cfg.DatabasePath)
	if err != nil {
		t.Fatalf("read restored db: %v", err)
	}
	if string(dbBytes) != "fake-db-bytes" {
		t.Errorf("db not restored: got %q", string(dbBytes))
	}

	bakBytes, err := os.ReadFile(cfg.DatabasePath + ".pre-restore.bak")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(bakBytes) != "old-db" {
		t.Errorf("pre-restore backup wrong: got %q", string(bakBytes))
	}

	authBytes, err := os.ReadFile(filepath.Join(confDir, "auth_users.json"))
	if err != nil {
		t.Fatalf("read auth_users.json: %v", err)
	}
	if string(authBytes) != `{"alpha":"hash"}` {
		t.Errorf("auth file not restored: got %q", string(authBytes))
	}

	cookieBytes, err := os.ReadFile(filepath.Join(confDir, "cookies", "x.txt"))
	if err != nil {
		t.Fatalf("read cookies/x.txt: %v", err)
	}
	if string(cookieBytes) != "cookie-contents" {
		t.Errorf("cookie file not restored: got %q", string(cookieBytes))
	}

	keepBytes, err := os.ReadFile(filepath.Join(confDir, "untouched.txt"))
	if err != nil {
		t.Fatalf("read untouched.txt: %v", err)
	}
	if string(keepBytes) != "keep-me" {
		t.Errorf("untouched file should be preserved: got %q", string(keepBytes))
	}

	if HasPending(dataDir) {
		t.Error("HasPending should be false after ApplyPending")
	}
	if _, err := os.Stat(filepath.Join(dataDir, stagingSubdir)); !os.IsNotExist(err) {
		t.Errorf("staging dir should be removed after ApplyPending")
	}
}

func TestStageZipRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	confDir := t.TempDir()

	zipBytes := buildZip(t, map[string]string{
		"igloo.db":           "fake-db-bytes",
		"config/config.json": `{"enabled_platforms":["youtube"]}`,
	})

	if err := StageZip(bytes.NewReader(zipBytes), int64(len(zipBytes)), dataDir); err != nil {
		t.Fatalf("StageZip: %v", err)
	}
	if !HasPending(dataDir) {
		t.Fatal("HasPending returned false after staging zip")
	}

	cfg := &config.Config{
		DataDir:      dataDir,
		ConfDir:      confDir,
		DatabasePath: filepath.Join(dataDir, "igloo.db"),
	}
	if err := os.WriteFile(cfg.DatabasePath, []byte("old-db"), 0o644); err != nil {
		t.Fatalf("seed old db: %v", err)
	}

	if err := ApplyPending(cfg); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
	dbBytes, err := os.ReadFile(cfg.DatabasePath)
	if err != nil {
		t.Fatalf("read restored db: %v", err)
	}
	if string(dbBytes) != "fake-db-bytes" {
		t.Errorf("db not restored: got %q", string(dbBytes))
	}
	cfgBytes, err := os.ReadFile(filepath.Join(confDir, "config.json"))
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if string(cfgBytes) != `{"enabled_platforms":["youtube"]}` {
		t.Errorf("config not restored: got %q", string(cfgBytes))
	}
}

func TestStageZipRestoresFullExportExtras(t *testing.T) {
	sourceDataDir := t.TempDir()
	sourceDBPath := filepath.Join(sourceDataDir, "igloo.db")
	sourceDB, err := igloodb.Open(sourceDBPath, sourceDataDir)
	if err != nil {
		t.Fatalf("open source db: %v", err)
	}
	if err := sourceDB.ExecRaw(`
		INSERT INTO videos (video_id, channel_id, title, file_path)
		VALUES ('sample_video', 'sample_channel', 'Sample Video', '/old/data/media/source/sample_video.mp4')
	`); err != nil {
		t.Fatalf("seed source db: %v", err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatalf("close source db: %v", err)
	}
	dbBytes, err := os.ReadFile(sourceDBPath)
	if err != nil {
		t.Fatalf("read source db: %v", err)
	}

	dataDir := t.TempDir()
	confDir := t.TempDir()
	repoDir := t.TempDir()
	zipBytes := buildZipBytes(t, map[string][]byte{
		"igloo.db":                             dbBytes,
		"runtime.json":                         []byte(`{"version":1,"data_dir":"/old/data","config_dir":"/old/config","repo_dir":"/old/repo"}`),
		"config/nginx.conf":                    []byte("pid /old/data/nginx.pid;\nssl_certificate /old/config/server.crt;\nroot /old/repo/static;\n"),
		"media/bookmarks/sample_video/000.mp4": []byte("sample-video-bytes"),
		"media/avatars/sample_channel.jpg":     []byte("avatar-bytes"),
	})

	if err := StageZip(bytes.NewReader(zipBytes), int64(len(zipBytes)), dataDir); err != nil {
		t.Fatalf("StageZip: %v", err)
	}
	cfg := &config.Config{
		DataDir:      dataDir,
		ConfDir:      confDir,
		RepoDir:      repoDir,
		DatabasePath: filepath.Join(dataDir, "igloo.db"),
	}
	if err := ApplyPending(cfg); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}

	nginxConf, err := os.ReadFile(filepath.Join(confDir, "nginx.conf"))
	if err != nil {
		t.Fatalf("read restored nginx.conf: %v", err)
	}
	nginxText := string(nginxConf)
	for _, want := range []string{
		filepath.Join(dataDir, "nginx.pid"),
		filepath.Join(confDir, "server.crt"),
		filepath.Join(repoDir, "static"),
	} {
		if !strings.Contains(nginxText, want) {
			t.Fatalf("restored nginx.conf missing rewritten path %q:\n%s", want, nginxText)
		}
	}

	mediaPath := filepath.Join(dataDir, "media", "imported", "bookmarks", "sample_video", "000.mp4")
	mediaBytes, err := os.ReadFile(mediaPath)
	if err != nil {
		t.Fatalf("read restored media: %v", err)
	}
	if string(mediaBytes) != "sample-video-bytes" {
		t.Fatalf("restored media = %q", string(mediaBytes))
	}
	avatarBytes, err := os.ReadFile(filepath.Join(dataDir, "thumbnails", "avatars", "sample_channel.jpg"))
	if err != nil {
		t.Fatalf("read restored avatar: %v", err)
	}
	if string(avatarBytes) != "avatar-bytes" {
		t.Fatalf("restored avatar = %q", string(avatarBytes))
	}

	restoredDB, err := igloodb.Open(cfg.DatabasePath, dataDir)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer func() {
		_ = restoredDB.Close()
	}()
	var videoPath, mediaRel string
	if err := restoredDB.QueryRow(`SELECT file_path FROM videos WHERE video_id = 'sample_video'`).Scan(&videoPath); err != nil {
		t.Fatalf("read restored video path: %v", err)
	}
	if err := restoredDB.QueryRow(`SELECT file_path FROM media_files WHERE owner_type = 'feed_media' AND owner_id = 'sample_video' AND media_index = 0`).Scan(&mediaRel); err != nil {
		t.Fatalf("read restored media_files row: %v", err)
	}
	wantRel := filepath.ToSlash(filepath.Join("media", "imported", "bookmarks", "sample_video", "000.mp4"))
	if mediaRel != wantRel {
		t.Fatalf("media_files file_path = %q, want %q", mediaRel, wantRel)
	}
	if videoPath != mediaPath {
		t.Fatalf("video file_path = %q, want %q", videoPath, mediaPath)
	}
}

func TestStageTarballRejectsMissingDB(t *testing.T) {
	dataDir := t.TempDir()
	tarBytes := buildTarball(t, map[string]string{
		"config/x.txt": "data",
	})
	if err := StageTarball(bytes.NewReader(tarBytes), dataDir); err == nil {
		t.Fatal("expected error for tarball without igloo.db")
	}
}

func TestStageZipMissingDBReturnsSentinelAndCleansStage(t *testing.T) {
	dataDir := t.TempDir()
	zipBytes := buildZip(t, map[string]string{
		"config/config.json": "{}",
	})
	err := StageZip(bytes.NewReader(zipBytes), int64(len(zipBytes)), dataDir)
	if !errors.Is(err, ErrMissingDatabase) {
		t.Fatalf("StageZip err = %v, want ErrMissingDatabase", err)
	}
	if HasPending(dataDir) {
		t.Fatal("missing-db zip should not leave pending marker")
	}
	if _, err := os.Stat(filepath.Join(dataDir, stagingSubdir)); !os.IsNotExist(err) {
		t.Fatalf("missing-db zip should clean staging dir, stat err=%v", err)
	}
}

func TestStageTarballRejectsUnsafePaths(t *testing.T) {
	dataDir := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "../escape", Size: 4, Mode: 0o644, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("evil")); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	if err := StageTarball(&buf, dataDir); err == nil {
		t.Fatal("expected error for tar entry escaping staging dir")
	}
}

func TestApplyPendingNoMarkerNoOp(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{
		DataDir:      dataDir,
		ConfDir:      t.TempDir(),
		DatabasePath: filepath.Join(dataDir, "igloo.db"),
	}
	if err := ApplyPending(cfg); err != nil {
		t.Fatalf("ApplyPending without marker: %v", err)
	}
}
