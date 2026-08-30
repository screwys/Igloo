//go:build windows

package windowsupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/screwys/igloo/internal/buildinfo"
	"golang.org/x/sys/windows"
)

var (
	publicKeyBase64   string
	releaseRepository = defaultRepository
)

func NewForCurrentProcess(settings Settings, serviceMode bool, healthURL string, requestStop func()) *Manager {
	info := buildinfo.Current()
	if strings.TrimSpace(publicKeyBase64) == "" || info.Version == "dev" {
		return nil
	}
	installRoot, err := currentInstallRoot()
	if err != nil {
		return nil
	}
	installer := PlatformInstaller{
		InstallRoot: installRoot,
		ServiceName: "Igloo",
		ServiceMode: serviceMode,
		HealthURL:   healthURL,
		RequestStop: requestStop,
	}
	source := GitHubSource{Repository: releaseRepository, PublicKeyBase64: publicKeyBase64}
	return NewManager(settings, source, installer, info.Version, installedRuntimeVersion(installRoot, info.BundleRevision))
}

func installedRuntimeVersion(installRoot, fallback string) string {
	data, err := os.ReadFile(filepath.Join(installRoot, "runtime", "current", "windows-runtime.lock.json"))
	if err != nil {
		return fallback
	}
	var lock struct {
		Revision string `json:"revision"`
	}
	if json.Unmarshal(data, &lock) != nil || strings.TrimSpace(lock.Revision) == "" {
		return fallback
	}
	return lock.Revision
}

func currentInstallRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv("IGLOO_INSTALL_DIR")); root != "" {
		return filepath.Abs(root)
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(executable)
	if strings.EqualFold(filepath.Base(dir), "current") && strings.EqualFold(filepath.Base(filepath.Dir(dir)), "app") {
		return filepath.Dir(filepath.Dir(dir)), nil
	}
	return "", errors.New("Igloo is not running from a managed Windows installation")
}

func (i PlatformInstaller) Apply(ctx context.Context, available Available) error {
	if i.RequestStop == nil {
		return errors.New("Windows update restart callback is unavailable")
	}
	plan, helper, err := i.stage(ctx, available)
	if err != nil {
		return err
	}
	planPath := filepath.Join(plan.StagingRoot, "plan.json")
	command := exec.Command(helper, "--plan", planPath)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		HideWindow:    true,
	}
	if err := command.Start(); err != nil {
		_ = os.RemoveAll(plan.StagingRoot)
		return err
	}
	i.RequestStop()
	return nil
}
