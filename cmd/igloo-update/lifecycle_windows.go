//go:build windows

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/screwys/igloo/internal/windowsupdate"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type platformLifecycle struct{}

func newPlatformLifecycle() windowsupdate.Lifecycle { return platformLifecycle{} }

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

func (platformLifecycle) Start(ctx context.Context, plan windowsupdate.ApplyPlan) error {
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
	executable := filepath.Join(plan.InstallRoot, "app", "current", "igloo.exe")
	command := exec.CommandContext(ctx, executable)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS, HideWindow: true}
	return command.Start()
}

func (platformLifecycle) Stop(ctx context.Context, plan windowsupdate.ApplyPlan) error {
	if !plan.ServiceMode {
		return nil
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
