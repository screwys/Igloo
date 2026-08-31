package windowsupdate

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Settings interface {
	GetSetting(key, fallback string) (string, error)
}

type Installer interface {
	Apply(context.Context, Available) error
}

type Source interface {
	Latest(context.Context, string, string) (Available, string, bool, error)
}

type Status struct {
	Supported        bool      `json:"supported"`
	Checking         bool      `json:"checking"`
	Applying         bool      `json:"applying"`
	CurrentApp       string    `json:"current_app"`
	CurrentRuntime   string    `json:"current_runtime"`
	AvailableApp     string    `json:"available_app,omitempty"`
	AvailableRuntime string    `json:"available_runtime,omitempty"`
	LastCheckedAt    time.Time `json:"last_checked_at,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
}

type Manager struct {
	settings       Settings
	source         Source
	installer      Installer
	currentApp     string
	currentRuntime string
	defaultEnabled bool
	checkNow       chan struct{}

	mu     sync.RWMutex
	status Status
	etag   string
}

func NewManager(settings Settings, source Source, installer Installer, currentApp, currentRuntime string) *Manager {
	return &Manager{
		settings:       settings,
		source:         source,
		installer:      installer,
		currentApp:     currentApp,
		currentRuntime: currentRuntime,
		defaultEnabled: true,
		checkNow:       make(chan struct{}, 1),
		status:         Status{Supported: true, CurrentApp: currentApp, CurrentRuntime: currentRuntime},
	}
}

func (m *Manager) Run(ctx context.Context) {
	if m == nil {
		return
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		forced := false
		select {
		case <-ctx.Done():
			return
		case <-m.checkNow:
			forced = true
		case <-timer.C:
		}
		enabled := m.enabled()
		if forced || enabled {
			m.check(ctx, enabled)
		}
		timer.Reset(m.interval())
	}
}

func (m *Manager) CheckNow() {
	if m == nil {
		return
	}
	select {
	case m.checkNow <- struct{}{}:
	default:
	}
}

func (m *Manager) Status() Status {
	if m == nil {
		return Status{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *Manager) check(ctx context.Context, apply bool) {
	m.updateStatus(func(status *Status) {
		status.Checking = true
		status.LastError = ""
	})
	defer m.updateStatus(func(status *Status) { status.Checking = false })

	available, etag, unchanged, err := m.source.Latest(ctx, m.channel(), m.etag)
	if err != nil {
		m.fail(err)
		return
	}
	m.updateStatus(func(status *Status) { status.LastCheckedAt = time.Now().UTC() })
	if unchanged {
		return
	}
	if available.Manifest.App == nil && available.Manifest.Runtime == nil {
		m.etag = etag
		return
	}
	needsApp := available.Manifest.App != nil && NewerVersion(available.Manifest.App.Version, m.currentApp)
	needsRuntime := available.Manifest.Runtime != nil && NewerVersion(available.Manifest.Runtime.Version, m.currentRuntime)
	effectiveApp := m.currentApp
	if needsApp {
		effectiveApp = available.Manifest.App.Version
	}
	if needsRuntime && NewerVersion(available.Manifest.MinimumAppVersion, effectiveApp) {
		needsRuntime = false
		m.fail(fmt.Errorf("Windows runtime %s requires Igloo %s", available.Manifest.Runtime.Version, available.Manifest.MinimumAppVersion))
	}
	if !needsApp {
		available.Manifest.App = nil
		available.AppURL = ""
	}
	if !needsRuntime {
		available.Manifest.Runtime = nil
		available.RuntimeURL = ""
	}
	m.updateStatus(func(status *Status) {
		if available.Manifest.App != nil {
			status.AvailableApp = available.Manifest.App.Version
		}
		if available.Manifest.Runtime != nil {
			status.AvailableRuntime = available.Manifest.Runtime.Version
		}
	})
	if !needsApp && !needsRuntime {
		m.etag = etag
		return
	}
	if !apply {
		return
	}
	if m.installer == nil {
		m.fail(fmt.Errorf("Windows update installer is unavailable"))
		return
	}
	m.updateStatus(func(status *Status) { status.Applying = true })
	defer m.updateStatus(func(status *Status) { status.Applying = false })
	if err := m.installer.Apply(ctx, available); err != nil {
		m.fail(fmt.Errorf("apply Windows update: %w", err))
		return
	}
	m.etag = etag
}

func (m *Manager) enabled() bool {
	value, _ := m.settings.GetSetting("windows_update_enabled", fmt.Sprint(m.defaultEnabled))
	return value != "false" && value != "0"
}

func (m *Manager) interval() time.Duration {
	value, _ := m.settings.GetSetting("windows_update_interval_hours", "6")
	var hours int
	_, _ = fmt.Sscan(value, &hours)
	if hours < 1 {
		hours = 1
	}
	if hours > 168 {
		hours = 168
	}
	return time.Duration(hours) * time.Hour
}

func (m *Manager) channel() string {
	value, _ := m.settings.GetSetting("windows_update_channel", "stable")
	if value == "latest" {
		return value
	}
	return "stable"
}

func (m *Manager) fail(err error) {
	m.updateStatus(func(status *Status) {
		status.LastCheckedAt = time.Now().UTC()
		status.LastError = err.Error()
	})
}

func (m *Manager) updateStatus(update func(*Status)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	update(&m.status)
}
