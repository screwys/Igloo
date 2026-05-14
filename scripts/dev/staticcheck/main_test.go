package main

import (
	"slices"
	"testing"
)

func TestScanSourceFindsUnsafePatterns(t *testing.T) {
	src := []byte(`package sample

import (
	"context"
	"io"
	"os/exec"
	"strings"
)

func scan(ctx context.Context, resp interface{ Body() }) {
	_ = exec.Command("bash", "-c", "echo hi")
	_ = exec.CommandContext(ctx, "/bin/sh", "-c", "echo hi")
	_ = exec.Command("ffmpeg", "-i", "clip.mp4")
	_ = strings.Contains("https://x.com/example", "x.com")
	_, _ = io.Copy(nil, resp.Body())
}
`)
	findings, err := scanSource("sample.go", src)
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]string, 0, len(findings))
	for _, finding := range findings {
		ids = append(ids, finding.ID)
	}
	expected := []string{
		"igloo.shell-wrapper-command",
		"igloo.shell-wrapper-command",
		"igloo.ffmpeg-without-context",
		"igloo.url-host-substring-routing",
	}
	for _, id := range expected {
		if !slices.Contains(ids, id) {
			t.Fatalf("missing finding %s in %#v", id, ids)
		}
	}
}

func TestScanSourceFindsResponseBodyCopy(t *testing.T) {
	src := []byte(`package sample

import "io"

func scan(resp struct{ Body any }) {
	_, _ = io.Copy(nil, resp.Body)
}
`)
	findings, err := scanSource("sample.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1: %#v", len(findings), findings)
	}
	if findings[0].ID != "igloo.network-copy-without-cap" {
		t.Fatalf("got finding %s", findings[0].ID)
	}
}

func TestScanSourceSkipsHostSubstringRuleInTests(t *testing.T) {
	src := []byte(`package sample

import "strings"

func scan() bool {
	return strings.Contains("https://x.com/example", "x.com")
}
`)
	findings, err := scanSource("sample_test.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("got findings in test file: %#v", findings)
	}
}
