package taskstore

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

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
	if err = store.db.QueryRow(`SELECT group_concat(status, ',') FROM (SELECT status FROM remote_operation_events WHERE operation_id=? ORDER BY sequence)`, op.ID).Scan(&statuses); err != nil || statuses != "prepared,unknown,confirmed" {
		t.Fatalf("remote operation event ordering=%q err=%v", statuses, err)
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
