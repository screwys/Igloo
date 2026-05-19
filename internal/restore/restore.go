// Package restore implements pending-restore staging and on-startup application
// for disaster-recovery archives (igloo.db + config dir, with optional
// full-export runtime/media extras) produced by the backup worker or manual
// Full Export. The flow is:
//
//  1. Import handler receives a backup archive upload, calls StageTarball() or StageZip() to
//     extract it into <DataDir>/restore-staging/ and write a marker file.
//  2. Process exits; systemd restarts igloo.
//  3. Startup calls ApplyPending() before opening the database. If the
//     marker exists, the staged db, config files, and supported media files
//     replace the live ones, and the staging directory is cleaned up.
package restore

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/screwys/igloo/internal/config"
	"github.com/screwys/igloo/internal/db"
)

const (
	stagingSubdir = "restore-staging"
	markerName    = ".pending-restore"
	configPrefix  = "config/"
	runtimeName   = "runtime.json"
)

var ErrMissingDatabase = errors.New("backup archive missing database")

func stagingDir(dataDir string) string { return filepath.Join(dataDir, stagingSubdir) }
func markerPath(dataDir string) string { return filepath.Join(stagingDir(dataDir), markerName) }

// HasPending reports whether a restore has been staged and is awaiting startup.
func HasPending(dataDir string) bool {
	_, err := os.Stat(markerPath(dataDir))
	return err == nil
}

// StageTarball extracts a legacy gzipped tar backup into the staging directory
// and writes the marker file.
func StageTarball(reader io.Reader, dataDir string) error {
	return stageBackup(dataDir, func(stage string) (bool, error) {
		return extractTarBackup(reader, stage)
	})
}

// StageZip extracts a DB-bearing backup or Full Export zip into the staging
// directory and writes the marker file. It returns ErrMissingDatabase when the
// zip is not a restore archive, allowing callers to fall through to legacy zip
// import formats.
func StageZip(readerAt io.ReaderAt, size int64, dataDir string) error {
	return stageBackup(dataDir, func(stage string) (bool, error) {
		return extractZipBackup(readerAt, size, stage)
	})
}

func stageBackup(dataDir string, extract func(stage string) (bool, error)) error {
	stage := stagingDir(dataDir)
	if err := os.RemoveAll(stage); err != nil {
		return fmt.Errorf("clear staging dir: %w", err)
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}

	dbSeen, err := extract(stage)
	if err != nil {
		return err
	}
	if !dbSeen {
		_ = os.RemoveAll(stage)
		return ErrMissingDatabase
	}

	if err := os.WriteFile(markerPath(dataDir), []byte(""), 0o644); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}
	return nil
}

func extractTarBackup(reader io.Reader, stage string) (bool, error) {
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return false, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() {
		_ = gz.Close()
	}()

	tr := tar.NewReader(gz)
	dbSeen := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, fmt.Errorf("tar next: %w", err)
		}

		clean, dbEntry, ok, err := backupArchiveEntry(hdr.Name)
		if err != nil {
			return false, err
		}
		if !ok {
			slog.Warn("restore: skipping unexpected tar entry", "name", filepath.Clean(hdr.Name))
			continue
		}

		dest := filepath.Join(stage, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return false, fmt.Errorf("mkdir %s: %w", dest, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return false, fmt.Errorf("mkdir parent of %s: %w", dest, err)
			}
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			if err := writeStream(dest, tr, mode); err != nil {
				return false, err
			}
			if dbEntry {
				dbSeen = true
			}
		}
	}

	return dbSeen, nil
}

func extractZipBackup(readerAt io.ReaderAt, size int64, stage string) (bool, error) {
	zr, err := zip.NewReader(readerAt, size)
	if err != nil {
		return false, fmt.Errorf("open zip: %w", err)
	}
	dbSeen := false
	for _, f := range zr.File {
		clean, dbEntry, ok, err := backupArchiveEntry(f.Name)
		if err != nil {
			return false, err
		}
		if !ok {
			slog.Warn("restore: skipping unexpected zip entry", "name", filepath.Clean(f.Name))
			continue
		}
		dest := filepath.Join(stage, clean)
		info := f.FileInfo()
		if info.IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return false, fmt.Errorf("mkdir %s: %w", dest, err)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return false, fmt.Errorf("mkdir parent of %s: %w", dest, err)
		}
		rc, err := f.Open()
		if err != nil {
			return false, fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		writeErr := writeStream(dest, rc, mode)
		closeErr := rc.Close()
		if writeErr != nil {
			return false, writeErr
		}
		if closeErr != nil {
			return false, closeErr
		}
		if dbEntry {
			dbSeen = true
		}
	}
	return dbSeen, nil
}

