package components

import (
	"os"
	"strings"
	"testing"
)

func TestWebShareHelperIncludesInstagramEmbedMirror(t *testing.T) {
	srcBytes, err := os.ReadFile("../../static/js/src/utils.js")
	if err != nil {
		t.Fatalf("read utils source: %v", err)
	}
	src := string(srcBytes)
	for _, want := range []string{
		"vxinstagram.com",
		"host === 'instagram.com'",
		"host.endsWith('.instagram.com')",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("utils.js missing Instagram embed-friendly mapping fragment %q", want)
		}
	}
}

func TestWebShareHelperFallsBackWhenAsyncClipboardIsRejected(t *testing.T) {
	for _, path := range []string{
		"../../static/js/src/utils.js",
		"../../static/js/site_base.js",
	} {
		srcBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(srcBytes)
		for _, want := range []string{
			".writeText(value).catch",
			"execCommand('copy')",
		} {
			if !strings.Contains(src, want) {
				t.Fatalf("%s missing clipboard fallback fragment %q", path, want)
			}
		}
	}
}
