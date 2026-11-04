package taskstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSchemaPersistsNextCloudRetryBackoffForAllScanners(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for table, column := range map[string]string{
		"cloud_inventory_scan_state":    "retry_backoff_base_millis",
		"cloud_inventory_refresh_state": "retry_backoff_base_millis",
		"cloud_duplicate_scan_state":    "retry_backoff_base_millis",
	} {
		var count int
		if err = store.db.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s.%s count=%d err=%v", table, column, count, err)
		}
	}
}

func TestOpenMigratesV40QueueMonitorRetryStateWithoutInventingCooldown(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TABLE queue_monitor_retry_state; UPDATE store_meta SET value='40' WHERE key='schema_version'`); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 40, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, err := store.QueueMonitorRetryState("old-account")
	if err != nil || state != nil {
		t.Fatalf("migration inferred retry state=%+v err=%v", state, err)
	}
	var version, integrity string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	// Open applies all subsequent migrations, including the download manifest
	// schema; the cooldown evidence must remain absent at the current version.
	if version != fmt.Sprint(sqliteSchemaVersion) || integrity != "ok" {
		t.Fatalf("version=%s integrity=%s", version, integrity)
	}
}

func TestMigrateV30PreservesScanAndRefreshEventsAndAddsBoundaryRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	statements := []string{
		`DROP TRIGGER cloud_inventory_scan_events_no_update`,
		`DROP TRIGGER cloud_inventory_scan_events_no_delete`,
		`DROP INDEX cloud_inventory_scan_events_order`,
		`DROP TABLE cloud_inventory_scan_events`,
		`CREATE TABLE cloud_inventory_scan_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL,run_id TEXT NOT NULL,event_kind TEXT NOT NULL CHECK(event_kind IN ('started','resumed','paused','request_budget_reached','retry_scheduled','failed','completed')),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,current_directory TEXT NOT NULL,pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),request_budget INTEGER NOT NULL CHECK(request_budget>=0),requests_used INTEGER NOT NULL CHECK(requests_used>=0),pending_count INTEGER NOT NULL CHECK(pending_count>=0),recorded_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL)`,
		`CREATE INDEX cloud_inventory_scan_events_order ON cloud_inventory_scan_events(account_fingerprint,run_id,sequence DESC)`,
		`CREATE TRIGGER cloud_inventory_scan_events_no_update BEFORE UPDATE ON cloud_inventory_scan_events BEGIN SELECT RAISE(ABORT,'scan history events are append-only'); END`,
		`CREATE TRIGGER cloud_inventory_scan_events_no_delete BEFORE DELETE ON cloud_inventory_scan_events BEGIN SELECT RAISE(ABORT,'scan history events are append-only'); END`,
		`DROP TRIGGER cloud_inventory_refresh_events_no_update`,
		`DROP TRIGGER cloud_inventory_refresh_events_no_delete`,
		`DROP INDEX cloud_inventory_refresh_events_order`,
		`DROP INDEX cloud_inventory_refresh_runs_order`,
		`DROP TABLE cloud_inventory_refresh_events`,
		`CREATE TABLE cloud_inventory_refresh_events (sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),event_kind TEXT NOT NULL CHECK(event_kind IN ('started','resumed','paused','failed','completed','directory_published')),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,current_directory TEXT NOT NULL CHECK(length(current_directory)<=4096),pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),request_budget INTEGER NOT NULL DEFAULT 0 CHECK(request_budget>=0),requests_used INTEGER NOT NULL DEFAULT 0 CHECK(requests_used>=0),pending_count INTEGER NOT NULL CHECK(pending_count>=0),recorded_at_utc INTEGER NOT NULL,cloud_observed_at_utc INTEGER,directory_changed INTEGER CHECK(directory_changed IN (0,1)),added_count INTEGER CHECK(added_count>=0),removed_count INTEGER CHECK(removed_count>=0),changed_count INTEGER CHECK(changed_count>=0))`,
		`CREATE INDEX cloud_inventory_refresh_events_order ON cloud_inventory_refresh_events(account_fingerprint,run_id,sequence DESC)`,
		`CREATE INDEX cloud_inventory_refresh_runs_order ON cloud_inventory_refresh_events(account_fingerprint,event_kind,sequence DESC)`,
		`CREATE TRIGGER cloud_inventory_refresh_events_no_update BEFORE UPDATE ON cloud_inventory_refresh_events BEGIN SELECT RAISE(ABORT,'refresh history events are append-only'); END`,
		`CREATE TRIGGER cloud_inventory_refresh_events_no_delete BEFORE DELETE ON cloud_inventory_refresh_events BEGIN SELECT RAISE(ABORT,'refresh history events are append-only'); END`,
		`INSERT INTO cloud_inventory_scan_events(sequence,account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc,last_error) VALUES(17,'acct','scan-run','started','running','listing','/root',1,3,20,4,1,1793433600000000,'')`,
		`INSERT INTO cloud_inventory_refresh_events(sequence,account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc) VALUES(23,'acct','refresh-run','started','running','listing','/root',1,3,20,4,1,1793433600000000)`,
		`UPDATE store_meta SET value='30' WHERE key='schema_version'`,
	}
	for _, statement := range statements {
		if _, err = store.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	body, _ := json.Marshal(manifest{Version: 30, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TABLE cloud_inventory_scan_boundary_restart_count(blocker INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err = migrateV30(store, "/queue"); err == nil {
		t.Fatal("migration succeeded despite a conflicting index name")
	}
	var version string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != "30" {
		t.Fatalf("failed migration partially advanced schema=%q err=%v", version, err)
	}
	var oldIndex, oldTrigger int
	if err = store.db.QueryRow(`SELECT (SELECT count(*) FROM sqlite_master WHERE type='index' AND name='cloud_inventory_scan_events_order'),(SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name='cloud_inventory_scan_events_no_update')`).Scan(&oldIndex, &oldTrigger); err != nil || oldIndex != 1 || oldTrigger != 1 {
		t.Fatalf("failed migration did not restore old scan objects index=%d trigger=%d err=%v", oldIndex, oldTrigger, err)
	}
	if _, err = store.db.Exec(`DROP TABLE cloud_inventory_scan_boundary_restart_count`); err != nil {
		t.Fatal(err)
	}
	if err = migrateV30(store, "/queue"); err != nil {
		t.Fatal(err)
	}
	for table, wantSequence := range map[string]int64{"cloud_inventory_scan_events": 17, "cloud_inventory_refresh_events": 23} {
		var sequence int64
		if err = store.db.QueryRow(`SELECT sequence FROM ` + table + ` LIMIT 1`).Scan(&sequence); err != nil || sequence != wantSequence {
			t.Fatalf("%s sequence=%d want=%d err=%v", table, sequence, wantSequence, err)
		}
		var count int
		index := strings.TrimSuffix(table, "_events") + "_boundary_restart_count"
		if err = store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, index).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s boundary index count=%d err=%v", table, count, err)
		}
		if table == "cloud_inventory_scan_events" {
			_, err = store.db.Exec(`INSERT INTO cloud_inventory_scan_events(account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc,last_error) VALUES('acct','new-run','boundary_restart','running','listing','/root',1,3,20,5,1,1793433600000001,'')`)
		} else {
			_, err = store.db.Exec(`INSERT INTO cloud_inventory_refresh_events(account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc) VALUES('acct','new-run','boundary_restart','running','listing','/root',1,3,20,5,1,1793433600000001)`)
		}
		if err != nil {
			t.Fatalf("%s rejects boundary_restart: %v", table, err)
		}
		if _, err = store.db.Exec(`UPDATE ` + table + ` SET event_kind='failed'`); err == nil {
			t.Fatalf("%s lost append-only update trigger", table)
		}
		if _, err = store.db.Exec(`DELETE FROM ` + table); err == nil {
			t.Fatalf("%s lost append-only delete trigger", table)
		}
	}
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != "31" {
		t.Fatalf("schema version=%q err=%v", version, err)
	}
}

func TestOpenMigratesV31RefreshHistoryProjectionAndPreservesEvents(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	state := CloudInventoryRefreshState{AccountFingerprint: "acct-v31", RunID: "refresh-v31", RootPath: "/scope", CurrentDirectory: "/scope", Status: "running", Phase: "listing", RequestBudget: 5, StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReserveCloudInventoryRefreshRequest(state.AccountFingerprint, state.RunID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	state, err = store.LoadCloudInventoryRefreshState(state.AccountFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	state.Status, state.Phase, state.UpdatedAtUTC = "paused", "paused", now.Add(2*time.Second)
	if err = store.SaveCloudInventoryRefreshState(state); err != nil {
		t.Fatal(err)
	}
	var sequence int64
	if err = store.db.QueryRow(`SELECT max(sequence) FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=?`, state.AccountFingerprint, state.RunID).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DROP TABLE cloud_inventory_refresh_runs`,
		`ALTER TABLE cloud_inventory_refresh_events DROP COLUMN request_budget_added`,
		`ALTER TABLE cloud_inventory_refresh_events DROP COLUMN retry_at_utc`,
		`UPDATE store_meta SET value='31' WHERE key='schema_version'`,
	} {
		if _, err = db.Exec(statement); err != nil {
			db.Close()
			t.Fatalf("prepare v31 database with %q: %v", statement, err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	manifestBody, _ := json.Marshal(manifest{Version: 31, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), manifestBody); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runs, err := store.ListCloudInventoryRefreshRuns(state.AccountFingerprint, 0, 10)
	if err != nil || len(runs) != 1 || runs[0].RootPath != "/scope" || runs[0].RequestBudget != 5 || runs[0].RequestsUsed != 1 || runs[0].Status != "paused" {
		t.Fatalf("migrated summary=%+v err=%v", runs, err)
	}
	var migratedSequence int64
	if err = store.db.QueryRow(`SELECT max(sequence) FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=?`, state.AccountFingerprint, state.RunID).Scan(&migratedSequence); err != nil || migratedSequence != sequence {
		t.Fatalf("event sequence changed during v31 migration: %d -> %d err=%v", sequence, migratedSequence, err)
	}
	var integrity string
	if err = store.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("SQLite integrity=%q err=%v", integrity, err)
	}
}

func TestCloudPageBoundariesPersistAcrossReopenAndMigrateFromV26(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now().UTC()
	fullBoundary := &CloudInventoryPageBoundary{FsID: 91, Path: "/item-91", Name: "item-91", Size: 12, ProviderMtime: 34, ContentMD5: "md5-91"}
	full := CloudScanState{AccountFingerprint: "account-boundary", RunID: "full-boundary", Status: "paused", Phase: "paused", CurrentDirectory: "/", NextOffset: 100, StartedAtUTC: now, UpdatedAtUTC: now, NextPageBoundary: fullBoundary}
	if err = store.StartCloudScan(&full); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanCheckpoint(full); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES('account-boundary',90,'/','/prior','prior',0,4,1,'',1,'full-boundary')`); err != nil {
		t.Fatal(err)
	}
	refreshBoundary := &CloudInventoryPageBoundary{FsID: 92, Path: "/target/item-92", Name: "item-92", Directory: true, Size: 0, ProviderMtime: 35}
	refresh := CloudInventoryRefreshState{AccountFingerprint: "account-refresh-boundary", RunID: "refresh-boundary", RootPath: "/target", Status: "paused", Phase: "paused", CurrentDirectory: "/target", NextOffset: 100, StartedAtUTC: now, UpdatedAtUTC: now, NextPageBoundary: refreshBoundary}
	if err = store.StartCloudInventoryRefresh(&refresh); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudInventoryRefreshState(refresh); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	gotFull, err := store.LoadCloudScanState("account-boundary")
	if err != nil || !reflect.DeepEqual(gotFull.NextPageBoundary, fullBoundary) || gotFull.NextOffset != 100 {
		t.Fatalf("full boundary after reopen=%+v err=%v", gotFull, err)
	}
	gotRefresh, err := store.LoadCloudInventoryRefreshState("account-refresh-boundary")
	if err != nil || !reflect.DeepEqual(gotRefresh.NextPageBoundary, refreshBoundary) || gotRefresh.NextOffset != 100 {
		t.Fatalf("refresh boundary after reopen=%+v err=%v", gotRefresh, err)
	}
	if _, err = store.db.Exec(`ALTER TABLE cloud_inventory_scan_state DROP COLUMN next_page_boundary_json; ALTER TABLE cloud_inventory_refresh_state DROP COLUMN next_page_boundary_json; UPDATE store_meta SET value='26' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 26, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	gotFull, err = store.LoadCloudScanState("account-boundary")
	if err != nil || gotFull.NextPageBoundary != nil || gotFull.NextOffset != 0 {
		t.Fatalf("legacy full boundary migration=%+v err=%v", gotFull, err)
	}
	gotRefresh, err = store.LoadCloudInventoryRefreshState("account-refresh-boundary")
	if err != nil || gotRefresh.NextPageBoundary != nil || gotRefresh.NextOffset != 0 {
		t.Fatalf("legacy refresh boundary migration=%+v err=%v", gotRefresh, err)
	}
	var lastSeenRun string
	if err = store.db.QueryRow(`SELECT last_seen_run_id FROM cloud_inventory WHERE account_fingerprint='account-boundary' AND fs_id=90`).Scan(&lastSeenRun); err != nil || lastSeenRun != "" {
		t.Fatalf("legacy page markers not cleared for safe replay: run=%q err=%v", lastSeenRun, err)
	}
	var version string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != fmt.Sprint(sqliteSchemaVersion) {
		t.Fatalf("schema after boundary migration=%q err=%v", version, err)
	}
}

func TestOpenMigratesV25RetryBackoffBaseAndPreservesSchedule(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	retryAt := now.Add(47 * time.Second)
	full := CloudScanState{AccountFingerprint: "acct", RunID: "full", Status: "paused", Phase: "paused", CurrentDirectory: "/", StartedAtUTC: now.Add(-time.Minute), UpdatedAtUTC: now, RetryDelayMillis: 20000, RetryAtUTC: &retryAt, LastError: "rate limited"}
	if err = store.SaveCloudScanCheckpoint(full); err != nil {
		t.Fatal(err)
	}
	refresh := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "refresh", RootPath: "/root", Status: "paused", Phase: "paused", CurrentDirectory: "/root", StartedAtUTC: now.Add(-time.Minute), UpdatedAtUTC: now, RetryDelayMillis: 30000, RetryAtUTC: &retryAt, LastError: "rate limited"}
	if err = store.StartCloudInventoryRefresh(&refresh); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudInventoryRefreshState(refresh); err != nil {
		t.Fatal(err)
	}
	duplicate := CloudDuplicateScanState{AccountFingerprint: "acct", RunID: "duplicate", Status: "paused", Phase: "paused", StartedAtUTC: now.Add(-time.Minute), UpdatedAtUTC: now, RetryDelayMillis: 40000, RetryAtUTC: &retryAt, LastError: "rate limited"}
	if err = store.SaveCloudDuplicateScanState(duplicate, false); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`ALTER TABLE cloud_inventory_scan_state DROP COLUMN retry_backoff_base_millis; ALTER TABLE cloud_inventory_refresh_state DROP COLUMN retry_backoff_base_millis; ALTER TABLE cloud_duplicate_scan_state DROP COLUMN retry_backoff_base_millis; UPDATE store_meta SET value='25' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 25, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	checks := []struct {
		name string
		got  int64
		want int64
	}{
		{"full scan", mustLoadFullBackoff(t, store), 40000},
		{"directory refresh", mustLoadRefreshBackoff(t, store), 60000},
		{"duplicate scan", mustLoadDuplicateBackoff(t, store), 60000},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s next backoff=%d ms, want %d", check.name, check.got, check.want)
		}
	}
}

func mustLoadFullBackoff(t *testing.T, store *Store) int64 {
	t.Helper()
	state, err := store.LoadCloudScanState("acct")
	if err != nil {
		t.Fatal(err)
	}
	return state.RetryBackoffBaseMillis
}

func mustLoadRefreshBackoff(t *testing.T, store *Store) int64 {
	t.Helper()
	state, err := store.LoadCloudInventoryRefreshState("acct")
	if err != nil {
		t.Fatal(err)
	}
	return state.RetryBackoffBaseMillis
}

func mustLoadDuplicateBackoff(t *testing.T, store *Store) int64 {
	t.Helper()
	state, err := store.LoadCloudDuplicateScanState("acct")
	if err != nil {
		t.Fatal(err)
	}
	return state.RetryBackoffBaseMillis
}

func TestOpenMigratesV22RetryScheduleColumnsAndPreservesExistingState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 31, 2, 3, 4, 0, time.UTC)
	if err = store.SaveCloudScanState("acct", "full-run", "paused", "paused", "/", 0, 4, 5, now.Add(-time.Minute), now, "old error"); err != nil {
		t.Fatal(err)
	}
	refresh := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "refresh-run", RootPath: "/root", Status: "paused", Phase: "paused", CurrentDirectory: "/root", StartedAtUTC: now.Add(-time.Minute), UpdatedAtUTC: now, LastError: "old refresh error"}
	if err = store.StartCloudInventoryRefresh(&refresh); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`ALTER TABLE cloud_inventory_scan_state DROP COLUMN retry_at_utc; ALTER TABLE cloud_inventory_scan_state DROP COLUMN retry_delay_millis; ALTER TABLE cloud_inventory_refresh_state DROP COLUMN retry_at_utc; ALTER TABLE cloud_inventory_refresh_state DROP COLUMN retry_delay_millis; UPDATE store_meta SET value='22' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 22, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	full, err := store.LoadCloudScanState("acct")
	if err != nil || full.RunID != "full-run" || full.LastError != "old error" || full.RetryDelayMillis != 0 || full.RetryAtUTC != nil {
		t.Fatalf("full state after v22 migration=%+v err=%v", full, err)
	}
	gotRefresh, err := store.LoadCloudInventoryRefreshState("acct")
	if err != nil || gotRefresh.RunID != "refresh-run" || gotRefresh.LastError != "old refresh error" || gotRefresh.RetryDelayMillis != 0 || gotRefresh.RetryAtUTC != nil {
		t.Fatalf("refresh state after v22 migration=%+v err=%v", gotRefresh, err)
	}
}

