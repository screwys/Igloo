//go:build !unix && !windows

package download

import "os/exec"

func configureCommandCancellation(_ *exec.Cmd) {}
