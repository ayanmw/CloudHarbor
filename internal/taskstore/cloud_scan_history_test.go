package taskstore

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildCloudScanCoverageComparisonRequiresAdjacentRecordedSnapshots(t *testing.T) {
	observed := time.Date(2026, 11, 2, 3, 4, 5, 0, time.UTC)
	previousCount, currentCount := int64(8), int64(3)
	previousStatus, currentStatus := "incomplete", "complete"
	previous := CloudScanRun{Sequence: 10, RunID: "previous", Status: "complete", CoverageStatus: &previousStatus, CoverageIncompleteDirectories: &previousCount, CoverageObservedAtUTC: &observed, CoverageReasons: &CloudInventoryCoverageReasons{StaleDirectories: 4, MissingRoot: 1}}
	currentObserved := observed.Add(time.Hour)
	current := CloudScanRun{Sequence: 12, RunID: "current", Status: "complete", CoverageStatus: &currentStatus, CoverageIncompleteDirectories: &currentCount, CoverageObservedAtUTC: &currentObserved, CoverageReasons: &CloudInventoryCoverageReasons{StaleDirectories: 1, MissingRoot: 0}}

	comparison := BuildCloudScanCoverageComparison(current, &previous)
	if comparison.Status != "comparable" || comparison.IncompleteDirectoriesDelta == nil || *comparison.IncompleteDirectoriesDelta != -5 || comparison.PreviousRunID != "previous" || comparison.PreviousSequence != 10 || comparison.PreviousObservedAtUTC == nil || !comparison.PreviousObservedAtUTC.Equal(observed) || comparison.ReasonDeltas == nil || comparison.ReasonDeltas.StaleDirectories != -3 || comparison.ReasonDeltas.MissingRoot != -1 {
		t.Fatalf("comparison=%+v", comparison)
	}

	noPrevious := BuildCloudScanCoverageComparison(current, nil)
	if noPrevious.Status != "no_previous_run" || noPrevious.IncompleteDirectoriesDelta != nil {
		t.Fatalf("first run comparison=%+v", noPrevious)
	}
	missingPrevious := previous
	missingPrevious.CoverageIncompleteDirectories = nil
	if got := BuildCloudScanCoverageComparison(current, &missingPrevious); got.Status != "previous_snapshot_unrecorded" || got.IncompleteDirectoriesDelta != nil {
		t.Fatalf("missing previous snapshot comparison=%+v", got)
	}
	missingCurrent := current
	missingCurrent.CoverageObservedAtUTC = nil
	if got := BuildCloudScanCoverageComparison(missingCurrent, &previous); got.Status != "current_snapshot_unrecorded" || got.IncompleteDirectoriesDelta != nil {
		t.Fatalf("missing current snapshot comparison=%+v", got)
	}
	missingReasons := current
	missingReasons.CoverageReasons = nil
	if got := BuildCloudScanCoverageComparison(missingReasons, &previous); got.Status != "comparable" || got.ReasonStatus != "reasons_unrecorded" || got.ReasonDeltas != nil {
		t.Fatalf("missing reasons comparison=%+v", got)
	}
}

