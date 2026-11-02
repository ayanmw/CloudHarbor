package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteOperationPageFiltersAndOrdersBoundedRecords(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	makeOperation := func(id string, at time.Time, status string) {
		t.Helper()
		op := RemoteOperation{ID: strings.Repeat(id, 32), AccountFingerprint: "token-fingerprint", Kind: "move", ObjectID: int64(len(id)), SourcePath: "/from", DestinationPath: "/to", Status: "prepared", CreatedAtUTC: at, Evidence: json.RawMessage(`{"checked":true}`)}
		if err := store.CreateRemoteOperation(op); err != nil {
			t.Fatal(err)
		}
		if status != "prepared" {
			observed := at.Add(time.Second)
			if err := store.UpdateRemoteOperation(op.ID, status, json.RawMessage(`{"result":"recorded"}`), observed); err != nil {
				t.Fatal(err)
			}
		}
	}
	makeOperation("a", base, "confirmed")
	makeOperation("b", base.Add(time.Second), "unknown")
	makeOperation("c", base.Add(2*time.Second), "confirmed")

	items, total, err := store.RemoteOperationPage("", 0, 2)
	if err != nil || total != 3 || len(items) != 2 || items[0].ID != strings.Repeat("c", 32) || items[1].ID != strings.Repeat("b", 32) {
		t.Fatalf("first page=%+v total=%d err=%v", items, total, err)
	}
	if items[0].CreatedAtUTC.Location() != time.UTC || items[0].SourcePath != "/from" || items[0].DestinationPath != "/to" || items[0].ObjectID == 0 {
		t.Fatalf("summary missing local audit fields: %+v", items[0])
	}
	confirmed, confirmedTotal, err := store.RemoteOperationPage("confirmed", 0, 10)
	if err != nil || confirmedTotal != 2 || len(confirmed) != 2 || confirmed[0].Status != "confirmed" || confirmed[1].Status != "confirmed" {
		t.Fatalf("confirmed page=%+v total=%d err=%v", confirmed, confirmedTotal, err)
	}
	older, olderTotal, err := store.RemoteOperationPage("", 2, 2)
	if err != nil || olderTotal != 3 || len(older) != 1 || older[0].ID != strings.Repeat("a", 32) {
		t.Fatalf("older page=%+v total=%d err=%v", older, olderTotal, err)
	}
	if _, _, err = store.RemoteOperationPage("invalid", 0, 10); err == nil {
		t.Fatal("invalid status accepted")
	}
	if _, _, err = store.RemoteOperationPage("", -1, 10); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, _, err = store.RemoteOperationPage("", 0, 51); err == nil {
		t.Fatal("unbounded limit accepted")
	}
}

func TestConfirmedRemoteDeleteForSourceRequiresExactUnreconciledReceipt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	responseAt := created.Add(time.Minute)
	operationID := strings.Repeat("d", 32)
	evidence := json.RawMessage(`{"expected":{"fs_id":906,"path":"/queue/file.bin","server_filename":"file.bin","size":7,"mtime":123,"isdir":0}}`)
	op := RemoteOperation{ID: operationID, AccountFingerprint: "account-a", Kind: "delete", ObjectID: 906, SourcePath: "/queue/file.bin", Status: "prepared", CreatedAtUTC: created, Evidence: evidence}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateRemoteOperation(operationID, "confirmed", json.RawMessage(`{"deleted":true}`), responseAt); err != nil {
		t.Fatal(err)
	}
	lookup := func(account, sourcePath string, objectID int64, jobCreated time.Time) (*ConfirmedRemoteDeleteReceipt, error) {
		t.Helper()
		return store.ConfirmedRemoteDeleteForSource(account, objectID, sourcePath, jobCreated)
	}
	for _, test := range []struct {
		name      string
		account   string
		objectID  int64
		path      string
		created   time.Time
		wantMatch bool
	}{
		{name: "exact receipt", account: "account-a", objectID: 906, path: "/queue/file.bin", created: created, wantMatch: true},
		{name: "different account", account: "account-b", objectID: 906, path: "/queue/file.bin", created: created},
		{name: "different object", account: "account-a", objectID: 907, path: "/queue/file.bin", created: created},
		{name: "different path", account: "account-a", objectID: 906, path: "/queue/other.bin", created: created},
		{name: "job created after operation", account: "account-a", objectID: 906, path: "/queue/file.bin", created: created.Add(time.Second)},
	} {
		t.Run(test.name, func(t *testing.T) {
			receipt, err := lookup(test.account, test.path, test.objectID, test.created)
			if err != nil {
				t.Fatal(err)
			}
			if (receipt != nil) != test.wantMatch {
				t.Fatalf("receipt=%+v wantMatch=%v", receipt, test.wantMatch)
			}
			if receipt != nil && (receipt.OperationID != operationID || !receipt.ResponseObservedAtUTC.Equal(responseAt) || string(receipt.PreparedEvidence) != string(evidence)) {
				t.Fatalf("receipt lost source evidence: %+v", receipt)
			}
		})
	}
	body := json.RawMessage(`{"fs_id":906}`)
	if err = store.SaveWithEvent(906, body, Event{Kind: "task_source_deleted_remotely", OperationID: operationID}); err != nil {
		t.Fatal(err)
	}
	receipt, err := lookup("account-a", "/queue/file.bin", 906, created)
	if err != nil || receipt != nil {
		t.Fatalf("already reconciled receipt=%+v err=%v", receipt, err)
	}
}

