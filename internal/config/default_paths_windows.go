//go:build windows

package config

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func defaultStateRoot() string {
	return windowsInstallString("DataDirectory", filepath.Join(windowsProgramData(), "Igloo", "data"))
}

func defaultConfigDir() string {
	return windowsInstallString("ConfigDirectory", filepath.Join(windowsProgramData(), "Igloo", "config"))
}

func defaultMediaRoot() string {
	return windowsInstallString("MediaDirectory", filepath.Join(windowsProgramData(), "Igloo", "media"))
}

func defaultWindowsAutoUpdate() bool {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Igloo`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return true
	}
	defer func() { _ = key.Close() }()
	value, _, err := key.GetIntegerValue("AutomaticUpdates")
	return err != nil || value != 0
}

func windowsProgramData() string {
	if dir := os.Getenv("ProgramData"); dir != "" {
		return dir
	}
	return filepath.Join(homeDir(), "AppData", "Local")
}

func windowsInstallString(name, fallback string) string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Igloo`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return fallback
	}
	defer func() { _ = key.Close() }()
	value, _, err := key.GetStringValue(name)
	if err != nil || strings.TrimSpace(value) == "" {
		return fallback
	}
	return filepath.Clean(os.ExpandEnv(value))
}