func TestOpenMigratesV17RefreshStagingTable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TABLE IF EXISTS cloud_inventory_refresh_entries; UPDATE store_meta SET value='17' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 17, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version, table string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != fmt.Sprint(sqliteSchemaVersion) {
		t.Fatalf("schema version=%q err=%v", version, err)
	}
	if err = store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='cloud_inventory_refresh_entries'`).Scan(&table); err != nil || table != "cloud_inventory_refresh_entries" {
		t.Fatalf("refresh staging table=%q err=%v", table, err)
	}
}

func TestOpenMigratesV18RefreshEventHistoryAndPreservesRefreshState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 1, 2, 3, 0, time.UTC)
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "run-18", RootPath: "/root", Status: "paused", Phase: "paused", CurrentDirectory: "/root", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE store_meta SET value='18' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 18, Scope: "/queue"})
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
	loaded, err := store.LoadCloudInventoryRefreshState("acct")
	if err != nil || loaded.RunID != "run-18" || loaded.Status != "paused" || !loaded.StartedAtUTC.Equal(now) {
		t.Fatalf("refresh state after migration=%+v err=%v", loaded, err)
	}
	var table string
	if err = store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='cloud_inventory_refresh_events'`).Scan(&table); err != nil || table != "cloud_inventory_refresh_events" {
		t.Fatalf("refresh events table=%q err=%v", table, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the SQLite migration commits but before its manifest update.
	body, _ = json.Marshal(manifest{Version: 18, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	var recovered manifest
	if err != nil || json.Unmarshal(manifestBody, &recovered) != nil || recovered.Version != sqliteSchemaVersion {
		t.Fatalf("manifest after migration recovery=%+v err=%v", recovered, err)
	}
}

func TestOpenMigratesV19RefreshRunIndexAndRecoversManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP INDEX IF EXISTS cloud_inventory_refresh_runs_order; UPDATE store_meta SET value='19' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 19, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	var version, index string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != fmt.Sprint(sqliteSchemaVersion) {
		t.Fatalf("schema version after v19 migration=%q err=%v", version, err)
	}
	if err = store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='cloud_inventory_refresh_runs_order'`).Scan(&index); err != nil || index != "cloud_inventory_refresh_runs_order" {
		t.Fatalf("refresh run index=%q err=%v", index, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate the index migration committing before its manifest update.
	body, _ = json.Marshal(manifest{Version: 19, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	var recovered manifest
	if err != nil || json.Unmarshal(manifestBody, &recovered) != nil || recovered.Version != sqliteSchemaVersion {
		t.Fatalf("manifest after v19 recovery=%+v err=%v", recovered, err)
	}
}

func TestOpenMigratesV21RefreshChangeDetailsAndRecoversManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Date(2026, 10, 30, 4, 5, 6, 0, time.UTC)
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_refresh_events(account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,pending_count,recorded_at_utc,cloud_observed_at_utc,directory_changed,added_count,removed_count,changed_count) VALUES('acct','old-run','directory_published','running','listing','/root',1,0,0,?,?,0,0,0,0)`, now.UnixMicro(), now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TABLE cloud_inventory_refresh_changes; UPDATE store_meta SET value='21' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 21, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	var version string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != fmt.Sprint(sqliteSchemaVersion) {
		t.Fatalf("schema version=%q err=%v", version, err)
	}
	changes, err := store.ListCloudInventoryRefreshChanges("acct", "old-run", 1, 0, 10)
	if err != nil || len(changes) != 0 {
		t.Fatalf("pre-migration event fabricated change details: changes=%+v err=%v", changes, err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_refresh_changes(account_fingerprint,run_id,event_sequence,ordinal,change_kind,new_snapshot,recorded_at_utc,cloud_observed_at_utc) VALUES('acct','old-run',1,1,'added','{}',?,?)`, now.UnixMicro(), now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_refresh_changes SET change_kind='added'`); err == nil {
		t.Fatal("refresh change history allowed update")
	}
	if _, err = store.db.Exec(`DELETE FROM cloud_inventory_refresh_changes`); err == nil {
		t.Fatal("refresh change history allowed delete")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after schema 22 committed but before the manifest was advanced.
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	var recovered manifest
	if err != nil || json.Unmarshal(manifestBody, &recovered) != nil || recovered.Version != sqliteSchemaVersion {
		t.Fatalf("manifest after v21 migration recovery=%+v err=%v", recovered, err)
	}
}

func TestOpenMigratesV20RefreshDeltaCountsAndRecoversManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	when := time.Date(2026, 10, 30, 4, 5, 6, 0, time.UTC)
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_refresh_events(account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,pending_count,recorded_at_utc) VALUES('acct','old-run','started','running','listing','/root',0,0,1,?)`, when.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`ALTER TABLE cloud_inventory_refresh_events DROP COLUMN changed_count; ALTER TABLE cloud_inventory_refresh_events DROP COLUMN removed_count; ALTER TABLE cloud_inventory_refresh_events DROP COLUMN added_count; UPDATE store_meta SET value='20' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	manifestBody, _ := json.Marshal(manifest{Version: 20, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), manifestBody); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", "old-run", "", 0, 10)
	if err != nil || len(events) != 1 || events[0].AddedCount != nil || events[0].RemovedCount != nil || events[0].ChangedCount != nil {
		t.Fatalf("old event was not preserved with unknown delta: events=%+v err=%v", events, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the schema migration commits but before manifest publication.
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), manifestBody); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != fmt.Sprint(sqliteSchemaVersion) {
		t.Fatalf("schema version after migration recovery=%q err=%v", version, err)
	}
	manifestBody, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	var recovered manifest
	if err != nil || json.Unmarshal(manifestBody, &recovered) != nil || recovered.Version != sqliteSchemaVersion {
		t.Fatalf("manifest after v20 recovery=%+v err=%v", recovered, err)
	}
}

func TestOpenMigratesV16DuplicateScanByteProgressAndBackfillsVerifiedBytes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	entries := []CloudInventoryEntry{{FsID: 1, Path: "/a.bin", Name: "a.bin", Size: 7, ProviderMtime: 1}, {FsID: 2, Path: "/b.bin", Name: "b.bin", Size: 7, ProviderMtime: 2}}
	if err = store.UpsertCloudInventoryPage("acct", "inventory", "/", entries, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveVerifiedCloudContentHash("acct", entries[0], strings.Repeat("a", 64), now); err != nil {
		t.Fatal(err)
	}
	state := CloudDuplicateScanState{AccountFingerprint: "acct", RunID: "resume", Status: "paused", Phase: "paused", NextFsID: 1, TotalFiles: 2, ProcessedFiles: 1, TotalBytes: 999, ProcessedBytes: 7, StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`ALTER TABLE cloud_duplicate_scan_state DROP COLUMN total_bytes; ALTER TABLE cloud_duplicate_scan_state DROP COLUMN processed_bytes; UPDATE store_meta SET value='16' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 16, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.LoadCloudDuplicateScanState("acct")
	if err != nil || loaded.ProcessedFiles != 1 || loaded.TotalFiles != 2 || loaded.ProcessedBytes != 7 || loaded.TotalBytes != 14 {
		t.Fatalf("migrated duplicate scan progress=%+v err=%v", loaded, err)
	}
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	var got manifest
	if err != nil || json.Unmarshal(manifestBody, &got) != nil || got.Version != sqliteSchemaVersion {
		t.Fatalf("manifest after byte-progress migration=%+v err=%v", got, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	oldManifest, _ := json.Marshal(manifest{Version: 16, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), oldManifest); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatalf("v17 database did not recover its lagging manifest: %v", err)
	}
	defer store.Close()
	manifestBody, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil || json.Unmarshal(manifestBody, &got) != nil || got.Version != sqliteSchemaVersion {
		t.Fatalf("manifest after migration recovery=%+v err=%v", got, err)
	}
}

func TestOpenMigratesV2SchemaAndPreservesTaskEvidence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveWithEvent(1, []byte(`{"status":"pending","updated":"2026-10-29T00:00:00Z"}`), Event{Kind: "job_registered", EvidenceType: "local_state", Evidence: json.RawMessage(`{"source":"test"}`)}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TABLE admin_sessions; UPDATE store_meta SET value='2' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 2, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.migrationAction != "migrated_v2" {
		t.Fatalf("migration action = %q", s.migrationAction)
	}
	jobs, err := s.Load()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs after migration: %d %v", len(jobs), err)
	}
	events, err := s.Events(1)
	if err != nil || len(events) != 1 || events[0].Kind != "job_registered" {
		t.Fatalf("events after migration: %#v %v", events, err)
	}
}

func TestOpenMigratesV14DirectorySizeCacheAndRecoversManifestPublish(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "migrates-v14-schema"
		if committed {
			name = "recovers-committed-schema"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "queue")
			store, err := Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			observed := time.Date(2026, 10, 29, 3, 0, 0, 0, time.UTC)
			if err = store.UpsertCloudInventoryPage("acct", "run", "/", []CloudInventoryEntry{{FsID: 1, Path: "/cached.bin", Name: "cached.bin", Size: 17}}, observed, nil); err != nil {
				t.Fatal(err)
			}
			if !committed {
				if _, err = store.db.Exec(`DROP TABLE cloud_directory_size_cache; DROP TABLE cloud_inventory_generations`); err != nil {
					t.Fatal(err)
				}
			}
			version := "14"
			if committed {
				version = "15"
			}
			if _, err = store.db.Exec(`UPDATE store_meta SET value=? WHERE key='schema_version'`, version); err != nil {
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			manifestBody, _ := json.Marshal(manifest{Version: 14, Scope: "/queue"})
			if err = atomicWrite(filepath.Join(dir, "manifest.json"), manifestBody); err != nil {
				t.Fatal(err)
			}
			store, err = Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			var gotVersion string
			if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&gotVersion); err != nil || gotVersion != fmt.Sprint(sqliteSchemaVersion) {
				t.Fatalf("schema version=%s err=%v", gotVersion, err)
			}
			var gotManifest manifest
			body, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil || json.Unmarshal(body, &gotManifest) != nil || gotManifest.Version != sqliteSchemaVersion {
				t.Fatalf("manifest=%+v err=%v", gotManifest, err)
			}
			var size int64
			if err = store.db.QueryRow(`SELECT size_bytes FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=1`).Scan(&size); err != nil || size != 17 {
				t.Fatalf("inventory was not preserved: size=%d err=%v", size, err)
			}
			var tableCount int
			if err = store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('cloud_inventory_generations','cloud_directory_size_cache')`).Scan(&tableCount); err != nil || tableCount != 2 {
				t.Fatalf("size cache tables=%d err=%v", tableCount, err)
			}
		})
	}
}

