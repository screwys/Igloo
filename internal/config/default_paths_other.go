//go:build !windows

package config

import "path/filepath"

func defaultStateRoot() string {
	return filepath.Join(homeDir(), ".local", "share", "igloo")
}

func defaultConfigDir() string {
	return filepath.Join(homeDir(), ".config", "igloo")
}

func defaultMediaRoot() string { return "" }

func defaultWindowsAutoUpdate() bool { return true }