func TestRemoteOperationRestartMarksAffectedInventoryStale(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for _, account := range []string{"account", "other-account"} {
		if err = store.UpsertCloudInventoryPage(account, "seed", "/", []CloudInventoryEntry{{FsID: 1, Path: "/source", Name: "source", Directory: true}, {FsID: 2, Path: "/archive", Name: "archive", Directory: true}}, now, nil); err != nil {
			t.Fatal(err)
		}
		if err = store.UpsertCloudInventoryPage(account, "seed", "/source", []CloudInventoryEntry{{FsID: 3, Path: "/source/folder", Name: "folder", Directory: true}, {FsID: 4, Path: "/source/file.bin", Name: "file.bin", Size: 17}}, now, nil); err != nil {
			t.Fatal(err)
		}
		if err = store.UpsertCloudInventoryPage(account, "seed", "/source/folder", []CloudInventoryEntry{{FsID: 5, Path: "/source/folder/child.bin", Name: "child.bin", Size: 29}}, now, nil); err != nil {
			t.Fatal(err)
		}
		if err = store.UpsertCloudInventoryPage(account, "seed", "/archive", nil, now, nil); err != nil {
			t.Fatal(err)
		}
		if _, err = store.db.Exec(`UPDATE cloud_inventory_directories SET status='complete',observed_at_utc=? WHERE account_fingerprint=?`, now.UnixMicro(), account); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_content_hashes(account_fingerprint,fs_id,path,size_bytes,provider_mtime,sha256,verified_at_utc) VALUES('account',4,'/source/file.bin',17,0,?,?)`, strings.Repeat("c", 64), now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	var generationBefore int64
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='account'`).Scan(&generationBefore); err != nil {
		t.Fatal(err)
	}
	op := RemoteOperation{ID: strings.Repeat("a", 32), AccountFingerprint: "account", Kind: "move", ObjectID: 3, SourcePath: "/source/folder", DestinationPath: "/archive/folder", Status: "prepared", CreatedAtUTC: now, Evidence: json.RawMessage(`{}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
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
	var status string
	if err = store.db.QueryRow(`SELECT status FROM remote_operations WHERE operation_id=?`, op.ID).Scan(&status); err != nil || status != "unknown" {
		t.Fatalf("operation status=%q err=%v", status, err)
	}
	for _, p := range []string{"/source", "/source/folder", "/archive"} {
		var got string
		if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='account' AND path=?`, p).Scan(&got); err != nil {
			t.Fatalf("read coverage %s: %v", p, err)
		}
		if got != "stale" {
			t.Errorf("affected coverage %s status=%q, want stale", p, got)
		}
	}
	var otherStatus string
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='other-account' AND path='/source/folder'`).Scan(&otherStatus); err != nil || otherStatus != "complete" {
		t.Errorf("other account coverage status=%q err=%v", otherStatus, err)
	}
	var generationAfter int64
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='account'`).Scan(&generationAfter); err != nil || generationAfter <= generationBefore {
		t.Errorf("generation before=%d after=%d err=%v", generationBefore, generationAfter, err)
	}
	var cachedSize int64
	if err = store.db.QueryRow(`SELECT size_bytes FROM cloud_inventory WHERE account_fingerprint='account' AND path='/source/file.bin'`).Scan(&cachedSize); err != nil || cachedSize != 17 {
		t.Errorf("cached inventory row changed: size=%d err=%v", cachedSize, err)
	}
	var cachedHash string
	if err = store.db.QueryRow(`SELECT sha256 FROM cloud_content_hashes WHERE account_fingerprint='account' AND fs_id=4`).Scan(&cachedHash); err != nil || cachedHash != strings.Repeat("c", 64) {
		t.Errorf("cached content hash changed: hash=%q err=%v", cachedHash, err)
	}
}

