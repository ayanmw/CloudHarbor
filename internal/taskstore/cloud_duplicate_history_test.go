package taskstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenMigratesV32DuplicateRunSummariesWithoutInventingPastProgress(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	base := time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC)
	oldRun := CloudDuplicateScanState{AccountFingerprint: "acct-a", RunID: "old-run", Status: "running", Phase: "hashing", TotalFiles: 10, TotalBytes: 1000, StartedAtUTC: base, UpdatedAtUTC: base}
	if err = store.SaveCloudDuplicateScanState(oldRun, true); err != nil {
		t.Fatal(err)
	}
	oldRun.Status, oldRun.Phase = "complete", "complete"
	oldRun.ProcessedFiles, oldRun.ProcessedBytes = 7, 700
	oldRun.UpdatedAtUTC = base.Add(time.Minute)
	if err = store.SaveCloudDuplicateScanState(oldRun, true); err != nil {
		t.Fatal(err)
	}
	active := CloudDuplicateScanState{AccountFingerprint: "acct-a", RunID: "active-run", Status: "running", Phase: "hashing", TotalFiles: 12, TotalBytes: 1200, StartedAtUTC: base.Add(2 * time.Minute), UpdatedAtUTC: base.Add(2 * time.Minute)}
	if err = store.SaveCloudDuplicateScanState(active, true); err != nil {
		t.Fatal(err)
	}
	active.Status, active.Phase = "paused", "paused"
	active.ProcessedFiles, active.ProcessedBytes = 4, 400
	active.LastError = "pause reason"
	active.UpdatedAtUTC = base.Add(4 * time.Minute)
	if err = store.SaveCloudDuplicateScanState(active, false); err != nil {
		t.Fatal(err)
	}
	other := CloudDuplicateScanState{AccountFingerprint: "acct-b", RunID: "other-run", Status: "running", Phase: "hashing", TotalFiles: 99, TotalBytes: 9900, StartedAtUTC: base, UpdatedAtUTC: base}
	if err = store.SaveCloudDuplicateScanState(other, true); err != nil {
		t.Fatal(err)
	}
	var lastSequence int64
	if err = store.db.QueryRow(`SELECT max(sequence) FROM cloud_duplicate_scan_events`).Scan(&lastSequence); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TABLE IF EXISTS cloud_duplicate_scan_runs; UPDATE store_meta SET value='32' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 32, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != fmt.Sprint(sqliteSchemaVersion) {
		t.Fatalf("schema version=%q err=%v", version, err)
	}
	assertRun := func(account, runID string, wantStatus string, wantProcessed int64, wantUpdated time.Time, wantError string) {
		t.Helper()
		var status, lastError string
		var processed, updated int64
		err := store.db.QueryRow(`SELECT status,processed_files,updated_at_utc,last_error FROM cloud_duplicate_scan_runs WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&status, &processed, &updated, &lastError)
		if err != nil || status != wantStatus || processed != wantProcessed || updated != wantUpdated.UnixMicro() || lastError != wantError {
			t.Errorf("summary %s/%s=(%s,%d,%d,%q), want (%s,%d,%d,%q), err=%v", account, runID, status, processed, updated, lastError, wantStatus, wantProcessed, wantUpdated.UnixMicro(), wantError, err)
		}
	}
	assertRun("acct-a", "old-run", "complete", 7, base.Add(time.Minute), "")
	assertRun("acct-a", "active-run", "paused", 4, base.Add(4*time.Minute), "pause reason")
	assertRun("acct-b", "other-run", "running", 0, base, "")
	var gotLastSequence int64
	if err = store.db.QueryRow(`SELECT max(sequence) FROM cloud_duplicate_scan_events`).Scan(&gotLastSequence); err != nil || gotLastSequence != lastSequence {
		t.Fatalf("event sequence changed across migration: got=%d want=%d err=%v", gotLastSequence, lastSequence, err)
	}
	var integrity string
	if err = store.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check=%q err=%v", integrity, err)
	}
	for _, name := range []string{"cloud_duplicate_scan_events_no_update", "cloud_duplicate_scan_events_no_delete"} {
		var count int
		if err = store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name=?`, name).Scan(&count); err != nil || count != 1 {
			t.Fatalf("append-only trigger %s count=%d err=%v", name, count, err)
		}
	}
	if _, err = store.db.Exec(`UPDATE cloud_duplicate_scan_events SET status='changed' WHERE sequence=1`); err == nil {
		t.Fatal("migration lost duplicate event append-only trigger")
	}
	if _, err = store.db.Exec(`DELETE FROM cloud_duplicate_scan_events WHERE sequence=1`); err == nil {
		t.Fatal("migration lost duplicate event append-only trigger")
	}
	var tableCount int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_duplicate_scan_runs WHERE account_fingerprint='acct-b'`).Scan(&tableCount); err != nil || tableCount != 1 {
		t.Fatalf("cross-account run count=%d err=%v", tableCount, err)
	}
}

func TestCloudDuplicateRunSummaryTracksCheckpointAndRollsBackWithEvent(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	started := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	state := CloudDuplicateScanState{AccountFingerprint: "acct", RunID: "run", Status: "running", Phase: "hashing", TotalFiles: 10, TotalBytes: 1000, StartedAtUTC: started, UpdatedAtUTC: started}
	if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
		t.Fatal(err)
	}
	state.ProcessedFiles, state.ProcessedBytes = 2, 200
	state.NextFsID = 20
	state.UpdatedAtUTC = started.Add(time.Minute)
	if err = store.SaveCloudDuplicateScanState(state, false); err != nil {
		t.Fatal(err)
	}
	var runCount int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_duplicate_scan_runs WHERE account_fingerprint='acct' AND run_id='run' AND processed_files=2 AND processed_bytes=200 AND next_fs_id=20`).Scan(&runCount); err != nil || runCount != 1 {
		t.Fatalf("checkpoint projection count=%d err=%v", runCount, err)
	}
	var eventCount int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_duplicate_scan_events WHERE account_fingerprint='acct' AND run_id='run'`).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("non-transition checkpoint unexpectedly appended an event: count=%d err=%v", eventCount, err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fail_duplicate_run_projection BEFORE UPDATE ON cloud_duplicate_scan_runs BEGIN SELECT RAISE(ABORT,'injected duplicate run projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	state.Status, state.Phase = "failed", "failed"
	state.ProcessedFiles, state.ProcessedBytes = 3, 300
	state.LastError = "injected failure"
	state.UpdatedAtUTC = started.Add(2 * time.Minute)
	if err = store.SaveCloudDuplicateScanState(state, true); err == nil {
		t.Fatal("state update succeeded despite run projection failure")
	}
	loaded, err := store.LoadCloudDuplicateScanState("acct")
	if err != nil || loaded.Status != "running" || loaded.ProcessedFiles != 2 {
		t.Fatalf("failed projection did not roll back state: %+v err=%v", loaded, err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_duplicate_scan_events WHERE account_fingerprint='acct' AND run_id='run'`).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("failed projection did not roll back event: count=%d err=%v", eventCount, err)
	}
}

