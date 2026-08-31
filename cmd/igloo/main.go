package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/screwys/igloo/internal/auth"
	"github.com/screwys/igloo/internal/buildinfo"
	"github.com/screwys/igloo/internal/config"
	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/restore"
	"github.com/screwys/igloo/internal/toolenv"
	"github.com/screwys/igloo/internal/translate"
	"github.com/screwys/igloo/internal/web"
	"github.com/screwys/igloo/internal/windowsupdate"
	"github.com/screwys/igloo/internal/worker"
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		info := buildinfo.Current()
		fmt.Printf("Igloo %s (runtime %s, %s/%s)\n", info.Version, info.BundleRevision, info.OS, info.Arch)
		return
	}
	if err := runEntrypoint(); err != nil {
		slog.Error("igloo stopped", "err", err)
		os.Exit(1)
	}
}

func runServer(externalStop <-chan struct{}, ready chan<- struct{}, serviceMode bool) error {
	toolenv.ApplyCommonToolPaths()
	cfg := config.Load()
	stateRoot := strings.TrimSpace(cfg.Storage.StateRoot())
	hadPendingRestore := stateRoot != "" && restore.HasPending(stateRoot)
	phaseStart := time.Now()
	if err := restore.ApplyPending(cfg); err != nil {
		return fmt.Errorf("restore: apply failed: %w", err)
	}
	if err := initialConfigError(cfg, hadPendingRestore); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	logStartupPhase("restore", time.Since(phaseStart))
	cfg = config.Load()
	if cfg.ConfigError != nil {
		return fmt.Errorf("invalid restored configuration: %w", cfg.ConfigError)
	}
	if err := cfg.EnsureRuntimeDirs(); err != nil {
		return fmt.Errorf("create runtime directories: %w", err)
	}
	if logFile := setupServerLogging(cfg); logFile != nil {
		defer func() {
			_ = logFile.Close()
		}()
	}
	phaseStart = time.Now()
	auth.InitCache(cfg.AuthUsersPath)
	logStartupPhase("config_auth", time.Since(phaseStart))

	phaseStart = time.Now()
	database, err := db.OpenWithOptions(cfg.Storage, db.OpenOptions{
		Phase: func(name string, elapsed time.Duration) {
			logStartupPhase(name, elapsed)
		},
	})
	if err != nil {
		return fmt.Errorf("open database %s: %w", cfg.Storage.DatabasePath(), err)
	}
	defer func() {
		_ = database.Close()
	}()
	if err := database.ReconcileAllMomentsOrders(); err != nil {
		return fmt.Errorf("reconcile Moments order: %w", err)
	}
	slog.Info("database opened", "path", cfg.Storage.DatabasePath())
	logStartupPhase("db_open", time.Since(phaseStart))

	// Build static version cache
	phaseStart = time.Now()
	staticVersions := buildStaticVersionCache(cfg.StaticDir)
	staticV := func(path string) string {
		if v, ok := staticVersions[path]; ok {
			return "/static/" + path + "?v=" + v
		}
		return "/static/" + path
	}
	logStartupPhase("static_version_cache", time.Since(phaseStart))

	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	phaseStart = time.Now()
	workers := worker.NewManager(database, cfg)
	updateStop := make(chan struct{})
	var requestUpdateStop sync.Once
	windowsUpdater := windowsupdate.NewForCurrentProcess(database, serviceMode, localHealthURL(cfg), cfg.WindowsAutoUpdateDefault, func() {
		requestUpdateStop.Do(func() { close(updateStop) })
	})
	workers.SetWindowsUpdater(windowsUpdater)
	go workers.StartAll()
	if windowsUpdater != nil {
		go windowsUpdater.Run(appCtx)
	}
	go translate.RunBackground(appCtx, database)
	logStartupPhase("worker_launch", time.Since(phaseStart))

	handler := web.NewServer(database, cfg, workers, staticV)
	srv := newHTTPServer(cfg.ListenAddr, handler)

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			slog.Info("shutting down")
			cancelApp()
			if !workers.ShutdownTimeout(3 * time.Second) {
				slog.Warn("worker shutdown timed out; continuing server shutdown")
			}
			_ = srv.Shutdown(context.Background())
		})
	}
	defer shutdown()

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		defer signal.Stop(sig)
		select {
		case <-sig:
		case <-externalStop:
		case <-updateStop:
		}
		shutdown()
	}()

	phaseStart = time.Now()
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listener bind failed on %s: %w", cfg.ListenAddr, err)
	}
	if ready != nil {
		close(ready)
	}
	logStartupPhase("listener_bind", time.Since(phaseStart))

	// Use TLS if cert/key exist, otherwise plain HTTP
	tlsEnabled := tlsFilesExist(cfg)
	go serveDiscovery(appCtx, cfg, tlsEnabled)
	if tlsEnabled {
		slog.Info("listening (TLS)", "addr", cfg.ListenAddr)
		if err := srv.ServeTLS(listener, cfg.TLSCert, cfg.TLSKey); err != http.ErrServerClosed {
			return fmt.Errorf("serve TLS: %w", err)
		}
	} else {
		slog.Info("listening", "addr", cfg.ListenAddr)
		if err := srv.Serve(listener); err != http.ErrServerClosed {
			return fmt.Errorf("serve: %w", err)
		}
	}
	return nil
}

func localHealthURL(cfg *config.Config) string {
	scheme := "http"
	if tlsFilesExist(cfg) {
		scheme = "https"
	}
	_, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil || port == "" {
		port = "5001"
	}
	return scheme + "://" + net.JoinHostPort("127.0.0.1", port) + "/api/health/live"
}

func initialConfigError(cfg *config.Config, hadPendingRestore bool) error {
	if cfg == nil || cfg.ConfigError == nil {
		return nil
	}
	if hadPendingRestore {
		return nil
	}
	return cfg.ConfigError
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}
}

func setupServerLogging(cfg *config.Config) io.Closer {
	logDir := filepath.Join(cfg.Storage.StateRoot(), "logs", "server")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil
	}
	logPath := filepath.Join(logDir, "server.log")
	if fi, err := os.Stat(logPath); err == nil && fi.Size() > 5*1024*1024 {
		_ = os.Rename(logPath, logPath+".1")
	}
	lf, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	w := io.MultiWriter(os.Stderr, lf)
	log.SetOutput(w)
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})))
	return lf
}

func logStartupPhase(name string, elapsed time.Duration) {
	slog.Info("startup phase complete", "phase", name, "dur", elapsed.String(), "dur_ms", elapsed.Milliseconds())
}

func buildStaticVersionCache(staticDir string) map[string]string {
	versions := make(map[string]string)
	_ = filepath.Walk(staticDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(staticDir, path)
		rel = strings.ReplaceAll(rel, string(filepath.Separator), "/")
		versions[rel] = fmt.Sprintf("%d", info.ModTime().Unix())
		return nil
	})
	return versions
}
