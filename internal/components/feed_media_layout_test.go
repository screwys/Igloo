package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestFeedMediaStripLayout(t *testing.T) {
	photo := func(w, h int) model.MediaRef { return model.MediaRef{Type: "photo", Width: w, Height: h} }
	for _, tt := range []struct {
		name    string
		media   []model.MediaRef
		missing bool
		wantRow bool
	}{
		{"unequal three strips", []model.MediaRef{photo(300, 1000), photo(550, 1000), photo(650, 1000)}, false, true},
		{"three strips beyond 3:2", []model.MediaRef{photo(420, 1000), photo(420, 1000), photo(420, 1000)}, false, true},
		{"four strips", []model.MediaRef{photo(375, 1000), photo(375, 1000), photo(375, 1000), photo(375, 1000)}, false, true},
		{"rounding", []model.MediaRef{photo(375, 999), photo(374, 1000), photo(375, 1000), photo(375, 1000)}, false, true},
		{"ordinary portraits", []model.MediaRef{photo(750, 1000), photo(750, 1000), photo(750, 1000)}, false, true},
		{"four ordinary portraits", []model.MediaRef{photo(750, 1000), photo(750, 1000), photo(750, 1000), photo(750, 1000)}, false, true},
		{"different heights", []model.MediaRef{photo(300, 1000), photo(300, 500), photo(600, 1000)}, false, true},
		{"unknown dimensions", []model.MediaRef{photo(0, 0), photo(550, 1000), photo(650, 1000)}, false, true},
		{"missing asset", []model.MediaRef{photo(300, 1000), photo(550, 1000), photo(650, 1000)}, true, true},
		{"mixed video", []model.MediaRef{photo(300, 1000), photo(550, 1000), {Type: "video", Width: 650, Height: 1000}}, false, true},
		{"two images", []model.MediaRef{photo(750, 1000), photo(750, 1000)}, false, true},
		{"wide middle panel", []model.MediaRef{photo(300, 1000), photo(900, 1000), photo(300, 1000)}, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			urls := make([]string, len(tt.media))
			for i := range urls {
				urls[i] = "/media/sample.jpg"
			}
			if tt.missing {
				urls[0] = ""
			}
			item := model.FeedItem{Media: tt.media, MediaSlideURLs: urls, QuoteMedia: tt.media, QuoteMediaSlideURLs: urls}
			for _, quoted := range []bool{false, true} {
				var out bytes.Buffer
				component := feedMedia(PageProps{}, item, false)
				if quoted {
					component = feedQuoteMedia(PageProps{}, item, len(tt.media), false)
				}
				if err := component.Render(context.Background(), &out); err != nil {
					t.Fatal(err)
				}
				if got := strings.Contains(out.String(), "feed-media-row"); got != tt.wantRow {
					t.Fatalf("quoted=%v row=%v want=%v: %s", quoted, got, tt.wantRow, out.String())
				}
				if tt.name == "unequal three strips" && !strings.Contains(out.String(), "minmax(0, 0.300000fr) minmax(0, 0.550000fr) minmax(0, 0.650000fr)") {
					t.Fatalf("missing proportional columns: %s", out.String())
				}
			}
		})
	}
}