func TestRemoteOperationRestartRecoveryRollsBackAsOneTransaction(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage("account", "seed", "/", []CloudInventoryEntry{{FsID: 1, Path: "/source", Name: "source", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_directories SET status='complete',observed_at_utc=? WHERE account_fingerprint='account' AND path='/'`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	op := RemoteOperation{ID: strings.Repeat("b", 32), AccountFingerprint: "account", Kind: "delete", ObjectID: 1, SourcePath: "/source", Status: "prepared", CreatedAtUTC: now, Evidence: json.RawMessage(`{}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	var generationBefore int64
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='account'`).Scan(&generationBefore); err != nil {
		t.Fatal(err)
	}
	var eventCountBefore int
	if err = store.db.QueryRow(`SELECT count(*) FROM remote_operation_events WHERE operation_id=?`, op.ID).Scan(&eventCountBefore); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fail_restart_cache_invalidation BEFORE UPDATE ON cloud_inventory_directories BEGIN SELECT RAISE(FAIL,'injected cache invalidation failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = store.recoverRemoteOperations(); err == nil {
		t.Fatal("recovery succeeded despite injected cache invalidation failure")
	}
	var status string
	var eventCount int
	var coverage string
	var generationAfter int64
	if err = store.db.QueryRow(`SELECT status FROM remote_operations WHERE operation_id=?`, op.ID).Scan(&status); err != nil || status != "prepared" {
		t.Errorf("operation status after rollback=%q err=%v", status, err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM remote_operation_events WHERE operation_id=?`, op.ID).Scan(&eventCount); err != nil || eventCount != eventCountBefore {
		t.Errorf("recovery event count after rollback=%d before=%d err=%v", eventCount, eventCountBefore, err)
	}
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='account' AND path='/'`).Scan(&coverage); err != nil || coverage != "complete" {
		t.Errorf("coverage after rollback=%q err=%v", coverage, err)
	}
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='account'`).Scan(&generationAfter); err != nil || generationAfter != generationBefore {
		t.Errorf("generation after rollback=%d before=%d err=%v", generationAfter, generationBefore, err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER fail_restart_cache_invalidation`); err != nil {
		t.Fatal(err)
	}
	if err = store.recoverRemoteOperations(); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT status FROM remote_operations WHERE operation_id=?`, op.ID).Scan(&status); err != nil || status != "unknown" {
		t.Errorf("operation status after retry=%q err=%v", status, err)
	}
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='account' AND path='/'`).Scan(&coverage); err != nil || coverage != "stale" {
		t.Errorf("coverage after retry=%q err=%v", coverage, err)
	}
}

func TestConfirmedRemoteOperationRestartInvalidatesUnreconciledCacheOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for _, directory := range []string{"/", "/source", "/target"} {
		if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,last_run_id,observed_at_utc) VALUES('account',?,'complete','seed',?)`, directory, now.UnixMicro()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,last_run_id,observed_at_utc) VALUES('other-account','/source','complete','seed',?)`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_generations(account_fingerprint,generation,updated_at_utc) VALUES('account',4,?)`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	op := RemoteOperation{ID: strings.Repeat("d", 32), AccountFingerprint: "account", Kind: "move", ObjectID: 10, SourcePath: "/source/file.bin", DestinationPath: "/target/file.bin", Status: "prepared", CreatedAtUTC: now, Evidence: json.RawMessage(`{}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateRemoteOperation(op.ID, "confirmed", json.RawMessage(`{"provider_response":"synchronous_success"}`), now); err != nil {
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
	var sourceCoverage string
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='account' AND path='/source'`).Scan(&sourceCoverage); err != nil || sourceCoverage != "stale" {
		t.Fatalf("unreconciled confirmed operation left source coverage %q, err=%v", sourceCoverage, err)
	}
	var targetCoverage string
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='account' AND path='/target'`).Scan(&targetCoverage); err != nil || targetCoverage != "stale" {
		t.Fatalf("unreconciled confirmed operation left target coverage %q, err=%v", targetCoverage, err)
	}
	var generation int64
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='account'`).Scan(&generation); err != nil || generation != 5 {
		t.Fatalf("generation after restart=%d, want 5, err=%v", generation, err)
	}
	var otherCoverage string
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='other-account' AND path='/source'`).Scan(&otherCoverage); err != nil || otherCoverage != "complete" {
		t.Errorf("other account coverage after recovery=%q err=%v", otherCoverage, err)
	}
	events, _, _, err := store.RemoteOperationEventPage(op.ID, RemoteOperationEventFilter{}, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	foundRecoveryEvidence := false
	for _, event := range events {
		var evidence struct {
			Event  string   `json:"event"`
			Reason string   `json:"reason"`
			Paths  []string `json:"invalidated_cache_paths"`
		}
		if json.Unmarshal(event.Evidence, &evidence) == nil && evidence.Event == "confirmed_cache_recovery" && evidence.Reason == "process_restart_before_local_reconciliation" && len(evidence.Paths) == 4 && event.RecordedAtUTC.Location() == time.UTC {
			foundRecoveryEvidence = true
		}
	}
	if !foundRecoveryEvidence {
		t.Fatalf("UTC cache recovery event evidence missing: %+v", events)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var generationAfterSecondOpen int64
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='account'`).Scan(&generationAfterSecondOpen); err != nil || generationAfterSecondOpen != generation {
		t.Errorf("completed recovery ran again: generation=%d first=%d err=%v", generationAfterSecondOpen, generation, err)
	}
}

func TestConfirmedRemoteOperationRestartRecoveryRollsBackAndRetries(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,last_run_id,observed_at_utc) VALUES('account','/source','complete','seed',?)`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_generations(account_fingerprint,generation,updated_at_utc) VALUES('account',7,?)`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	op := RemoteOperation{ID: strings.Repeat("e", 32), AccountFingerprint: "account", Kind: "delete", ObjectID: 10, SourcePath: "/source/file.bin", Status: "prepared", CreatedAtUTC: now, Evidence: json.RawMessage(`{}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateRemoteOperation(op.ID, "confirmed", json.RawMessage(`{"ok":true}`), now); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fail_confirmed_recovery BEFORE UPDATE ON cloud_inventory_directories BEGIN SELECT RAISE(FAIL,'injected recovery failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = store.recoverRemoteOperations(); err == nil {
		t.Fatal("confirmed recovery succeeded despite injected cache failure")
	}
	var coverage string
	var generation int64
	var markerCount int
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='account' AND path='/source'`).Scan(&coverage); err != nil || coverage != "complete" {
		t.Errorf("coverage after failed recovery=%q err=%v", coverage, err)
	}
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='account'`).Scan(&generation); err != nil || generation != 7 {
		t.Errorf("generation after failed recovery=%d err=%v", generation, err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM remote_operation_events WHERE operation_id=? AND evidence_json IN (?,?)`, op.ID, remoteOperationLocalReconciliationCompleteEvidence, remoteOperationLocalCacheRecoveryCompleteEvidence).Scan(&markerCount); err != nil || markerCount != 0 {
		t.Errorf("marker after failed recovery=%d err=%v", markerCount, err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER fail_confirmed_recovery`); err != nil {
		t.Fatal(err)
	}
	if err = store.recoverRemoteOperations(); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='account' AND path='/source'`).Scan(&coverage); err != nil || coverage != "stale" {
		t.Errorf("coverage after recovery retry=%q err=%v", coverage, err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM remote_operation_events WHERE operation_id=? AND evidence_json=?`, op.ID, remoteOperationLocalCacheRecoveryCompleteEvidence).Scan(&markerCount); err != nil || markerCount != 1 {
		t.Errorf("marker after recovery retry=%d err=%v", markerCount, err)
	}
}

func TestUnknownRemoteOperationRestartRetriesMissingCacheInvalidation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for _, directory := range []string{"/source", "/target"} {
		if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,last_run_id,observed_at_utc) VALUES('account',?,'complete','seed',?)`, directory, now.UnixMicro()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_generations(account_fingerprint,generation,updated_at_utc) VALUES('account',12,?)`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	op := RemoteOperation{ID: strings.Repeat("f", 32), AccountFingerprint: "account", Kind: "move", ObjectID: 20, SourcePath: "/source/file.bin", DestinationPath: "/target/file.bin", Status: "prepared", CreatedAtUTC: now, Evidence: json.RawMessage(`{}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateRemoteOperation(op.ID, "unknown", json.RawMessage(`{"error":"provider timeout"}`), now); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fail_unknown_cache_recovery BEFORE UPDATE ON cloud_inventory_directories BEGIN SELECT RAISE(FAIL,'injected unknown cache recovery failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = store.recoverRemoteOperations(); err == nil {
		t.Fatal("unknown recovery succeeded despite injected cache failure")
	}
	var markerCount int
	if err = store.db.QueryRow(`SELECT count(*) FROM remote_operation_events WHERE operation_id=? AND evidence_json=?`, op.ID, remoteOperationLocalCacheRecoveryCompleteEvidence).Scan(&markerCount); err != nil || markerCount != 0 {
		t.Fatalf("unknown cache recovery marker survived rollback: count=%d err=%v", markerCount, err)
	}
	var statusAfterRollback string
	var generationAfterRollback int64
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='account' AND path='/source'`).Scan(&statusAfterRollback); err != nil || statusAfterRollback != "complete" {
		t.Fatalf("unknown recovery partially changed coverage: status=%q err=%v", statusAfterRollback, err)
	}
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='account'`).Scan(&generationAfterRollback); err != nil || generationAfterRollback != 12 {
		t.Fatalf("unknown recovery partially changed generation: generation=%d err=%v", generationAfterRollback, err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER fail_unknown_cache_recovery`); err != nil {
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
	for _, directory := range []string{"/source", "/target"} {
		var status string
		if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='account' AND path=?`, directory).Scan(&status); err != nil || status != "stale" {
			t.Errorf("unknown operation left %s coverage=%q err=%v", directory, status, err)
		}
	}
	var operationStatus string
	if err = store.db.QueryRow(`SELECT status FROM remote_operations WHERE operation_id=?`, op.ID).Scan(&operationStatus); err != nil || operationStatus != "unknown" {
		t.Errorf("unknown operation state changed to %q err=%v", operationStatus, err)
	}
	var generation int64
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='account'`).Scan(&generation); err != nil || generation != 13 {
		t.Errorf("generation after unknown recovery=%d want 13 err=%v", generation, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var afterSecondOpen int64
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='account'`).Scan(&afterSecondOpen); err != nil || afterSecondOpen != generation {
		t.Errorf("unknown recovery repeated: generation=%d first=%d err=%v", afterSecondOpen, generation, err)
	}
}

