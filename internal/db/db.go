// Package db stores Igloo content, user state, and work queues in PostgreSQL.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/screwys/igloo/internal/storage"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	dbquery "github.com/screwys/igloo/internal/db/query"
	_ "modernc.org/sqlite" // Legacy archive import.
)

type PhaseFunc func(name string, elapsed time.Duration)

type OpenOptions struct {
	Phase       PhaseFunc
	DatabaseURL string
}

type EnsureSchemaOptions struct {
	Phase PhaseFunc
}

const databaseMaxOpenConnections = 8

type DB struct {
	conn        *sql.DB
	readTx      *sql.Tx
	mu          sync.Mutex // serialize writes
	storage     storage.Layout
	databaseURL string
	postgresBin string
	postgres    *postgresRuntime
}

type sqlReader interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (db *DB) reader() sqlReader {
	if db.readTx != nil {
		return postgresReader{db.readTx}
	}
	return postgresReader{db.conn}
}

func (db *DB) queries() *dbquery.Queries {
	if db.readTx != nil {
		return dbquery.New(db.readTx)
	}
	return dbquery.New(db.conn)
}

type postgresReader struct{ sqlReader }

func bind(query string) string { return sqlx.Rebind(sqlx.DOLLAR, query) }

func (r postgresReader) Query(query string, args ...any) (*sql.Rows, error) {
	return r.sqlReader.Query(bind(query), args...)
}
func (r postgresReader) QueryRow(query string, args ...any) *sql.Row {
	return r.sqlReader.QueryRow(bind(query), args...)
}
func (r postgresReader) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return r.sqlReader.QueryContext(ctx, bind(query), args...)
}
func (r postgresReader) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return r.sqlReader.QueryRowContext(ctx, bind(query), args...)
}

func (db *DB) WithReadSnapshot(fn func(*DB) error) error {
	tx, err := db.conn.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	snapshot := &DB{
		conn: db.conn, readTx: tx, storage: db.storage, databaseURL: db.databaseURL, postgresBin: db.postgresBin,
	}
	if err := fn(snapshot); err != nil {
		return err
	}
	return tx.Commit()
}

// Open opens the configured server database.
func Open(layout storage.Layout) (*DB, error) {
	return OpenWithOptions(layout, OpenOptions{})
}

// OpenWithOptions opens PostgreSQL with optional startup phase reporting.
func OpenWithOptions(layout storage.Layout, opts OpenOptions) (*DB, error) {
	if err := layout.Ensure(); err != nil {
		return nil, fmt.Errorf("validate storage layout: %w", err)
	}
	return openPostgres(layout, opts, false)
}

// OpenForRestore opens the target of an explicit database replacement.
func OpenForRestore(layout storage.Layout, opts OpenOptions) (*DB, error) {
	if err := layout.Ensure(); err != nil {
		return nil, fmt.Errorf("validate storage layout: %w", err)
	}
	return openPostgres(layout, opts, true)
}

// OpenAtStateRoot opens an isolated PostgreSQL store for maintenance or fixtures.
func OpenAtStateRoot(stateRoot string) (*DB, error) {
	layout, err := storage.New(stateRoot, "")
	if err != nil {
		return nil, err
	}
	return openPostgres(layout, OpenOptions{}, false)
}