func TestOpenMigratesV15ScopedCloudRefreshWithoutChangingFullScanState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	full := CloudScanState{AccountFingerprint: "acct", RunID: "full-run", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&full); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "inventory-run", "/", []CloudInventoryEntry{{FsID: 1, Path: "/folder", Name: "folder", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
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
	fullAfter, err := store.LoadCloudScanState("acct")
	if err != nil || fullAfter.RunID != full.RunID || fullAfter.Status != "running" || fullAfter.PendingCount != 1 {
		t.Fatalf("full scan state changed by migration: state=%+v err=%v", fullAfter, err)
	}
	queued, err := store.CloudScanQueueHead("acct", full.RunID)
	if err != nil || queued != "/" {
		t.Fatalf("full scan queue changed by migration: path=%q err=%v", queued, err)
	}
	items, meta, total, err := store.ListCloudInventory("acct", "/", 0, 10)
	if err != nil || total != 1 || len(items) != 1 || items[0].Path != "/folder" || meta.Status != "complete" {
		t.Fatalf("inventory changed by migration: items=%+v meta=%+v total=%d err=%v", items, meta, total, err)
	}
	var newTables int
	if err = store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('cloud_inventory_refresh_state','cloud_inventory_refresh_queue')`).Scan(&newTables); err != nil || newTables != 2 {
		t.Fatalf("scoped refresh tables=%d err=%v", newTables, err)
	}
}

func TestOpenMigratesV8CloudListingIndexAndPreservesInventory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", []CloudInventoryEntry{
		{FsID: 1, Path: "/docs", Name: "docs", Directory: true},
		{FsID: 2, Path: "/readme.txt", Name: "readme.txt", Size: 9},
	}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TRIGGER cloud_inventory_file_size_update; DROP TRIGGER cloud_inventory_file_size_delete; DROP TRIGGER cloud_inventory_file_size_insert; DROP TABLE cloud_inventory_file_sizes; DROP INDEX IF EXISTS cloud_inventory_parent_name_id; UPDATE store_meta SET value='8' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	manifestBody, _ := json.Marshal(manifest{Version: 8, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), manifestBody); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.migrationAction != "migrated_v8" {
		t.Fatalf("migration action = %q", store.migrationAction)
	}
	items, meta, total, err := store.ListCloudInventory("acct", "/", 0, 10)
	if err != nil || total != 2 || len(items) != 2 || meta.Status != "complete" || items[0].Name != "docs" {
		t.Fatalf("inventory after migration: items=%+v meta=%+v total=%d err=%v", items, meta, total, err)
	}
	rows, err := store.db.Query(`EXPLAIN QUERY PLAN SELECT fs_id,path,name,is_dir,size_bytes,provider_mtime,content_md5 FROM cloud_inventory WHERE account_fingerprint=? AND parent_path=? ORDER BY name,fs_id LIMIT ? OFFSET ?`, "acct", "/", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	queryPlan := strings.Join(plan, " | ")
	if !strings.Contains(queryPlan, "cloud_inventory_parent_name_id") || strings.Contains(queryPlan, "TEMP B-TREE") {
		t.Fatalf("cloud listing did not use ordered parent index: %s", queryPlan)
	}
}

func TestOpenFinishesV8MigrationAfterDatabaseCommitBeforeManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TRIGGER cloud_inventory_file_size_update; DROP TRIGGER cloud_inventory_file_size_delete; DROP TRIGGER cloud_inventory_file_size_insert; DROP TABLE cloud_inventory_file_sizes; DROP INDEX cloud_inventory_parent_name_id; CREATE INDEX cloud_inventory_parent_path ON cloud_inventory(account_fingerprint,parent_path); UPDATE store_meta SET value='8' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	manifestBody, _ := json.Marshal(manifest{Version: 8, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), manifestBody); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`BEGIN; DROP INDEX cloud_inventory_parent_path; CREATE INDEX cloud_inventory_parent_name_id ON cloud_inventory(account_fingerprint,parent_path,name,fs_id); UPDATE store_meta SET value='9' WHERE key='schema_version'; COMMIT`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manifestBody, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got manifest
	if err = json.Unmarshal(manifestBody, &got); err != nil || got.Version != sqliteSchemaVersion || store.migrationAction != "migrated_v8" {
		t.Fatalf("interrupted migration not finalized: manifest=%+v action=%q err=%v", got, store.migrationAction, err)
	}
}

func TestOpenMigratesV9DuplicateSizeIndexAndPreservesInventory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", []CloudInventoryEntry{{FsID: 1, Path: "/file.bin", Name: "file.bin", Size: 7}}, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER cloud_inventory_file_size_update; DROP TRIGGER cloud_inventory_file_size_delete; DROP TRIGGER cloud_inventory_file_size_insert; DROP TABLE cloud_inventory_file_sizes; DROP INDEX cloud_inventory_duplicate_size; UPDATE store_meta SET value='9' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 9, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.migrationAction != "migrated_v9" {
		t.Fatalf("migration action = %q", store.migrationAction)
	}
	items, meta, total, err := store.ListCloudInventory("acct", "/", 0, 10)
	if err != nil || total != 1 || len(items) != 1 || meta.Status != "complete" || items[0].FsID != 1 {
		t.Fatalf("inventory after migration: items=%+v meta=%+v total=%d err=%v", items, meta, total, err)
	}
}

func TestOpenFinishesV9MigrationAfterDatabaseCommitBeforeManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER cloud_inventory_file_size_update; DROP TRIGGER cloud_inventory_file_size_delete; DROP TRIGGER cloud_inventory_file_size_insert; DROP TABLE cloud_inventory_file_sizes; DROP INDEX cloud_inventory_duplicate_size; UPDATE store_meta SET value='9' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 9, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`BEGIN; CREATE INDEX cloud_inventory_duplicate_size ON cloud_inventory(account_fingerprint,is_dir,size_bytes,fs_id); UPDATE store_meta SET value='10' WHERE key='schema_version'; COMMIT`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	body, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got manifest
	if err = json.Unmarshal(body, &got); err != nil || got.Version != sqliteSchemaVersion || store.migrationAction != "migrated_v9" {
		t.Fatalf("interrupted migration not finalized: manifest=%+v action=%q err=%v", got, store.migrationAction, err)
	}
}

func TestOpenMigratesV10VerifiedDuplicateIndexAndPreservesHashes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	entry := CloudInventoryEntry{FsID: 31, Path: "/verified.bin", Name: "verified.bin", Size: 7, ProviderMtime: 42}
	if err = store.UpsertCloudInventoryPage("acct", "inventory", "/", []CloudInventoryEntry{entry}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveVerifiedCloudContentHash("acct", entry, strings.Repeat("a", 64), now); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER cloud_inventory_file_size_update; DROP TRIGGER cloud_inventory_file_size_delete; DROP TRIGGER cloud_inventory_file_size_insert; DROP TABLE cloud_inventory_file_sizes; DROP INDEX cloud_content_hashes_duplicate_group; UPDATE store_meta SET value='10' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 10, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.migrationAction != "migrated_v10" {
		t.Fatalf("migration action=%q", store.migrationAction)
	}
	var digest string
	if err = store.db.QueryRow(`SELECT sha256 FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id=31`).Scan(&digest); err != nil || digest != strings.Repeat("a", 64) {
		t.Fatalf("verified hash after migration=%q err=%v", digest, err)
	}
	var version string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != fmt.Sprint(sqliteSchemaVersion) {
		t.Fatalf("schema version=%q err=%v", version, err)
	}
}

func TestOpenFinishesV10MigrationAfterDatabaseCommitBeforeManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER cloud_inventory_file_size_update; DROP TRIGGER cloud_inventory_file_size_delete; DROP TRIGGER cloud_inventory_file_size_insert; DROP TABLE cloud_inventory_file_sizes; DROP INDEX cloud_content_hashes_duplicate_group; UPDATE store_meta SET value='10' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 10, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`BEGIN; CREATE INDEX cloud_content_hashes_duplicate_group ON cloud_content_hashes(account_fingerprint,sha256,size_bytes,fs_id); UPDATE store_meta SET value='11' WHERE key='schema_version'; COMMIT`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	body, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got manifest
	if err = json.Unmarshal(body, &got); err != nil || got.Version != sqliteSchemaVersion || store.migrationAction != "migrated_v10" {
		t.Fatalf("interrupted migration not finalized: manifest=%+v action=%q err=%v", got, store.migrationAction, err)
	}
}

func TestOpenMigratesV11FileSizeSummaryAndBackfillsInventory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	entries := []CloudInventoryEntry{
		{FsID: 41, Path: "/one.bin", Name: "one.bin", Size: 13},
		{FsID: 42, Path: "/two.bin", Name: "two.bin", Size: 13},
		{FsID: 43, Path: "/folder", Name: "folder", Directory: true, Size: 13},
	}
	if err = store.UpsertCloudInventoryPage("acct", "inventory", "/", entries, now, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER cloud_inventory_file_size_update; DROP TRIGGER cloud_inventory_file_size_delete; DROP TRIGGER cloud_inventory_file_size_insert; DROP TABLE cloud_inventory_file_sizes; UPDATE store_meta SET value='11' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 11, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.migrationAction != "migrated_v11" {
		t.Fatalf("migration action=%q", store.migrationAction)
	}
	var count int
	if err = store.db.QueryRow(`SELECT file_count FROM cloud_inventory_file_sizes WHERE account_fingerprint='acct' AND size_bytes=13`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("backfilled size count=%d err=%v", count, err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "inventory", "/", append(entries, CloudInventoryEntry{FsID: 44, Path: "/three.bin", Name: "three.bin", Size: 13}), now.Add(time.Second), nil); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT file_count FROM cloud_inventory_file_sizes WHERE account_fingerprint='acct' AND size_bytes=13`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("post-migration insert count=%d err=%v", count, err)
	}
}

