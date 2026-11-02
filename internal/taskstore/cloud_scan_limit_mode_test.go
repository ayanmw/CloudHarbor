package taskstore

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCloudScanLimitChangeRequiresAtomicResumeEvidence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "account", RunID: "limit-run", Status: "running", Phase: "listing", CurrentDirectory: "/", RequestBudget: 1, StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReserveCloudScanRequest("account", "limit-run", now); err != nil {
		t.Fatal(err)
	}
	state, err = store.LoadCloudScanState("account")
	if err != nil {
		t.Fatal(err)
	}
	retryAt := now.Add(time.Minute)
	state.Phase = "backoff"
	state.RetryAtUTC = &retryAt
	state.RetryDelayMillis = 60000
	if err = store.SaveCloudScanCheckpoint(state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER reject_limit_resume BEFORE INSERT ON cloud_inventory_scan_events WHEN NEW.event_kind='resumed' BEGIN SELECT RAISE(ABORT,'injected evidence failure'); END`); err != nil {
		t.Fatal(err)
	}
	state.RequestBudget = 0
	state.UpdatedAtUTC = now.Add(time.Second)
	if err = store.SaveCloudScanCheckpoint(state); err == nil {
		t.Fatal("limit changed without its resume evidence")
	}
	old, err := store.LoadCloudScanState("account")
	if err != nil || old.RequestBudget != 1 || old.RequestsUsed != 1 {
		t.Fatalf("partial limit commit: %+v %v", old, err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER reject_limit_resume`); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanCheckpoint(state); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListCloudScanEvents("account", "limit-run", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		found = found || (event.Kind == "resumed" && event.RequestBudget == 0 && event.RequestsUsed == 1)
	}
	if !found {
		t.Fatal("limit evidence missing")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.LoadCloudScanState("account")
	if err != nil || got.RequestBudget != 0 || got.RequestsUsed != 1 || got.RetryAtUTC == nil || !got.RetryAtUTC.Equal(retryAt.Truncate(time.Microsecond)) {
		t.Fatalf("reopened state=%+v %v", got, err)
	}
}
