//go:build windows

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/screwys/igloo/internal/windowsuninstall"
	"golang.org/x/sys/windows/registry"
)

func main() {
	if len(os.Args) != 2 {
		fail("expected one uninstall mode")
	}
	value, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fail("invalid uninstall mode: %v", err)
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Igloo`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		fail("open installer settings: %v", err)
	}
	defer func() { _ = key.Close() }()
	roots := windowsuninstall.Roots{
		Data:   registryPath(key, "DataDirectory"),
		Media:  registryPath(key, "MediaDirectory"),
		Config: registryPath(key, "ConfigDirectory"),
	}
	if err := windowsuninstall.Cleanup(windowsuninstall.Mode(value), roots); err != nil {
		fail("remove Igloo files: %v", err)
	}
}

func registryPath(key registry.Key, name string) string {
	value, _, err := key.GetStringValue(name)
	if err != nil || strings.TrimSpace(value) == "" {
		fail("read installer setting %s: %v", name, err)
	}
	return os.ExpandEnv(value)
}

func fail(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
