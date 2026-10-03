package db

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/screwys/igloo/internal/toolenv"
)

// WithSnapshotExport reads the supplementary export and pg_dump from one snapshot.
func (db *DB) WithSnapshotExport(ctx context.Context, path string, fn func(*DB) error) (exportErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create database archive: %w", err)
	}
	defer func() {
		if exportErr != nil {
			_ = os.Remove(path)
		}
	}()
	if err := output.Close(); err != nil {
		return err
	}
	tx, err := db.conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin archive snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var snapshotID string
	if err := tx.QueryRowContext(ctx, `SELECT pg_export_snapshot()`).Scan(&snapshotID); err != nil {
		return fmt.Errorf("export archive snapshot: %w", err)
	}
	if fn != nil {
		snapshot := &DB{conn: db.conn, readTx: tx, storage: db.storage, databaseURL: db.databaseURL, postgresBin: db.postgresBin}
		if err := fn(snapshot); err != nil {
			return err
		}
	}
	command, err := db.postgresCommand(ctx, "pg_dump", "--format=custom", "--no-owner", "--no-privileges", "--snapshot="+snapshotID, "--file="+path)
	if err != nil {
		return err
	}
	if err := runPostgresCommand(command, db.databaseURL); err != nil {
		return fmt.Errorf("dump database snapshot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// ValidatePostgresArchive makes pg_restore read the complete archive before staging.
func ValidatePostgresArchive(ctx context.Context, path, postgresBin string) error {
	command := exec.CommandContext(ctx, filepath.Join(postgresBin, postgresExecutable("pg_restore")), "--no-owner", "--no-privileges", "--file=-", path)
	command.Stdout = io.Discard
	return runPostgresCommand(command, "")
}

// RestorePostgresArchive replaces public in one transaction, including tables
// that were introduced after the archive was made. Startup owns this operation.
func (db *DB) RestorePostgresArchive(ctx context.Context, path string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".igloo-restore-*.sql")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := io.WriteString(file, "DROP SCHEMA public CASCADE;\nCREATE SCHEMA public;\n"); err != nil {
		_ = file.Close()
		return err
	}
	dump, err := db.postgresCommand(ctx, "pg_restore", "--no-owner", "--no-privileges", "--file=-", path)
	if err != nil {
		_ = file.Close()
		return err
	}
	dump.Stdout = file
	if err := runPostgresCommand(dump, db.databaseURL); err != nil {
		_ = file.Close()
		return fmt.Errorf("read database archive: %w", err)
	}
	epoch, err := archiveSyncEpoch()
	if err != nil {
		_ = file.Close()
		return err
	}
	if _, err := fmt.Fprintf(file, "\nDELETE FROM public.android_sync_heads;\nDO $restore$ BEGIN\nUPDATE public.android_sync_clock SET epoch = '%s', revision = 0 WHERE id = 1;\nIF NOT FOUND THEN RAISE EXCEPTION 'Android sync clock row is missing'; END IF;\nEND $restore$;\n", epoch); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	restore, err := db.postgresCommand(ctx, "psql", "--no-psqlrc", "--set=ON_ERROR_STOP=1", "--single-transaction", "--file="+file.Name())
	if err != nil {
		return err
	}
	restore.Stdout = io.Discard
	if err := runPostgresCommand(restore, db.databaseURL); err != nil {
		return fmt.Errorf("restore database archive: %w", err)
	}
	return MigrateNativeSchema(ctx, db.conn)
}

func (db *DB) postgresCommand(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	bin := db.postgresBin
	if bin == "" {
		var err error
		bin, err = toolenv.PostgresBinDir()
		if err != nil {
			return nil, err
		}
	}
	return exec.CommandContext(ctx, filepath.Join(bin, postgresExecutable(name)), args...), nil
}

func runPostgresCommand(command *exec.Cmd, databaseURL string) error {
	if databaseURL != "" {
		environment, cleanup, err := postgresArchiveEnvironment(databaseURL)
		if err != nil {
			return err
		}
		defer cleanup()
		command.Env = environment
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if databaseURL != "" {
			message = strings.ReplaceAll(message, databaseURL, "***")
			if connection, parseErr := pgx.ParseConfig(databaseURL); parseErr == nil && connection.Password != "" {
				message = strings.ReplaceAll(message, connection.Password, "***")
			}
		}
		if message != "" {
			return fmt.Errorf("%s failed: %w: %s", filepath.Base(command.Path), err, message)
		}
		return fmt.Errorf("%s failed: %w", filepath.Base(command.Path), err)
	}
	return nil
}

func postgresArchiveEnvironment(databaseURL string) ([]string, func(), error) {
	connection, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, func() {}, fmt.Errorf("parse PostgreSQL archive connection")
	}
	parameters := make(url.Values)
	if uri, err := url.Parse(databaseURL); err == nil && (uri.Scheme == "postgres" || uri.Scheme == "postgresql") {
		parameters = uri.Query()
	}
	for _, key := range []string{"database", "service", "servicefile", "ssl", "default_query_exec_mode", "statement_cache_capacity", "description_cache_capacity"} {
		parameters.Del(key)
	}
	hosts, ports := []string{connection.Host}, []string{fmt.Sprint(connection.Port)}
	seenHosts := map[string]bool{connection.Host + ":" + fmt.Sprint(connection.Port): true}
	for _, fallback := range connection.Fallbacks {
		address := fallback.Host + ":" + fmt.Sprint(fallback.Port)
		if !seenHosts[address] {
			hosts = append(hosts, fallback.Host)
			ports = append(ports, fmt.Sprint(fallback.Port))
			seenHosts[address] = true
		}
	}
	for parameter, value := range map[string]string{"host": strings.Join(hosts, ","), "port": strings.Join(ports, ","), "dbname": connection.Database, "user": connection.User, "password": connection.Password} {
		if !parameters.Has(parameter) {
			parameters.Set(parameter, value)
		}
	}
	if !parameters.Has("connect_timeout") && connection.ConnectTimeout > 0 {
		parameters.Set("connect_timeout", fmt.Sprint(int64(connection.ConnectTimeout.Seconds())))
	}
	if !parameters.Has("sslmode") {
		mode := "disable"
		if connection.TLSConfig != nil {
			mode = "require"
			if !connection.TLSConfig.InsecureSkipVerify {
				mode = "verify-full"
			}
		}
		for _, fallback := range connection.Fallbacks {
			if fallback.Host == connection.Host && fallback.Port == connection.Port && fallback.TLSConfig == nil && connection.TLSConfig != nil {
				mode = "prefer"
			}
			if fallback.Host == connection.Host && fallback.Port == connection.Port && fallback.TLSConfig != nil && connection.TLSConfig == nil {
				mode = "allow"
			}
		}
		parameters.Set("sslmode", mode)
	}
	var runtimeOptions strings.Builder
	for key, value := range connection.RuntimeParams {
		if key == "application_name" || key == "client_encoding" || key == "options" {
			parameters.Set(key, value)
			continue
		}
		parameters.Del(key)
		runtimeOptions.WriteString(" -c ")
		runtimeOptions.WriteString(key)
		runtimeOptions.WriteByte('=')
		runtimeOptions.WriteString(strings.NewReplacer(`\`, `\\`, " ", `\ `, "\t", "\\\t", "\n", "\\\n").Replace(value))
	}
	if runtimeOptions.Len() > 0 {
		parameters.Set("options", parameters.Get("options")+runtimeOptions.String())
	}
	environment := os.Environ()
	// Connection identity and password stay in the subprocess environment.
	for parameter, variable := range map[string]string{"host": "PGHOST", "port": "PGPORT", "dbname": "PGDATABASE", "user": "PGUSER", "password": "PGPASSWORD"} {
		if parameters.Has(parameter) {
			environment = append(environment, variable+"="+parameters.Get(parameter))
			parameters.Del(parameter)
		}
	}
	// A libpq service file preserves the remaining documented connection options,
	// such as SSL certificates, without putting connection details in argv.
	service, err := os.CreateTemp("", ".igloo-postgres-archive-*.conf")
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() { _ = os.Remove(service.Name()) }
	var options strings.Builder
	options.WriteString("[igloo_archive]\n")
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&options, "%s=%s\n", key, parameters.Get(key))
	}
	if _, err := io.WriteString(service, options.String()); err != nil {
		_ = service.Close()
		cleanup()
		return nil, func() {}, err
	}
	if err := service.Close(); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	environment = append(environment, "PGSERVICEFILE="+service.Name(), "PGSERVICE=igloo_archive")
	return environment, cleanup, nil
}