func TestOpenFinishesV11MigrationAfterDatabaseCommitBeforeManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "inventory", "/", []CloudInventoryEntry{{FsID: 51, Path: "/file.bin", Name: "file.bin", Size: 17}}, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER cloud_inventory_file_size_update; DROP TRIGGER cloud_inventory_file_size_delete; DROP TRIGGER cloud_inventory_file_size_insert; DROP TABLE cloud_inventory_file_sizes; UPDATE store_meta SET value='11' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 11, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`BEGIN; CREATE TABLE cloud_inventory_file_sizes (account_fingerprint TEXT NOT NULL,size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),file_count INTEGER NOT NULL CHECK(file_count>=0),PRIMARY KEY(account_fingerprint,size_bytes)); INSERT INTO cloud_inventory_file_sizes SELECT account_fingerprint,size_bytes,count(*) FROM cloud_inventory WHERE is_dir=0 GROUP BY account_fingerprint,size_bytes; CREATE TRIGGER cloud_inventory_file_size_insert AFTER INSERT ON cloud_inventory WHEN NEW.is_dir=0 BEGIN INSERT INTO cloud_inventory_file_sizes(account_fingerprint,size_bytes,file_count) VALUES(NEW.account_fingerprint,NEW.size_bytes,1) ON CONFLICT(account_fingerprint,size_bytes) DO UPDATE SET file_count=file_count+1; END; CREATE TRIGGER cloud_inventory_file_size_delete AFTER DELETE ON cloud_inventory WHEN OLD.is_dir=0 BEGIN UPDATE cloud_inventory_file_sizes SET file_count=file_count-1 WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes; DELETE FROM cloud_inventory_file_sizes WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes AND file_count<=0; END; CREATE TRIGGER cloud_inventory_file_size_update AFTER UPDATE OF account_fingerprint,is_dir,size_bytes ON cloud_inventory WHEN OLD.account_fingerprint<>NEW.account_fingerprint OR OLD.is_dir<>NEW.is_dir OR OLD.size_bytes<>NEW.size_bytes BEGIN UPDATE cloud_inventory_file_sizes SET file_count=file_count-1 WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes AND OLD.is_dir=0; DELETE FROM cloud_inventory_file_sizes WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes AND file_count<=0; INSERT INTO cloud_inventory_file_sizes(account_fingerprint,size_bytes,file_count) SELECT NEW.account_fingerprint,NEW.size_bytes,1 WHERE NEW.is_dir=0 ON CONFLICT(account_fingerprint,size_bytes) DO UPDATE SET file_count=file_count+1; END; UPDATE store_meta SET value='12' WHERE key='schema_version'; COMMIT`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	body, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got manifest
	if err = json.Unmarshal(body, &got); err != nil || got.Version != sqliteSchemaVersion || store.migrationAction != "migrated_v11" {
		t.Fatalf("interrupted migration not finalized: manifest=%+v action=%q err=%v", got, store.migrationAction, err)
	}
}

