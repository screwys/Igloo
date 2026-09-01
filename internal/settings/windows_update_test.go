package settings

import "testing"

func TestNormalizeWindowsUpdateChannel(t *testing.T) {
	for input, want := range map[string]string{
		"stable":  "stable",
		"nightly": "nightly",
		"latest":  "nightly",
		"unknown": "stable",
	} {
		if got := NormalizeWindowsUpdateChannel(input); got != want {
			t.Errorf("NormalizeWindowsUpdateChannel(%q) = %q, want %q", input, got, want)
		}
	}
}
