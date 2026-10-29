package taskstore

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
			if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&gotVersion); err != nil || gotVersion != "15" {
				t.Fatalf("schema version=%s err=%v", gotVersion, err)
			}
			var gotManifest manifest
			body, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil || json.Unmarshal(body, &gotManifest) != nil || gotManifest.Version != 15 {
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
	if err = json.Unmarshal(manifestBody, &got); err != nil || got.Version != 15 || store.migrationAction != "migrated_v8" {
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
	if err = json.Unmarshal(body, &got); err != nil || got.Version != 15 || store.migrationAction != "migrated_v9" {
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
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != "15" {
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
	if err = json.Unmarshal(body, &got); err != nil || got.Version != 15 || store.migrationAction != "migrated_v10" {
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
	if err = json.Unmarshal(body, &got); err != nil || got.Version != 15 || store.migrationAction != "migrated_v11" {
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
	if err = store.SaveCloudScanState("acct", "run", "paused", "paused", "/", 100, 4, 350, now.Add(-time.Minute), now, "temporary"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_scan_state SET pending_json=? WHERE account_fingerprint='acct'`, []byte(`["/","/a","/b"]`)); err != nil {
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
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != "15" {
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
	if err = store.SaveCloudScanState("acct", "run", "paused", "paused", "/", 100, 2, 150, now.Add(-time.Minute), now, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_scan_state SET pending_json='["/","/next"]'; INSERT INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES('acct','run',0,'/'),('acct','run',1,'/next'); UPDATE store_meta SET value='13' WHERE key='schema_version'`); err != nil {
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
	if err = json.Unmarshal(body, &got); err != nil || got.Version != 15 || store.migrationAction != "migrated_v12" {
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
		if err = json.Unmarshal(body, &got); err != nil || got.Version != 15 {
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
