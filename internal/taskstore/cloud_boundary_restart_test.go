package taskstore

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testCloudBoundaryOffset = int64(999)

func startBoundaryScan(t *testing.T, store *Store, account, runID string) CloudScanState {
	t.Helper()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	state := CloudScanState{AccountFingerprint: account, RunID: runID, Status: "running", Phase: "listing", PendingCount: 1, CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err := store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	state.NextOffset = testCloudBoundaryOffset
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 1000, Path: "/next.bin", Name: "next.bin"}
	state.UpdatedAtUTC = now.Add(time.Second)
	if err := store.SaveCloudScanCheckpoint(state); err != nil {
		t.Fatal(err)
	}
	return state
}

func startBoundaryRefresh(t *testing.T, store *Store, account, runID string) CloudInventoryRefreshState {
	t.Helper()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	state := CloudInventoryRefreshState{AccountFingerprint: account, RunID: runID, RootPath: "/root", Status: "running", Phase: "listing", CurrentDirectory: "/root", StartedAtUTC: now, UpdatedAtUTC: now}
	if err := store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	state.NextOffset = testCloudBoundaryOffset
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 1000, Path: "/root/next.bin", Name: "next.bin"}
	state.UpdatedAtUTC = now.Add(time.Second)
	if err := store.SaveCloudInventoryRefreshState(state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestRestartCloudScanBoundaryIsAtomicAndLimitedPerDirectory(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state := startBoundaryScan(t, store, "acct", "scan-run")
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES('acct',1,'/','/old.bin','old.bin',0,7,3,'md5',10,'scan-run')`); err != nil {
		t.Fatal(err)
	}
	restartedAt := state.UpdatedAtUTC.Add(time.Second)
	state, err = store.RestartCloudScanBoundary(state, restartedAt)
	if err != nil || state.BoundaryRestartCount != 1 || state.NextOffset != 0 || state.NextPageBoundary != nil || state.Status != "running" {
		t.Fatalf("first restart state=%+v err=%v", state, err)
	}
	var cachedPath, seenRun string
	if err = store.db.QueryRow(`SELECT path,last_seen_run_id FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=1`).Scan(&cachedPath, &seenRun); err != nil || cachedPath != "/old.bin" || seenRun != "" {
		t.Fatalf("restart deleted or retained current-run mark on cache path=%q run=%q err=%v", cachedPath, seenRun, err)
	}
	loaded, err := store.LoadCloudScanState("acct")
	if err != nil || loaded.BoundaryRestartCount != 1 {
		t.Fatalf("derived scan restart count=%d err=%v", loaded.BoundaryRestartCount, err)
	}
	events, err := store.ListCloudScanEvents("acct", "scan-run", 0, 10)
	if err != nil || len(events) != 2 || events[0].Kind != "boundary_restart" || !events[0].RecordedAtUTC.Equal(restartedAt) || events[0].RequestsUsed != state.RequestsUsed {
		t.Fatalf("scan boundary event=%+v err=%v", events, err)
	}
	for attempt := int64(2); attempt <= 3; attempt++ {
		state.NextOffset = testCloudBoundaryOffset
		state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 1000 + attempt, Path: "/next.bin", Name: "next.bin"}
		state.UpdatedAtUTC = restartedAt.Add(time.Duration(attempt) * time.Second)
		if err = store.SaveCloudScanCheckpoint(state); err != nil {
			t.Fatal(err)
		}
		restartedAt = state.UpdatedAtUTC.Add(time.Second)
		state, err = store.RestartCloudScanBoundary(state, restartedAt)
		if attempt == 2 && (err != nil || state.BoundaryRestartCount != 2 || state.NextOffset != 0) {
			t.Fatalf("second restart state=%+v err=%v", state, err)
		}
		if attempt == 3 && (err != ErrCloudInventoryBoundaryRestartLimitReached || state.BoundaryRestartCount != 2 || state.NextOffset != testCloudBoundaryOffset) {
			t.Fatalf("third restart state=%+v err=%v", state, err)
		}
	}

	// A new run has no prior restart evidence, even for the same directory.
	newRun := CloudScanState{AccountFingerprint: "acct", RunID: "new-run", Status: "running", Phase: "listing", PendingCount: 1, CurrentDirectory: "/", StartedAtUTC: restartedAt, UpdatedAtUTC: restartedAt}
	if err = store.StartCloudScan(&newRun); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.LoadCloudScanState("acct")
	if err != nil || loaded.BoundaryRestartCount != 0 {
		t.Fatalf("new run inherited restart count=%d err=%v", loaded.BoundaryRestartCount, err)
	}
}

func TestRestartCloudScanBoundaryRollsBackWhenHistoryInsertFails(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state := startBoundaryScan(t, store, "acct", "scan-atomic")
	if _, err = store.db.Exec(`CREATE TRIGGER reject_scan_boundary_restart BEFORE INSERT ON cloud_inventory_scan_events WHEN NEW.event_kind='boundary_restart' BEGIN SELECT RAISE(ABORT,'injected boundary event failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RestartCloudScanBoundary(state, state.UpdatedAtUTC.Add(time.Second)); err == nil {
		t.Fatal("checkpoint reset committed without its boundary event")
	}
	loaded, err := store.LoadCloudScanState("acct")
	if err != nil || loaded.NextOffset != testCloudBoundaryOffset || loaded.BoundaryRestartCount != 0 {
		t.Fatalf("failed restart partially committed state=%+v err=%v", loaded, err)
	}
}

func TestRestartCloudRefreshBoundaryClearsOnlyCurrentStaging(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state := startBoundaryRefresh(t, store, "acct", "refresh-run")
	for _, item := range []struct {
		directory string
		id        int64
		name      string
	}{{"/root", 1, "old.bin"}, {"/root/child", 2, "child.bin"}} {
		if _, err = store.db.Exec(`INSERT INTO cloud_inventory_refresh_entries(account_fingerprint,run_id,directory,fs_id,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc) VALUES('acct','refresh-run',?,?,?,?,0,1,1,'',10)`, item.directory, item.id, item.directory+"/"+item.name, item.name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES('acct',3,'/root','/root/published.bin','published.bin',0,9,2,'',11,'old-run')`); err != nil {
		t.Fatal(err)
	}
	var queueBefore int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_refresh_queue WHERE account_fingerprint='acct' AND run_id='refresh-run'`).Scan(&queueBefore); err != nil {
		t.Fatal(err)
	}
	restartedAt := state.UpdatedAtUTC.Add(time.Second)
	state, err = store.RestartCloudInventoryRefreshBoundary(state, restartedAt)
	if err != nil || state.BoundaryRestartCount != 1 || state.NextOffset != 0 || state.NextPageBoundary != nil || state.Status != "running" {
		t.Fatalf("refresh restart state=%+v err=%v", state, err)
	}
	var currentStaging, childStaging, published, queueAfter int
	if err = store.db.QueryRow(`SELECT (SELECT count(*) FROM cloud_inventory_refresh_entries WHERE account_fingerprint='acct' AND run_id='refresh-run' AND directory='/root'),(SELECT count(*) FROM cloud_inventory_refresh_entries WHERE account_fingerprint='acct' AND run_id='refresh-run' AND directory='/root/child'),(SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=3),(SELECT count(*) FROM cloud_inventory_refresh_queue WHERE account_fingerprint='acct' AND run_id='refresh-run')`).Scan(&currentStaging, &childStaging, &published, &queueAfter); err != nil {
		t.Fatal(err)
	}
	if currentStaging != 0 || childStaging != 1 || published != 1 || queueAfter != queueBefore {
		t.Fatalf("refresh reset affected unrelated state staging=%d child=%d published=%d queue=%d/%d", currentStaging, childStaging, published, queueAfter, queueBefore)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", "refresh-run", "boundary_restart", 0, 10)
	if err != nil || len(events) != 1 || !events[0].RecordedAtUTC.Equal(restartedAt) || events[0].RestartReason != "page_boundary_mismatch" {
		t.Fatalf("refresh restart event=%+v err=%v", events, err)
	}
	loaded, err := store.LoadCloudInventoryRefreshState("acct")
	if err != nil || loaded.BoundaryRestartCount != 1 {
		t.Fatalf("derived refresh restart count=%d err=%v", loaded.BoundaryRestartCount, err)
	}
	for attempt := int64(2); attempt <= 3; attempt++ {
		state.NextOffset = testCloudBoundaryOffset
		state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 1000 + attempt, Path: "/root/next.bin", Name: "next.bin"}
		state.UpdatedAtUTC = restartedAt.Add(time.Duration(attempt) * time.Second)
		if err = store.SaveCloudInventoryRefreshState(state); err != nil {
			t.Fatal(err)
		}
		restartedAt = state.UpdatedAtUTC.Add(time.Second)
		state, err = store.RestartCloudInventoryRefreshBoundary(state, restartedAt)
		if attempt == 2 && (err != nil || state.BoundaryRestartCount != 2 || state.NextOffset != 0) {
			t.Fatalf("second refresh restart state=%+v err=%v", state, err)
		}
		if attempt == 3 && (err != ErrCloudInventoryBoundaryRestartLimitReached || state.BoundaryRestartCount != 2 || state.NextOffset != testCloudBoundaryOffset) {
			t.Fatalf("third refresh restart state=%+v err=%v", state, err)
		}
	}
}

func TestCloudBoundaryRestartCountUsesPartialIndex(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, query := range []struct {
		sql   string
		index string
	}{{`EXPLAIN QUERY PLAN SELECT count(*) FROM cloud_inventory_scan_events WHERE account_fingerprint=? AND run_id=? AND current_directory=? AND event_kind='boundary_restart'`, "cloud_inventory_scan_boundary_restart_count"}, {`EXPLAIN QUERY PLAN SELECT count(*) FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=? AND current_directory=? AND event_kind='boundary_restart'`, "cloud_inventory_refresh_boundary_restart_count"}} {
		rows, queryErr := store.db.Query(query.sql, "acct", "run", "/root")
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		var plan strings.Builder
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if queryErr = rows.Scan(&id, &parent, &unused, &detail); queryErr != nil {
				rows.Close()
				t.Fatal(queryErr)
			}
			plan.WriteString(detail)
		}
		if queryErr = rows.Err(); queryErr != nil {
			rows.Close()
			t.Fatal(queryErr)
		}
		rows.Close()
		if !strings.Contains(plan.String(), query.index) {
			t.Fatalf("count query did not use partial index %s: %s", query.index, plan.String())
		}
	}
}
