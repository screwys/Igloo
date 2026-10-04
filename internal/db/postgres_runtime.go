package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/screwys/igloo/internal/storage"
	"github.com/screwys/igloo/internal/toolenv"
)

type postgresRuntime struct {
	bin, cluster, socket, connectionFile string
	owned                                bool
}

type postgresConnection struct {
	URL      string `json:"url"`
	OwnerPID int    `json:"owner_pid"`
}

func startPostgres(ctx context.Context, layout storage.Layout) (*postgresRuntime, string, error) {
	bin, err := toolenv.PostgresBinDir()
	if err != nil {
		return nil, "", err
	}
	r := &postgresRuntime{bin: bin, cluster: filepath.Join(layout.StateRoot(), "postgresql"), connectionFile: filepath.Join(layout.StateRoot(), ".postgresql-connection.json")}
	if err := os.MkdirAll(r.cluster, 0700); err != nil {
		return nil, "", err
	}
	passwordPath := filepath.Join(layout.StateRoot(), ".postgresql-password")
	passwordBytes, err := os.ReadFile(passwordPath)
	if os.IsNotExist(err) {
		passwordBytes = []byte(NewRandomID())
		err = os.WriteFile(passwordPath, passwordBytes, 0600)
	}
	if err != nil {
		return nil, "", err
	}
	identity, err := postgresProcessIdentity(r.cluster)
	if err != nil {
		return nil, "", err
	}
	if err := setPostgresDirectoryOwner(passwordPath); err != nil {
		return nil, "", err
	}
	run := func(name string, args ...string) error {
		cmd := exec.CommandContext(ctx, filepath.Join(bin, postgresExecutable(name)), args...)
		identity(cmd)
		var out []byte
		var err error
		if runtime.GOOS == "windows" && name == "pg_ctl" && args[len(args)-1] == "start" {
			// The Windows command shell keeps inherited output handles until PostgreSQL stops.
			// Write directly to a file so waiting for pg_ctl does not wait for that shell.
			outputPath := filepath.Join(r.cluster, "pg_ctl.log")
			output, openErr := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if openErr != nil {
				return openErr
			}
			cmd.Stdout, cmd.Stderr = output, output
			err = cmd.Run()
			closeErr := output.Close()
			if err != nil {
				out, _ = os.ReadFile(outputPath)
			} else {
				err = closeErr
			}
		} else {
			out, err = cmd.CombinedOutput()
		}
		if err != nil {
			return fmt.Errorf("postgres %s: %w: %s", name, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if _, err := os.Stat(filepath.Join(r.cluster, "PG_VERSION")); os.IsNotExist(err) {
		args := []string{"-D", r.cluster, "--username=igloo", "--encoding=UTF8", "--locale=C", "--auth-local=trust", "--auth-host=scram-sha-256", "--pwfile=" + passwordPath}
		if err := run("initdb", args...); err != nil {
			return nil, "", err
		}
	} else if err != nil {
		return nil, "", err
	}
	u := &url.URL{Scheme: "postgres", User: url.User("igloo"), Path: "/igloo"}
	q := u.Query()
	q.Set("sslmode", "disable")
	statusErr := run("pg_ctl", "-D", r.cluster, "status")
	if statusErr == nil {
		connection, err := readPostgresConnection(layout.StateRoot())
		if err != nil {
			return nil, "", err
		}
		if postgresOwnerAlive(connection.OwnerPID) {
			return nil, "", fmt.Errorf("managed PostgreSQL is owned by a running Igloo process")
		}
		u, err = url.Parse(connection.URL)
		if err != nil {
			return nil, "", err
		}
		r.socket = u.Query().Get("host")
	} else {
		var exitError *exec.ExitError
		if !errors.As(statusErr, &exitError) || exitError.ExitCode() != 3 {
			return nil, "", statusErr
		}
		options := ""
		if runtime.GOOS == "windows" {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return nil, "", err
			}
			port := listener.Addr().(*net.TCPAddr).Port
			if err := listener.Close(); err != nil {
				return nil, "", err
			}
			options = "-h 127.0.0.1 -p " + strconv.Itoa(port)
			u.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			u.User = url.UserPassword("igloo", string(passwordBytes))
		} else {
			socket, err := os.MkdirTemp("", "igloo-postgres-")
			if err != nil {
				return nil, "", err
			}
			r.socket = socket
			if err := setPostgresDirectoryOwner(socket); err != nil {
				return nil, "", errors.Join(err, os.RemoveAll(socket))
			}
			q.Set("host", socket)
			q.Set("port", "5432")
			options = "-c listen_addresses='' -k " + postgresOptionQuote(socket)
		}
		u.RawQuery = q.Encode()
		if err := r.writeConnection(u.String()); err != nil {
			return nil, "", errors.Join(err, os.RemoveAll(r.socket))
		}
		if err := run("pg_ctl", "-D", r.cluster, "-l", filepath.Join(r.cluster, "server.log"), "-o", options, "-w", "start"); err != nil {
			return nil, "", errors.Join(err, os.RemoveAll(r.socket))
		}
	}
	r.owned = true
	// Create the application database through the same private server connection.
	adminURL := *u
	adminURL.Path = "/postgres"
	admin, err := sql.Open("pgx", adminURL.String())
	if err != nil {
		return nil, "", errors.Join(err, r.stop())
	}
	var present bool
	err = admin.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='igloo')").Scan(&present)
	if err == nil && !present {
		_, err = admin.ExecContext(ctx, "CREATE DATABASE igloo")
	}
	closeErr := admin.Close()
	if err != nil {
		return nil, "", fmt.Errorf("prepare PostgreSQL database: %w", errors.Join(err, closeErr, r.stop()))
	}
	if closeErr != nil {
		return nil, "", errors.Join(closeErr, r.stop())
	}
	connectionURL := u.String()
	if err := r.writeConnection(connectionURL); err != nil {
		return nil, "", errors.Join(err, r.stop())
	}
	return r, connectionURL, nil
}

func (r *postgresRuntime) writeConnection(connectionURL string) error {
	encoded, err := json.Marshal(postgresConnection{URL: connectionURL, OwnerPID: os.Getpid()})
	if err != nil {
		return err
	}
	return os.WriteFile(r.connectionFile, encoded, 0600)
}

func (r *postgresRuntime) stop() error {
	if !r.owned {
		return nil
	}
	cmd := exec.Command(filepath.Join(r.bin, postgresExecutable("pg_ctl")), "-D", r.cluster, "-m", "fast", "-w", "stop")
	identity, err := postgresProcessIdentity(r.cluster)
	if err != nil {
		return err
	}
	identity(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stop PostgreSQL: %w: %s", err, strings.TrimSpace(string(out)))
	}
	r.owned = false
	if err := os.Remove(r.connectionFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.RemoveAll(r.socket)
}

func postgresExecutable(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func postgresOptionQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func readPostgresURL(stateRoot string) (string, error) {
	if configured := strings.TrimSpace(os.Getenv("IGLOO_DATABASE_URL")); configured != "" {
		return configured, nil
	}
	connection, err := readPostgresConnection(stateRoot)
	return connection.URL, err
}

func readPostgresConnection(stateRoot string) (postgresConnection, error) {
	b, err := os.ReadFile(filepath.Join(stateRoot, ".postgresql-connection.json"))
	if err != nil {
		return postgresConnection{}, fmt.Errorf("read running PostgreSQL connection: %w", err)
	}
	var connection postgresConnection
	if err := json.Unmarshal(b, &connection); err != nil {
		return postgresConnection{}, err
	}
	return connection, nil
}
