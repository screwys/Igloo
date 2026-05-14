package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type finding struct {
	ID       string
	Severity string
	Message  string
	Path     string
	Line     int
	Column   int
}

var shellPrograms = map[string]bool{
	"sh":        true,
	"bash":      true,
	"/bin/sh":   true,
	"/bin/bash": true,
}

var unsafeHostSubstrings = map[string]bool{
	"youtube.com":     true,
	"youtu.be":        true,
	"tiktok.com":      true,
	"tnktok.com":      true,
	"twitter.com":     true,
	"x.com":           true,
	"pbs.twimg.com":   true,
	"video.twimg.com": true,
}

func main() {
	flag.Parse()
	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	findings, err := scanRoot(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, finding := range findings {
		fmt.Fprintf(
			os.Stderr,
			"%s:%d:%d: %s %s: %s\n",
			finding.Path,
			finding.Line,
			finding.Column,
			finding.Severity,
			finding.ID,
			finding.Message,
		)
	}
	if len(findings) > 0 {
		os.Exit(1)
	}
	fmt.Println("[static] Go source checks passed")
}

func scanRoot(root string) ([]finding, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	var findings []finding
	err = filepath.WalkDir(rootAbs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if shouldSkipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		fileFindings, err := scanGoFile(path, rel)
		if err != nil {
			return err
		}
		findings = append(findings, fileFindings...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

func shouldSkipDir(rel string) bool {
	switch rel {
	case ".git", "android/.gradle", "android/app/build", "static/dist":
		return true
	default:
		return false
	}
}

func scanGoFile(path string, displayPath string) ([]finding, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return scanSource(displayPath, src)
}

func scanSource(displayPath string, src []byte) ([]finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, displayPath, src, 0)
	if err != nil {
		return nil, err
	}

	var findings []finding
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		findings = append(findings, scanCall(fset, displayPath, call)...)
		return true
	})
	return findings, nil
}

func scanCall(fset *token.FileSet, displayPath string, call *ast.CallExpr) []finding {
	pkg, name, ok := selectorName(call.Fun)
	if !ok {
		return nil
	}

	var findings []finding
	add := func(id string, severity string, message string) {
		pos := fset.Position(call.Pos())
		findings = append(findings, finding{
			ID:       id,
			Severity: severity,
			Message:  message,
			Path:     displayPath,
			Line:     pos.Line,
			Column:   pos.Column,
		})
	}

	switch {
	case pkg == "exec" && name == "Command":
		if commandUsesShell(call.Args, 0) {
			add(
				"igloo.shell-wrapper-command",
				"ERROR",
				"Do not pass user-controlled data through sh -c or bash -c. Use argv-style exec.Command/CommandContext with validated arguments.",
			)
		}
		if argString(call.Args, 0) == "ffmpeg" {
			add(
				"igloo.ffmpeg-without-context",
				"WARNING",
				"ffmpeg should run with exec.CommandContext, a timeout/cancel path, and -nostdin.",
			)
		}
	case pkg == "exec" && name == "CommandContext":
		if commandUsesShell(call.Args, 1) {
			add(
				"igloo.shell-wrapper-command",
				"ERROR",
				"Do not pass user-controlled data through sh -c or bash -c. Use argv-style exec.Command/CommandContext with validated arguments.",
			)
		}
	case !strings.HasSuffix(displayPath, "_test.go") && pkg == "strings" && name == "Contains":
		if unsafeHostSubstrings[argString(call.Args, 1)] {
			add(
				"igloo.url-host-substring-routing",
				"WARNING",
				"Do not detect platform or trust CDN hosts with substring checks. Parse the URL and allowlist the hostname.",
			)
		}
	case pkg == "io" && name == "Copy":
		if len(call.Args) >= 2 && selectorEndsWith(call.Args[1], "Body") {
			add(
				"igloo.network-copy-without-cap",
				"WARNING",
				"Copying an HTTP response body to disk should use an explicit byte cap.",
			)
		}
	}

	return findings
}

func commandUsesShell(args []ast.Expr, commandIndex int) bool {
	if len(args) <= commandIndex+1 {
		return false
	}
	return shellPrograms[argString(args, commandIndex)] && argString(args, commandIndex+1) == "-c"
}

func selectorName(expr ast.Expr) (string, string, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}
	return ident.Name, selector.Sel.Name, true
}

func selectorEndsWith(expr ast.Expr, name string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == name
}

func argString(args []ast.Expr, index int) string {
	if len(args) <= index {
		return ""
	}
	lit, ok := args[index].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return value
}
