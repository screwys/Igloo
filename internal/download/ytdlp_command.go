package download

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ytdlp "github.com/lrstanley/go-ytdlp"
)

// runYtDlpCommand keeps the upstream result parser while the shared command
// runner owns cancellation of the CLI and its child processes.
func runYtDlpCommand(ctx context.Context, command *ytdlp.Command, rawURL string) (*ytdlp.Result, error) {
	result := CommandRunner{}.RunBuilt(ctx, command.BuildCommand(ctx, rawURL))
	out := &ytdlp.Result{
		Executable: result.Tool, Args: result.Args, ExitCode: result.ExitCode,
		Stdout: strings.TrimSpace(string(result.Stdout)), Stderr: strings.TrimSpace(string(result.Stderr)),
	}
	for _, line := range strings.Split(out.Stdout, "\n") {
		if json.Valid([]byte(line)) {
			payload := json.RawMessage(line)
			out.OutputLogs = append(out.OutputLogs, &ytdlp.ResultLog{Pipe: "stdout", Line: line, JSON: &payload})
		}
	}
	if result.Err != nil {
		return out, fmt.Errorf("%w: %s", result.Err, out.Stderr)
	}
	return out, nil
}
