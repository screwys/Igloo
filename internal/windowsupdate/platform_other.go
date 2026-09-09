//go:build !windows

package windowsupdate

import "context"

func StartControl(context.Context, *Manager) error { return nil }

func NewForCurrentProcess(Settings, bool, string, bool, func()) *Manager {
	return nil
}
