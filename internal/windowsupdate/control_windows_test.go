//go:build windows

package windowsupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

type controlInstaller struct{ applied chan Available }

func (i controlInstaller) Apply(_ context.Context, available Available) error {
	i.applied <- available
	return nil
}

func TestTrayUpdateControl(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	installer := controlInstaller{applied: make(chan Available, 1)}
	manager := NewManager(fakeSettings{"windows_update_enabled": "false"}, fakeSource{available: map[string]Available{
		"stable": {Manifest: Manifest{App: &Payload{Version: "3.5.0"}}, AppURL: "https://example.test/app.zip"},
	}}, installer, "3.4.0", "18")
	if err := StartControl(ctx, manager); err != nil {
		t.Fatal(err)
	}
	go manager.Run(ctx)
	request := func(command string) Status {
		t.Helper()
		connection, err := winio.DialPipeContext(ctx, `\\.\pipe\Igloo.Updates`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = connection.Close() }()
		if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintln(connection, command); err != nil {
			t.Fatal(err)
		}
		var status Status
		if err := json.NewDecoder(connection).Decode(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	if status := request("status"); !status.Supported || status.CurrentApp != "3.4.0" {
		t.Fatalf("status: %+v", status)
	}
	status := request("check")
	deadline := time.Now().Add(5 * time.Second)
	for status.Checking && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		status = request("status")
	}
	if status.AvailableApp != "3.5.0" {
		t.Fatalf("check: %+v", status)
	}
	request("apply")
	select {
	case available := <-installer.applied:
		if available.Manifest.App.Version != "3.5.0" {
			t.Fatalf("apply: %+v", available)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tray did not request installation")
	}
}