func TestOpenMigratesV12CloudScanWorklistInOrder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = store.SaveCloudScanState("acct", "run", "paused", "paused", "/", 0, 4, 350, now.Add(-time.Minute), now, "temporary"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_scan_state SET next_offset=100,pending_json=? WHERE account_fingerprint='acct'`, []byte(`["/","/a","/b"]`)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DELETE FROM cloud_inventory_scan_queue; UPDATE store_meta SET value='12' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 12, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.db.Query(`SELECT sequence,path FROM cloud_inventory_scan_queue WHERE account_fingerprint='acct' AND run_id='run' ORDER BY sequence`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := []string{"/", "/a", "/b"}
	for i, path := range want {
		var sequence int
		var got string
		if !rows.Next() || rows.Scan(&sequence, &got) != nil || sequence != i || got != path {
			t.Fatalf("queue row %d: sequence=%d path=%q", i, sequence, got)
		}
	}
	if rows.Next() {
		t.Fatal("unexpected additional queue row")
	}
	var version string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != fmt.Sprint(sqliteSchemaVersion) {
		t.Fatalf("schema version=%q err=%v", version, err)
	}
}

func TestOpenFinishesV12QueueMigrationAfterDatabaseCommitBeforeManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = store.SaveCloudScanState("acct", "run", "paused", "paused", "/", 0, 2, 150, now.Add(-time.Minute), now, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_scan_state SET next_offset=100,pending_json='["/","/next"]'; INSERT INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES('acct','run',0,'/'),('acct','run',1,'/next'); UPDATE store_meta SET value='13' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 12, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	body, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got manifest
	if err = json.Unmarshal(body, &got); err != nil || got.Version != sqliteSchemaVersion || store.migrationAction != "migrated_v12" {
		t.Fatalf("interrupted migration not finalized: manifest=%+v action=%q err=%v", got, store.migrationAction, err)
	}
}

func TestOpenMigratesV13PendingCountAndRecoversPublishedCount(t *testing.T) {
	newFixture := func(t *testing.T, version string, pendingCount int64) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "queue")
		store, err := Open(dir, "/queue")
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		if err = store.SaveCloudScanState("acct", "run", "paused", "paused", "/", 0, 2, 3, now, now, ""); err != nil {
			t.Fatal(err)
		}
		if _, err = store.db.Exec(`INSERT INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES('acct','run',0,'/'),('acct','run',1,'/child')`); err != nil {
			t.Fatal(err)
		}
		if version == "13" {
			if _, err = store.db.Exec(`ALTER TABLE cloud_inventory_scan_state DROP COLUMN pending_count`); err != nil {
				t.Fatal(err)
			}
		} else if _, err = store.db.Exec(`UPDATE cloud_inventory_scan_state SET pending_count=? WHERE account_fingerprint='acct'`, pendingCount); err != nil {
			t.Fatal(err)
		}
		if _, err = store.db.Exec(`UPDATE store_meta SET value=? WHERE key='schema_version'`, version); err != nil {
			t.Fatal(err)
		}
		if err = store.Close(); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(manifest{Version: 13, Scope: "/queue"})
		if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("backfill from queue", func(t *testing.T) {
		dir := newFixture(t, "13", 0)
		store, err := Open(dir, "/queue")
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		state, err := store.LoadCloudScanState("acct")
		if err != nil || state.PendingCount != 2 {
			t.Fatalf("migrated pending count=%d err=%v", state.PendingCount, err)
		}
	})
	t.Run("finish manifest after database commit", func(t *testing.T) {
		dir := newFixture(t, "14", 2)
		store, err := Open(dir, "/queue")
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		body, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var got manifest
		if err = json.Unmarshal(body, &got); err != nil || got.Version != sqliteSchemaVersion {
			t.Fatalf("v14 manifest=%+v err=%v", got, err)
		}
	})
	t.Run("reject inconsistent committed count", func(t *testing.T) {
		dir := newFixture(t, "14", 0)
		if store, err := Open(dir, "/queue"); err == nil {
			store.Close()
			t.Fatal("opened a v14 database whose pending count disagrees with its queue")
		}
	})
}

func writeLegacyStore(t *testing.T, dir string, records map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "jobs"), 0700); err != nil {
		t.Fatal(err)
	}
	manifestBody, _ := json.Marshal(manifest{Version: 1, Scope: "/queue"})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifestBody, 0600); err != nil {
		t.Fatal(err)
	}
	for name, body := range records {
		if err := os.WriteFile(filepath.Join(dir, "jobs", name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenMigratesV1JobsWithoutFabricatingPastTimestamps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	first := `{"version":1,"file":{"fs_id":1,"path":"/queue/one.bin","server_filename":"one.bin","size":1},"mode":"keep","status":"done","attempts":0,"failures":0,"created":"2026-10-20T00:00:00Z","updated":"2026-10-21T00:00:00Z"}`
	second := `{"version":1,"file":{"fs_id":2,"path":"/queue/two.bin","server_filename":"two.bin","size":2},"mode":"delete","status":"pending","attempts":0,"failures":0,"created":"2026-10-22T00:00:00Z","updated":"2026-10-22T00:00:00Z"}`
	writeLegacyStore(t, dir, map[string]string{"1.json": first, "2.json": second})
	for name, body := range map[string]string{
		"credentials.json": `{"version":1,"access_token":"sidecar-token"}`,
		"settings.json":    `{"download_mode":"delete"}`,
		"account.json":     `{"version":1,"uk":"123"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	intakeDir := filepath.Join(dir, "intake")
	if err := os.Mkdir(intakeDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(intakeDir, "0123456789abcdef0123456789abcdef.json"), []byte(`{"state":"finished"}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	jobs, err := store.Load()
	if err != nil || string(jobs[1]) != first || string(jobs[2]) != second {
		t.Fatalf("migrated payload mismatch: jobs=%q err=%v", jobs, err)
	}
	wantTimes := map[int64][2]string{
		1: {"2026-10-20T00:00:00Z", "2026-10-21T00:00:00Z"},
		2: {"2026-10-22T00:00:00Z", "2026-10-22T00:00:00Z"},
	}
	for _, id := range []int64{1, 2} {
		events, err := store.Events(id)
		if err != nil || len(events) != 1 || events[0].Kind != "legacy_import" || events[0].OccurredAtUTC != nil || events[0].CloudObservedAtUTC != nil || events[0].RecordedAtUTC.IsZero() {
			t.Fatalf("legacy evidence has fabricated time or missing import evidence: events=%#v err=%v", events, err)
		}
		var evidence struct {
			SourceFormat  int    `json:"source_format"`
			LegacyCreated string `json:"legacy_created"`
			LegacyUpdated string `json:"legacy_updated"`
		}
		if err = json.Unmarshal(events[0].Evidence, &evidence); err != nil || evidence.SourceFormat != 1 || evidence.LegacyCreated != wantTimes[id][0] || evidence.LegacyUpdated != wantTimes[id][1] {
			t.Fatalf("legacy import evidence lost original timestamps: evidence=%s err=%v", events[0].Evidence, err)
		}
	}
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got manifest
	if err = json.Unmarshal(manifestBody, &got); err != nil || got.Version != sqliteSchemaVersion {
		t.Fatalf("manifest not switched after migration: %#v err=%v", got, err)
	}
	if _, err = os.Stat(filepath.Join(dir, "jobs", "1.json")); err != nil {
		t.Fatalf("legacy source was removed before verified cleanup policy: %v", err)
	}
	credential, err := store.LoadCredentials()
	if err != nil || string(credential) != `{"version":1,"access_token":"sidecar-token"}` {
		t.Fatalf("credential sidecar changed during migration: %q err=%v", credential, err)
	}
	settings, err := store.LoadSettings()
	if err != nil || string(settings) != `{"download_mode":"delete"}` {
		t.Fatalf("settings sidecar changed during migration: %q err=%v", settings, err)
	}
	if err = store.BindAccount("123"); err != nil {
		t.Fatalf("account binding changed during migration: %v", err)
	}
	intakes, err := store.LoadIntakes()
	if err != nil || len(intakes) != 1 {
		t.Fatalf("intake journal changed during migration: count=%d err=%v", len(intakes), err)
	}
}

func TestFailedV1MigrationKeepsLegacySourceAuthoritative(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	writeLegacyStore(t, dir, map[string]string{"1.json": `{invalid`})
	if store, err := Open(dir, "/queue"); err == nil {
		store.Close()
		t.Fatal("opened corrupt v1 queue")
	}
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got manifest
	if err = json.Unmarshal(manifestBody, &got); err != nil || got.Version != 1 {
		t.Fatalf("failed migration changed authoritative version: %#v err=%v", got, err)
	}
	if _, err = os.Stat(filepath.Join(dir, "jobs", "1.json")); err != nil {
		t.Fatalf("failed migration modified source record: %v", err)
	}
}

func TestOpenRetriesInterruptedV1MigrationFromOriginalJobs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	record := `{"version":1,"file":{"fs_id":1,"path":"/queue/one.bin","server_filename":"one.bin","size":1},"mode":"keep","status":"pending"}`
	writeLegacyStore(t, dir, map[string]string{"1.json": record})
	for name, body := range map[string]string{"queue.sqlite.migrating": "partial temp", "queue.sqlite": "partial published file"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	jobs, err := store.Load()
	if err != nil || string(jobs[1]) != record {
		t.Fatalf("interrupted migration did not recover from v1: %q err=%v", jobs[1], err)
	}
	events, err := store.Events(1)
	if err != nil || len(events) != 1 || string(events[0].Evidence) != `{"source_format":1,"legacy_created":null,"legacy_updated":null}` || events[0].OccurredAtUTC != nil {
		t.Fatalf("missing legacy times were not kept explicitly unknown: events=%#v err=%v", events, err)
	}
}

func TestSQLiteV23AddsDuplicateHashCheckpointColumns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	entry := CloudInventoryEntry{FsID: 88, Path: "/keep.bin", Name: "keep.bin", Size: 7, ProviderMtime: 91}
	if err = store.UpsertCloudInventoryPage("acct", "inventory", "/", []CloudInventoryEntry{entry}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveVerifiedCloudContentHash("acct", entry, strings.Repeat("a", 64), now); err != nil {
		t.Fatal(err)
	}
	state := CloudDuplicateScanState{AccountFingerprint: "acct", RunID: "old-run", Status: "paused", Phase: "paused", NextFsID: 88, TotalFiles: 3, ProcessedFiles: 2, TotalBytes: 21, ProcessedBytes: 14, StartedAtUTC: now, UpdatedAtUTC: now, LastError: "old checkpoint"}
	if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`ALTER TABLE cloud_duplicate_scan_state DROP COLUMN current_sha256_state; ALTER TABLE cloud_duplicate_scan_state DROP COLUMN current_bytes; ALTER TABLE cloud_duplicate_scan_state DROP COLUMN current_content_md5; ALTER TABLE cloud_duplicate_scan_state DROP COLUMN current_provider_mtime; ALTER TABLE cloud_duplicate_scan_state DROP COLUMN current_size; ALTER TABLE cloud_duplicate_scan_state DROP COLUMN current_name; ALTER TABLE cloud_duplicate_scan_state DROP COLUMN current_path; ALTER TABLE cloud_duplicate_scan_state DROP COLUMN current_fs_id; UPDATE store_meta SET value='23' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 23, Scope: "/queue"})
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
	columns := map[string]bool{}
	rows, err := store.db.Query(`PRAGMA table_info(cloud_duplicate_scan_state)`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var def any
		if err = rows.Scan(&cid, &name, &kind, &notNull, &def, &pk); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"current_fs_id", "current_path", "current_name", "current_size", "current_provider_mtime", "current_content_md5", "current_bytes", "current_sha256_state"} {
		if !columns[name] {
			t.Errorf("checkpoint column %q missing", name)
		}
	}
	loaded, err := store.LoadCloudDuplicateScanState("acct")
	if err != nil || loaded.RunID != state.RunID || loaded.NextFsID != state.NextFsID || loaded.ProcessedFiles != state.ProcessedFiles || loaded.ProcessedBytes != state.ProcessedBytes || loaded.LastError != state.LastError {
		t.Fatalf("preexisting duplicate scan state after v23 migration=%+v err=%v", loaded, err)
	}
	var hashCount int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id=88`).Scan(&hashCount); err != nil || hashCount != 1 {
		t.Fatalf("preexisting verified hash rows=%d err=%v", hashCount, err)
	}
	var inventoryCount int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=88`).Scan(&inventoryCount); err != nil || inventoryCount != 1 {
		t.Fatalf("preexisting cloud inventory rows=%d err=%v", inventoryCount, err)
	}
	events, err := store.CloudDuplicateScanEvents("acct", 0, 10)
	if err != nil || len(events) != 1 || events[0].RunID != state.RunID {
		t.Fatalf("preexisting duplicate scan events after v23 migration=%+v err=%v", events, err)
	}
}

func TestMigrateV24KeepsUnknownRefreshPageTimeUnknown(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 31, 7, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "baseline", "/root", nil, base, nil); err != nil {
		t.Fatal(err)
	}
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "legacy-refresh", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: base, UpdatedAtUTC: base}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	next := 100
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 2, Path: "/root/new.bin", Name: "new.bin", Size: 2}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", []CloudInventoryEntry{{FsID: 1, Path: "/root/legacy.bin", Name: "legacy.bin", Size: 1}}, base.Add(time.Minute), &next, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`ALTER TABLE cloud_inventory_refresh_entries DROP COLUMN observed_at_utc; UPDATE store_meta SET value='24' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 24, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, err = store.LoadCloudInventoryRefreshState("acct")
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the pre-v27 staged page after the migration reset its offset to 0.
	// This keeps the test focused on v24's missing observation-time evidence.
	state.NextOffset = 100
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 2, Path: "/root/new.bin", Name: "new.bin", Size: 2}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_refresh_state SET next_offset=100,next_page_boundary_json=? WHERE account_fingerprint='acct'`, `{"fs_id":2,"path":"/root/new.bin","name":"new.bin","size":2}`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_refresh_entries SET observed_at_utc=0 WHERE account_fingerprint='acct' AND run_id='legacy-refresh' AND directory='/root' AND fs_id=1`); err != nil {
		t.Fatal(err)
	}
	pageAt := base.Add(2 * time.Minute)
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", []CloudInventoryEntry{{FsID: 2, Path: "/root/new.bin", Name: "new.bin", Size: 2}}, pageAt, nil, nil); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", state.RunID, "directory_published", 0, 10)
	if err != nil || len(events) != 1 || events[0].CloudObservedAtUTC == nil || !events[0].CloudObservedAtUTC.Equal(pageAt) {
		t.Fatalf("legacy publish event=%+v err=%v", events, err)
	}
	changes, err := store.ListCloudInventoryRefreshChanges("acct", state.RunID, events[0].Sequence, 0, 10)
	if err != nil || len(changes) != 2 {
		t.Fatalf("legacy changes=%+v err=%v", changes, err)
	}
	for _, change := range changes {
		if change.New != nil && change.New.Path == "/root/legacy.bin" && change.New.ObservedAtUTC != nil {
			body, _ := json.Marshal(change.New)
			t.Fatalf("legacy page timestamp was fabricated: %s", body)
		}
		if change.New != nil && change.New.Path == "/root/legacy.bin" {
			body, marshalErr := json.Marshal(change.New)
			if marshalErr != nil || !strings.Contains(string(body), `"observed_at_utc":null`) {
				t.Fatalf("legacy API evidence should expose an explicit null timestamp: %s err=%v", body, marshalErr)
			}
		}
		if change.New != nil && change.New.Path == "/root/new.bin" && (change.New.ObservedAtUTC == nil || !change.New.ObservedAtUTC.Equal(pageAt)) {
			t.Fatalf("new page timestamp=%+v", change.New)
		}
	}
	var legacyObserved int64
	if err = store.db.QueryRow(`SELECT observed_at_utc FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=1`).Scan(&legacyObserved); err != nil || legacyObserved != 0 {
		t.Fatalf("legacy cached observation=%d err=%v", legacyObserved, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the SQLite schema commit but before manifest replacement.
	body, _ = json.Marshal(manifest{Version: 24, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	var recovered manifest
	if err != nil || json.Unmarshal(manifestBody, &recovered) != nil || recovered.Version != sqliteSchemaVersion {
		t.Fatalf("manifest after v24 recovery=%+v err=%v", recovered, err)
	}
}

func TestMigrateV27PreservesScopedRefreshCheckpointAndAddsUnlimitedBudget(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	state := CloudInventoryRefreshState{AccountFingerprint: "acct-v27", RunID: "refresh-v27", RootPath: "/root", CurrentDirectory: "/root", Status: "paused", Phase: "paused", StartedAtUTC: started, UpdatedAtUTC: started}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	state.NextOffset = 100
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 22, Path: "/root/next.bin", Name: "next.bin", Size: 7}
	state.PagesProcessed = 3
	state.ItemsProcessed = 299
	state.UpdatedAtUTC = started.Add(time.Minute)
	if err = store.SaveCloudInventoryRefreshState(state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`ALTER TABLE cloud_inventory_refresh_state DROP COLUMN request_budget; ALTER TABLE cloud_inventory_refresh_state DROP COLUMN requests_used; ALTER TABLE cloud_inventory_refresh_events DROP COLUMN request_budget; ALTER TABLE cloud_inventory_refresh_events DROP COLUMN requests_used; UPDATE store_meta SET value='27' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	manifestBody, _ := json.Marshal(manifest{Version: 27, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), manifestBody); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.LoadCloudInventoryRefreshState("acct-v27")
	if err != nil {
		t.Fatal(err)
	}
	if got.RunID != state.RunID || got.Status != "paused" || got.NextOffset != state.NextOffset || got.PagesProcessed != state.PagesProcessed || got.ItemsProcessed != state.ItemsProcessed || got.NextPageBoundary == nil || got.NextPageBoundary.FsID != 22 || got.RequestBudget != 0 || got.RequestsUsed != 0 {
		t.Fatalf("v27 refresh checkpoint changed during migration: %+v", got)
	}
	var schemaVersion, integrity string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&schemaVersion); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" || schemaVersion != fmt.Sprint(sqliteSchemaVersion) {
		t.Fatalf("migrated schema=%q integrity=%q err=%v", schemaVersion, integrity, err)
	}
}

func TestOpenMigratesV33QueueMonitorScanHistory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TRIGGER queue_monitor_scan_events_no_update; DROP TRIGGER queue_monitor_scan_events_no_delete; DROP INDEX queue_monitor_scan_events_order; DROP TABLE queue_monitor_scan_events; DROP INDEX queue_monitor_scan_runs_order; DROP TABLE queue_monitor_scan_runs; UPDATE store_meta SET value='33' WHERE key='schema_version'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 33, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version, integrity string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" || version != fmt.Sprint(sqliteSchemaVersion) {
		t.Fatalf("version=%q integrity=%q err=%v", version, integrity, err)
	}
	var objects int
	if err = store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('queue_monitor_scan_runs','queue_monitor_scan_events','queue_monitor_scan_runs_order','queue_monitor_scan_events_order','queue_monitor_scan_events_no_update','queue_monitor_scan_events_no_delete')`).Scan(&objects); err != nil || objects != 6 {
		t.Fatalf("objects=%d err=%v", objects, err)
	}
}

func TestOpenMigratesV34RefreshRestartReasonEvidence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`ALTER TABLE cloud_inventory_refresh_events DROP COLUMN restart_reason; UPDATE store_meta SET value='34' WHERE key='schema_version'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 34, Scope: "/queue"})
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
	var columns int
	if err = store.db.QueryRow(`SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name='restart_reason'`).Scan(&columns); err != nil || columns != 1 {
		t.Fatalf("restart_reason columns=%d err=%v", columns, err)
	}
}

func TestOpenMigratesV35ScanRestartReasonEvidence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`ALTER TABLE cloud_inventory_scan_events DROP COLUMN restart_reason; UPDATE store_meta SET value='35' WHERE key='schema_version'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 35, Scope: "/queue"})
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
	var columns int
	if err = store.db.QueryRow(`SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_events') WHERE name='restart_reason'`).Scan(&columns); err != nil || columns != 1 {
		t.Fatalf("restart_reason columns=%d err=%v", columns, err)
	}
}

