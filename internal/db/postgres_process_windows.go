package db

import (
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func postgresProcessIdentity(string) (func(*exec.Cmd), error) {
	return func(cmd *exec.Cmd) {
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.HideWindow = true
	}, nil
}
func setPostgresDirectoryOwner(string) error { return nil }

func postgresOwnerAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var exitCode uint32
	return windows.GetExitCodeProcess(handle, &exitCode) == nil && exitCode == 259
}
