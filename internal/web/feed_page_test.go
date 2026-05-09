package web

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/db"
)

func TestHandlePageFeedResetsRankCursorWhenSnapshotChanges(t *testing.T) {
	srv := newTestServer(t)
	user := "alice"
	now := time.Now().UnixMilli()

	for i, id := range []string{"t1", "t2", "t3", "t4", "t5"} {
		if err := srv.db.ExecRaw(`INSERT INTO feed_items
			(tweet_id, author_handle, body_text, published_at, algo_interest, algo_scored_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			id, "author_"+id, "body "+id, now-int64(i), 1.0, 1); err != nil {
			t.Fatal(err)
		}
	}

	if err := srv.db.ReplaceFeedRankSnapshot(user, []db.SnapshotRow{
		{TweetID: "t1", RankPosition: 1, FinalScore: 5},
		{TweetID: "t2", RankPosition: 2, FinalScore: 4},
		{TweetID: "t3", RankPosition: 3, FinalScore: 3},
		{TweetID: "t4", RankPosition: 4, FinalScore: 2},
		{TweetID: "t5", RankPosition: 5, FinalScore: 1},
	}); err != nil {
		t.Fatal(err)
	}
	oldSnapAt := int64(1000)
	if err := srv.db.ExecRaw(`UPDATE feed_rank_snapshot SET computed_at = ?`, oldSnapAt); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"t1", "t2"} {
		if err := srv.db.ExecRaw(`INSERT INTO feed_seen (username, tweet_id, seen_at) VALUES (?, ?, ?)`,
			user, id, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.db.ReplaceFeedRankSnapshot(user, []db.SnapshotRow{
		{TweetID: "t3", RankPosition: 1, FinalScore: 3},
		{TweetID: "t4", RankPosition: 2, FinalScore: 2},
		{TweetID: "t5", RankPosition: 3, FinalScore: 1},
	}); err != nil {
		t.Fatal(err)
	}
	newSnapAt := int64(2000)
	if err := srv.db.ExecRaw(`UPDATE feed_rank_snapshot SET computed_at = ?`, newSnapAt); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/feed?offset=2&snapshot_at=1000", nil)
	req.Header.Set("HX-Request", "true")
	req = attachTestAuth(req, user)
	rec := httptest.NewRecorder()
	srv.handlePageFeed(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"body t3", "body t4", "body t5"} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q after snapshot reset:\n%s", want, body)
		}
	}
	if strings.Contains(body, "body t1") || strings.Contains(body, "body t2") {
		t.Fatalf("response included seen first-page rows after reset:\n%s", body)
	}
}

func TestHandlePageFeedCarriesSnapshotAtInNextCursor(t *testing.T) {
	srv := newTestServer(t)
	user := "alice"
	now := time.Now().UnixMilli()

	var rows []db.SnapshotRow
	for i := 1; i <= 41; i++ {
		id := fmt.Sprintf("t%02d", i)
		if err := srv.db.ExecRaw(`INSERT INTO feed_items
			(tweet_id, author_handle, body_text, published_at, algo_interest, algo_scored_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			id, "author_"+id, "body "+id, now-int64(i), 1.0, 1); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, db.SnapshotRow{TweetID: id, RankPosition: i, FinalScore: float64(100 - i)})
	}
	if err := srv.db.ReplaceFeedRankSnapshot(user, rows); err != nil {
		t.Fatal(err)
	}
	snapAt := int64(1234)
	if err := srv.db.ExecRaw(`UPDATE feed_rank_snapshot SET computed_at = ?`, snapAt); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/feed", nil)
	req.Header.Set("HX-Request", "true")
	req = attachTestAuth(req, user)
	rec := httptest.NewRecorder()
	srv.handlePageFeed(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "snapshot_at=1234") {
		t.Fatalf("next cursor did not carry snapshot_at:\n%s", body)
	}
}
