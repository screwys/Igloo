//go:build windows

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/screwys/igloo/internal/config"
	"github.com/screwys/igloo/internal/storage"
	"github.com/screwys/igloo/internal/windowsupdate"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type platformLifecycle struct{ startedProcessID int }

func newPlatformLifecycle() windowsupdate.Lifecycle { return &platformLifecycle{} }

func (platformLifecycle) WaitForProcess(ctx context.Context, processID int) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(processID))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	for {
		result, err := windows.WaitForSingleObject(handle, 1000)
		if err != nil {
			return err
		}
		if result == windows.WAIT_OBJECT_0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (platformLifecycle) MigrateSQLite(ctx context.Context, plan windowsupdate.ApplyPlan) (func() error, error) {
	if plan.AppIncoming == "" {
		return nil, nil
	}
	cfg := config.Load()
	if cfg.ConfigError != nil {
		return nil, cfg.ConfigError
	}
	legacyPath := cfg.Storage.DatabasePath()
	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	before, err := os.ReadDir(cfg.Storage.StateRoot())
	if err != nil {
		return nil, err
	}
	existing := make(map[string]struct{}, len(before))
	for _, entry := range before {
		existing[entry.Name()] = struct{}{}
	}
	executable := filepath.Join(plan.InstallRoot, "app", "current", "igloo-user.exe")
	command := exec.CommandContext(ctx, executable, "migrate-sqlite")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	migrationErr := command.Run()
	after, readErr := os.ReadDir(cfg.Storage.StateRoot())
	backupPath := ""
	for _, entry := range after {
		if _, present := existing[entry.Name()]; !present && strings.HasPrefix(entry.Name(), "igloo.sqlite-backup-") && strings.HasSuffix(entry.Name(), ".db") {
			backupPath = filepath.Join(cfg.Storage.StateRoot(), entry.Name())
		}
	}
	rollback := func() error {
		if _, err := os.Stat(legacyPath); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if backupPath == "" {
			return errors.New("retained SQLite backup is unavailable for rollback")
		}
		source, err := os.Open(backupPath)
		if err != nil {
			return err
		}
		defer func() { _ = source.Close() }()
		destination, err := os.CreateTemp(cfg.Storage.StateRoot(), ".igloo-update-sqlite-*.db")
		if err != nil {
			return err
		}
		temporaryPath := destination.Name()
		defer func() { _ = os.Remove(temporaryPath) }()
		_, copyErr := io.Copy(destination, source)
		if err := errors.Join(copyErr, destination.Sync(), destination.Close()); err != nil {
			return err
		}
		if err := os.Rename(temporaryPath, legacyPath); err != nil {
			return err
		}
		return storage.SyncDirectory(cfg.Storage.StateRoot())
	}
	return rollback, errors.Join(migrationErr, readErr)
}

func (l *platformLifecycle) Start(ctx context.Context, plan windowsupdate.ApplyPlan) error {
	if plan.ServiceMode {
		manager, service, err := openService(plan.ServiceName)
		if err != nil {
			return err
		}
		defer func() { _ = manager.Disconnect() }()
		defer func() { _ = service.Close() }()
		for {
			err := service.Start()
			if err == nil || errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
				return nil
			}
			select {
			case <-ctx.Done():
				return err
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	executable := filepath.Join(plan.InstallRoot, "app", "current", "igloo-user.exe")
	// The restarted server must outlive this update helper and its context.
	command := exec.Command(executable)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS, HideWindow: true}
	if err := command.Start(); err != nil {
		return err
	}
	l.startedProcessID = command.Process.Pid
	return command.Process.Release()
}

func (l *platformLifecycle) Stop(ctx context.Context, plan windowsupdate.ApplyPlan) error {
	if !plan.ServiceMode {
		if l.startedProcessID == 0 {
			return nil
		}
		name, err := windows.UTF16PtrFromString(`Local\Igloo.Server.Stop`)
		if err != nil {
			return err
		}
		event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
		if err == nil {
			err = windows.SetEvent(event)
			_ = windows.CloseHandle(event)
		}
		if err != nil && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return err
		}
		return l.WaitForProcess(ctx, l.startedProcessID)
	}
	manager, service, err := openService(plan.ServiceName)
	if err != nil {
		return err
	}
	defer func() { _ = manager.Disconnect() }()
	defer func() { _ = service.Close() }()
	if _, err := service.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return err
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := service.Query()
		if err != nil || status.State == svc.Stopped {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (platformLifecycle) WaitHealthy(ctx context.Context, plan windowsupdate.ApplyPlan) error {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if len(plan.HealthURL) >= 8 && plan.HealthURL[:8] == "https://" {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true} // loopback self-signed certificate
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: transport}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, plan.HealthURL, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(req)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("health check timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func openService(name string) (*mgr.Mgr, *mgr.Service, error) {
	managerHandle, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, nil, err
	}
	manager := &mgr.Mgr{Handle: managerHandle}
	namePointer, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		_ = manager.Disconnect()
		return nil, nil, err
	}
	serviceHandle, err := windows.OpenService(manager.Handle, namePointer, windows.SERVICE_START|windows.SERVICE_STOP|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		_ = manager.Disconnect()
		return nil, nil, err
	}
	service := &mgr.Service{Name: name, Handle: serviceHandle}
	return manager, service, nil
}
