//go:build !windows

package windowsupdate

func NewForCurrentProcess(Settings, bool, string, bool, func()) *Manager {
	return nil
}