func TestCloudScanHistoryAttachesAdjacentSameAccountCoverageComparison(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 11, 2, 3, 4, 5, 0, time.UTC).UnixMicro()
	insert := func(account, runID, status string, coverage *int64) {
		t.Helper()
		var coverageStatus, reasons any
		var observed any
		if coverage != nil {
			coverageStatus, observed, reasons = "incomplete", now, `{"stale_directories":2}`
		}
		_, insertErr := store.db.Exec(`INSERT INTO cloud_scan_runs(account_fingerprint,run_id,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error,coverage_status,coverage_incomplete_directories,coverage_generation,coverage_observed_at_utc,coverage_reasons_json) VALUES(?,?,?,'complete','/',0,0,0,0,0,?,?, '',?,?,1,?,?)`, account, runID, status, now, now, coverageStatus, coverage, observed, reasons)
		if insertErr != nil {
			t.Fatal(insertErr)
		}
	}
	previousCount, currentCount := int64(5), int64(1)
	insert("acct", "previous", "complete", &previousCount)
	insert("acct", "failed-gap", "failed", nil)
	insert("other", "other-account", "complete", &previousCount)
	insert("acct", "current", "complete", &currentCount)

	runs, err := store.ListCloudScanRuns("acct", 0, 10)
	if err != nil || len(runs) != 3 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	comparison := runs[0].CoverageComparison
	if runs[0].RunID != "current" || comparison == nil || comparison.Status != "comparable" || comparison.PreviousRunID != "previous" || comparison.PreviousSequence != runs[2].Sequence || comparison.IncompleteDirectoriesDelta == nil || *comparison.IncompleteDirectoriesDelta != -4 {
		t.Fatalf("current run comparison=%+v runs=%+v", comparison, runs)
	}
	bundle, err := store.ExportCloudScanEvidence(context.Background(), "acct", "current")
	if err != nil || bundle.Run.CoverageComparison == nil || bundle.Run.CoverageComparison.PreviousRunID != "previous" {
		t.Fatalf("export comparison=%+v err=%v", bundle.Run.CoverageComparison, err)
	}
	exported, err := json.Marshal(bundle)
	if err != nil || strings.Contains(string(exported), "account_fingerprint") || strings.Contains(string(exported), `"acct"`) {
		t.Fatalf("export exposed account identity: %s err=%v", exported, err)
	}

	missingCount := int64(0)
	insert("acct", "current-after-unrecorded", "complete", &missingCount)
	if _, err = store.db.Exec(`UPDATE cloud_scan_runs SET coverage_status=NULL,coverage_incomplete_directories=NULL,coverage_observed_at_utc=NULL,coverage_reasons_json=NULL WHERE account_fingerprint='acct' AND run_id='current'`); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListCloudScanRuns("acct", 0, 1)
	if err != nil || len(page) != 1 || page[0].CoverageComparison == nil || page[0].CoverageComparison.Status != "previous_snapshot_unrecorded" || page[0].CoverageComparison.PreviousRunID != "current" || page[0].CoverageComparison.IncompleteDirectoriesDelta != nil {
		t.Fatalf("unrecorded adjacent comparison=%+v err=%v", page, err)
	}
}

