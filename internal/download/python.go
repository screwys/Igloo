package download

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/screwys/igloo/internal/toolenv"
)

func pythonCommand(ctx context.Context, args ...string) *exec.Cmd {
	toolenv.ApplyCommonToolPaths()
	python := strings.TrimSpace(os.Getenv("IGLOO_PYTHON"))
	if python == "" {
		python = "python3"
		if runtime.GOOS == "windows" {
			python = "python"
		}
	}
	cmd := exec.CommandContext(ctx, python, args...)
	configureCommandCancellation(cmd)
	return cmd
}