func TestOpenMigratesV36CoverageColumnsWithoutBackfillingOldRuns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_scan_runs(account_fingerprint,run_id,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error) VALUES('acct','old-run','complete','complete','/',1,1,0,1,0,100,200,'')`); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`ALTER TABLE cloud_scan_runs DROP COLUMN coverage_observed_at_utc; ALTER TABLE cloud_scan_runs DROP COLUMN coverage_generation; ALTER TABLE cloud_scan_runs DROP COLUMN coverage_incomplete_directories; ALTER TABLE cloud_scan_runs DROP COLUMN coverage_status; UPDATE store_meta SET value='36' WHERE key='schema_version'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 36, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var status sql.NullString
	var incomplete, generation, observed sql.NullInt64
	if err = store.db.QueryRow(`SELECT coverage_status,coverage_incomplete_directories,coverage_generation,coverage_observed_at_utc FROM cloud_scan_runs WHERE run_id='old-run'`).Scan(&status, &incomplete, &generation, &observed); err != nil {
		t.Fatal(err)
	}
	if status.Valid || incomplete.Valid || generation.Valid || observed.Valid {
		t.Fatalf("migration fabricated historical coverage: %v %v %v %v", status, incomplete, generation, observed)
	}
}

func TestOpenMigratesV37CoverageReasonsWithoutBackfillingOldRuns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_scan_runs(account_fingerprint,run_id,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error) VALUES('acct','old-run','complete','complete','/',1,1,0,1,0,100,200,'')`); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`ALTER TABLE cloud_scan_runs DROP COLUMN coverage_reasons_json; UPDATE store_meta SET value='37' WHERE key='schema_version'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 37, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version string
	var reasons sql.NullString
	if err = store.db.QueryRow(`SELECT (SELECT value FROM store_meta WHERE key='schema_version'),coverage_reasons_json FROM cloud_scan_runs WHERE run_id='old-run'`).Scan(&version, &reasons); err != nil {
		t.Fatal(err)
	}
	if version != fmt.Sprint(sqliteSchemaVersion) || reasons.Valid {
		t.Fatalf("migration fabricated old coverage reasons: version=%q reasons=%+v", version, reasons)
	}
}

