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
	applyNow       chan struct{}

	mu            sync.RWMutex
	status        Status
	appETag       string
	runtimeETag   string
	lastAvailable Available
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
		applyNow:       make(chan struct{}, 1),
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
		apply := false
		select {
		case <-ctx.Done():
			return
		case <-m.checkNow:
			forced = true
		case <-m.applyNow:
			apply = true
		case <-timer.C:
		}
		if apply {
			m.apply(ctx)
		} else if forced || m.enabled() || m.runtimeEnabled() {
			m.check(ctx, forced)
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

func (m *Manager) ApplyNow() {
	if m == nil {
		return
	}
	select {
	case m.applyNow <- struct{}{}:
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

func (m *Manager) queryAvailable(ctx context.Context) (Available, error) {
	available := Available{
		Manifest: Manifest{
			Schema:            ManifestSchema,
			OS:                "windows",
			Arch:              "amd64",
			App:               m.lastAvailable.Manifest.App,
			Runtime:           m.lastAvailable.Manifest.Runtime,
			MinimumAppVersion: m.lastAvailable.Manifest.MinimumAppVersion,
		},
		AppURL:     m.lastAvailable.AppURL,
		RuntimeURL: m.lastAvailable.RuntimeURL,
	}

	app, etag, unchanged, err := m.source.Latest(ctx, m.channel(), m.appETag)
	if err != nil {
		return available, err
	}
	if !unchanged {
		m.appETag = etag
		available.Manifest.App = app.Manifest.App
		available.AppURL = app.AppURL
		if app.Manifest.MinimumAppVersion != "" {
			available.Manifest.MinimumAppVersion = app.Manifest.MinimumAppVersion
		}
	}

	runtimeAvailable, etag, unchanged, err := m.source.Latest(ctx, "runtime", m.runtimeETag)
	if err != nil {
		return available, err
	}
	if !unchanged {
		m.runtimeETag = etag
		available.Manifest.Runtime = runtimeAvailable.Manifest.Runtime
		available.RuntimeURL = runtimeAvailable.RuntimeURL
		if runtimeAvailable.Manifest.MinimumAppVersion != "" {
			available.Manifest.MinimumAppVersion = runtimeAvailable.Manifest.MinimumAppVersion
		}
	}

	m.lastAvailable = available

	needsApp := available.Manifest.App != nil && NewerVersion(available.Manifest.App.Version, m.currentApp)
	needsRuntime := available.Manifest.Runtime != nil && NewerVersion(available.Manifest.Runtime.Version, m.currentRuntime)
	effectiveApp := m.currentApp
	if needsApp {
		effectiveApp = available.Manifest.App.Version
	}
	if needsRuntime && NewerVersion(available.Manifest.MinimumAppVersion, effectiveApp) {
		needsRuntime = false
		return available, fmt.Errorf("Windows runtime %s requires Igloo %s", available.Manifest.Runtime.Version, available.Manifest.MinimumAppVersion)
	}
	if !needsApp {
		available.Manifest.App = nil
		available.AppURL = ""
	}
	if !needsRuntime {
		available.Manifest.Runtime = nil
		available.RuntimeURL = ""
	}
	return available, nil
}

func (m *Manager) check(ctx context.Context, forced bool) {
	m.updateStatus(func(status *Status) {
		status.Checking = true
		status.LastError = ""
	})
	defer m.updateStatus(func(status *Status) { status.Checking = false })

	available, err := m.queryAvailable(ctx)
	if err != nil {
		m.fail(err)
		return
	}
	m.updateStatus(func(status *Status) {
		status.LastCheckedAt = time.Now().UTC()
		if available.Manifest.App != nil {
			status.AvailableApp = available.Manifest.App.Version
		} else {
			status.AvailableApp = ""
		}
		if available.Manifest.Runtime != nil {
			status.AvailableRuntime = available.Manifest.Runtime.Version
		} else {
			status.AvailableRuntime = ""
		}
	})

	if forced {
		return
	}

	applyApp := m.enabled()
	applyRuntime := m.runtimeEnabled()
	if !applyApp {
		available.Manifest.App = nil
		available.AppURL = ""
	}
	if !applyRuntime {
		available.Manifest.Runtime = nil
		available.RuntimeURL = ""
	}
	if available.Manifest.App == nil && available.Manifest.Runtime == nil {
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
}

func (m *Manager) apply(ctx context.Context) {
	if m.installer == nil {
		m.fail(fmt.Errorf("Windows update installer is unavailable"))
		return
	}
	m.updateStatus(func(status *Status) {
		status.Applying = true
		status.LastError = ""
	})
	defer m.updateStatus(func(status *Status) { status.Applying = false })

	available, err := m.queryAvailable(ctx)
	if err != nil {
		m.fail(err)
		return
	}
	if available.Manifest.App == nil && available.Manifest.Runtime == nil {
		return
	}
	if err := m.installer.Apply(ctx, available); err != nil {
		m.fail(fmt.Errorf("apply Windows update: %w", err))
		return
	}
}

func (m *Manager) enabled() bool {
	value, _ := m.settings.GetSetting("windows_update_enabled", fmt.Sprint(m.defaultEnabled))
	return value != "false" && value != "0"
}

func (m *Manager) runtimeEnabled() bool {
	fallback := m.channel() == "nightly"
	value, _ := m.settings.GetSetting("windows_runtime_update_enabled", fmt.Sprint(fallback))
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
	if value == "nightly" || value == "latest" {
		return "nightly"
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
