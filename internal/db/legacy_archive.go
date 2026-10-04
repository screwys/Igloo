package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/screwys/igloo/internal/config"
	"github.com/screwys/igloo/internal/storage"
	"modernc.org/sqlite"
)

// PrepareLegacyArchive normalizes a disposable copy. The supplied SQLite
// archive stays read-only, including during its ordered schema migrations.
func PrepareLegacyArchive(ctx context.Context, path string) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", func() {}, err
	}
	copyFile, err := os.CreateTemp(filepath.Dir(path), ".igloo-legacy-*.db")
	if err != nil {
		return "", func() {}, err
	}
	copyPath := copyFile.Name()
	cleanup := func() { _ = os.Remove(copyPath) }
	if err := copyFile.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := snapshotLegacyDatabase(ctx, path, copyPath); err != nil {
		cleanup()
		return "", func() {}, err
	}
	conn, err := sql.Open("sqlite", legacyArchiveDSN(copyPath, false))
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	conn.SetMaxOpenConns(1)
	if err := checkLegacyArchiveIntegrity(ctx, conn); err != nil {
		_ = conn.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := ApplySchemaMigrations(conn); err != nil {
		_ = conn.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := ValidateCurrentSchema(conn); err != nil {
		_ = conn.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := conn.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return copyPath, cleanup, nil
}

func snapshotLegacyDatabase(ctx context.Context, sourcePath, destinationPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := sql.Open("sqlite", legacyArchiveDSN(sourcePath, true))
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	connection, err := source.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	if err := connection.Raw(func(driverConn any) error {
		starter := driverConn.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		})
		backup, err := starter.NewBackup(legacyArchiveDSN(destinationPath, false))
		if err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				return errors.Join(err, backup.Finish())
			}
			more, err := backup.Step(256)
			if err != nil {
				return errors.Join(err, backup.Finish())
			}
			if !more {
				return backup.Finish()
			}
		}
	}); err != nil {
		return err
	}
	// The retained copy is one file, even when the source uses WAL mode.
	destination, err := sql.Open("sqlite", legacyArchiveDSN(destinationPath, false))
	if err != nil {
		return err
	}
	return errors.Join(destination.PingContext(ctx), destination.Close())
}

// RetainLegacyDatabase keeps a complete SQLite backup before retiring its live
// filename. Callers invoke it only after PostgreSQL replacement has committed.
func (db *DB) RetainLegacyDatabase(ctx context.Context) (string, error) {
	legacyPath := filepath.Join(db.storage.StateRoot(), config.DatabaseFilename)
	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(db.storage.StateRoot(), ".igloo-retained-sqlite-*.db")
	if err != nil {
		return "", err
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := snapshotLegacyDatabase(ctx, legacyPath, temporaryPath); err != nil {
		return "", fmt.Errorf("snapshot legacy SQLite database: %w", err)
	}
	file, err = os.OpenFile(temporaryPath, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return "", err
	}
	backupName := "igloo.sqlite-backup-" + time.Now().UTC().Format("20060102T150405.000000000") + ".db"
	backupPath := filepath.Join(db.storage.StateRoot(), backupName)
	if err := os.Rename(temporaryPath, backupPath); err != nil {
		return "", err
	}
	if err := storage.SyncDirectory(db.storage.StateRoot()); err != nil {
		return backupPath, err
	}
	if err := os.Remove(legacyPath); err != nil {
		return backupPath, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(legacyPath + suffix); err != nil && !os.IsNotExist(err) {
			return backupPath, err
		}
	}
	return backupPath, storage.SyncDirectory(db.storage.StateRoot())
}

func legacyArchiveDSN(path string, readOnly bool) string {
	uri := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := url.Values{"_pragma": {"foreign_keys(on)", "busy_timeout(30000)"}}
	if readOnly {
		query.Set("mode", "ro")
		query.Add("_pragma", "query_only(1)")
	} else {
		query.Set("mode", "rw")
		query.Add("_pragma", "journal_mode(delete)")
	}
	uri.RawQuery = query.Encode()
	return uri.String()
}

func checkLegacyArchiveIntegrity(ctx context.Context, conn *sql.DB) error {
	var result string
	if err := conn.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("SQLite archive integrity check failed: %s", result)
	}
	rows, err := conn.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		return fmt.Errorf("SQLite archive foreign key check failed")
	}
	return rows.Err()
}

