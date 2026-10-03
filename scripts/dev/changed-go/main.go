package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
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
		return fmt.Errorf("usage: changed-go packages FILE... | nix-inputs BASE HEAD")
	}
	switch args[0] {
	case "packages":
		return packages(args[1:])
	case "nix-inputs":
		if len(args) != 3 {
			return fmt.Errorf("nix-inputs requires base and head revisions")
		}
		return nixInputs(args[1], args[2])
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

func nixInputs(base, head string) error {
	module, err := command("go", "list", "-m", "-f", "{{.Path}}")
	if err != nil {
		return err
	}
	modulePath := strings.TrimSpace(string(module))
	diff, err := command("git", "diff", "--name-status", "--no-renames", "-z", "--diff-filter=ACDM", base, head, "--", "*.go")
	if err != nil {
		return err
	}
	fields := strings.Split(strings.TrimSuffix(string(diff), "\x00"), "\x00")
	oldImports, newImports := make(map[string]bool), make(map[string]bool)
	for i := 0; i+1 < len(fields); i += 2 {
		status, path := fields[i], fields[i+1]
		var oldSource, newSource []byte
		if status != "A" {
			oldSource, err = command("git", "show", base+":"+path)
			if err != nil {
				return err
			}
		}
		if status != "D" {
			newSource, err = command("git", "show", head+":"+path)
			if err != nil {
				return err
			}
		}
		oldTags, err := sourceInputs(path, oldSource, modulePath, oldImports)
		if err != nil {
			return err
		}
		newTags, err := sourceInputs(path, newSource, modulePath, newImports)
		if err != nil {
			return err
		}
		if !slices.Equal(oldTags, newTags) {
			fmt.Println("yes")
			return nil
		}
	}
	if len(oldImports) != len(newImports) {
		fmt.Println("yes")
		return nil
	}
	for path := range oldImports {
		if !newImports[path] {
			fmt.Println("yes")
			return nil
		}
	}
	fmt.Println("no")
	return nil
}

func sourceInputs(path string, source []byte, modulePath string, imports map[string]bool) ([]string, error) {
	if source == nil {
		return nil, nil
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, err
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, err
		}
		first, _, _ := strings.Cut(path, "/")
		if strings.Contains(first, ".") && path != modulePath && !strings.HasPrefix(path, modulePath+"/") {
			imports[path] = true
		}
	}
	var tags []string
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if comment.Pos() >= file.Package || (!constraint.IsGoBuild(comment.Text) && !constraint.IsPlusBuild(comment.Text)) {
				continue
			}
			expression, err := constraint.Parse(comment.Text)
			if err != nil {
				return nil, err
			}
			tags = append(tags, expression.String())
		}
	}
	slices.Sort(tags)
	return tags, nil
}
