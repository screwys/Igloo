package windowsupdate

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxUpdateArchiveBytes   = int64(2 << 30)
	maxExtractedUpdateBytes = int64(4 << 30)
	maxUpdateArchiveFiles   = 20_000
)

type ApplyPlan struct {
	InstallRoot     string `json:"install_root"`
	ServiceName     string `json:"service_name"`
	ServiceMode     bool   `json:"service_mode"`
	ProcessID       int    `json:"process_id"`
	HealthURL       string `json:"health_url"`
	AppIncoming     string `json:"app_incoming,omitempty"`
	RuntimeIncoming string `json:"runtime_incoming,omitempty"`
	StagingRoot     string `json:"staging_root"`
}

type PlatformInstaller struct {
	InstallRoot string
	ServiceName string
	ServiceMode bool
	HealthURL   string
	Client      *http.Client
	RequestStop func()
}

func (i PlatformInstaller) stage(ctx context.Context, available Available) (ApplyPlan, string, error) {
	root, err := filepath.Abs(i.InstallRoot)
	if err != nil || strings.TrimSpace(i.InstallRoot) == "" {
		return ApplyPlan{}, "", errors.New("Windows install root is invalid")
	}
	stagingRoot := filepath.Join(root, "updates", fmt.Sprintf("stage-%d", time.Now().UnixNano()))
	pruneStaging(filepath.Join(root, "updates"), time.Now().Add(-24*time.Hour))
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		return ApplyPlan{}, "", fmt.Errorf("create update staging root: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stagingRoot)
		}
	}()

	plan := ApplyPlan{
		InstallRoot: root,
		ServiceName: i.ServiceName,
		ServiceMode: i.ServiceMode,
		ProcessID:   os.Getpid(),
		HealthURL:   i.HealthURL,
		StagingRoot: stagingRoot,
	}
	if available.Manifest.App != nil {
		plan.AppIncoming = filepath.Join(stagingRoot, "app")
		if err := i.stagePayload(ctx, available.AppURL, *available.Manifest.App, plan.AppIncoming); err != nil {
			return ApplyPlan{}, "", err
		}
	}
	if available.Manifest.Runtime != nil {
		plan.RuntimeIncoming = filepath.Join(stagingRoot, "runtime")
		if err := i.stagePayload(ctx, available.RuntimeURL, *available.Manifest.Runtime, plan.RuntimeIncoming); err != nil {
			return ApplyPlan{}, "", err
		}
	}

	helperSource := filepath.Join(plan.AppIncoming, "igloo-update.exe")
	if plan.AppIncoming == "" {
		helperSource = filepath.Join(root, "app", "current", "igloo-update.exe")
	}
	if info, err := os.Stat(helperSource); err != nil || !info.Mode().IsRegular() {
		return ApplyPlan{}, "", errors.New("Windows update helper is missing from the application bundle")
	}
	helper := filepath.Join(stagingRoot, "runner", "igloo-update.exe")
	if err := copyRegularFile(helperSource, helper); err != nil {
		return ApplyPlan{}, "", fmt.Errorf("stage Windows update helper: %w", err)
	}
	planPath := filepath.Join(stagingRoot, "plan.json")
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return ApplyPlan{}, "", err
	}
	if err := os.WriteFile(planPath, data, 0o600); err != nil {
		return ApplyPlan{}, "", fmt.Errorf("write update plan: %w", err)
	}
	cleanup = false
	return plan, helper, nil
}

func copyRegularFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func pruneStaging(root string, before time.Time) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "stage-") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(before) {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}

func (i PlatformInstaller) stagePayload(ctx context.Context, url string, payload Payload, destination string) error {
	archivePath := destination + ".zip"
	if err := downloadPayload(ctx, i.Client, url, payload, archivePath); err != nil {
		return err
	}
	if err := extractUpdateZip(archivePath, destination); err != nil {
		return err
	}
	if err := os.Remove(archivePath); err != nil {
		return fmt.Errorf("remove staged update archive: %w", err)
	}
	return nil
}

func downloadPayload(ctx context.Context, client *http.Client, url string, payload Payload, destination string) error {
	if !strings.HasPrefix(url, "https://") {
		return errors.New("Windows update payload URL is not HTTPS")
	}
	if payload.Size > maxUpdateArchiveBytes {
		return errors.New("Windows update payload is too large")
	}
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Hour}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Igloo-Windows-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download Windows update: %s", resp.Status)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, payload.Size+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return closeErr
	}
	if written != payload.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), payload.SHA256) {
		_ = os.Remove(destination)
		return errors.New("Windows update payload size or SHA-256 does not match its signed manifest")
	}
	return nil
}

func extractUpdateZip(archivePath, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open Windows update archive: %w", err)
	}
	defer func() { _ = reader.Close() }()
	if len(reader.File) == 0 || len(reader.File) > maxUpdateArchiveFiles {
		return errors.New("Windows update archive has an invalid file count")
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	var total uint64
	for _, file := range reader.File {
		if strings.Contains(file.Name, `\`) || strings.Contains(file.Name, ":") || strings.ContainsRune(file.Name, 0) {
			return fmt.Errorf("Windows update archive contains unsafe path %q", file.Name)
		}
		slashName := path.Clean(file.Name)
		if slashName == "." || path.IsAbs(slashName) || slashName == ".." || strings.HasPrefix(slashName, "../") {
			return fmt.Errorf("Windows update archive contains unsafe path %q", file.Name)
		}
		name := filepath.FromSlash(slashName)
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Windows update archive contains symlink %q", file.Name)
		}
		total += file.UncompressedSize64
		if total > uint64(maxExtractedUpdateBytes) {
			return errors.New("Windows update archive expands beyond its size limit")
		}
		target := filepath.Join(destination, name)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		destinationFile, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
		var written int64
		if err == nil {
			written, err = io.Copy(destinationFile, io.LimitReader(source, int64(file.UncompressedSize64)+1))
		}
		closeDestinationErr := error(nil)
		if destinationFile != nil {
			closeDestinationErr = destinationFile.Close()
		}
		closeSourceErr := source.Close()
		if err != nil {
			return err
		}
		if written != int64(file.UncompressedSize64) {
			return fmt.Errorf("Windows update archive entry %q has the wrong size", file.Name)
		}
		if closeDestinationErr != nil {
			return closeDestinationErr
		}
		if closeSourceErr != nil {
			return closeSourceErr
		}
	}
	return nil
}
