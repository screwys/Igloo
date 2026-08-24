package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/download"
)

type stubVideoMetadataFetcher struct {
	result db.VideoMetadataRefreshResult
	err    error
	calls  int
}

func (s *stubVideoMetadataFetcher) FetchVideoMetadata(context.Context, string, int, download.Opts) (db.VideoMetadataRefreshResult, error) {
	s.calls++
	return s.result, s.err
}

func TestVideoMetadataWorkerPublishesAndSchedulesTheNextYoungRefresh(t *testing.T) {
	dataDir := t.TempDir()
	m := &Manager{
		db:                newTestWorkerDBAt(t, dataDir),
		cfg:               testCfg(dataDir),
		videoMetadataKick: make(chan struct{}, 1),
		mediaKick:         make(chan struct{}, 1),
	}
	seedVideo(t, m, "sample_video")
	if err := m.QueueVideoMetadataRefresh("sample_video"); err != nil {
		t.Fatal(err)
	}
	views := int64(200)
	fetcher := &stubVideoMetadataFetcher{result: db.VideoMetadataRefreshResult{
		Comments:  []db.CommentInput{{CommentID: "sample_comment", Author: "Sample", Text: "hello"}},
		ViewCount: &views,
	}}

	if worked := m.processVideoMetadataJob(t.Context(), fetcher); !worked {
		t.Fatal("metadata worker reported no work")
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetch calls = %d, want 1", fetcher.calls)
	}
	comments, err := m.db.GetComments("sample_video", 10)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments = (%+v, %v)", comments, err)
	}
	var status string
	var nextAttempt int64
	if err := m.db.QueryRow(`SELECT status, next_attempt_at_ms FROM video_metadata_jobs WHERE video_id = 'sample_video'`).Scan(&status, &nextAttempt); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || nextAttempt < time.Now().Add(23*time.Hour).UnixMilli() {
		t.Fatalf("scheduled state = (%q, %d)", status, nextAttempt)
	}
}

func TestVideoMetadataWorkerKeepsFailedWorkDurable(t *testing.T) {
	dataDir := t.TempDir()
	m := &Manager{
		db:                newTestWorkerDBAt(t, dataDir),
		cfg:               testCfg(dataDir),
		videoMetadataKick: make(chan struct{}, 1),
	}
	seedVideo(t, m, "sample_video")
	if err := m.QueueVideoMetadataRefresh("sample_video"); err != nil {
		t.Fatal(err)
	}
	fetcher := &stubVideoMetadataFetcher{err: errors.New("temporary metadata failure")}

	if worked := m.processVideoMetadataJob(t.Context(), fetcher); !worked {
		t.Fatal("metadata worker reported no work")
	}
	var status string
	var attempts, nextAttempt int64
	if err := m.db.QueryRow(`SELECT status, attempts, next_attempt_at_ms FROM video_metadata_jobs WHERE video_id = 'sample_video'`).Scan(&status, &attempts, &nextAttempt); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 1 || nextAttempt <= time.Now().UnixMilli() {
		t.Fatalf("retry state = (%q, %d, %d)", status, attempts, nextAttempt)
	}
}
