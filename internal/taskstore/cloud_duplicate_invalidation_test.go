package taskstore

import (
	"path/filepath"
	"testing"
	"time"
)

func TestInvalidateIdleDuplicateCheckpointForNamespaceWrite(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now().UTC()
	state := CloudDuplicateScanState{AccountFingerprint: "acct", RunID: "old-run", Status: "paused", Phase: "paused", NextFsID: 42, TotalFiles: 10, ProcessedFiles: 3, TotalBytes: 100, ProcessedBytes: 30, StartedAtUTC: now, UpdatedAtUTC: now}
	if e = s.SaveCloudDuplicateScanState(state, true); e != nil {
		t.Fatal(e)
	}
	if e = s.InvalidateIdleDuplicateCheckpoint("acct"); e != nil {
		t.Fatal(e)
	}
	after, e := s.LoadCloudDuplicateScanState("acct")
	if e != nil || after.Status != "failed" || after.Phase != "restart_required" || after.NextFsID != 42 || after.ProcessedFiles != 3 || after.RunID != "old-run" {
		t.Fatalf("state=%+v err=%v", after, e)
	}
	conflict, e := s.CloudMutationScanConflict("acct", []string{"/new"})
	if e != nil || conflict {
		t.Fatalf("obsolete checkpoint blocks namespace: %v %v", conflict, e)
	}
	events, e := s.CloudDuplicateScanEvents("acct", 0, 10)
	if e != nil || len(events) != 2 {
		t.Fatalf("events=%d err=%v", len(events), e)
	}
	if e = s.InvalidateIdleDuplicateCheckpoint("acct"); e != nil {
		t.Fatal(e)
	}
	events, _ = s.CloudDuplicateScanEvents("acct", 0, 10)
	if len(events) != 2 {
		t.Fatal("repeated writes duplicated invalidation")
	}
}

func TestInvalidateIdleDuplicateCheckpointRollbackAndReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	now := time.Now().UTC()
	for _, account := range []string{"acct", "other"} {
		state := CloudDuplicateScanState{AccountFingerprint: account, RunID: account + "-old", Status: "paused", Phase: "paused", NextFsID: 42, TotalFiles: 10, ProcessedFiles: 3, StartedAtUTC: now, UpdatedAtUTC: now}
		if err := s.SaveCloudDuplicateScanState(state, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_test_invalidation BEFORE INSERT ON cloud_duplicate_scan_events WHEN NEW.phase='restart_required' BEGIN SELECT RAISE(ABORT,'injected event failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.InvalidateIdleDuplicateCheckpoint("acct"); err == nil {
		t.Fatal("event failure should roll back state")
	}
	after, err := s.LoadCloudDuplicateScanState("acct")
	if err != nil || after.Status != "paused" || after.Phase != "paused" {
		t.Fatalf("partial update: %+v %v", after, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER reject_test_invalidation`); err != nil {
		t.Fatal(err)
	}
	if err := s.InvalidateIdleDuplicateCheckpoint("acct"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	after, err = s.LoadCloudDuplicateScanState("acct")
	if err != nil || after.Phase != "restart_required" || after.NextFsID != 42 || after.RunID != "acct-old" {
		t.Fatalf("restart evidence lost: %+v %v", after, err)
	}
	other, err := s.LoadCloudDuplicateScanState("other")
	if err != nil || other.Phase != "paused" {
		t.Fatalf("other account changed: %+v %v", other, err)
	}
}