func TestCloudScanHistoryRetainsRunsAndKeyEvents(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	first := CloudScanState{AccountFingerprint: "acct", RunID: "run-one", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now, RequestBudget: 4}
	if err = store.StartCloudScan(&first); err != nil {
		t.Fatal(err)
	}
	first.Status, first.Phase = "paused", "request_budget_reached"
	first.RequestsUsed = 4
	first.UpdatedAtUTC = now.Add(time.Minute)
	if err = store.SaveCloudScanCheckpoint(first); err != nil {
		t.Fatal(err)
	}
	first.Status, first.Phase = "running", "listing"
	first.RequestBudget = 8
	first.UpdatedAtUTC = now.Add(2 * time.Minute)
	if err = store.SaveCloudScanCheckpoint(first); err != nil {
		t.Fatal(err)
	}
	first.Status, first.Phase = "complete", "complete"
	first.PendingCount = 0
	first.UpdatedAtUTC = now.Add(3 * time.Minute)
	if err = store.SaveCloudScanCheckpoint(first); err != nil {
		t.Fatal(err)
	}
	second := CloudScanState{AccountFingerprint: "acct", RunID: "run-two", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now.Add(4 * time.Minute), UpdatedAtUTC: now.Add(4 * time.Minute)}
	if err = store.StartCloudScan(&second); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListCloudScanRuns("acct", 0, 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	if runs[0].RunID != "run-two" || runs[1].RunID != "run-one" || runs[1].Status != "complete" || runs[1].RequestBudget != 8 || runs[1].RequestsUsed != 4 {
		t.Fatalf("run history=%+v", runs)
	}
	events, err := store.ListCloudScanEvents("acct", "run-one", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"started", "request_budget_reached", "resumed", "completed"}
	if len(events) != len(want) {
		t.Fatalf("events=%+v", events)
	}
	for i, kind := range want {
		if events[len(events)-1-i].Kind != kind {
			t.Fatalf("events=%+v", events)
		}
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_scan_events SET last_error='changed' WHERE run_id='run-one'`); err == nil {
		t.Fatal("scan history events must be append-only")
	}
}

func TestCompletedCloudScanCapturesImmutableCoverageSnapshot(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Date(2026, 11, 2, 3, 4, 5, 0, time.UTC)
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run-snapshot", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now.Add(-time.Minute), UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_generations(account_fingerprint,generation,updated_at_utc) VALUES('acct',42,?)`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES('acct','/','complete',?,'run-snapshot')`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES('acct','/stale','stale',?,'older-run')`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	state.Status, state.Phase = "complete", "complete"
	state.UpdatedAtUTC = now.Add(time.Second)
	if err = store.SaveCloudScanCheckpoint(state); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListCloudScanRuns("acct", 0, 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	run := runs[0]
	if run.CoverageStatus == nil || *run.CoverageStatus != "incomplete" || run.CoverageIncompleteDirectories == nil || *run.CoverageIncompleteDirectories != 1 || run.CoverageGeneration == nil || *run.CoverageGeneration != 42 || run.CoverageObservedAtUTC == nil || !run.CoverageObservedAtUTC.Equal(state.UpdatedAtUTC) || run.CoverageReasons == nil || run.CoverageReasons.StaleDirectories != 1 {
		t.Fatalf("completion snapshot=%+v", run)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_generations SET generation=43 WHERE account_fingerprint='acct'`); err != nil {
		t.Fatal(err)
	}
	runs, err = store.ListCloudScanRuns("acct", 0, 10)
	if err != nil || *runs[0].CoverageGeneration != 42 || runs[0].CoverageReasons == nil || runs[0].CoverageReasons.StaleDirectories != 1 {
		t.Fatalf("historical snapshot changed with current cache: runs=%+v err=%v", runs, err)
	}
	dir := store.dir
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	runs, err = store.ListCloudScanRuns("acct", 0, 10)
	if err != nil || len(runs) != 1 || runs[0].CoverageGeneration == nil || *runs[0].CoverageGeneration != 42 || runs[0].CoverageObservedAtUTC == nil || !runs[0].CoverageObservedAtUTC.Equal(state.UpdatedAtUTC) || runs[0].CoverageReasons == nil || runs[0].CoverageReasons.StaleDirectories != 1 {
		t.Fatalf("historical snapshot did not survive reopen: runs=%+v err=%v", runs, err)
	}
}

func TestCloudScanCompletionSnapshotRollsBackWithCompletionEvent(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run-atomic-snapshot", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER reject_completed_scan_event BEFORE INSERT ON cloud_inventory_scan_events WHEN NEW.event_kind='completed' BEGIN SELECT RAISE(ABORT,'injected completed event failure'); END`); err != nil {
		t.Fatal(err)
	}
	state.Status, state.Phase, state.UpdatedAtUTC = "complete", "complete", now.Add(time.Second)
	if err = store.SaveCloudScanCheckpoint(state); err == nil {
		t.Fatal("completion snapshot committed without its completion event")
	}
	runs, err := store.ListCloudScanRuns("acct", 0, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != "running" || runs[0].CoverageStatus != nil {
		t.Fatalf("partially committed completion: runs=%+v err=%v", runs, err)
	}
}

func TestCloudScanHistoryEventAndCheckpointCommitTogether(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run-atomic", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER reject_scan_pause BEFORE INSERT ON cloud_inventory_scan_events WHEN NEW.event_kind='paused' BEGIN SELECT RAISE(ABORT,'injected scan history failure'); END`); err != nil {
		t.Fatal(err)
	}
	state.Status, state.Phase = "paused", "paused"
	state.UpdatedAtUTC = now.Add(time.Minute)
	if err = store.SaveCloudScanCheckpoint(state); err == nil {
		t.Fatal("checkpoint committed without its required history event")
	}
	got, err := store.LoadCloudScanState("acct")
	if err != nil || got.Status != "running" {
		t.Fatalf("checkpoint partially committed: %+v err=%v", got, err)
	}
	runs, err := store.ListCloudScanRuns("acct", 0, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != "running" {
		t.Fatalf("run summary partially committed: %+v err=%v", runs, err)
	}
}

func TestMigrateV29AddsScanHistoryTablesWithoutInventingRows(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.db.Exec(`DROP TRIGGER cloud_inventory_scan_events_no_update; DROP TRIGGER cloud_inventory_scan_events_no_delete; DROP INDEX cloud_inventory_scan_events_order; DROP TABLE cloud_inventory_scan_events; DROP INDEX cloud_scan_runs_order; DROP TABLE cloud_scan_runs; UPDATE store_meta SET value='29' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = migrateV29(store, "/queue"); err != nil {
		t.Fatal(err)
	}
	var version string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != "30" {
		t.Fatalf("schema version=%q err=%v", version, err)
	}
	var runs, events int
	if err = store.db.QueryRow(`SELECT (SELECT count(*) FROM cloud_scan_runs),(SELECT count(*) FROM cloud_inventory_scan_events)`).Scan(&runs, &events); err != nil || runs != 0 || events != 0 {
		t.Fatalf("migration fabricated scan history runs=%d events=%d err=%v", runs, events, err)
	}
}

func TestCloudScanEvidenceExportContainsOneConsistentRunAndChronologicalEvents(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run-export", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now, RequestBudget: 5}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	state.Status, state.Phase = "paused", "paused"
	state.UpdatedAtUTC = now.Add(time.Minute)
	if err = store.SaveCloudScanCheckpoint(state); err != nil {
		t.Fatal(err)
	}
	state.Status, state.Phase = "running", "listing"
	state.UpdatedAtUTC = now.Add(2 * time.Minute)
	if err = store.SaveCloudScanCheckpoint(state); err != nil {
		t.Fatal(err)
	}
	state.Status, state.Phase = "complete", "complete"
	state.PendingCount = 0
	state.UpdatedAtUTC = now.Add(3 * time.Minute)
	if err = store.SaveCloudScanCheckpoint(state); err != nil {
		t.Fatal(err)
	}
	bundle, err := store.ExportCloudScanEvidence(context.Background(), "acct", "run-export")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Run.RunID != "run-export" || bundle.Run.Status != "complete" || bundle.Run.RequestBudget != 5 || len(bundle.Events) != 4 {
		t.Fatalf("evidence bundle=%+v", bundle)
	}
	want := []string{"started", "paused", "resumed", "completed"}
	for i, k := range want {
		if bundle.Events[i].Kind != k {
			t.Fatalf("events=%+v", bundle.Events)
		}
	}
}

func TestCloudScanRunHistoryFiltersByStatusAndUTCStartDate(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i, day := range []int{30, 31} {
		now := time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC)
		state := CloudScanState{AccountFingerprint: "acct", RunID: fmt.Sprintf("filter-%d", i), Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
		if err = store.StartCloudScan(&state); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			state.Status, state.Phase = "paused", "paused"
			state.UpdatedAtUTC = now.Add(time.Minute)
			if err = store.SaveCloudScanCheckpoint(state); err != nil {
				t.Fatal(err)
			}
		} else {
			state.Status, state.Phase = "complete", "complete"
			state.PendingCount = 0
			state.UpdatedAtUTC = now.Add(time.Minute)
			if err = store.SaveCloudScanCheckpoint(state); err != nil {
				t.Fatal(err)
			}
		}
	}
	filter := CloudScanRunFilter{Status: "paused", StartedFromUTC: "2026-10-31", StartedThroughUTC: "2026-10-31"}
	runs, err := store.ListCloudScanRunsFiltered("acct", 0, filter, 10)
	if err != nil || len(runs) != 1 || runs[0].RunID != "filter-1" {
		t.Fatalf("filtered runs=%+v err=%v", runs, err)
	}
}

func TestCloudScanRunHistoryFiltersByHistoricalCoverageAndReason(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 11, 2, 4, 0, 0, 0, time.UTC)
	rows := []struct {
		id       string
		coverage *string
		count    *int64
		reasons  string
	}{
		{id: "coverage-complete", coverage: stringPointer("complete"), count: int64Pointer(0), reasons: `{"stale_directories":0}`},
		{id: "coverage-incomplete", coverage: stringPointer("incomplete"), count: int64Pointer(2), reasons: `{"stale_directories":1,"path_mismatch":0}`},
		{id: "coverage-scanning", coverage: stringPointer("scanning"), count: int64Pointer(3), reasons: `{"stale_directories":0,"path_mismatch":1}`},
		{id: "coverage-no-reasons", coverage: stringPointer("incomplete"), count: int64Pointer(1)},
		{id: "coverage-legacy-older"},
		{id: "coverage-legacy"},
	}
	for i, item := range rows {
		state := CloudScanState{AccountFingerprint: "acct", RunID: item.id, Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now.Add(time.Duration(i) * time.Minute), UpdatedAtUTC: now.Add(time.Duration(i) * time.Minute)}
		if err = store.StartCloudScan(&state); err != nil {
			t.Fatal(err)
		}
		if item.coverage != nil && item.reasons != "" {
			if _, err = store.db.Exec(`UPDATE cloud_scan_runs SET coverage_status=?,coverage_incomplete_directories=?,coverage_reasons_json=? WHERE account_fingerprint='acct' AND run_id=?`, *item.coverage, *item.count, item.reasons, item.id); err != nil {
				t.Fatal(err)
			}
		} else if item.coverage != nil {
			if _, err = store.db.Exec(`UPDATE cloud_scan_runs SET coverage_status=?,coverage_incomplete_directories=? WHERE account_fingerprint='acct' AND run_id=?`, *item.coverage, *item.count, item.id); err != nil {
				t.Fatal(err)
			}
		}
	}
	min, max := int64(2), int64(3)
	filtered, err := store.ListCloudScanRunsFiltered("acct", 0, CloudScanRunFilter{CoverageIncompleteMin: &min, CoverageIncompleteMax: &max, CoverageReason: "stale_directories"}, 1)
	if err != nil || len(filtered) != 1 || filtered[0].RunID != "coverage-incomplete" {
		t.Fatalf("coverage range and reason filter=%+v err=%v", filtered, err)
	}
	filtered, err = store.ListCloudScanRunsFiltered("acct", 0, CloudScanRunFilter{CoverageStatus: "unrecorded"}, 10)
	if err != nil || len(filtered) != 2 || filtered[0].RunID != "coverage-legacy" || filtered[1].RunID != "coverage-legacy-older" {
		t.Fatalf("unrecorded coverage filter=%+v err=%v", filtered, err)
	}
	filtered, err = store.ListCloudScanRunsFiltered("acct", 0, CloudScanRunFilter{CoverageReason: "unrecorded"}, 10)
	if err != nil || len(filtered) != 3 || filtered[0].RunID != "coverage-legacy" || filtered[1].RunID != "coverage-legacy-older" || filtered[2].RunID != "coverage-no-reasons" {
		t.Fatalf("unrecorded reason snapshot filter=%+v err=%v", filtered, err)
	}
	filtered, err = store.ListCloudScanRunsFiltered("acct", 0, CloudScanRunFilter{CoverageStatus: "scanning", CoverageReason: "path_mismatch"}, 10)
	if err != nil || len(filtered) != 1 || filtered[0].RunID != "coverage-scanning" {
		t.Fatalf("reason filter=%+v err=%v", filtered, err)
	}
	firstPage, err := store.ListCloudScanRunsFiltered("acct", 0, CloudScanRunFilter{CoverageStatus: "unrecorded"}, 1)
	if err != nil || len(firstPage) != 1 || firstPage[0].RunID != "coverage-legacy" {
		t.Fatalf("first filtered history page=%+v err=%v", firstPage, err)
	}
	secondPage, err := store.ListCloudScanRunsFiltered("acct", firstPage[0].Sequence, CloudScanRunFilter{CoverageStatus: "unrecorded"}, 1)
	if err != nil || len(secondPage) != 1 || secondPage[0].RunID != "coverage-legacy-older" {
		t.Fatalf("filtered history page=%+v err=%v", secondPage, err)
	}
	tooLarge, invertedMin, invertedMax := int64(1000001), int64(3), int64(2)
	negative := int64(-1)
	for _, filter := range []CloudScanRunFilter{
		{CoverageStatus: "not-a-status"},
		{CoverageReason: "x') OR 1=1 --"},
		{CoverageIncompleteMin: &negative},
		{CoverageIncompleteMax: &tooLarge},
		{CoverageIncompleteMin: &invertedMin, CoverageIncompleteMax: &invertedMax},
	} {
		if _, err = store.ListCloudScanRunsFiltered("acct", 0, filter, 10); err == nil {
			t.Fatalf("accepted invalid coverage filter: %+v", filter)
		}
	}
}

func stringPointer(value string) *string { return &value }

func int64Pointer(value int64) *int64 { return &value }
