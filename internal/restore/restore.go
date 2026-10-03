// Package restore implements pending-restore staging and on-startup application
// for database and config archives produced by the backup worker or manual Full
// Export. The flow is:
//
//  1. Import handler receives a backup archive upload and calls StageZip() to
//     validate and stage it below the state root.
//  2. Process exits; systemd restarts igloo.
//  3. Startup applies config before starting PostgreSQL, then replaces the
//     database before workers start. The marker stays until both succeed.
package restore

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/screwys/igloo/internal/auth"
	"github.com/screwys/igloo/internal/config"
	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/storage"
	"github.com/screwys/igloo/internal/toolenv"
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
	info, err := os.Lstat(markerPath(dataDir))
	return err == nil && info.Mode().IsRegular()
}

// StageZip extracts and validates a DB-bearing zip before making it
// visible to startup restore.
func StageZip(readerAt io.ReaderAt, size int64, layout storage.Layout) (stageErr error) {
	if err := layout.Ensure(); err != nil {
		return fmt.Errorf("validate storage layout: %w", err)
	}
	stage := stagingDir(layout.StateRoot())
	if err := cleanupStaging(stage); err != nil {
		return fmt.Errorf("clear staging dir: %w", err)
	}
	defer func() {
		if stageErr != nil {
			stageErr = errors.Join(stageErr, cleanupStaging(stage))
		}
	}()
	if err := storage.EnsureDirectory(stage, 0o755); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}

	dbSeen, err := extractZipBackup(readerAt, size, stage)
	if err != nil {
		return err
	}
	if !dbSeen {
		return ErrMissingDatabase
	}
	stagedDB, err := stagedDatabasePath(stage)
	if err != nil {
		return err
	}
	if err := validateStagedDatabase(stagedDB); err != nil {
		return fmt.Errorf("validate staged db: %w", err)
	}
	if err := validateStagedRestoreConfig(stage); err != nil {
		return fmt.Errorf("validate staged config: %w", err)
	}
	if err := replaceFile(markerPath(layout.StateRoot()), 0o644, func(io.Writer) error { return nil }); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}
	return nil
}