func backupArchiveEntry(name string) (clean string, dbEntry bool, ok bool, err error) {
	clean = filepath.Clean(name)
	if clean == "." || clean == "" {
		return "", false, false, nil
	}
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) || strings.Contains(clean, "..") {
		return "", false, false, fmt.Errorf("unsafe backup path: %s", name)
	}
	slash := filepath.ToSlash(clean)
	dbEntry = slash == config.DatabaseFilename
	if dbEntry || slash == runtimeName ||
		strings.HasPrefix(slash+"/", configPrefix) ||
		slash == strings.TrimSuffix(configPrefix, "/") {
		return clean, dbEntry, true, nil
	}
	if mapped, ok := restoreMediaArchiveEntry(slash); ok {
		return filepath.FromSlash(mapped), false, true, nil
	}
	return clean, false, false, nil
}

func restoreMediaArchiveEntry(clean string) (string, bool) {
	parts := strings.Split(clean, "/")
	if len(parts) == 3 && parts[0] == "media" && parts[1] == "avatars" {
		fileName := filepath.Base(parts[2])
		if safeArchiveLeaf(fileName) && avatarMediaName(fileName) {
			return filepath.ToSlash(filepath.Join("thumbnails", "avatars", fileName)), true
		}
		return "", false
	}
	if len(parts) == 4 && parts[0] == "media" && parts[1] == "bookmarks" {
		bookmarkID := strings.TrimSpace(parts[2])
		fileName := filepath.Base(parts[3])
		if safeArchiveLeaf(bookmarkID) && safeArchiveLeaf(fileName) {
			return filepath.ToSlash(filepath.Join("media", "imported", "bookmarks", bookmarkID, fileName)), true
		}
	}
	return "", false
}