// RestoreLegacyArchive copies every logical table, preserving IDs and relations.
// SQLite search indexes are replaced by PostgreSQL's native search documents.
func (db *DB) RestoreLegacyArchive(ctx context.Context, path string) error {
	normalized, cleanup, err := PrepareLegacyArchive(ctx, path)
	if err != nil {
		return fmt.Errorf("prepare SQLite archive: %w", err)
	}
	defer cleanup()
	source, err := sql.Open("sqlite", legacyArchiveDSN(normalized, true))
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	source.SetMaxOpenConns(1)
	db.mu.Lock()
	defer db.mu.Unlock()
	target, err := db.conn.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = target.Close() }()
	tx, err := target.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	tables, err := legacyArchiveTables(ctx, source, tx)
	if err != nil {
		return err
	}
	quoted := make([]string, len(tables))
	for i, table := range tables {
		quoted[i] = quoteArchiveIdentifier(table)
	}
	quoted = append(quoted, quoteArchiveIdentifier("tiktok_lives"))
	if _, err := tx.ExecContext(ctx, "TRUNCATE "+strings.Join(quoted, ",")+" RESTART IDENTITY"); err != nil {
		return fmt.Errorf("clear restored tables: %w", err)
	}
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE "+quoteArchiveIdentifier(table)+" DISABLE TRIGGER USER"); err != nil {
			return err
		}
	}
	for _, table := range tables {
		// Restored clients bootstrap under a new epoch; old heads are discarded.
		if table == "android_sync_heads" {
			continue
		}
		if err := copyLegacyArchiveTable(ctx, source, target, table); err != nil {
			return fmt.Errorf("restore SQLite table %s: %w", table, err)
		}
	}
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE "+quoteArchiveIdentifier(table)+" ENABLE TRIGGER USER"); err != nil {
			return err
		}
	}
	for _, table := range []string{"channels", "videos"} {
		if _, err := tx.ExecContext(ctx, "UPDATE "+quoteArchiveIdentifier(table)+" SET search_document = search_document"); err != nil {
			return fmt.Errorf("rebuild native search: %w", err)
		}
	}
	if err := resetArchiveSequences(ctx, source, tx); err != nil {
		return err
	}
	if err := resetArchiveSyncIdentity(tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "ANALYZE"); err != nil {
		return fmt.Errorf("analyze restored database: %w", err)
	}
	return tx.Commit()
}

