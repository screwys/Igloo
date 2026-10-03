//go:build !windows

package db

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

func postgresProcessIdentity(path string) (func(*exec.Cmd), error) {
	if os.Geteuid() != 0 {
		return func(*exec.Cmd) {}, nil
	}
	account, err := user.Lookup("postgres")
	if err != nil {
		return nil, err
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return nil, err
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return nil, err
	}
	if err := os.Chown(path, int(uid), int(gid)); err != nil {
		return nil, err
	}
	return func(cmd *exec.Cmd) {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
	}, nil
}

func setPostgresDirectoryOwner(path string) error {
	_, err := postgresProcessIdentity(path)
	return err
}

func postgresOwnerAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
