//go:build windows

package windowsupdate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
)

// StartControl exposes the existing updater to the local Windows tray.
// OS pipe permissions replace web-session authentication for local users.
func StartControl(ctx context.Context, manager *Manager) error {
	if manager == nil {
		return nil
	}
	listener, err := winio.ListenPipe(`\\.\pipe\Igloo.Updates`, &winio.PipeConfig{
		SecurityDescriptor: "D:P(D;;GA;;;NU)(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;LS)(A;;GRGW;;;BU)",
	})
	if err != nil {
		return err
	}
	go func() { <-ctx.Done(); _ = listener.Close() }()
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				if ctx.Err() == nil {
					slog.Error("Windows tray update connection failed", "err", err)
				}
				return
			}
			go serveControl(connection.(winio.PipeConn), manager)
		}
	}()
	return nil
}

func serveControl(connection winio.PipeConn, manager *Manager) {
	defer func() { _ = connection.Close() }()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}
	command, err := bufio.NewReader(io.LimitReader(connection, 128)).ReadString('\n')
	if err != nil {
		return
	}
	switch strings.TrimSpace(command) {
	case "check":
		manager.CheckNow()
	case "apply":
		manager.ApplyNow()
	case "status":
	default:
		_ = json.NewEncoder(connection).Encode(map[string]string{"last_error": fmt.Sprintf("Unknown update command: %s", strings.TrimSpace(command))})
		return
	}
	if err := json.NewEncoder(connection).Encode(manager.Status()); err == nil {
		_ = connection.Flush()
	}
}
