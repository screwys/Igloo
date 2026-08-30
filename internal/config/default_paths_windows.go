//go:build windows

package config

import (
	"os"
	"path/filepath"
)

func defaultStateRoot() string {
	return filepath.Join(windowsProgramData(), "Igloo", "data")
}

func defaultConfigDir() string {
	return filepath.Join(windowsProgramData(), "Igloo", "config")
}

func windowsProgramData() string {
	if dir := os.Getenv("ProgramData"); dir != "" {
		return dir
	}
	return filepath.Join(homeDir(), "AppData", "Local")
}
