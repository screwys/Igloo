package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/screwys/igloo/internal/config"
	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/toolenv"
)

func migrateSQLite() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	toolenv.ApplyCommonToolPaths()
	cfg := config.Load()
	if cfg.ConfigError != nil {
		return cfg.ConfigError
	}
	if _, err := os.Stat(cfg.Storage.DatabasePath()); os.IsNotExist(err) {
		if err := cfg.Storage.Ensure(); err != nil {
			return err
		}
		fmt.Println("No SQLite database to migrate.")
		return nil
	} else if err != nil {
		return fmt.Errorf("open SQLite migration source: %w", err)
	}
	store, err := db.OpenForRestore(cfg.Storage, db.OpenOptions{DatabaseURL: cfg.DatabaseURL})
	if err != nil {
		return err
	}
	fmt.Println("Migrating SQLite database to PostgreSQL...")
	if err := store.RestoreLegacyArchive(ctx, cfg.Storage.DatabasePath()); err != nil {
		return errors.Join(err, store.Close())
	}
	backup, err := store.RetainLegacyDatabase(ctx)
	if err != nil {
		return errors.Join(err, store.Close())
	}
	if err := store.Close(); err != nil {
		return err
	}
	fmt.Println("SQLite migration complete. Original database retained:", backup)
	return nil
}
