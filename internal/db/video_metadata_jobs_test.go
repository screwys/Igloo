package db

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/model"
)

func seedVideoMetadataJobVideo(t *testing.T, d *DB, videoID string, publishedAtMs int64) {
	t.Helper()
	seedTestChannel(t, d, "youtube_sample_channel")
	if err := d.InsertVideo(
		videoID, "youtube_sample_channel", "youtube_video", "Sample video", "",
		60, publishedAtMs, `{"duration":60}`, "video", 0, false,
	); err != nil {
		t.Fatal(err)
	}
}

func TestVideoMetadataJobRetriesAnInterruptedLeaseAndPublishesOneSnapshot(t *testing.T) {
	d := openWritableTestDB(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	seedVideoMetadataJobVideo(t, d, "sample_video", now.Add(-time.Hour).UnixMilli())
	if err := d.QueueVideoMetadataJob("sample_video", now.UnixMilli()); err != nil {
		t.Fatal(err)
	}

	first, ok, err := d.ClaimVideoMetadataJob(LeaseOptions{
		Owner: "first-worker", NowMs: now.UnixMilli(), LeaseMs: time.Minute.Milliseconds(),
	})
	if err != nil || !ok {
		t.Fatalf("first claim = (%+v, %v, %v)", first, ok, err)
	}
	if _, ok, err := d.ClaimVideoMetadataJob(LeaseOptions{
		Owner: "second-worker", NowMs: now.Add(30 * time.Second).UnixMilli(), LeaseMs: time.Minute.Milliseconds(),
	}); err != nil || ok {
		t.Fatalf("claim before lease expiry = (%v, %v), want no work", ok, err)
	}

	recovered, ok, err := d.ClaimVideoMetadataJob(LeaseOptions{
		Owner: "second-worker", NowMs: now.Add(time.Minute).UnixMilli(), LeaseMs: time.Minute.Milliseconds(),
	})
	if err != nil || !ok {
		t.Fatalf("claim after lease expiry = (%+v, %v, %v)", recovered, ok, err)
	}
	views, likes, upstreamComments := int64(1234), int64(56), int64(1)
	if err := d.CompleteVideoMetadataJob(recovered, VideoMetadataRefreshResult{
		Comments:  []CommentInput{{CommentID: "sample_comment", Author: "Sample", Text: "hello", LikeCount: 3}},
		ViewCount: &views, LikeCount: &likes, CommentCount: &upstreamComments,
	}, now.Add(time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}

	comments, err := d.GetComments("sample_video", 10)
	if err != nil || len(comments) != 1 || comments[0].Text != "hello" {
		t.Fatalf("comments = (%+v, %v)", comments, err)
	}
	video, err := d.GetVideo("sample_video")
	if err != nil || video == nil {
		t.Fatalf("video = (%+v, %v)", video, err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(video.MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["view_count"] != float64(views) || metadata["like_count"] != float64(likes) || metadata["comment_count"] != float64(upstreamComments) {
		t.Fatalf("metadata counts = %#v", metadata)
	}
	if metadata["view_count_label"] != model.CompactCountLabel(views) {
		t.Fatalf("view count label = %#v", metadata["view_count_label"])
	}

	var status, age string
	var nextAttempt int64
	if err := d.QueryRow(`
		SELECT status, video_age_at_check, next_attempt_at_ms
		FROM video_metadata_jobs WHERE video_id = 'sample_video'
	`).Scan(&status, &age, &nextAttempt); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || age != "young" {
		t.Fatalf("young job state = (%q, %q)", status, age)
	}
	wantNext := now.Add(time.Minute + VideoMetadataRefresh).UnixMilli()
	if nextAttempt != wantNext {
		t.Fatalf("next attempt = %d, want %d", nextAttempt, wantNext)
	}
}

func TestVideoMetadataJobStopsRefreshingOldVideos(t *testing.T) {
	d := openWritableTestDB(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	seedVideoMetadataJobVideo(t, d, "sample_old_video", now.Add(-7*24*time.Hour).UnixMilli())
	if err := d.QueueVideoMetadataJob("sample_old_video", now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	job, ok, err := d.ClaimVideoMetadataJob(LeaseOptions{Owner: "worker", NowMs: now.UnixMilli()})
	if err != nil || !ok {
		t.Fatalf("claim = (%+v, %v, %v)", job, ok, err)
	}
	if err := d.CompleteVideoMetadataJob(job, VideoMetadataRefreshResult{}, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	var status, age string
	if err := d.QueryRow(`SELECT status, video_age_at_check FROM video_metadata_jobs WHERE video_id = ?`, job.VideoID).Scan(&status, &age); err != nil {
		t.Fatal(err)
	}
	if status != "done" || age != "old" {
		t.Fatalf("old job state = (%q, %q), want done/old", status, age)
	}
}

func TestVideoMetadataJobPreservesCommentsWhenTheSnapshotHasNoCommentSignal(t *testing.T) {
	d := openWritableTestDB(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	seedVideoMetadataJobVideo(t, d, "sample_video", now.Add(-time.Hour).UnixMilli())
	if _, err := d.AddComments("sample_video", []CommentInput{{CommentID: "existing", Text: "keep me"}}); err != nil {
		t.Fatal(err)
	}
	if err := d.QueueVideoMetadataJob("sample_video", now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	job, ok, err := d.ClaimVideoMetadataJob(LeaseOptions{Owner: "worker", NowMs: now.UnixMilli()})
	if err != nil || !ok {
		t.Fatalf("claim = (%+v, %v, %v)", job, ok, err)
	}
	if err := d.CompleteVideoMetadataJob(job, VideoMetadataRefreshResult{}, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	comments, err := d.GetComments("sample_video", 10)
	if err != nil || len(comments) != 1 || comments[0].CommentID != "existing" {
		t.Fatalf("comments = (%+v, %v), want existing comment", comments, err)
	}
}
