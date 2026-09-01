package windowsupdate

import (
	"context"
	"testing"
)

type fakeSettings map[string]string

func (s fakeSettings) GetSetting(key, fallback string) (string, error) {
	if value := s[key]; value != "" {
		return value, nil
	}
	return fallback, nil
}

type fakeSource struct {
	available map[string]Available
}

func (s fakeSource) Latest(_ context.Context, feed, _ string) (Available, string, bool, error) {
	return s.available[feed], feed + "-etag", false, nil
}

type fakeInstaller struct {
	applied []Available
}

func (i *fakeInstaller) Apply(_ context.Context, available Available) error {
	i.applied = append(i.applied, available)
	return nil
}

func TestManagerAppliesOnlyNewPayloads(t *testing.T) {
	installer := &fakeInstaller{}
	app := Available{
		Manifest: Manifest{
			MinimumAppVersion: "3.4.0",
			App:               &Payload{Version: "3.4.0"},
		},
		AppURL: "https://example.test/app.zip",
	}
	runtime := Available{
		Manifest: Manifest{
			MinimumAppVersion: "3.4.0",
			Runtime:           &Payload{Version: "19"},
		},
		RuntimeURL: "https://example.test/runtime.zip",
	}
	manager := NewManager(
		fakeSettings{"windows_runtime_update_enabled": "true"},
		fakeSource{available: map[string]Available{"stable": app, "runtime": runtime}},
		installer,
		"3.4.0",
		"18",
	)
	manager.check(t.Context(), false)

	if len(installer.applied) != 1 {
		t.Fatalf("applied updates = %d", len(installer.applied))
	}
	applied := installer.applied[0]
	if applied.Manifest.App != nil || applied.AppURL != "" || applied.Manifest.Runtime == nil || applied.Manifest.Runtime.Version != "19" {
		t.Fatalf("applied update = %+v", applied)
	}
}

func TestManagerClampsConfiguredInterval(t *testing.T) {
	manager := NewManager(fakeSettings{"windows_update_interval_hours": "999"}, fakeSource{}, &fakeInstaller{}, "3.4.0", "18")
	if got := manager.interval().Hours(); got != 168 {
		t.Fatalf("interval hours = %v", got)
	}
}

func TestManagerUsesChannelDefaultForRuntimeUpdates(t *testing.T) {
	if manager := NewManager(fakeSettings{}, fakeSource{}, &fakeInstaller{}, "3.4.0", "18"); manager.runtimeEnabled() {
		t.Fatal("stable channel enabled runtime updates by default")
	}
	if manager := NewManager(fakeSettings{"windows_update_channel": "nightly"}, fakeSource{}, &fakeInstaller{}, "3.4.0", "18"); !manager.runtimeEnabled() {
		t.Fatal("nightly channel did not enable runtime updates by default")
	}
	if manager := NewManager(fakeSettings{"windows_update_channel": "nightly", "windows_runtime_update_enabled": "false"}, fakeSource{}, &fakeInstaller{}, "3.4.0", "18"); manager.runtimeEnabled() {
		t.Fatal("explicit runtime update setting did not override nightly default")
	}
}
