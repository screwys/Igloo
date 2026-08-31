//go:build windows

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	serviceName  = "Igloo"
	localHTTPURL = "http://127.0.0.1:5001"
	localTLSURL  = "https://127.0.0.1:5001"
)

func main() {
	target := healthyURL()
	if target == "" {
		if err := startService(); err != nil {
			_ = startUserServer()
		}
		target = waitForHealth(30 * time.Second)
	}
	if target == "" {
		target = localHTTPURL
	}
	_ = openBrowser(target)
}

func startService() error {
	managerHandle, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return err
	}
	manager := &mgr.Mgr{Handle: managerHandle}
	defer func() { _ = manager.Disconnect() }()
	namePointer, err := syscall.UTF16PtrFromString(serviceName)
	if err != nil {
		return err
	}
	serviceHandle, err := windows.OpenService(manager.Handle, namePointer, windows.SERVICE_START|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return err
	}
	service := &mgr.Service{Name: serviceName, Handle: serviceHandle}
	defer func() { _ = service.Close() }()
	status, err := service.Query()
	if err == nil && status.State == svc.Running {
		return nil
	}
	err = service.Start()
	if errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return nil
	}
	return err
}

func startUserServer() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	server := filepath.Join(filepath.Dir(executable), "igloo-user.exe")
	command := exec.Command(server)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		HideWindow:    true,
	}
	return command.Start()
}

func waitForHealth(timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if target := healthyURL(); target != "" {
			return target
		}
		time.Sleep(500 * time.Millisecond)
	}
	return ""
}

func healthyURL() string {
	for _, target := range []string{localHTTPURL, localTLSURL} {
		if healthy(target) {
			return target
		}
	}
	return ""
}

func healthy(target string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+"/api/health/live", nil)
	if err != nil {
		return false
	}
	client := http.DefaultClient
	if target == localTLSURL {
		client = &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- loopback health probe accepts Igloo's configured certificate.
		}}
	}
	response, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode == http.StatusOK
}

func openBrowser(url string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}
