package db

import (
	"testing"
	"time"
)

func TestTempDownloadQueueRecoversAndRetriesDurableWork(t *testing.T) {
	d := openWritableTestDB(t)
	const rawURL = "https://www.youtube.com/watch?v=sample_video"
	queued, err := d.EnqueueTempDownload(rawURL, "youtube")
	if err != nil || !queued {
		t.Fatalf("enqueue = %v, %v", queued, err)
	}

	now := time.Now()
	work, ok, err := d.ClaimTempDownloadWork("worker-a", now.UnixMilli(), time.Hour)
	if err != nil || !ok || work.URL != rawURL || work.Platform != "youtube" {
		t.Fatalf("claim = %+v, %v, %v", work, ok, err)
	}
	if err := d.ResetTempDownloadWork(); err != nil {
		t.Fatal(err)
	}
	work, ok, err = d.ClaimTempDownloadWork("worker-b", now.UnixMilli(), time.Hour)
	if err != nil || !ok || work.LeaseOwner != "worker-b" {
		t.Fatalf("recovered claim = %+v, %v, %v", work, ok, err)
	}
	if err := d.RetryTempDownloadWork(rawURL, work.LeaseOwner, "temporary", "sample failure", time.Second); err != nil {
		t.Fatal(err)
	}
	state, found, err := d.TempDownloadState(rawURL)
	if err != nil || !found || state.Status != "pending" || state.Error != "sample failure" {
		t.Fatalf("retry state = %+v, found=%v, err=%v", state, found, err)
	}
	work, ok, err = d.ClaimTempDownloadWork("worker-c", now.Add(2*time.Second).UnixMilli(), time.Hour)
	if err != nil || !ok || work.RetryCount != 1 || work.LeaseOwner != "worker-c" {
		t.Fatalf("retry claim = %+v, %v, %v", work, ok, err)
	}
	if err := d.CompleteTempDownloadWork(rawURL, work.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	if got := testRowCount(t, d, `SELECT COUNT(*) FROM temp_download_queue`); got != 0 {
		t.Fatalf("completed queue rows = %d", got)
	}
}