func TestOpenMigratesV39RefreshRestartEvidenceAndRecoversManifest(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "normal migration"
		if committed {
			name = "recover manifest after SQLite commit"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "queue")
			store, err := Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			when := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
			tx, err := store.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			started := CloudInventoryRefreshEvent{AccountFingerprint: "acct", RunID: "restart-evidence", Kind: "started", Status: "running", CurrentDirectory: "/root", RecordedAtUTC: when}
			if err = appendCloudInventoryRefreshEvent(tx, started); err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
			restart := CloudInventoryRefreshEvent{AccountFingerprint: "acct", RunID: "restart-evidence", Kind: "boundary_restart", Status: "running", CurrentDirectory: "/root", RecordedAtUTC: when.Add(time.Second), RestartReason: "provider_duplicate_page_id"}
			if err = appendCloudInventoryRefreshEvent(tx, restart); err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var oldSequence int64
			if err = store.db.QueryRow(`SELECT sequence FROM cloud_inventory_refresh_events WHERE run_id='restart-evidence' AND event_kind='boundary_restart'`).Scan(&oldSequence); err != nil {
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			db, openErr := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
			if openErr != nil {
				t.Fatal(openErr)
			}
			if _, openErr = db.Exec(`ALTER TABLE cloud_inventory_refresh_events DROP COLUMN restart_fs_id`); openErr == nil {
				_, openErr = db.Exec(`ALTER TABLE cloud_inventory_refresh_events DROP COLUMN restart_offset`)
			}
			if openErr == nil {
				_, openErr = db.Exec(`UPDATE store_meta SET value='39' WHERE key='schema_version'`)
			}
			if closeErr := db.Close(); openErr != nil {
				t.Fatal(openErr)
			} else if closeErr != nil {
				t.Fatal(closeErr)
			}
			manifestBody, _ := json.Marshal(manifest{Version: 39, Scope: "/queue"})
			if err = atomicWrite(filepath.Join(dir, "manifest.json"), manifestBody); err != nil {
				t.Fatal(err)
			}
			if committed {
				store, err = Open(dir, "/queue")
				if err != nil {
					t.Fatal(err)
				}
				if err = store.Close(); err != nil {
					t.Fatal(err)
				}
				manifestBody, _ = json.Marshal(manifest{Version: 39, Scope: "/queue"})
				if err = atomicWrite(filepath.Join(dir, "manifest.json"), manifestBody); err != nil {
					t.Fatal(err)
				}
			}
			store, err = Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			var version string
			var sequence int64
			var reason string
			var offset, fsID sql.NullInt64
			if err = store.db.QueryRow(`SELECT (SELECT value FROM store_meta WHERE key='schema_version'),sequence,restart_reason,restart_offset,restart_fs_id FROM cloud_inventory_refresh_events WHERE run_id='restart-evidence' AND event_kind='boundary_restart'`).Scan(&version, &sequence, &reason, &offset, &fsID); err != nil {
				t.Fatal(err)
			}
			var integrity string
			if err = store.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
				t.Fatal(err)
			}
			if version != fmt.Sprint(sqliteSchemaVersion) || sequence != oldSequence || reason != "provider_duplicate_page_id" || offset.Valid || fsID.Valid || integrity != "ok" {
				t.Fatalf("version=%q sequence=%d/%d reason=%q offset=%+v fs_id=%+v integrity=%q", version, sequence, oldSequence, reason, offset, fsID, integrity)
			}
			var eventColumns int
			if err = store.db.QueryRow(`SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name IN ('restart_offset','restart_fs_id')`).Scan(&eventColumns); err != nil || eventColumns != 2 {
				t.Fatalf("new restart evidence columns=%d err=%v", eventColumns, err)
			}
			if _, err = store.db.Exec(`UPDATE cloud_inventory_refresh_events SET last_error='changed' WHERE sequence=?`, oldSequence); err == nil {
				t.Fatal("refresh restart event no longer append-only after schema migration")
			}
			if committed && store.migrationAction != "recovered_manifest" {
				t.Fatalf("migration action=%q, want recovered_manifest", store.migrationAction)
			}
		})
	}
}

