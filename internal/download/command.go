package download

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// CommandRunner executes external downloader CLIs and captures redacted
// execution metadata without interpreting platform-specific output.
type CommandRunner struct{}

type CommandOptions struct {
	Timeout time.Duration
}

type CommandResult struct {
	Tool         string
	Args         []string
	RedactedArgs []string
	Stdout       []byte
	Stderr       []byte
	StartedAtMs  int64
	EndedAtMs    int64
	ElapsedMs    int64
	ExitCode     int
	Err          error
}

func (r CommandResult) CombinedOutput() []byte {
	if len(r.Stderr) == 0 {
		return append([]byte(nil), r.Stdout...)
	}
	out := make([]byte, 0, len(r.Stdout)+len(r.Stderr)+1)
	out = append(out, r.Stdout...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, r.Stderr...)
	return out
}

func (r CommandRunner) Run(ctx context.Context, tool string, args []string, opts CommandOptions) CommandResult {
	start := time.Now()
	runCtx := ctx
	cancel := func() {}
	if opts.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, tool, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	ended := time.Now()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		if runCtx.Err() != nil {
			err = runCtx.Err()
		}
	}

	return CommandResult{
		Tool:         tool,
		Args:         append([]string(nil), args...),
		RedactedArgs: RedactArgs(args),
		Stdout:       append([]byte(nil), stdout.Bytes()...),
		Stderr:       append([]byte(nil), stderr.Bytes()...),
		StartedAtMs:  start.UnixMilli(),
		EndedAtMs:    ended.UnixMilli(),
		ElapsedMs:    ended.Sub(start).Milliseconds(),
		ExitCode:     exitCode,
		Err:          err,
	}
}

func RedactArgs(args []string) []string {
	out := append([]string(nil), args...)
	secretFlags := map[string]bool{
		"--cookies":              true,
		"--cookies-from-browser": true,
		"--username":             true,
		"--password":             true,
		"--proxy":                true,
	}
	for i := 0; i < len(out); i++ {
		arg := out[i]
		for flag := range secretFlags {
			if arg == flag {
				if i+1 < len(out) {
					out[i+1] = "***"
				}
				continue
			}
			if strings.HasPrefix(arg, flag+"=") {
				out[i] = flag + "=***"
			}
		}
	}
	return out
}

func RedactText(s string) string {
	if s == "" {
		return ""
	}
	replacers := []struct {
		prefix string
	}{
		{"auth_token="},
		{"ct0="},
		{"cookies="},
		{"--cookies "},
		{"--cookies-from-browser "},
		{"password="},
		{"token="},
	}
	for _, repl := range replacers {
		for {
			idx := strings.Index(strings.ToLower(s), strings.ToLower(repl.prefix))
			if idx < 0 {
				break
			}
			start := idx + len(repl.prefix)
			end := start
			for end < len(s) {
				c := s[end]
				if c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '"' || c == '\'' || c == '&' {
					break
				}
				end++
			}
			s = s[:start] + "***" + s[end:]
		}
	}
	if len(s) > 2000 {
		return s[:2000]
	}
	return s
}