func extractZipBackup(readerAt io.ReaderAt, size int64, stage string) (bool, error) {
	zr, err := zip.NewReader(readerAt, size)
	if err != nil {
		return false, fmt.Errorf("open zip: %w", err)
	}
	dbSeen := false
	seen := make(map[string]struct{})
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
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if _, exists := seen[clean]; exists {
			return false, fmt.Errorf("duplicate backup entry: %s", f.Name)
		}
		seen[clean] = struct{}{}
		rc, err := f.Open()
		if err != nil {
			return false, fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		writeErr := replaceFile(dest, mode, func(dst io.Writer) error {
			_, err := io.Copy(dst, rc)
			return err
		})
		closeErr := rc.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return false, err
		}
		if dbEntry {
			if dbSeen {
				return false, fmt.Errorf("backup archive has more than one database")
			}
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
	dbEntry = slash == config.DatabaseFilename || slash == config.DatabaseBackupFilename
	if dbEntry || slash == runtimeName ||
		strings.HasPrefix(slash+"/", configPrefix) ||
		slash == strings.TrimSuffix(configPrefix, "/") {
		return clean, dbEntry, true, nil
	}
	return clean, false, false, nil
}

func cleanupStaging(stage string) error {
	if err := os.RemoveAll(stage); err != nil {
		return fmt.Errorf("remove staging dir %q: %w", stage, err)
	}
	return nil
}

// ApplyPendingConfig runs before runtime configuration is loaded or PostgreSQL
// is started. Database replacement happens through ApplyPendingDatabase.
func ApplyPendingConfig(cfg *config.Config) error {
	if !HasPending(cfg.Storage.StateRoot()) {
		return nil
	}
	if err := cfg.Storage.Ensure(); err != nil {
		return fmt.Errorf("validate storage layout: %w", err)
	}
	stage := stagingDir(cfg.Storage.StateRoot())
	stagedDB, err := stagedDatabasePath(stage)
	if err != nil {
		return err
	}
	if err := validateStagedDatabase(stagedDB); err != nil {
		return fmt.Errorf("validate staged db: %w", err)
	}
	if err := validateStagedRestoreConfig(stage); err != nil {
		return fmt.Errorf("validate staged config: %w", err)
	}
	configFiles, err := collectRestoreConfigFiles(stage, cfg)
	if err != nil {
		return fmt.Errorf("read staged config: %w", err)
	}
	if err := validateEffectiveStartupConfig(cfg, configFiles); err != nil {
		return fmt.Errorf("validate effective startup config: %w", err)
	}
	slog.Info("restore: applying pending restore", "stage", stage)
	for _, file := range configFiles {
		if err := storage.ValidateContainedPath(cfg.ConfDir, file.target); err != nil {
			return fmt.Errorf("revalidate config destination %s: %w", file.target, err)
		}
		if err := installRestoreConfigFile(file); err != nil {
			return fmt.Errorf("restore config %s: %w", file.rel, err)
		}
	}
	if len(configFiles) > 0 {
		slog.Info("restore: config files restored", "count", len(configFiles), "dir", cfg.ConfDir)
	}
	return nil
}

// ApplyPendingDatabase runs after PostgreSQL opens and before background work.
func ApplyPendingDatabase(ctx context.Context, cfg *config.Config, store *db.DB) error {
	if !HasPending(cfg.Storage.StateRoot()) {
		return nil
	}
	if err := cfg.Storage.Ensure(); err != nil {
		return fmt.Errorf("revalidate storage layout: %w", err)
	}
	stage := stagingDir(cfg.Storage.StateRoot())
	stagedDB, err := stagedDatabasePath(stage)
	if err != nil {
		return err
	}
	if filepath.Base(stagedDB) == config.DatabaseBackupFilename {
		err = store.RestorePostgresArchive(ctx, stagedDB)
	} else {
		err = store.RestoreLegacyArchive(ctx, stagedDB)
	}
	if err != nil {
		return fmt.Errorf("restore database: %w", err)
	}
	retainedPath, err := store.RetainLegacyDatabase(ctx)
	if err != nil {
		return fmt.Errorf("retain previous SQLite database: %w", err)
	}
	if retainedPath != "" {
		slog.Info("restore: previous SQLite database retained", "path", retainedPath)
	}
	if err := os.Remove(markerPath(cfg.Storage.StateRoot())); err != nil {
		return fmt.Errorf("clear restore marker: %w", err)
	}
	if err := storage.SyncDirectory(stage); err != nil {
		slog.Warn("restore: marker directory sync failed", "err", err)
	}
	if err := cleanupStaging(stage); err != nil {
		slog.Warn("restore: staging cleanup failed", "state_dir", stage, "err", err)
	}
	slog.Info("restore: database restored")
	return nil
}

func validateStagedDatabase(path string) error {
	if filepath.Base(path) == config.DatabaseBackupFilename {
		postgresBin, err := toolenv.PostgresBinDir()
		if err != nil {
			return err
		}
		return db.ValidatePostgresArchive(context.Background(), path, postgresBin)
	}
	_, cleanup, err := db.PrepareLegacyArchive(context.Background(), path)
	cleanup()
	return err
}

func stagedDatabasePath(stage string) (string, error) {
	var found string
	for _, name := range []string{config.DatabaseBackupFilename, config.DatabaseFilename} {
		path := filepath.Join(stage, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("staged database is not a regular file")
		}
		if found != "" {
			return "", fmt.Errorf("backup archive has more than one database")
		}
		found = path
	}
	if found == "" {
		return "", ErrMissingDatabase
	}
	return found, nil
}

func replaceFile(target string, mode os.FileMode, write func(io.Writer) error) error {
	tmpPath, err := prepareFile(target, mode, write)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	return storage.SyncDirectory(filepath.Dir(target))
}

func prepareFile(target string, mode os.FileMode, write func(io.Writer) error) (string, error) {
	if target == "" || write == nil {
		return "", fmt.Errorf("restore target and writer are required")
	}
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("restore target %q is not a regular file", target)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	dir := filepath.Dir(target)
	if err := storage.EnsureDirectory(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".igloo-restore-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	remove := func(err error) (string, error) {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := write(tmp); err != nil {
		return remove(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return remove(err)
	}
	if err := tmp.Sync(); err != nil {
		return remove(err)
	}
	if err := tmp.Close(); err != nil {
		return remove(err)
	}
	return tmpPath, nil
}

type restoreConfigFile struct {
	rel    string
	source string
	target string
	data   []byte
	mode   os.FileMode
}

func collectRestoreConfigFiles(stage string, cfg *config.Config) ([]restoreConfigFile, error) {
	src := filepath.Join(stage, "config")
	info, err := os.Stat(src)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("staged config is not a directory")
	}
	if strings.TrimSpace(cfg.ConfDir) == "" {
		return nil, fmt.Errorf("config directory is not configured")
	}
	sourceRuntime, err := readStagedRuntimeManifest(stage)
	if err != nil {
		return nil, err
	}
	targetRuntime := runtimeManifest{
		Version:   2,
		DataDir:   cfg.Storage.StateRoot(),
		MediaDir:  cfg.Storage.MediaRoot(),
		ConfigDir: cfg.ConfDir,
		RepoDir:   cfg.RepoDir,
	}
	files := make([]restoreConfigFile, 0)
	err = filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		target := filepath.Join(cfg.ConfDir, rel)
		if err := storage.ValidateContainedPath(cfg.ConfDir, target); err != nil {
			return fmt.Errorf("staged config path escapes destination: %s: %w", rel, err)
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, restoreConfigFile{
			rel: rel, source: path, target: target, mode: mode,
			data: rewriteRuntimeConfigPaths(rel, data, sourceRuntime, targetRuntime),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func installRestoreConfigFile(file restoreConfigFile) error {
	return replaceFile(file.target, file.mode, func(dst io.Writer) error {
		_, err := dst.Write(file.data)
		return err
	})
}

func validateEffectiveStartupConfig(cfg *config.Config, files []restoreConfigFile) error {
	runtimeConfigPath := strings.TrimSpace(cfg.RuntimeConfigPath)
	if runtimeConfigPath == "" {
		runtimeConfigPath = filepath.Join(cfg.ConfDir, "config.json")
	}
	if err := config.ValidateEffectiveRuntimeConfigFile(effectiveRestorePath(runtimeConfigPath, files)); err != nil {
		return err
	}
	authUsersPath := strings.TrimSpace(cfg.AuthUsersPath)
	if authUsersPath == "" {
		authUsersPath = filepath.Join(cfg.ConfDir, "auth_users.json")
	}
	if _, err := auth.LoadUsers(effectiveRestorePath(authUsersPath, files)); err != nil {
		return err
	}
	if strings.TrimSpace(os.Getenv("AUTH_SECRET_KEY")) == "" {
		authSecretPath := effectiveRestorePath(filepath.Join(cfg.ConfDir, "auth_secret"), files)
		secret, err := os.ReadFile(authSecretPath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && len(secret) == 0 {
			return fmt.Errorf("effective auth secret is empty")
		}
	}
	return nil
}

func effectiveRestorePath(target string, files []restoreConfigFile) string {
	for _, file := range files {
		if file.target == target {
			return file.source
		}
	}
	return target
}

type runtimeManifest struct {
	Version   int    `json:"version"`
	DataDir   string `json:"data_dir,omitempty"`
	MediaDir  string `json:"media_dir,omitempty"`
	ConfigDir string `json:"config_dir,omitempty"`
	RepoDir   string `json:"repo_dir,omitempty"`
}

func validateStagedRestoreConfig(stage string) error {
	if _, err := readStagedRuntimeManifest(stage); err != nil {
		return err
	}
	runtimeConfigPath := filepath.Join(stage, "config", "config.json")
	if exists, err := stagedRegularFile(runtimeConfigPath); err != nil {
		return err
	} else if exists {
		if err := config.ValidateRuntimeConfigFile(runtimeConfigPath); err != nil {
			return err
		}
	}
	authUsersPath := filepath.Join(stage, "config", "auth_users.json")
	if exists, err := stagedRegularFile(authUsersPath); err != nil {
		return err
	} else if exists {
		if _, err := auth.LoadUsers(authUsersPath); err != nil {
			return err
		}
	}
	authSecretPath := filepath.Join(stage, "config", "auth_secret")
	if exists, err := stagedRegularFile(authSecretPath); err != nil {
		return err
	} else if exists {
		secret, err := os.ReadFile(authSecretPath)
		if err != nil {
			return err
		}
		if len(secret) == 0 {
			return fmt.Errorf("staged auth secret is empty")
		}
	}
	return nil
}

func stagedRegularFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("staged config file %q is not a regular file", path)
	}
	return true, nil
}

func readStagedRuntimeManifest(stage string) (runtimeManifest, error) {
	path := filepath.Join(stage, runtimeName)
	exists, err := stagedRegularFile(path)
	if err != nil || !exists {
		return runtimeManifest{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return runtimeManifest{}, err
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest runtimeManifest
	decodeErr := decoder.Decode(&manifest)
	var trailing any
	if decodeErr == nil {
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				decodeErr = fmt.Errorf("trailing JSON value")
			} else {
				decodeErr = err
			}
		}
	}
	closeErr := file.Close()
	if err := errors.Join(decodeErr, closeErr); err != nil {
		return runtimeManifest{}, fmt.Errorf("parse staged %s: %w", runtimeName, err)
	}
	if manifest.Version != 2 {
		return runtimeManifest{}, fmt.Errorf("unsupported staged %s version %d", runtimeName, manifest.Version)
	}
	return manifest, nil
}

func rewriteRuntimeConfigPaths(rel string, data []byte, source, target runtimeManifest) []byte {
	if filepath.ToSlash(rel) != "nginx.conf" {
		return data
	}
	replacements := [][2]string{
		{runtimeMediaDir(source), runtimeMediaDir(target)},
		{source.DataDir, target.DataDir},
		{source.ConfigDir, target.ConfigDir},
		{source.RepoDir, target.RepoDir},
	}
	args := make([]string, 0, len(replacements)*2)
	for _, pair := range replacements {
		oldPath := cleanReplacementPath(pair[0])
		newPath := cleanReplacementPath(pair[1])
		if oldPath == "" || newPath == "" || oldPath == newPath {
			continue
		}
		args = append(args, oldPath, newPath)
	}
	if len(args) == 0 {
		return data
	}
	return []byte(strings.NewReplacer(args...).Replace(string(data)))
}

func runtimeMediaDir(manifest runtimeManifest) string {
	if strings.TrimSpace(manifest.MediaDir) != "" {
		return manifest.MediaDir
	}
	if strings.TrimSpace(manifest.DataDir) == "" {
		return ""
	}
	return filepath.Join(manifest.DataDir, "media")
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
