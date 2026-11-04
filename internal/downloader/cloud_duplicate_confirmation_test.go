package downloader

import (
	"context"
	"testing"
)

func TestDuplicateStartRejectsStaleConfirmation(t *testing.T) {
	d, _, _, _, _ := prepareDuplicateDriftRun(t, nil)
	defer shutdown(t, d)
	expected := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err := d.StartCloudDuplicateScan(WithInventoryExpectedRun(context.Background(), &expected))
	if err == nil {
		d.PauseCloudDuplicateScan()
		t.Fatal("stale confirmation resumed a different hash run")
	}
}
func TestDuplicatePauseBindsRunAndWorkerObservation(t *testing.T) {
	d, _, _, _, state := prepareDuplicateDriftRun(t, nil)
	if d.CloudDuplicateScanWorkerActive() {
		t.Fatal("persisted running checkpoint invented a live worker")
	}
	cancelled := false
	d.inventoryScanKind = "duplicate"
	d.inventoryScanCancel = func() { cancelled = true }
	defer func() { d.inventoryScanCancel = nil; d.inventoryScanKind = "" }()
	if !d.CloudDuplicateScanWorkerActive() {
		t.Fatal("live duplicate worker not observed")
	}
	if err := d.PauseCloudInventoryRun(context.Background(), "duplicate", "newer-run"); err == nil || cancelled {
		t.Fatal("stale pause cancelled hash worker")
	}
	if err := d.PauseCloudInventoryRun(context.Background(), "duplicate", state.RunID); err != nil || !cancelled {
		t.Fatalf("matching duplicate pause failed: %v", err)
	}
}
