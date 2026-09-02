package components

import (
	"os"
	"strings"
	"testing"
)

func TestFeedThreeMediaItemsRenderAsOneUncroppedRow(t *testing.T) {
	cssBytes, err := os.ReadFile("../../static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)

	rowRule := cssRuleBody(t, css, ".feed-media-grid.count-3")
	for _, want := range []string{"display: flex;", "align-items: stretch;"} {
		if !strings.Contains(rowRule, want) {
			t.Fatalf("three-media row missing %q; rule=%s", want, rowRule)
		}
	}

	tileRule := cssRuleBody(t, css, ".feed-media-grid.count-3 .feed-media-tile")
	if !strings.Contains(tileRule, "flex: 1 1 0;") {
		t.Fatalf("three-media tiles should share one row equally; rule=%s", tileRule)
	}

	imageRule := cssRuleBody(t, css, ".feed-media-grid.count-3 .feed-media-tile .feed-media-image")
	for _, want := range []string{"height: auto;", "object-fit: contain;"} {
		if !strings.Contains(imageRule, want) {
			t.Fatalf("three-media image missing %q; rule=%s", want, imageRule)
		}
	}

	if strings.Contains(css, ".feed-media-grid.count-3 .feed-media-tile:first-child") {
		t.Fatal("three-media row should not give the first image a spanning tile")
	}
}