func safeArchiveLeaf(name string) bool {
	return name != "" && name != "." && name != ".." &&
		name == filepath.Base(name) &&
		!strings.ContainsAny(name, `/\`)
}

func avatarMediaName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	default:
		return false
	}
}

// ApplyPending runs at startup before the database is opened. If a staged
// restore is present, it swaps the staged database and config files into
// place. The staging directory is removed regardless of success so the next
// boot does not loop on a broken restore.
func ApplyPending(cfg *config.Config) error {
	if !HasPending(cfg.DataDir) {
		return nil
	}
	stage := stagingDir(cfg.DataDir)
	defer func() {
		if err := os.RemoveAll(stage); err != nil {
			slog.Warn("restore: cleanup failed", "dir", stage, "err", err)
		}
	}()

	stagedDB := filepath.Join(stage, config.DatabaseFilename)
	if _, err := os.Stat(stagedDB); err != nil {
		return fmt.Errorf("staged db missing: %w", err)
	}

	slog.Info("restore: applying pending restore", "stage", stage)

	if _, err := os.Stat(cfg.DatabasePath); err == nil {
		bak := cfg.DatabasePath + ".pre-restore.bak"
		_ = os.Remove(bak)
		if err := os.Rename(cfg.DatabasePath, bak); err != nil {
			return fmt.Errorf("backup current db: %w", err)
		}
		// WAL/SHM siblings belong to the previous db file.
		_ = os.Remove(cfg.DatabasePath + "-wal")
		_ = os.Remove(cfg.DatabasePath + "-shm")
	}
	if err := os.Rename(stagedDB, cfg.DatabasePath); err != nil {
		return fmt.Errorf("install restored db: %w", err)
	}
	slog.Info("restore: database swapped", "path", cfg.DatabasePath)

	stagedConfig := filepath.Join(stage, "config")
	if fi, err := os.Stat(stagedConfig); err == nil && fi.IsDir() {
		count, err := mirrorConfigDir(stagedConfig, cfg)
		if err != nil {
			return fmt.Errorf("apply config: %w", err)
		}
		slog.Info("restore: config files restored", "count", count, "dir", cfg.ConfDir)
	}

	restoredMedia, err := mirrorStagedDataFiles(stage, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("apply media: %w", err)
	}
	if restoredMedia > 0 {
		slog.Info("restore: media files restored", "count", restoredMedia, "dir", cfg.DataDir)
		if err := reconcileRestoredBookmarkMedia(cfg); err != nil {
			return fmt.Errorf("reconcile restored media: %w", err)
		}
	}

	return nil
}

func writeStream(path string, src io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := io.Copy(f, src); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	return f.Close()
}

// mirrorDir copies every regular file from src into dst, creating directories
// as needed. Files in dst that are not present in src are left untouched.
func mirrorDir(src, dst string) (int, error) {
	return mirrorDirWithRewrite(src, dst, nil)
}

func mirrorConfigDir(src string, cfg *config.Config) (int, error) {
	sourceRuntime := readStagedRuntimeManifest(filepath.Dir(src))
	targetRuntime := runtimeManifest{
		Version:   1,
		DataDir:   cfg.DataDir,
		ConfigDir: cfg.ConfDir,
		RepoDir:   cfg.RepoDir,
	}
	return mirrorDirWithRewrite(src, cfg.ConfDir, func(rel string, data []byte) []byte {
		return rewriteRuntimeConfigPaths(rel, data, sourceRuntime, targetRuntime)
	})
}

func mirrorStagedDataFiles(stage, dataDir string) (int, error) {
	total := 0
	for _, rel := range []string{"media", "thumbnails"} {
		src := filepath.Join(stage, rel)
		info, err := os.Stat(src)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return total, err
		}
		if !info.IsDir() {
			continue
		}
		count, err := mirrorDir(src, filepath.Join(dataDir, rel))
		total += count
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func mirrorDirWithRewrite(src, dst string, rewrite func(rel string, data []byte) []byte) (int, error) {
	count := 0
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			_ = in.Close()
			return err
		}
		if rewrite == nil {
			_, err = io.Copy(out, in)
		} else {
			var data []byte
			data, err = io.ReadAll(in)
			if err == nil {
				_, err = out.Write(rewrite(rel, data))
			}
		}
		closeInErr := in.Close()
		if err != nil {
			_ = out.Close()
			return err
		}
		if closeInErr != nil {
			_ = out.Close()
			return closeInErr
		}
		if err := out.Close(); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

type runtimeManifest struct {
	Version   int    `json:"version"`
	DataDir   string `json:"data_dir,omitempty"`
	ConfigDir string `json:"config_dir,omitempty"`
	RepoDir   string `json:"repo_dir,omitempty"`
}

func readStagedRuntimeManifest(stage string) runtimeManifest {
	raw, err := os.ReadFile(filepath.Join(stage, runtimeName))
	if err != nil {
		return runtimeManifest{}
	}
	var manifest runtimeManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return runtimeManifest{}
	}
	return manifest
}

func rewriteRuntimeConfigPaths(rel string, data []byte, source, target runtimeManifest) []byte {
	if filepath.ToSlash(rel) != "nginx.conf" {
		return data
	}
	text := string(data)
	for _, pair := range [][2]string{
		{source.DataDir, target.DataDir},
		{source.ConfigDir, target.ConfigDir},
		{source.RepoDir, target.RepoDir},
	} {
		oldPath := cleanReplacementPath(pair[0])
		newPath := cleanReplacementPath(pair[1])
		if oldPath == "" || newPath == "" || oldPath == newPath {
			continue
		}
		text = strings.ReplaceAll(text, oldPath, newPath)
	}
	return []byte(text)
}

func cleanReplacementPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == string(filepath.Separator) {
		return ""
	}
	return clean
}

func reconcileRestoredBookmarkMedia(cfg *config.Config) error {
	root := filepath.Join(cfg.DataDir, "media", "imported", "bookmarks")
	if info, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	} else if !info.IsDir() {
		return nil
	}

	store, err := db.Open(cfg.DatabasePath, cfg.DataDir)
	if err != nil {
		return err
	}
	defer func() {
		_ = store.Close()
	}()

	type restoredFile struct {
		videoID    string
		relPath    string
		mediaIndex int
		mediaType  string
		fileSize   int64
	}
	var files []restoredFile
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(cfg.DataDir, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 5 || parts[0] != "media" || parts[1] != "imported" || parts[2] != "bookmarks" {
			return nil
		}
		videoID := strings.TrimSpace(parts[3])
		fileName := parts[4]
		if videoID == "" || !safeArchiveLeaf(videoID) || !safeArchiveLeaf(fileName) {
			return nil
		}
		files = append(files, restoredFile{
			videoID:    videoID,
			relPath:    filepath.ToSlash(rel),
			mediaIndex: restoredMediaIndex(fileName),
			mediaType:  mediaTypeForPath(fileName),
			fileSize:   info.Size(),
		})
		return nil
	}); err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	return store.WithWrite(func(tx *sql.Tx) error {
		for _, file := range files {
			if _, err := tx.Exec(`
				INSERT INTO media_files
					(owner_type, owner_id, media_index, file_path, media_type, file_size)
				VALUES ('feed_media', ?, ?, ?, ?, ?)
				ON CONFLICT(owner_type, owner_id, media_index) DO UPDATE SET
					file_path = excluded.file_path,
					media_type = excluded.media_type,
					file_size = excluded.file_size
			`, file.videoID, file.mediaIndex, file.relPath, file.mediaType, file.fileSize); err != nil {
				return err
			}
			if file.mediaIndex == 0 {
				if _, err := tx.Exec(`
					UPDATE videos
					SET file_path = ?
					WHERE video_id = ?
				`, filepath.Join(cfg.DataDir, file.relPath), file.videoID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func restoredMediaIndex(fileName string) int {
	stem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	idx, err := strconv.Atoi(stem)
	if err != nil || idx < 0 {
		return 0
	}
	return idx
}

func mediaTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".image":
		return "photo"
	case ".gif":
		return "gif"
	case ".mp3", ".m4a", ".ogg", ".aac", ".wav":
		return "audio"
	default:
		return "video"
	}
}
