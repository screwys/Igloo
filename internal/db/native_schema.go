package db

import (
	"context"
	"database/sql"
	"io/fs"

	"github.com/pressly/goose/v3"
	pgschema "github.com/screwys/igloo/internal/db/postgres"
)

func MigrateNativeSchema(ctx context.Context, conn *sql.DB) error {
	source, err := fs.Sub(pgschema.Migrations, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, conn, source, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}
