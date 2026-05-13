package worker

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/download"
)

func TestRecordAssetRepairFailureRetriesTransient(t *testing.T) {
	d := openWorkerTestDB(t)
	m := &Manager{db: d}
	now := time.Now().UnixMilli()
	asset := seedClaimedAssetRepair(t, d, "sample_tweet_asset_retry", "worker-a", now)

	if err := m.recordAssetRepairFailure(asset, errors.New("temporary timeout"), nil, now+1); err != nil {
		t.Fatalf("recordAssetRepairFailure: %v", err)
	}
	got, err := d.GetAsset(asset.AssetID, asset.AssetKind)
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if got.State != db.AssetStateQueued ||
		got.Attempts != 1 ||
		got.LastErrorKind != download.ErrorKindTemporary ||
		got.NextAttemptAtMs <= now ||
		got.LeaseOwner != "" ||
		got.LeaseUntilMs != 0 {
		t.Fatalf("transient asset repair state = %+v", *got)
	}
}

func TestRecordAssetRepairFailureFailsPermanent(t *testing.T) {
	d := openWorkerTestDB(t)
	m := &Manager{db: d}
	now := time.Now().UnixMilli()
	asset := seedClaimedAssetRepair(t, d, "sample_tweet_asset_permanent", "worker-a", now)

	if err := m.recordAssetRepairFailure(asset, errors.New("login required; cookies missing"), nil, now+1); err != nil {
		t.Fatalf("recordAssetRepairFailure: %v", err)
	}
	got, err := d.GetAsset(asset.AssetID, asset.AssetKind)
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if got.State != db.AssetStateFailed ||
		got.Attempts != 1 ||
		got.LastErrorKind != download.ErrorKindAuth ||
		got.NextAttemptAtMs != 0 ||
		got.LeaseOwner != "" ||
		got.LeaseUntilMs != 0 {
		t.Fatalf("permanent asset repair state = %+v", *got)
	}
}

func seedClaimedAssetRepair(t *testing.T, d *db.DB, ownerID, owner string, nowMs int64) db.Asset {
	t.Helper()
	assetID := db.BuildManifestAssetID("twitter", "tweet", ownerID, "post_media", 0)
	if err := d.UpsertAsset(db.Asset{
		AssetID:    assetID,
		AssetKind:  "post_media",
		OwnerKind:  "tweet",
		OwnerID:    ownerID,
		MediaIndex: 0,
		State:      db.AssetStateQueued,
	}, nowMs-1); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	claimed, err := d.ClaimAssetRepairBatchWithLease(db.LeaseOptions{
		Owner:   owner,
		NowMs:   nowMs,
		LeaseMs: int64(time.Minute / time.Millisecond),
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("ClaimAssetRepairBatchWithLease: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d assets, want 1", len(claimed))
	}
	return claimed[0]
}

func openWorkerTestDB(t *testing.T) *db.DB {
	t.Helper()
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "igloo.db"), tmp)
	if err != nil {
		t.Fatalf("open worker test db: %v", err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Fatalf("close worker test db: %v", err)
		}
	})
	return d
}
