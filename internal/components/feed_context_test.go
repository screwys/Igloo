package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestFeedContextShowsSnapshotAndHonorsNotesOptOut(t *testing.T) {
	const poll = `{"choices":[{"label":"First","count":3,"percentage":75},{"label":"Second","count":1,"percentage":25}],"total_votes":4,"ends_at":"2020-01-02T12:00:00Z","captured_at":1577962800000}`
	for _, enabled := range []bool{true, false} {
		var html bytes.Buffer
		if err := feedContext(PageProps{CommunityNotesEnabled: enabled}, poll, "Context <script>ignored()</script> https://example.test/evidence").Render(context.Background(), &html); err != nil {
			t.Fatal(err)
		}
		body := html.String()
		for _, want := range []string{"First", "75.0%", "4 votes", "Captured", "Voting was open when captured"} {
			if !strings.Contains(body, want) {
				t.Fatalf("missing %q: %s", want, body)
			}
		}
		if strings.Contains(body, "Closed when captured") || strings.Contains(body, "<script>") || strings.Contains(body, "<button") {
			t.Fatalf("unsafe or misleading snapshot: %s", body)
		}
		if strings.Contains(body, "Community Note") != enabled || strings.Contains(body, "https://example.test/evidence") != enabled {
			t.Fatalf("note opt-out ignored: %s", body)
		}
	}
}