func TestRemoteOperationFilteredPageSearchesLiteralFieldsAndTimeBounds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	operations := []RemoteOperation{
		{ID: strings.Repeat("a", 32), AccountFingerprint: "account", Kind: "rename", ObjectID: 123456, SourcePath: "/queue/keep%_literal", DestinationPath: "/archive/keep\\literal", Status: "prepared", CreatedAtUTC: base, Evidence: json.RawMessage(`{}`)},
		{ID: strings.Repeat("b", 32), AccountFingerprint: "account", Kind: "move", ObjectID: 98765, SourcePath: "/queue/target-file", DestinationPath: "/archive/target-file", Status: "prepared", CreatedAtUTC: base.Add(time.Second), Evidence: json.RawMessage(`{}`)},
		{ID: strings.Repeat("c", 32), AccountFingerprint: "account", Kind: "delete", ObjectID: 11111, SourcePath: "/queue/target-folder", DestinationPath: "", Status: "prepared", CreatedAtUTC: base.Add(2 * time.Second), Evidence: json.RawMessage(`{}`)},
	}
	if err = store.CreateRemoteOperation(operations[0]); err != nil {
		t.Fatal(err)
	}
	for _, operation := range operations[1:] {
		if err = store.CreateRemoteOperation(operation); err != nil {
			t.Fatal(err)
		}
		if err = store.UpdateRemoteOperation(operation.ID, "confirmed", json.RawMessage(`{"ok":true}`), operation.CreatedAtUTC.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	literalWildcard, total, err := store.RemoteOperationPageFiltered(RemoteOperationFilter{Query: "%_"}, 0, 10)
	if err != nil || total != 1 || len(literalWildcard) != 1 || literalWildcard[0].ID != operations[0].ID {
		t.Fatalf("literal wildcard search=%+v total=%d err=%v", literalWildcard, total, err)
	}
	literalSlash, total, err := store.RemoteOperationPageFiltered(RemoteOperationFilter{Query: `keep\`}, 0, 10)
	if err != nil || total != 1 || len(literalSlash) != 1 || literalSlash[0].ID != operations[0].ID {
		t.Fatalf("literal escape search=%+v total=%d err=%v", literalSlash, total, err)
	}
	objectID, total, err := store.RemoteOperationPageFiltered(RemoteOperationFilter{Query: "987"}, 0, 10)
	if err != nil || total != 1 || len(objectID) != 1 || objectID[0].ID != operations[1].ID {
		t.Fatalf("object id search=%+v total=%d err=%v", objectID, total, err)
	}
	operationID, total, err := store.RemoteOperationPageFiltered(RemoteOperationFilter{Query: operations[2].ID[8:16]}, 0, 10)
	if err != nil || total != 1 || len(operationID) != 1 || operationID[0].ID != operations[2].ID {
		t.Fatalf("operation id search=%+v total=%d err=%v", operationID, total, err)
	}
	destinationPath, total, err := store.RemoteOperationPageFiltered(RemoteOperationFilter{Query: "archive/target-file"}, 0, 10)
	if err != nil || total != 1 || len(destinationPath) != 1 || destinationPath[0].ID != operations[1].ID {
		t.Fatalf("destination path search=%+v total=%d err=%v", destinationPath, total, err)
	}
	from := base.Add(time.Second).In(time.FixedZone("UTC+08", 8*60*60))
	to := base.Add(time.Second)
	filtered, total, err := store.RemoteOperationPageFiltered(RemoteOperationFilter{Status: "confirmed", Query: "target", FromUTC: &from, ToUTC: &to}, 0, 10)
	if err != nil || total != 1 || len(filtered) != 1 || filtered[0].ID != operations[1].ID {
		t.Fatalf("combined inclusive filters=%+v total=%d err=%v", filtered, total, err)
	}
	filtered, total, err = store.RemoteOperationPageFiltered(RemoteOperationFilter{Kind: "rename"}, 0, 10)
	if err != nil || total != 1 || len(filtered) != 1 || filtered[0].ID != operations[0].ID {
		t.Fatalf("operation kind filter=%+v total=%d err=%v", filtered, total, err)
	}
	page, total, err := store.RemoteOperationPageFiltered(RemoteOperationFilter{Query: "target"}, 1, 1)
	if err != nil || total != 2 || len(page) != 1 || page[0].ID != operations[1].ID {
		t.Fatalf("filtered page=%+v total=%d err=%v", page, total, err)
	}
	later := base.Add(2 * time.Second)
	if _, _, err = store.RemoteOperationPageFiltered(RemoteOperationFilter{FromUTC: &later, ToUTC: &from}, 0, 10); err == nil {
		t.Fatal("reversed time range accepted")
	}
	if _, _, err = store.RemoteOperationPageFiltered(RemoteOperationFilter{Query: strings.Repeat("界", 129)}, 0, 10); err == nil {
		t.Fatal("query longer than 128 Unicode characters accepted")
	}
	if _, _, err = store.RemoteOperationPageFiltered(RemoteOperationFilter{Kind: "unsupported"}, 0, 10); err == nil {
		t.Fatal("unsupported operation kind accepted")
	}
}

func TestRemoteOperationCursorPageKeepsBoundaryWhenNewOperationIsInserted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b", "c"} {
		op := RemoteOperation{ID: strings.Repeat(id, 32), AccountFingerprint: "account", Kind: "move", ObjectID: 1, SourcePath: "/source", DestinationPath: "/queue", Status: "prepared", CreatedAtUTC: base, Evidence: json.RawMessage(`{}`)}
		if err := store.CreateRemoteOperation(op); err != nil {
			t.Fatal(err)
		}
	}
	first, total, next, err := store.RemoteOperationCursorPageFiltered(RemoteOperationFilter{}, nil, 2)
	if err != nil || total != 3 || len(first) != 2 || first[0].ID != strings.Repeat("c", 32) || first[1].ID != strings.Repeat("b", 32) || next == nil || next.ID != first[1].ID || !next.CreatedAtUTC.Equal(base) {
		t.Fatalf("first cursor page=%+v total=%d next=%+v err=%v", first, total, next, err)
	}
	newest := RemoteOperation{ID: strings.Repeat("d", 32), AccountFingerprint: "account", Kind: "move", ObjectID: 1, SourcePath: "/new", DestinationPath: "/queue/new", Status: "prepared", CreatedAtUTC: base.Add(time.Second), Evidence: json.RawMessage(`{}`)}
	if err := store.CreateRemoteOperation(newest); err != nil {
		t.Fatal(err)
	}
	older, total, next, err := store.RemoteOperationCursorPageFiltered(RemoteOperationFilter{}, next, 2)
	if err != nil || total != 4 || len(older) != 1 || older[0].ID != strings.Repeat("a", 32) || next != nil {
		t.Fatalf("older cursor page=%+v total=%d next=%+v err=%v", older, total, next, err)
	}
	confirmed, total, _, err := store.RemoteOperationCursorPageFiltered(RemoteOperationFilter{Status: "confirmed"}, nil, 2)
	if err != nil || total != 0 || len(confirmed) != 0 {
		t.Fatalf("filtered empty cursor page=%+v total=%d err=%v", confirmed, total, err)
	}
	invalid := &RemoteOperationCursor{CreatedAtUTC: base, ID: "not-an-operation-id"}
	if _, _, _, err = store.RemoteOperationCursorPageFiltered(RemoteOperationFilter{}, invalid, 2); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}

func TestRemoteOperationExportUsesFilteredStableSummary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	operations := []RemoteOperation{
		{ID: strings.Repeat("a", 32), AccountFingerprint: "private-account-fingerprint", Kind: "move", ObjectID: 55, SourcePath: "/source/target", DestinationPath: "/queue/target", Status: "prepared", CreatedAtUTC: base, Evidence: json.RawMessage(`{"secret":"not-exported"}`)},
		{ID: strings.Repeat("b", 32), AccountFingerprint: "private-account-fingerprint", Kind: "rename", ObjectID: 66, SourcePath: "/source/target-two", DestinationPath: "/queue/target-two", Status: "prepared", CreatedAtUTC: base.Add(time.Second), Evidence: json.RawMessage(`{"secret":"not-exported"}`)},
		{ID: strings.Repeat("c", 32), AccountFingerprint: "private-account-fingerprint", Kind: "delete", ObjectID: 77, SourcePath: "/other", Status: "prepared", CreatedAtUTC: base.Add(2 * time.Second), Evidence: json.RawMessage(`{}`)},
	}
	for _, operation := range operations {
		if err = store.CreateRemoteOperation(operation); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.UpdateRemoteOperation(operations[0].ID, "confirmed", json.RawMessage(`{"ok":true}`), base.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	from, to := base, base.Add(time.Second)
	exported, err := store.RemoteOperationExport(context.Background(), RemoteOperationFilter{Status: "confirmed", Kind: "move", Query: "target", FromUTC: &from, ToUTC: &to})
	if err != nil || len(exported) != 1 || exported[0].ID != operations[0].ID || exported[0].ObjectID != 55 || exported[0].SourcePath != "/source/target" {
		t.Fatalf("export=%+v err=%v", exported, err)
	}
	body, err := json.Marshal(exported)
	if err != nil || strings.Contains(string(body), "private-account-fingerprint") || strings.Contains(string(body), "not-exported") || strings.Contains(string(body), "account_fingerprint") || strings.Contains(string(body), "evidence") {
		t.Fatalf("export leaked account/evidence data: body=%s err=%v", body, err)
	}
}

func TestRemoteOperationExportRejectsOversizeAndUnavailableStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	statement, err := tx.Prepare(`INSERT INTO remote_operations(operation_id,account_fingerprint,operation_kind,object_id,source_path,destination_path,status,created_at_utc,evidence_json) VALUES(?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5001; i++ {
		if _, err = statement.Exec(fmt.Sprintf("%032x", i), "account", "move", i, "/source", "/destination", "confirmed", int64(i), `{}`); err != nil {
			t.Fatal(err)
		}
	}
	if err = statement.Close(); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	exported, err := store.RemoteOperationExport(context.Background(), RemoteOperationFilter{})
	if !errors.Is(err, ErrRemoteOperationExportTooLarge) || exported != nil {
		t.Fatalf("oversize export returned partial data: count=%d err=%v", len(exported), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = store.RemoteOperationExport(ctx, RemoteOperationFilter{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled export error=%v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RemoteOperationExport(context.Background(), RemoteOperationFilter{}); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed store error=%v", err)
	}
}

func TestRemoteOperationEventPageIsBoundedAndAccountStoreLocal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)
	op := RemoteOperation{ID: strings.Repeat("d", 32), AccountFingerprint: "token-fingerprint", Kind: "rename", ObjectID: 9, SourcePath: "/old", DestinationPath: "/new", Status: "prepared", CreatedAtUTC: created, Evidence: json.RawMessage(`{"expected":{"path":"/old"}}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	confirmedAt := created.Add(time.Second)
	if err = store.UpdateRemoteOperation(op.ID, "confirmed", json.RawMessage(`{"provider_response":"synchronous_success"}`), confirmedAt); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordRemoteCacheRefreshFailure(op.ID, "HTTP=503 code=77"); err != nil {
		t.Fatal(err)
	}
	first, before, more, err := store.RemoteOperationEventPage(op.ID, RemoteOperationEventFilter{}, 0, 2)
	if err != nil || len(first) != 2 || !more || before != first[1].Sequence || first[0].Status != "confirmed" || first[1].Status != "confirmed" {
		t.Fatalf("first event page=%+v before=%d more=%v err=%v", first, before, more, err)
	}
	if first[0].RecordedAtUTC.IsZero() || first[0].Evidence == nil || first[0].ResponseObservedAtUTC != nil {
		t.Fatalf("local cache event evidence/times missing: %+v", first[0])
	}
	older, _, more, err := store.RemoteOperationEventPage(op.ID, RemoteOperationEventFilter{}, before, 2)
	if err != nil || len(older) != 1 || more || older[0].Status != "prepared" || older[0].Evidence == nil {
		t.Fatalf("older event page=%+v more=%v err=%v", older, more, err)
	}
	if _, _, _, err = store.RemoteOperationEventPage(strings.Repeat("e", 32), RemoteOperationEventFilter{}, 0, 10); !errors.Is(err, ErrRemoteOperationNotFound) {
		t.Fatalf("unknown operation error=%v", err)
	}
	if _, _, _, err = store.RemoteOperationEventPage("invalid", RemoteOperationEventFilter{}, 0, 10); !errors.Is(err, ErrRemoteOperationNotFound) {
		t.Fatalf("malformed operation id error=%v", err)
	}
	if _, _, _, err = store.RemoteOperationEventPage(op.ID, RemoteOperationEventFilter{}, -1, 10); err == nil {
		t.Fatal("negative event cursor accepted")
	}
	if _, _, _, err = store.RemoteOperationEventPage(op.ID, RemoteOperationEventFilter{}, 0, 51); err == nil {
		t.Fatal("unbounded event limit accepted")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = store.RemoteOperationEventPage(op.ID, RemoteOperationEventFilter{}, 0, 10); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed store error=%v", err)
	}
}

func TestRemoteOperationEventPageFiltersStatusWithinOperationAndCursor(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := strings.Repeat("a", 32)
	op := RemoteOperation{ID: id, AccountFingerprint: "acct", Kind: "move", ObjectID: 1, SourcePath: "/a", DestinationPath: "/b", Status: "prepared", CreatedAtUTC: time.Now().UTC(), Evidence: json.RawMessage(`{}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateRemoteOperation(id, "confirmed", json.RawMessage(`{}`), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordRemoteCacheRefreshFailure(id, "HTTP=503"); err != nil {
		t.Fatal(err)
	}
	filtered, before, more, err := store.RemoteOperationEventPage(id, RemoteOperationEventFilter{Status: "confirmed"}, 0, 1)
	if err != nil || len(filtered) != 1 || filtered[0].Status != "confirmed" || !more || before != filtered[0].Sequence {
		t.Fatalf("filtered=%+v before=%d more=%v err=%v", filtered, before, more, err)
	}
	older, _, more, err := store.RemoteOperationEventPage(id, RemoteOperationEventFilter{Status: "confirmed"}, before, 1)
	if err != nil || len(older) != 1 || older[0].Status != "confirmed" || more {
		t.Fatalf("filtered older=%+v more=%v err=%v", older, more, err)
	}
	empty, _, _, err := store.RemoteOperationEventPage(id, RemoteOperationEventFilter{Status: "failed"}, 0, 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty filtered page=%+v err=%v", empty, err)
	}
	if _, _, _, err = store.RemoteOperationEventPage(id, RemoteOperationEventFilter{Status: "bogus"}, 0, 10); err == nil {
		t.Fatal("invalid status accepted")
	}
	if _, _, _, err = store.RemoteOperationEventPage(strings.Repeat("b", 32), RemoteOperationEventFilter{Status: "confirmed"}, 0, 10); !errors.Is(err, ErrRemoteOperationNotFound) {
		t.Fatalf("other operation error=%v", err)
	}
}

func TestRemoteOperationEventDateRangeAndFilteredEvidenceExport(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := strings.Repeat("c", 32)
	op := RemoteOperation{ID: id, AccountFingerprint: "private", Kind: "move", ObjectID: 7, SourcePath: "/source", DestinationPath: "/target", Status: "prepared", CreatedAtUTC: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), Evidence: json.RawMessage(`{}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	for _, e := range []struct {
		status string
		at     time.Time
	}{
		{"unknown", time.Date(2026, 10, 29, 23, 59, 59, 999999999, time.UTC)},
		{"unknown", time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)},
		{"unknown", time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)},
		{"failed", time.Date(2026, 10, 30, 23, 59, 59, 999999999, time.UTC)},
		{"unknown", time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)},
	} {
		if _, err = store.db.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json) VALUES(?,?,?,?)`, id, e.status, e.at.UnixNano(), `{"note":"ok"}`); err != nil {
			t.Fatal(err)
		}
	}
	from := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	before := from.AddDate(0, 0, 1)
	filter := RemoteOperationEventFilter{Status: "unknown", RecordedFromUTC: &from, RecordedBeforeUTC: &before}
	events, cursor, more, err := store.RemoteOperationEventPage(id, filter, 0, 1)
	if err != nil || len(events) != 1 || events[0].Status != "unknown" || events[0].RecordedAtUTC.Hour() != 12 || !more {
		t.Fatalf("events=%+v more=%v err=%v", events, more, err)
	}
	older, _, more, err := store.RemoteOperationEventPage(id, filter, cursor, 1)
	if err != nil || len(older) != 1 || older[0].Status != "unknown" || !older[0].RecordedAtUTC.Equal(from) || more {
		t.Fatalf("older=%+v more=%v err=%v", older, more, err)
	}
	bundle, err := store.RemoteOperationEvidenceExportFiltered(context.Background(), id, filter)
	if err != nil || len(bundle.Events) != 2 || bundle.Operation.ID != id {
		t.Fatalf("bundle=%+v err=%v", bundle, err)
	}
	if _, _, _, err = store.RemoteOperationEventPage(id, RemoteOperationEventFilter{RecordedFromUTC: &before, RecordedBeforeUTC: &from}, 0, 10); err == nil {
		t.Fatal("inverted date range accepted")
	}
	if _, err = store.RemoteOperationEvidenceExportFiltered(context.Background(), strings.Repeat("d", 32), filter); !errors.Is(err, ErrRemoteOperationNotFound) {
		t.Fatalf("unknown export error=%v", err)
	}
}

func TestRemoteOperationFilteredEvidenceExportRejectsMoreThanLimit(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := strings.Repeat("e", 32)
	op := RemoteOperation{ID: id, AccountFingerprint: "acct", Kind: "delete", ObjectID: 3, SourcePath: "/a", DestinationPath: "", Status: "prepared", CreatedAtUTC: time.Now().UTC(), Evidence: json.RawMessage(`{}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= RemoteOperationEvidenceExportLimit; i++ {
		if _, err = store.db.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json) VALUES(?,'unknown',?,?)`, id, time.Now().UTC().Add(time.Duration(i)*time.Second).UnixNano(), `{}`); err != nil {
			t.Fatal(err)
		}
	}
	matching, err := store.RemoteOperationEvidenceExportFiltered(context.Background(), id, RemoteOperationEventFilter{Status: "prepared"})
	if err != nil || len(matching.Events) != 1 {
		t.Fatalf("filtered under-limit export events=%d err=%v", len(matching.Events), err)
	}
	if _, err = store.RemoteOperationEvidenceExportFiltered(context.Background(), id, RemoteOperationEventFilter{Status: "unknown"}); !errors.Is(err, ErrRemoteOperationEvidenceExportTooLarge) {
		t.Fatalf("filtered export error=%v", err)
	}
}

func TestRemoteOperationEvidenceExportReturnsOneCompleteOrderedSnapshot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created := time.Date(2026, 10, 30, 3, 0, 0, 0, time.UTC)
	op := RemoteOperation{ID: strings.Repeat("f", 32), AccountFingerprint: "private-account-fingerprint", Kind: "move", ObjectID: 42, SourcePath: "/source", DestinationPath: "/destination", Status: "prepared", CreatedAtUTC: created, Evidence: json.RawMessage(`{"stage":"prepared"}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateRemoteOperation(op.ID, "unknown", json.RawMessage(`{"stage":"unknown","access_token":"raw-secret"}`), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateRemoteOperation(op.ID, "confirmed", json.RawMessage(`{"stage":"confirmed"}`), created.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	exported, err := store.RemoteOperationEvidenceExport(context.Background(), op.ID)
	if err != nil || exported.Operation.ID != op.ID || exported.Operation.ObjectID != 42 || len(exported.Events) != 3 {
		t.Fatalf("evidence export=%+v err=%v", exported, err)
	}
	for i, want := range []string{"prepared", "unknown", "confirmed"} {
		if exported.Events[i].Status != want || exported.Events[i].Sequence == 0 || exported.Events[i].RecordedAtUTC.IsZero() {
			t.Fatalf("event %d=%+v, want status %q with sequence/time", i, exported.Events[i], want)
		}
	}
	if exported.Events[2].ResponseObservedAtUTC == nil || !exported.Events[2].ResponseObservedAtUTC.Equal(created.Add(2*time.Second)) {
		t.Fatalf("confirmed event observation time missing: %+v", exported.Events[2])
	}
	body, err := json.Marshal(exported)
	if err != nil || strings.Contains(string(body), "private-account-fingerprint") || strings.Contains(string(body), "account_fingerprint") {
		t.Fatalf("export leaked account fingerprint: %s err=%v", body, err)
	}
}

func TestRemoteOperationEvidenceExportRejectsMissingOversizeAndCancelled(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created := time.Now().UTC()
	op := RemoteOperation{ID: strings.Repeat("e", 32), AccountFingerprint: "account", Kind: "rename", ObjectID: 9, SourcePath: "/a", DestinationPath: "/b", Status: "prepared", CreatedAtUTC: created, Evidence: json.RawMessage(`{}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < RemoteOperationEvidenceExportLimit-1; i++ {
		if _, err = store.db.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json) VALUES(?,'unknown',?,?)`, op.ID, created.Add(time.Duration(i+1)*time.Second).UnixNano(), `{}`); err != nil {
			t.Fatal(err)
		}
	}
	complete, err := store.RemoteOperationEvidenceExport(context.Background(), op.ID)
	if err != nil || len(complete.Events) != RemoteOperationEvidenceExportLimit {
		t.Fatalf("maximum-size export count=%d err=%v", len(complete.Events), err)
	}
	if _, err = store.db.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json) VALUES(?,'unknown',?,?)`, op.ID, created.Add(time.Duration(RemoteOperationEvidenceExportLimit)*time.Second).UnixNano(), `{}`); err != nil {
		t.Fatal(err)
	}
	partial, err := store.RemoteOperationEvidenceExport(context.Background(), op.ID)
	if !errors.Is(err, ErrRemoteOperationEvidenceExportTooLarge) || partial.Operation.ID != "" || partial.Events != nil {
		t.Fatalf("oversized export returned partial evidence: %+v err=%v", partial, err)
	}
	if _, err = store.RemoteOperationEvidenceExport(context.Background(), strings.Repeat("a", 32)); !errors.Is(err, ErrRemoteOperationNotFound) {
		t.Fatalf("missing operation error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = store.RemoteOperationEvidenceExport(ctx, op.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled export error=%v", err)
	}
}

func TestRemoteOperationTransitionsPersistAndPreparedBecomesUnknownOnRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 10, 29, 1, 2, 3, 0, time.UTC)
	op := RemoteOperation{ID: "0123456789abcdef0123456789abcdef", AccountFingerprint: "account-a", Kind: "move", ObjectID: 42, SourcePath: "/a", DestinationPath: "/queue", Status: "prepared", CreatedAtUTC: created, Evidence: json.RawMessage(`{"checked":true}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
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
	items, err := store.LoadRemoteOperations()
	if err != nil || len(items) != 1 || items[0].Status != "unknown" || items[0].ObjectID != 42 || !items[0].CreatedAtUTC.Equal(created) {
		t.Fatalf("restart recovery = %#v, err=%v", items, err)
	}
	if err = store.UpdateRemoteOperation(op.ID, "confirmed", json.RawMessage(`{"response":"success"}`), created.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	items, err = store.LoadRemoteOperations()
	if err != nil || len(items) != 1 || items[0].Status != "confirmed" || items[0].ResponseObservedAtUTC == nil || !items[0].ResponseObservedAtUTC.Equal(created.Add(time.Minute)) {
		t.Fatalf("confirmed operation = %#v, err=%v", items, err)
	}
	var statuses string
	if err = store.db.QueryRow(`SELECT group_concat(status, ',') FROM (SELECT status FROM remote_operation_events WHERE operation_id=? ORDER BY sequence)`, op.ID).Scan(&statuses); err != nil || statuses != "prepared,unknown,unknown,confirmed" {
		t.Fatalf("remote operation event ordering=%q err=%v", statuses, err)
	}
}

func TestRemoteCacheRefreshFailureIsAppendedWithoutChangingConfirmedOutcome(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	created := time.Now().UTC().Add(-time.Minute)
	confirmedAt := created.Add(15 * time.Second)
	op := RemoteOperation{ID: "1123456789abcdef0123456789abcdef", AccountFingerprint: "account-a", Kind: "rename", ObjectID: 42, SourcePath: "/a", DestinationPath: "/b", Status: "prepared", CreatedAtUTC: created, Evidence: json.RawMessage(`{"checked":true}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateRemoteOperation(op.ID, "confirmed", json.RawMessage(`{"provider_response":"synchronous_success"}`), confirmedAt); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordRemoteCacheRefreshFailure(op.ID, "provider identity verification failed"); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.LoadRemoteOperations()
	if err != nil || len(items) != 1 || items[0].Status != "confirmed" || items[0].ResponseObservedAtUTC == nil || !items[0].ResponseObservedAtUTC.Equal(confirmedAt) {
		t.Fatalf("local cache diagnostic changed confirmed remote outcome: operations=%+v err=%v", items, err)
	}
	var status string
	var recordedAt int64
	var evidence []byte
	if err = store.db.QueryRow(`SELECT status,recorded_at_utc,evidence_json FROM remote_operation_events WHERE operation_id=? AND CAST(evidence_json AS TEXT) LIKE '%cache_refresh_failed%' ORDER BY sequence DESC LIMIT 1`, op.ID).Scan(&status, &recordedAt, &evidence); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err = json.Unmarshal(evidence, &decoded); err != nil || status != "confirmed" || decoded["event"] != "cache_refresh_failed" || decoded["reason"] != "provider identity verification failed" || time.Unix(0, recordedAt).Before(confirmedAt) {
		t.Fatalf("cache failure event=%s %s %d err=%v", status, evidence, recordedAt, err)
	}
}

func TestOpenMigratesV3StoreToRemoteOperationSchema(t *testing.T) {
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
	if _, err = db.Exec(`DROP TRIGGER remote_operation_events_no_update; DROP TRIGGER remote_operation_events_no_delete; DROP INDEX remote_operation_events_order; DROP TABLE remote_operation_events; DROP TABLE remote_operations; UPDATE store_meta SET value='3' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 3, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.migrationAction != "migrated_v3" {
		t.Fatalf("migration action = %q", store.migrationAction)
	}
}