func openPostgres(layout storage.Layout, opts OpenOptions, replacingDatabase bool) (*DB, error) {
	if !replacingDatabase {
		if _, err := os.Stat(layout.DatabasePath()); err == nil {
			return nil, fmt.Errorf("SQLite migration required: stop Igloo and run 'igloo migrate-sqlite'")
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("check legacy database: %w", err)
		}
	}
	totalStart := time.Now()

	phaseStart := time.Now()
	dsn := opts.DatabaseURL
	if dsn == "" {
		dsn = os.Getenv("IGLOO_DATABASE_URL")
	}
	var managed *postgresRuntime
	var err error
	if dsn == "" {
		managed, dsn, err = startPostgres(context.Background(), layout)
	}
	if err != nil {
		return nil, err
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		if managed != nil {
			_ = managed.stop()
		}
		return nil, fmt.Errorf("parse database connection: %w", err)
	}
	conn := stdlib.OpenDB(*config)
	reportPhase(opts.Phase, "db.sql_open", phaseStart)
	conn.SetMaxOpenConns(databaseMaxOpenConnections)
	conn.SetMaxIdleConns(databaseMaxOpenConnections)

	phaseStart = time.Now()
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		if managed != nil {
			_ = managed.stop()
		}
		reportPhase(opts.Phase, "db.ping", phaseStart)
		return nil, fmt.Errorf("ping db: %w", err)
	}
	reportPhase(opts.Phase, "db.ping", phaseStart)

	bin := ""
	if managed != nil {
		bin = managed.bin
	}
	d := &DB{conn: conn, storage: layout, databaseURL: dsn, postgresBin: bin, postgres: managed}
	phaseStart = time.Now()
	err = MigrateNativeSchema(context.Background(), conn)
	if err != nil {
		_ = conn.Close()
		if managed != nil {
			_ = managed.stop()
		}
		reportPhase(opts.Phase, "db.ensure_schema", phaseStart)
		return nil, fmt.Errorf("ensure schema: %w", err)
	}
	reportPhase(opts.Phase, "db.ensure_schema", phaseStart)

	reportPhase(opts.Phase, "db.open_total", totalStart)
	return d, nil
}

// OpenReadOnlyAtStateRoot attaches a read-only connection to the state root.
func OpenReadOnlyAtStateRoot(stateRoot string) (*DB, error) {
	layout, err := storage.New(stateRoot, "")
	if err != nil {
		return nil, err
	}
	return OpenReadOnlyLayout(layout)
}

func OpenReadOnlyLayout(layout storage.Layout) (*DB, error) {
	dsn, err := readPostgresURL(layout.StateRoot())
	if err != nil {
		return nil, fmt.Errorf("open db readonly: %w", err)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.RuntimeParams["default_transaction_read_only"] = "on"
	conn := stdlib.OpenDB(*config)
	conn.SetMaxOpenConns(databaseMaxOpenConnections)
	conn.SetMaxIdleConns(databaseMaxOpenConnections)
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &DB{conn: conn, storage: layout, databaseURL: dsn}, nil
}

// OpenExisting attaches maintenance tools to the configured running database.
func OpenExisting(layout storage.Layout) (*DB, error) {
	dsn, err := readPostgresURL(layout.StateRoot())
	if err != nil {
		return nil, err
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database connection")
	}
	conn := stdlib.OpenDB(*config)
	conn.SetMaxOpenConns(databaseMaxOpenConnections)
	conn.SetMaxIdleConns(databaseMaxOpenConnections)
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &DB{conn: conn, storage: layout, databaseURL: dsn}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	err := db.conn.Close()
	if db.postgres != nil {
		if stopErr := db.postgres.stop(); err == nil {
			err = stopErr
		}
	}
	return err
}

// WithRead executes a read function against the database connection.
func (db *DB) WithRead(fn func(conn *sql.DB) error) error {
	if db.readTx != nil {
		return fmt.Errorf("direct connection read unavailable inside read snapshot")
	}
	return fn(db.conn)
}

// ExecRaw executes native PostgreSQL SQL without changing its placeholders.
func (db *DB) ExecRaw(query string, args ...any) error {
	_, err := db.conn.Exec(query, args...)
	return err
}

// QueryRow reads one row from native PostgreSQL SQL without changing it.
func (db *DB) QueryRow(query string, args ...any) *sql.Row {
	if db.readTx != nil {
		return db.readTx.QueryRow(query, args...)
	}
	return db.conn.QueryRow(query, args...)
}

// WithWrite executes a write function inside a transaction with mutex.
func (db *DB) WithWrite(fn func(tx *sql.Tx) error) error {
	if db.readTx != nil {
		return fmt.Errorf("write transaction unavailable inside read snapshot")
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func reportPhase(cb PhaseFunc, name string, started time.Time) {
	if cb != nil {
		cb(name, time.Since(started))
	}
}
