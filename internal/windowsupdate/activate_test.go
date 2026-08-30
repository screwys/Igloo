package windowsupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeLifecycle struct {
	healthErr error
	starts    int
	stops     int
}

func (f *fakeLifecycle) WaitForProcess(context.Context, int) error { return nil }
func (f *fakeLifecycle) Start(context.Context, ApplyPlan) error {
	f.starts++
	return nil
}
func (f *fakeLifecycle) Stop(context.Context, ApplyPlan) error {
	f.stops++
	return nil
}
func (f *fakeLifecycle) WaitHealthy(context.Context, ApplyPlan) error { return f.healthErr }

func TestExecutePlanActivatesBothComponents(t *testing.T) {
	root := t.TempDir()
	appIncoming := prepareComponent(t, root, "app", "new-app")
	runtimeIncoming := prepareComponent(t, root, "runtime", "new-runtime")
	lifecycle := &fakeLifecycle{}
	plan := ApplyPlan{InstallRoot: root, ProcessID: 42, StagingRoot: filepath.Join(root, "updates"), AppIncoming: appIncoming, RuntimeIncoming: runtimeIncoming}

	if err := ExecutePlan(context.Background(), plan, lifecycle); err != nil {
		t.Fatal(err)
	}
	assertComponent(t, root, "app", "new-app")
	assertComponent(t, root, "runtime", "new-runtime")
	if lifecycle.starts != 1 || lifecycle.stops != 0 {
		t.Fatalf("lifecycle starts/stops = %d/%d", lifecycle.starts, lifecycle.stops)
	}
}

func TestExecutePlanRollsBackFailedHealthCheck(t *testing.T) {
	root := t.TempDir()
	appIncoming := prepareComponent(t, root, "app", "new-app")
	lifecycle := &fakeLifecycle{healthErr: errors.New("not healthy")}
	plan := ApplyPlan{InstallRoot: root, ProcessID: 42, StagingRoot: filepath.Join(root, "updates"), AppIncoming: appIncoming}

	if err := ExecutePlan(context.Background(), plan, lifecycle); err == nil {
		t.Fatal("failed health check returned success")
	}
	assertComponent(t, root, "app", "old-app")
	if lifecycle.starts != 2 || lifecycle.stops != 1 {
		t.Fatalf("lifecycle starts/stops = %d/%d", lifecycle.starts, lifecycle.stops)
	}
}

func prepareComponent(t *testing.T, root, name, next string) string {
	t.Helper()
	current := filepath.Join(root, name, "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, "version"), []byte("old-"+name), 0o600); err != nil {
		t.Fatal(err)
	}
	incoming := filepath.Join(root, "updates", name)
	if err := os.MkdirAll(incoming, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incoming, "version"), []byte(next), 0o600); err != nil {
		t.Fatal(err)
	}
	return incoming
}

func assertComponent(t *testing.T, root, name, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, name, "current", "version"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s version = %q, want %q", name, got, want)
	}
}