func TestOpenMigratesV38GlobalTaskEventIndexesAndRecoversManifest(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "normal migration"
		if committed {
			name = "recover manifest after SQLite commit"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "queue")
			store, err := Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.db.Exec(`INSERT INTO jobs(id,payload_json,status,updated_at_utc) VALUES(42,?,?,?)`, []byte(`{"status":"pending"}`), "pending", time.Now().UTC().UnixMicro()); err != nil {
				store.Close()
				t.Fatal(err)
			}
			if _, err = store.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,evidence_type,evidence_json) VALUES(42,'task_retry_requested',1,'','test','{}')`); err != nil {
				store.Close()
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`DROP INDEX job_events_kind_sequence; DROP INDEX job_events_operation_sequence; DROP INDEX job_events_attempt_sequence; DROP INDEX job_events_recorded_sequence`); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if !committed {
				if _, err = db.Exec(`ALTER TABLE cloud_inventory_refresh_events DROP COLUMN restart_fs_id; ALTER TABLE cloud_inventory_refresh_events DROP COLUMN restart_offset`); err != nil {
					db.Close()
					t.Fatal(err)
				}
			}
			version := "38"
			if committed {
				if _, err = db.Exec(`CREATE INDEX job_events_kind_sequence ON job_events(event_type,sequence); CREATE INDEX job_events_operation_sequence ON job_events(operation_id,sequence); CREATE INDEX job_events_attempt_sequence ON job_events(attempt_id,sequence); CREATE INDEX job_events_recorded_sequence ON job_events(recorded_at_utc,sequence)`); err != nil {
					db.Close()
					t.Fatal(err)
				}
				version = "40"
			}
			if _, err = db.Exec(`UPDATE store_meta SET value=? WHERE key='schema_version'`, version); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(manifest{Version: 38, Scope: "/queue"})
			if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
				t.Fatal(err)
			}
			store, err = Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			var schemaVersion string
			var eventCount, indexCount int
			var integrity string
			if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&schemaVersion); err != nil {
				t.Fatal(err)
			}
			if err = store.db.QueryRow(`SELECT count(*) FROM job_events WHERE job_id=42 AND event_type='task_retry_requested'`).Scan(&eventCount); err != nil {
				t.Fatal(err)
			}
			if err = store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name IN ('job_events_kind_sequence','job_events_operation_sequence','job_events_attempt_sequence','job_events_recorded_sequence')`).Scan(&indexCount); err != nil {
				t.Fatal(err)
			}
			if err = store.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
				t.Fatal(err)
			}
			if schemaVersion != fmt.Sprint(sqliteSchemaVersion) || eventCount != 1 || indexCount != 4 || integrity != "ok" {
				t.Fatalf("version=%q events=%d indexes=%d integrity=%q", schemaVersion, eventCount, indexCount, integrity)
			}
		})
	}
}
