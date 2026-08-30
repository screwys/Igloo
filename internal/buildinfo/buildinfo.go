package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
)

var (
	version        = "dev"
	bundleRevision = "dev"
	commit         = ""
)

type Info struct {
	Version        string `json:"version"`
	BundleRevision string `json:"bundle_revision"`
	Commit         string `json:"commit,omitempty"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
}

func Current() Info {
	info := Info{
		Version:        normalized(version, moduleVersion()),
		BundleRevision: normalized(bundleRevision, "dev"),
		Commit:         strings.TrimSpace(commit),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
	}
	if info.Commit == "" {
		info.Commit = buildSetting("vcs.revision")
	}
	return info
}

func IsWindows() bool { return runtime.GOOS == "windows" }

func normalized(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "(devel)" {
		return fallback
	}
	return strings.TrimPrefix(value, "v")
}

func moduleVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return normalized(info.Main.Version, "dev")
	}
	return "dev"
}

func buildSetting(key string) string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == key {
				return setting.Value
			}
		}
	}
	return ""
}
