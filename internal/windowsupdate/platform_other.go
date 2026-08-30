//go:build !windows

package windowsupdate

func NewForCurrentProcess(Settings, bool, string, func()) *Manager {
	return nil
}