func TestCloudDuplicateRunHistoryFiltersPagesAndExportsSelectedRun(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	for index, run := range []struct {
		id, status string
	}{{"first", "complete"}, {"second", "failed"}, {"third", "complete"}, {"other-account", "complete"}} {
		account := "acct"
		if index == 3 {
			account = "other"
		}
		started := base.Add(time.Duration(index) * time.Hour)
		state := CloudDuplicateScanState{AccountFingerprint: account, RunID: run.id, Status: "running", Phase: "hashing", TotalFiles: 10, TotalBytes: 1000, StartedAtUTC: started, UpdatedAtUTC: started}
		if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
			t.Fatal(err)
		}
		state.Status, state.Phase = run.status, run.status
		state.ProcessedFiles, state.ProcessedBytes = int64(index+1), int64(index+1)*100
		state.UpdatedAtUTC = started.Add(time.Minute)
		if run.status == "failed" {
			state.LastError = "scan failed"
		}
		if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
			t.Fatal(err)
		}
	}
	page, next, more, err := store.CloudDuplicateScanRunPage("acct", 0, CloudDuplicateScanRunFilter{}, 1)
	if err != nil || len(page) != 1 || page[0].RunID != "third" || next == 0 || !more {
		t.Fatalf("first run page=%+v next=%d more=%v err=%v", page, next, more, err)
	}
	older, next, more, err := store.CloudDuplicateScanRunPage("acct", next, CloudDuplicateScanRunFilter{}, 1)
	if err != nil || len(older) != 1 || older[0].RunID != "second" || !more {
		t.Fatalf("second run page=%+v next=%d more=%v err=%v", older, next, more, err)
	}
	complete, _, more, err := store.CloudDuplicateScanRunPage("acct", 0, CloudDuplicateScanRunFilter{Status: "complete", StartedFromUTC: "2026-10-30", StartedThroughUTC: "2026-10-30"}, 10)
	if err != nil || len(complete) != 2 || more || complete[0].RunID != "third" || complete[1].RunID != "first" {
		t.Fatalf("filtered complete runs=%+v more=%v err=%v", complete, more, err)
	}
	if _, _, _, err = store.CloudDuplicateScanRunPage("acct", 0, CloudDuplicateScanRunFilter{Status: "unknown"}, 10); err == nil {
		t.Fatal("invalid status filter accepted")
	}
	if _, _, _, err = store.CloudDuplicateScanRunPage("acct", 0, CloudDuplicateScanRunFilter{StartedFromUTC: "2026-10-31", StartedThroughUTC: "2026-10-30"}, 10); err == nil {
		t.Fatal("reversed UTC date range accepted")
	}
	exported, err := store.ExportCloudDuplicateScanRuns(context.Background(), "acct", CloudDuplicateScanRunFilter{Status: "failed"})
	if err != nil || len(exported) != 1 || exported[0].RunID != "second" || exported[0].LastError != "scan failed" {
		t.Fatalf("filtered summary export=%+v err=%v", exported, err)
	}
	encoded, err := json.Marshal(exported)
	if err != nil || strings.Contains(string(encoded), "account_fingerprint") || strings.Contains(string(encoded), "acct") {
		t.Fatalf("run summary export leaked account fingerprint: %s err=%v", encoded, err)
	}
	events, _, more, err := store.CloudDuplicateScanRunEvents("acct", "first", 0, 10)
	if err != nil || len(events) != 2 || more || events[0].RunID != "first" || events[1].RunID != "first" {
		t.Fatalf("selected run events=%+v more=%v err=%v", events, more, err)
	}
	bundle, err := store.CloudDuplicateScanEvidenceExport(context.Background(), "acct", "first")
	if err != nil || bundle.Run.RunID != "first" || len(bundle.Events) != 2 {
		t.Fatalf("selected run evidence=%+v err=%v", bundle, err)
	}
	encoded, err = json.Marshal(bundle)
	if err != nil || strings.Contains(string(encoded), "account_fingerprint") || strings.Contains(string(encoded), "acct") {
		t.Fatalf("evidence export leaked account fingerprint: %s err=%v", encoded, err)
	}
	if _, err = store.CloudDuplicateScanEvidenceExport(context.Background(), "acct", "other-account"); !errors.Is(err, ErrCloudDuplicateScanRunNotFound) {
		t.Fatalf("cross-account run lookup error=%v", err)
	}
}

