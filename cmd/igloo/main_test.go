package main

import (
	"errors"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/config"
	"github.com/screwys/igloo/internal/storage"
)

func TestServerLoggingWithoutStderr(t *testing.T) {
	root := t.TempDir()
	layout, err := storage.New(root, filepath.Join(root, "media"))
	if err != nil {
		t.Fatal(err)
	}
	closed, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	stderr, logger, output := os.Stderr, slog.Default(), log.Writer()
	os.Stderr = closed
	t.Cleanup(func() { os.Stderr = stderr; slog.SetDefault(logger); log.SetOutput(output) })
	file := setupServerLogging(&config.Config{Storage: layout})
	if file == nil {
		t.Fatal("log file was not opened")
	}
	slog.Info("server logging is available")
	log.Print("standard logging is available")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "logs", "server", "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"server logging is available", "standard logging is available"} {
		if !strings.Contains(string(data), message) {
			t.Fatalf("log file is missing %q: %s", message, data)
		}
	}
}

func TestInitialConfigErrorAllowsPendingRestore(t *testing.T) {
	injected := errors.New("invalid current config")
	cfg := &config.Config{ConfigError: injected}
	if err := initialConfigError(cfg, false); !errors.Is(err, injected) {
		t.Fatalf("initialConfigError without restore = %v", err)
	}
	if err := initialConfigError(cfg, true); err != nil {
		t.Fatalf("pending restore was blocked by current config: %v", err)
	}
}

func TestLocalHealthURLUsesConfiguredPort(t *testing.T) {
	cfg := &config.Config{ListenAddr: ":6123"}
	if got := localHealthURL(cfg); got != "http://127.0.0.1:6123/api/health/live" {
		t.Fatalf("localHealthURL = %q", got)
	}
}
