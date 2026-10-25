package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/model"
)

func TestBookmarksPageRendersFilters(t *testing.T) {
	p := newTestPageProps()
	categories := []db.BookmarkCategoryRow{{ID: 7, Name: "Archive"}}
	labels := []db.BookmarkLabelCountRow{
		{Label: "", IsNoLabel: true, BookmarkCount: 57},
		{Label: "cinema", BookmarkCount: 15},
	}
	var buf bytes.Buffer
	err := BookmarksPage(
		p,
		nil,
		categories,
		labels,
		[]string{"artist"},
		[]string{"friend"},
		BookmarkSelection{Label: "cinema"},
		model.Pager{Page: 1, PerPage: 200, Total: 15},
	).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("BookmarksPage render: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		`href="/bookmarks"`,
		`href="/bookmarks?category_id=7&amp;label=cinema"`,
		`data-bookmark-filters-open`,
		`data-value="empty"`,
		`data-value="label:cinema"`,
		`name="category_id"`,
		`name="posted_by"`,
		`name="tagged"`,
		`data-name="artist"`,
		`data-name="friend"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("BookmarksPage missing %s in:\n%s", want, html)
		}
	}
}

func TestBookmarksNextURLPreservesActiveFilter(t *testing.T) {
	pager := model.Pager{Page: 1, PerPage: 2, Total: 5}

	if got := bookmarksNextURL(pager, BookmarkSelection{CategoryID: 7}); got != "/bookmarks?category_id=7&page=2" {
		t.Fatalf("category next URL = %q", got)
	}
	if got := bookmarksNextURL(pager, BookmarkSelection{CategoryID: 7, Label: "cinema", PostedBy: "artist", Tagged: "friend", Query: "sketch", OldestFirst: true, SavedFrom: "2026-10-01", SavedThrough: "2026-10-20", TimeZone: "UTC"}); got != "/bookmarks?category_id=7&label=cinema&page=2&posted_by=artist&q=sketch&saved_from=2026-10-01&saved_through=2026-10-20&sort=oldest&tagged=friend&tz=UTC" {
		t.Fatalf("label next URL = %q", got)
	}
	if got := bookmarksNextURL(pager, BookmarkSelection{CategoryID: 7, IsNoLabel: true}); got != "/bookmarks?category_id=7&label_empty=1&page=2" {
		t.Fatalf("no-label next URL = %q", got)
	}
}