func TestCloudDuplicateRunExportsRejectOverLimitWithoutPartialResults(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const overLimit = CloudDuplicateScanEvidenceExportLimit + 1
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	runInsert, err := tx.Prepare(`INSERT INTO cloud_duplicate_scan_runs(start_sequence,account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,total_bytes,processed_bytes,started_at_utc,updated_at_utc,last_error,latest_sequence) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	eventInsert, err := tx.Prepare(`INSERT INTO cloud_duplicate_scan_events(account_fingerprint,run_id,status,phase,recorded_at_utc,evidence_json) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < overLimit; index++ {
		if _, err = runInsert.Exec(index+1, "account", fmt.Sprintf("summary-%05d", index), "complete", "complete", 0, 0, 0, 0, 0, int64(1), int64(2), "", 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = runInsert.Exec(0, "evidence-account", "large-run", "complete", "complete", 0, 0, 0, 0, 0, int64(1), int64(2), "", 0); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < overLimit; index++ {
		if _, err = eventInsert.Exec("evidence-account", "large-run", "complete", "complete", int64(index+1), []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err = runInsert.Close(); err != nil {
		t.Fatal(err)
	}
	if err = eventInsert.Close(); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if runs, err := store.ExportCloudDuplicateScanRuns(context.Background(), "account", CloudDuplicateScanRunFilter{}); !errors.Is(err, ErrCloudDuplicateScanSummaryExportTooLarge) || runs != nil {
		t.Fatalf("over-limit summary returned %d partial runs, err=%v", len(runs), err)
	}
	if bundle, err := store.CloudDuplicateScanEvidenceExport(context.Background(), "evidence-account", "large-run"); !errors.Is(err, ErrCloudDuplicateScanEvidenceExportTooLarge) || bundle.Run.RunID != "" || bundle.Events != nil {
		t.Fatalf("over-limit evidence returned partial bundle %+v, err=%v", bundle, err)
	}
}
