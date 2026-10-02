package windowsupdate

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
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

func extractUpdateZip(archivePath, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open Windows update archive: %w", err)
	}
	defer func() { _ = reader.Close() }()
	if len(reader.File) == 0 || len(reader.File) > maxUpdateArchiveFiles {
		return errors.New("the Windows update archive has an invalid file count")
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	var total uint64
	for _, file := range reader.File {
		if strings.Contains(file.Name, `\`) || strings.Contains(file.Name, ":") || strings.ContainsRune(file.Name, 0) {
			return fmt.Errorf("the Windows update archive contains unsafe path %q", file.Name)
		}
		slashName := path.Clean(file.Name)
		if slashName == "." || path.IsAbs(slashName) || slashName == ".." || strings.HasPrefix(slashName, "../") {
			return fmt.Errorf("the Windows update archive contains unsafe path %q", file.Name)
		}
		name := filepath.FromSlash(slashName)
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("the Windows update archive contains symlink %q", file.Name)
		}
		total += file.UncompressedSize64
		if total > uint64(maxExtractedUpdateBytes) {
			return errors.New("the Windows update archive expands beyond its size limit")
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
			return fmt.Errorf("the Windows update archive entry %q has the wrong size", file.Name)
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
