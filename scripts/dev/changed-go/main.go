package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: changed-go packages FILES")
	}
	switch args[0] {
	case "packages":
		return packages(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func command(name string, args ...string) ([]byte, error) {
	output, err := exec.Command(name, args...).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil, fmt.Errorf("%s: %s", name, bytes.TrimSpace(exit.Stderr))
	}
	return output, err
}

func packages(paths []string) error {
	data, err := command("go", "list", "-json", "./...")
	if err != nil {
		return err
	}
	byDir := make(map[string]string)
	reverse := make(map[string][]string)
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var pkg struct {
			Dir, ImportPath                    string
			Imports, TestImports, XTestImports []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		byDir[pkg.Dir] = pkg.ImportPath
		for _, imports := range [][]string{pkg.Imports, pkg.TestImports, pkg.XTestImports} {
			for _, dependency := range imports {
				reverse[dependency] = append(reverse[dependency], pkg.ImportPath)
			}
		}
	}
	selected := make(map[string]bool)
	var queue []string
	for _, path := range paths {
		dir, err := filepath.Abs(filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg, found := byDir[dir]
		if !found {
			fmt.Println("./...")
			return nil
		}
		selected[pkg] = true
		if !strings.HasSuffix(path, "_test.go") {
			queue = append(queue, pkg)
		}
	}
	visited := make(map[string]bool)
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if visited[pkg] {
			continue
		}
		visited[pkg] = true
		selected[pkg] = true
		queue = append(queue, reverse[pkg]...)
	}
	var result []string
	for pkg := range selected {
		result = append(result, pkg)
	}
	slices.Sort(result)
	for _, pkg := range result {
		fmt.Println(pkg)
	}
	return nil
}