func legacyArchiveTables(ctx context.Context, source *sql.DB, target *sql.Tx) ([]string, error) {
	rows, err := target.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if _, recognized := schemaTableLifecycles[table]; recognized && !strings.HasPrefix(table, "search_") && table != "goose_db_version" {
			tables = append(tables, table)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	targetTables := make(map[string]bool, len(tables))
	for _, table := range tables {
		targetTables[table] = true
	}
	legacyTables, err := source.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	for legacyTables.Next() {
		var table string
		if err := legacyTables.Scan(&table); err != nil {
			_ = legacyTables.Close()
			return nil, err
		}
		if _, recognized := schemaTableLifecycles[table]; recognized && !strings.HasPrefix(table, "search_") && table != "goose_db_version" && !targetTables[table] {
			_ = legacyTables.Close()
			return nil, fmt.Errorf("PostgreSQL schema missing logical table %s", table)
		}
	}
	if err := errors.Join(legacyTables.Err(), legacyTables.Close()); err != nil {
		return nil, err
	}
	for _, table := range tables {
		var exists bool
		if err := source.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = ?)`, table).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("SQLite archive missing logical table %s", table)
		}
	}
	dependencies, err := target.QueryContext(ctx, `
		SELECT child.relname, parent.relname FROM pg_constraint fk
		JOIN pg_class child ON child.oid = fk.conrelid
		JOIN pg_class parent ON parent.oid = fk.confrelid
		JOIN pg_namespace ns ON ns.oid = child.relnamespace
		WHERE fk.contype = 'f' AND ns.nspname = 'public'`)
	if err != nil {
		return nil, err
	}
	parents := make(map[string][]string)
	for dependencies.Next() {
		var table, parent string
		if err := dependencies.Scan(&table, &parent); err != nil {
			_ = dependencies.Close()
			return nil, err
		}
		parents[table] = append(parents[table], parent)
	}
	if err := errors.Join(dependencies.Err(), dependencies.Close()); err != nil {
		return nil, err
	}
	var ordered []string
	remaining := append([]string(nil), tables...)
	inserted := make(map[string]bool)
	for len(remaining) > 0 {
		progress := false
		for i := 0; i < len(remaining); {
			table := remaining[i]
			ready := true
			for _, parent := range parents[table] {
				if parent != table && !inserted[parent] {
					ready = false
					break
				}
			}
			if !ready {
				i++
				continue
			}
			ordered = append(ordered, table)
			inserted[table] = true
			remaining = append(remaining[:i], remaining[i+1:]...)
			progress = true
		}
		if !progress {
			return nil, fmt.Errorf("cannot order restored table dependencies: %s", strings.Join(remaining, ", "))
		}
	}
	return ordered, nil
}

func copyLegacyArchiveTable(ctx context.Context, source *sql.DB, target *sql.Conn, table string) error {
	selection := "*"
	if table == "feed_items" {
		selection = "rowid AS ingest_order, *"
	}
	rows, err := source.QueryContext(ctx, "SELECT "+selection+" FROM "+quoteArchiveIdentifier(table))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	values, pointers := make([]any, len(columns)), make([]any, len(columns))
	for i := range columns {
		pointers[i] = &values[i]
	}
	err = target.Raw(func(driverConn any) error {
		connection := driverConn.(*stdlib.Conn).Conn()
		_, err := connection.CopyFrom(ctx, pgx.Identifier{"public", table}, columns, pgx.CopyFromFunc(func() ([]any, error) {
			if !rows.Next() {
				return nil, rows.Err()
			}
			if err := rows.Scan(pointers...); err != nil {
				return nil, err
			}
			for i, value := range values {
				switch value := value.(type) {
				case string:
					values[i] = strings.ReplaceAll(value, "\x00", "")
				case []byte:
					values[i] = strings.ReplaceAll(string(value), "\x00", "")
				}
			}
			return values, nil
		}))
		return err
	})
	return errors.Join(err, rows.Err())
}

func resetArchiveSequences(ctx context.Context, source *sql.DB, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT table_name, column_name, pg_get_serial_sequence(format('%I.%I', table_schema, table_name), column_name) FROM information_schema.columns WHERE table_schema = 'public' AND is_identity = 'YES' AND table_name != 'goose_db_version' ORDER BY table_name, ordinal_position`)
	if err != nil {
		return err
	}
	type identityColumn struct{ table, column, sequence string }
	var columns []identityColumn
	for rows.Next() {
		var column identityColumn
		if err := rows.Scan(&column.table, &column.column, &column.sequence); err != nil {
			_ = rows.Close()
			return err
		}
		columns = append(columns, column)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, column := range columns {
		var highWater int64
		if err := tx.QueryRowContext(ctx, "SELECT GREATEST(COALESCE(MAX("+quoteArchiveIdentifier(column.column)+"), 0), 0) FROM "+quoteArchiveIdentifier(column.table)).Scan(&highWater); err != nil {
			return err
		}
		var legacyHighWater int64
		if err := source.QueryRowContext(ctx, `SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name = ?), 0)`, column.table).Scan(&legacyHighWater); err != nil {
			return err
		}
		if legacyHighWater > highWater {
			highWater = legacyHighWater
		}
		// Sequence names come from PostgreSQL's catalog, rather than the archive.
		parts := strings.Split(column.sequence, ".")
		for i := range parts {
			parts[i] = quoteArchiveIdentifier(strings.Trim(parts[i], `"`))
		}
		start := highWater
		if start == 0 {
			start = 1
		}
		if _, err := tx.ExecContext(ctx, "ALTER SEQUENCE "+strings.Join(parts, ".")+fmt.Sprintf(" RESTART WITH %d", start)); err != nil {
			return err
		}
		if highWater > 0 {
			var consumed int64
			if err := tx.QueryRowContext(ctx, `SELECT nextval($1::regclass)`, column.sequence).Scan(&consumed); err != nil {
				return err
			}
		}
	}
	return nil
}

func resetArchiveSyncIdentity(tx *sql.Tx) error {
	epoch, err := archiveSyncEpoch()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM android_sync_heads`); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE android_sync_clock SET epoch = $1, revision = 0 WHERE id = 1`, epoch)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("android sync clock row is missing")
	}
	return nil
}

func archiveSyncEpoch() (string, error) {
	var epoch [16]byte
	if _, err := rand.Read(epoch[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(epoch[:]), nil
}

func quoteArchiveIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
