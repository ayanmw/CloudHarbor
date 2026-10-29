package taskstore

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func BenchmarkListVerifiedCloudDuplicateGroups(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "queue"), "/queue")
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	const groupCount = 3000
	const copiesPerGroup = 3
	entries := make([]CloudInventoryEntry, 0, groupCount*copiesPerGroup)
	for group := 0; group < groupCount; group++ {
		for _, copyIndex := range []int{2, 0, 1} {
			name := fmt.Sprintf("group-%05d-%c.bin", group, 'a'+copyIndex)
			fsID := int64(group*copiesPerGroup + copyIndex + 1)
			entries = append(entries, CloudInventoryEntry{FsID: fsID, Path: "/" + name, Name: name, Size: 5, ProviderMtime: 1})
		}
	}
	if err = store.UpsertCloudInventoryPage("bench-account", "bench-run", "/", entries, time.Now().UTC(), nil); err != nil {
		b.Fatal(err)
	}
	tx, err := store.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO cloud_content_hashes(account_fingerprint,fs_id,path,size_bytes,provider_mtime,sha256,verified_at_utc) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	verifiedAt := time.Now().UTC().UnixMicro()
	for group := 0; group < groupCount; group++ {
		digest := fmt.Sprintf("%064x", group+1)
		for _, copyIndex := range []int{2, 0, 1} {
			name := fmt.Sprintf("group-%05d-%c.bin", group, 'a'+copyIndex)
			fsID := int64(group*copiesPerGroup + copyIndex + 1)
			if _, err = stmt.Exec("bench-account", fsID, "/"+name, 5, 1, digest, verifiedAt); err != nil {
				stmt.Close()
				tx.Rollback()
				b.Fatal(err)
			}
		}
	}
	if err = stmt.Close(); err != nil {
		tx.Rollback()
		b.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err = store.ListVerifiedCloudDuplicateGroups("bench-account"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestDuplicateSizeGroupingUsesDedicatedIndex(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.db.Query(`EXPLAIN QUERY PLAN SELECT size_bytes FROM cloud_inventory WHERE account_fingerprint=? AND is_dir=0 GROUP BY size_bytes HAVING count(*)>1`, "acct")
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
	if !strings.Contains(queryPlan, "cloud_inventory_duplicate_size") || strings.Contains(queryPlan, "TEMP B-TREE") {
		t.Fatalf("duplicate size grouping did not use its ordered index: %s", queryPlan)
	}
}

func TestDuplicateCandidatePageUsesFileSizeSummaryAndStablePrimaryKeyOrder(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.db.Query(`EXPLAIN QUERY PLAN `+listCloudDuplicateHashCandidatePageSQL, "acct", int64(0), 100)
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
	if !strings.Contains(queryPlan, "cloud_inventory_file_sizes") || !strings.Contains(queryPlan, "sqlite_autoindex_cloud_inventory_1") {
		t.Fatalf("candidate page did not use the derived size counts and filesystem ID index: %s", queryPlan)
	}
	if strings.Contains(queryPlan, "TEMP B-TREE") {
		t.Fatalf("candidate page used a temporary sort: %s", queryPlan)
	}
}

func TestVerifiedDuplicateGroupsAvoidTemporaryGroupingSort(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.db.Query(`EXPLAIN QUERY PLAN `+listVerifiedCloudDuplicateGroupsSQL, "acct")
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
	if !strings.Contains(queryPlan, "cloud_content_hashes_duplicate_group") || strings.Contains(queryPlan, "TEMP B-TREE FOR GROUP BY") {
		t.Fatalf("verified duplicate grouping did not follow the indexed order: %s", queryPlan)
	}
}

func TestDuplicateHashCandidatesUseCompleteInventoryAndRepeatedSizeOnly(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	files := []CloudInventoryEntry{
		{FsID: 1, Path: "/a.bin", Name: "a.bin", Size: 42, ContentMD5: "untrusted-one"},
		{FsID: 2, Path: "/b.bin", Name: "b.bin", Size: 42, ContentMD5: "untrusted-two"},
		{FsID: 3, Path: "/unique.bin", Name: "unique.bin", Size: 99},
		{FsID: 4, Path: "/stale/one.bin", Name: "one.bin", Size: 42},
		{FsID: 5, Path: "/stale/two.bin", Name: "two.bin", Size: 42},
	}
	if err = store.UpsertCloudInventoryPage("acct", "inventory-run", "/", files[:3], now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "inventory-run", "/stale", files[3:], now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.InvalidateCloudInventory("acct", []string{"/stale"}); err != nil {
		t.Fatal(err)
	}
	candidates, total, err := store.ListCloudDuplicateHashCandidates("acct", 0, 10)
	if err != nil || total != 2 || len(candidates) != 2 || candidates[0].FsID != 1 || candidates[1].FsID != 2 {
		t.Fatalf("duplicate size candidates=%+v total=%d err=%v", candidates, total, err)
	}
	page, pageTotal, err := store.ListCloudDuplicateHashCandidates("acct", 1, 10)
	if err != nil || pageTotal != 2 || len(page) != 1 || page[0].FsID != 2 {
		t.Fatalf("duplicate hash cursor page=%+v total=%d err=%v", page, pageTotal, err)
	}
	empty, emptyTotal, err := store.ListCloudDuplicateHashCandidates("acct", 99, 10)
	if err != nil || emptyTotal != 2 || len(empty) != 0 {
		t.Fatalf("empty duplicate hash cursor page=%+v total=%d err=%v", empty, emptyTotal, err)
	}
}

func TestDuplicateHashCandidatesPageByFilesystemID(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	files := make([]CloudInventoryEntry, 125)
	for i := range files {
		name := fmt.Sprintf("duplicate-%03d.bin", i)
		files[i] = CloudInventoryEntry{FsID: int64(i + 1), Path: "/" + name, Name: name, Size: 7}
	}
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", files, now, nil); err != nil {
		t.Fatal(err)
	}
	first, total, err := store.ListCloudDuplicateHashCandidates("acct", 0, 100)
	if err != nil || total != 125 || len(first) != 100 || first[0].FsID != 1 || first[99].FsID != 100 {
		t.Fatalf("first candidate page len=%d total=%d first=%+v last=%+v err=%v", len(first), total, first[0], first[len(first)-1], err)
	}
	second, total, err := store.ListCloudDuplicateHashCandidates("acct", first[len(first)-1].FsID, 100)
	if err != nil || total != 125 || len(second) != 25 || second[0].FsID != 101 || second[24].FsID != 125 {
		t.Fatalf("second candidate page len=%d total=%d first=%+v last=%+v err=%v", len(second), total, second[0], second[len(second)-1], err)
	}
}

func TestVerifiedContentHashesGroupOnlyCurrentIdentityAndPersistScanEvidence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sha := hex.EncodeToString(make([]byte, 32))
	files := []CloudInventoryEntry{
		{FsID: 10, Path: "/z.bin", Name: "z.bin", Size: 7, ProviderMtime: 3},
		{FsID: 11, Path: "/a.bin", Name: "a.bin", Size: 7, ProviderMtime: 4},
		{FsID: 12, Path: "/changed.bin", Name: "changed.bin", Size: 8, ProviderMtime: 5},
		{FsID: 13, Path: "/m.bin", Name: "m.bin", Size: 7, ProviderMtime: 6},
		{FsID: 14, Path: "/b.bin", Name: "b.bin", Size: 7, ProviderMtime: 7},
	}
	if err = store.UpsertCloudInventoryPage("acct", "inventory-run", "/", files, now, nil); err != nil {
		t.Fatal(err)
	}
	for _, item := range files {
		if err = store.SaveVerifiedCloudContentHash("acct", item, sha, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	changed := files[2]
	changed.Size++
	if err = store.SaveVerifiedCloudContentHash("acct", changed, sha, now.Add(time.Second)); err == nil {
		t.Fatal("digest recorded for an identity that does not match cached inventory")
	}
	groups, err := store.ListVerifiedCloudDuplicateGroups("acct")
	if err != nil || len(groups) != 1 || groups[0].Size != 7 || groups[0].SHA256 != sha || groups[0].Keep.Path != "/a.bin" || len(groups[0].Delete) != 3 || groups[0].Delete[0].Path != "/b.bin" || groups[0].Delete[1].Path != "/m.bin" || groups[0].Delete[2].Path != "/z.bin" {
		t.Fatalf("verified duplicate groups=%+v err=%v", groups, err)
	}
	state := CloudDuplicateScanState{AccountFingerprint: "acct", RunID: "duplicate-run", Status: "running", Phase: "hashing", NextFsID: 11, TotalFiles: 2, ProcessedFiles: 1, StartedAtUTC: now, UpdatedAtUTC: now.Add(time.Second)}
	if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_duplicate_scan_events SET status='complete' WHERE run_id=?`, state.RunID); err == nil {
		t.Fatal("duplicate scan event was mutable")
	}
	events, err := store.CloudDuplicateScanEvents("acct", 0, 10)
	if err != nil || len(events) != 1 || events[0].RunID != state.RunID || events[0].Status != "running" || events[0].RecordedAtUTC.IsZero() {
		t.Fatalf("duplicate scan events=%+v err=%v", events, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.LoadCloudDuplicateScanState("acct")
	if err != nil || loaded.NextFsID != state.NextFsID || loaded.ProcessedFiles != 1 || !loaded.UpdatedAtUTC.Equal(state.UpdatedAtUTC.Truncate(time.Microsecond)) {
		t.Fatalf("duplicate scan state after restart=%+v err=%v", loaded, err)
	}
	var eventCount int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_duplicate_scan_events`).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("scan event table did not survive restart: count=%d err=%v", eventCount, err)
	}
}

func TestSQLiteV6StoreMigratesToV14CloudScanQueueSchema(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER cloud_inventory_file_size_update; DROP TRIGGER cloud_inventory_file_size_delete; DROP TRIGGER cloud_inventory_file_size_insert; DROP TABLE cloud_inventory_file_sizes; DROP TABLE cloud_content_hashes; DROP TABLE cloud_duplicate_scan_events; DROP TABLE cloud_duplicate_scan_state; UPDATE store_meta SET value='6' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 6, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version string
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != "15" {
		t.Fatalf("schema version=%s err=%v", version, err)
	}
}

func TestSQLiteV7StoreMigratesAndPreservesRetryDeadlineAcrossRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER cloud_inventory_file_size_update; DROP TRIGGER cloud_inventory_file_size_delete; DROP TRIGGER cloud_inventory_file_size_insert; DROP TABLE cloud_inventory_file_sizes; DROP TABLE cloud_duplicate_scan_state; CREATE TABLE cloud_duplicate_scan_state (account_fingerprint TEXT PRIMARY KEY,run_id TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,next_fs_id INTEGER NOT NULL CHECK(next_fs_id>=0),total_files INTEGER NOT NULL CHECK(total_files>=0),processed_files INTEGER NOT NULL CHECK(processed_files>=0),started_at_utc INTEGER NOT NULL,updated_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE store_meta SET value='7' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest{Version: 7, Scope: "/queue"})
	if err = atomicWrite(filepath.Join(dir, "manifest.json"), body); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	retryAt := time.Now().UTC().Add(30 * time.Second).Truncate(time.Microsecond)
	state := CloudDuplicateScanState{AccountFingerprint: "retry-account", RunID: "retry-run", Status: "running", Phase: "backoff", NextFsID: 91, TotalFiles: 4, ProcessedFiles: 2, StartedAtUTC: time.Now().UTC(), UpdatedAtUTC: time.Now().UTC(), LastError: "temporary rate limit", RetryDelayMillis: 30000, RetryAtUTC: &retryAt}
	if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
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
	loaded, err := store.LoadCloudDuplicateScanState("retry-account")
	if err != nil || loaded.RetryDelayMillis != state.RetryDelayMillis || loaded.RetryAtUTC == nil || !loaded.RetryAtUTC.Equal(retryAt) {
		t.Fatalf("retry state after migration/restart=%+v err=%v", loaded, err)
	}
	events, err := store.CloudDuplicateScanEvents("retry-account", 0, 5)
	var evidence map[string]any
	if len(events) == 1 {
		_ = json.Unmarshal(events[0].Evidence, &evidence)
	}
	if err != nil || len(events) != 1 || evidence["retry_at_utc"] != retryAt.Format(time.RFC3339Nano) {
		t.Fatalf("retry event evidence=%+v err=%v", events, err)
	}
}
