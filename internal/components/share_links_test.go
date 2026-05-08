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
