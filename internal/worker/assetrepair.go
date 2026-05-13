package worker

import (
	"errors"
	"time"

	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/download"
)

func (m *Manager) recordAssetRepairFailure(asset db.Asset, err error, output []byte, nowMs int64) error {
	if m == nil || m.db == nil || err == nil {
		return nil
	}
	if nowMs == 0 {
		nowMs = time.Now().UnixMilli()
	}
	message := download.RedactText(err.Error())
	if len(output) > 0 {
		message = download.RedactText(message + ": " + string(output))
	}
	classification := download.ClassifyFailure(err, output, asset.Attempts+1)
	if classification.Permanent {
		return m.db.FailAssetRepair(asset.AssetID, asset.AssetKind, asset.LeaseOwner, classification.Kind, message, nowMs)
	}
	if classification.Kind == "" {
		classification.Kind = download.ErrorKindUnknown
	}
	if classification.RetryDelay <= 0 {
		classification.RetryDelay = download.ClassifyFailure(errors.New(message), nil, asset.Attempts+1).RetryDelay
	}
	return m.db.RetryAssetRepair(asset.AssetID, asset.AssetKind, asset.LeaseOwner, classification.Kind, message, classification.RetryDelay, nowMs)
}
