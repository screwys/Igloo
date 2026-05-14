// Package restore implements pending-restore staging and on-startup application
// for full backup archives (igloo.db + config dir) produced by the backup
// worker. The flow is:
//
//  1. Import handler receives a backup archive upload, calls StageTarball() or StageZip() to
//     extract it into <DataDir>/restore-staging/ and write a marker file.
//  2. Process exits; systemd restarts igloo.
//  3. Startup calls ApplyPending() before opening the database. If the
//     marker exists, the staged db and config files replace the live ones,
//     and the staging directory is cleaned up.
package restore

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/screwys/igloo/internal/config"
)

const (
	stagingSubdir = "restore-staging"
	markerName    = ".pending-restore"
	configPrefix  = "config/"
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

// StageZip extracts a zip backup into the staging directory and writes the
// marker file. It returns ErrMissingDatabase when the zip is not a backup
// archive, allowing callers to fall through to other zip import formats.
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
	dbEntry = clean == config.DatabaseFilename
	if !dbEntry && !strings.HasPrefix(filepath.ToSlash(clean)+"/", configPrefix) && filepath.ToSlash(clean) != strings.TrimSuffix(configPrefix, "/") {
		return clean, false, false, nil
	}
	return clean, dbEntry, true, nil
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
		count, err := mirrorDir(stagedConfig, cfg.ConfDir)
		if err != nil {
			return fmt.Errorf("apply config: %w", err)
		}
		slog.Info("restore: config files restored", "count", count, "dir", cfg.ConfDir)
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
		defer func() {
			_ = in.Close()
		}()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}
