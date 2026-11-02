package taskstore

import (
	"bytes"
	"crypto/sha256"
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

func TestDuplicateScanCheckpointStateRoundTripsAcrossReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	h := sha256.New()
	_, _ = h.Write([]byte("already-read-prefix"))
	marshaler := h.(interface{ MarshalBinary() ([]byte, error) })
	hashState, err := marshaler.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	state := CloudDuplicateScanState{AccountFingerprint: "acct", RunID: "checkpoint-run", Status: "paused", Phase: "paused", NextFsID: 10, TotalFiles: 2, ProcessedFiles: 1, TotalBytes: 2048, ProcessedBytes: 1024, StartedAtUTC: now, UpdatedAtUTC: now, CurrentFsID: 11, CurrentPath: "/large.bin", CurrentName: "large.bin", CurrentSize: 1024, CurrentProviderMtime: 99, CurrentContentMD5: "provider-md5", CurrentBytes: 512, CurrentSHA256State: hashState}
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
	loaded, err := store.LoadCloudDuplicateScanState("acct")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentFsID != state.CurrentFsID || loaded.CurrentPath != state.CurrentPath || loaded.CurrentName != state.CurrentName || loaded.CurrentSize != state.CurrentSize || loaded.CurrentProviderMtime != state.CurrentProviderMtime || loaded.CurrentContentMD5 != state.CurrentContentMD5 || loaded.CurrentBytes != state.CurrentBytes || string(loaded.CurrentSHA256State) != string(hashState) {
		t.Fatalf("duplicate checkpoint after restart=%+v state_equal=%v", loaded, string(loaded.CurrentSHA256State) == string(hashState))
	}
	encoded, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), string(hashState)) {
		t.Fatalf("private SHA state appeared in JSON: %s", encoded)
	}
}

func TestCompleteCloudDuplicateCandidateAtomicallyStoresHashAndAdvancesProgress(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	account := "acct"
	item := CloudInventoryEntry{FsID: 41, Path: "/a.bin", Name: "a.bin", Size: 12, ProviderMtime: 7, ContentMD5: "md5-a"}
	other := CloudInventoryEntry{FsID: 42, Path: "/b.bin", Name: "b.bin", Size: 12, ProviderMtime: 8, ContentMD5: "md5-b"}
	if err = store.UpsertCloudInventoryPage(account, "inventory", "/", []CloudInventoryEntry{item, other}, now, nil); err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	_, _ = h.Write([]byte("complete-content"))
	hashState, err := h.(interface{ MarshalBinary() ([]byte, error) }).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	state := CloudDuplicateScanState{AccountFingerprint: account, RunID: "hash-run", Status: "running", Phase: "hashing", TotalFiles: 2, TotalBytes: 24, StartedAtUTC: now, UpdatedAtUTC: now, CurrentFsID: item.FsID, CurrentPath: item.Path, CurrentName: item.Name, CurrentSize: item.Size, CurrentProviderMtime: item.ProviderMtime, CurrentContentMD5: item.ContentMD5, CurrentBytes: item.Size, CurrentSHA256State: hashState}
	if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("d", 64)
	if err = store.CompleteCloudDuplicateCandidate(account, state, item, digest, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadCloudDuplicateScanState(account)
	if err != nil || loaded.NextFsID != item.FsID || loaded.ProcessedFiles != 1 || loaded.ProcessedBytes != item.Size || loaded.CurrentFsID != 0 || len(loaded.CurrentSHA256State) != 0 {
		t.Fatalf("completed scan state=%+v err=%v", loaded, err)
	}
	var hashes int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint=? AND fs_id=? AND sha256=?`, account, item.FsID, digest).Scan(&hashes); err != nil || hashes != 1 {
		t.Fatalf("verified hash rows=%d err=%v", hashes, err)
	}
}

func TestCompleteCloudDuplicateCandidateRollsBackHashWhenCheckpointChanged(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	item := CloudInventoryEntry{FsID: 51, Path: "/changed.bin", Name: "changed.bin", Size: 6, ProviderMtime: 9}
	if err = store.UpsertCloudInventoryPage("acct", "inventory", "/", []CloudInventoryEntry{item}, now, nil); err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	_, _ = h.Write([]byte("abcdef"))
	stateBytes, err := h.(interface{ MarshalBinary() ([]byte, error) }).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	state := CloudDuplicateScanState{AccountFingerprint: "acct", RunID: "run", Status: "running", Phase: "hashing", TotalFiles: 1, TotalBytes: item.Size, StartedAtUTC: now, UpdatedAtUTC: now, CurrentFsID: item.FsID, CurrentPath: item.Path, CurrentName: item.Name, CurrentSize: item.Size, CurrentProviderMtime: item.ProviderMtime, CurrentBytes: item.Size, CurrentSHA256State: stateBytes}
	if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_duplicate_scan_state SET current_bytes=0 WHERE account_fingerprint='acct'`); err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteCloudDuplicateCandidate("acct", state, item, strings.Repeat("a", 64), now.Add(time.Second)); err == nil {
		t.Fatal("completed against a changed checkpoint")
	}
	var hashes int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct'`).Scan(&hashes); err != nil || hashes != 0 {
		t.Fatalf("hash persisted despite stale checkpoint: count=%d err=%v", hashes, err)
	}
	loaded, err := store.LoadCloudDuplicateScanState("acct")
	if err != nil || loaded.NextFsID != 0 || loaded.ProcessedFiles != 0 {
		t.Fatalf("scan advanced despite stale checkpoint: %+v err=%v", loaded, err)
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

func TestVerifiedDuplicateGroupsPageIsStableBoundedAndAccountScoped(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	var files []CloudInventoryEntry
	for group := range []int{0, 1, 2} {
		copies := 2
		if group == 1 {
			copies = 3
		}
		for copyIndex := 0; copyIndex < copies; copyIndex++ {
			name := fmt.Sprintf("group-%d-copy-%d.bin", group, copyIndex)
			files = append(files, CloudInventoryEntry{FsID: int64(group*10 + copyIndex + 1), Path: "/" + name, Name: name, Size: int64(100 + group), ProviderMtime: int64(copyIndex + 1)})
		}
	}
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", files, now, nil); err != nil {
		t.Fatal(err)
	}
	for _, item := range files {
		group := int(item.FsID / 10)
		digest := strings.Repeat(string(rune('0'+group)), 64)
		if err = store.SaveVerifiedCloudContentHash("acct", item, digest, now); err != nil {
			t.Fatal(err)
		}
	}
	groups, totalGroups, deleteCount, err := store.ListVerifiedCloudDuplicateGroupsPage("acct", 1, 1)
	if err != nil || totalGroups != 3 || deleteCount != 4 || len(groups) != 1 || groups[0].SHA256 != strings.Repeat("1", 64) || len(groups[0].Delete) != 2 {
		t.Fatalf("page groups=%+v total=%d delete=%d err=%v", groups, totalGroups, deleteCount, err)
	}
	groups, totalGroups, deleteCount, err = store.ListVerifiedCloudDuplicateGroupsPage("acct", 3, 1)
	if err != nil || len(groups) != 0 || totalGroups != 3 || deleteCount != 4 {
		t.Fatalf("end page groups=%+v total=%d delete=%d err=%v", groups, totalGroups, deleteCount, err)
	}
	if _, _, _, err = store.ListVerifiedCloudDuplicateGroupsPage("acct", -1, 1); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, _, _, err = store.ListVerifiedCloudDuplicateGroupsPage("acct", 0, 51); err == nil {
		t.Fatal("overlarge page accepted")
	}
	groups, totalGroups, deleteCount, err = store.ListVerifiedCloudDuplicateGroupsPage("other-account", 0, 50)
	if err != nil || len(groups) != 0 || totalGroups != 0 || deleteCount != 0 {
		t.Fatalf("account leaked duplicate groups=%+v total=%d delete=%d err=%v", groups, totalGroups, deleteCount, err)
	}
}

func TestCloudDuplicateScanEventPageIsNewestFirstAndAccountScoped(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Now().UTC()
	for i, phase := range []string{"hashing", "backoff", "hashing"} {
		at := base.Add(time.Duration(i) * time.Second)
		state := CloudDuplicateScanState{AccountFingerprint: "acct-a", RunID: "run-a", Status: "running", Phase: phase, TotalFiles: 5, ProcessedFiles: int64(i), StartedAtUTC: base, UpdatedAtUTC: at}
		if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
			t.Fatal(err)
		}
	}
	other := CloudDuplicateScanState{AccountFingerprint: "acct-b", RunID: "run-b", Status: "paused", Phase: "paused", StartedAtUTC: base, UpdatedAtUTC: base}
	if err = store.SaveCloudDuplicateScanState(other, true); err != nil {
		t.Fatal(err)
	}
	page, before, more, err := store.CloudDuplicateScanEventPage("acct-a", 0, 2)
	if err != nil || len(page) != 2 || !more || page[0].Phase != "hashing" || page[1].Phase != "backoff" || before != page[1].Sequence {
		t.Fatalf("newest page=%+v before=%d more=%v err=%v", page, before, more, err)
	}
	older, nextBefore, hasMore, err := store.CloudDuplicateScanEventPage("acct-a", before, 2)
	if err != nil || len(older) != 1 || hasMore || older[0].Sequence >= before || nextBefore != 0 {
		t.Fatalf("older page=%+v before=%d more=%v err=%v", older, nextBefore, hasMore, err)
	}
	empty, _, emptyMore, err := store.CloudDuplicateScanEventPage("missing", 0, 20)
	if err != nil || len(empty) != 0 || emptyMore {
		t.Fatalf("empty account page=%+v more=%v err=%v", empty, emptyMore, err)
	}
	if _, _, _, err = store.CloudDuplicateScanEventPage("acct-a", -1, 20); err == nil {
		t.Fatal("negative before sequence accepted")
	}
	if _, _, _, err = store.CloudDuplicateScanEventPage("acct-a", 0, 51); err == nil {
		t.Fatal("overlarge event page accepted")
	}
}

func TestCloudDuplicateScanSchemaStoresVerifiedByteProgress(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().UnixMicro()
	_, err = store.db.Exec(`INSERT INTO cloud_duplicate_scan_state(account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,total_bytes,processed_bytes,started_at_utc,updated_at_utc,last_error) VALUES('acct','run','running','hashing',5,3,1,300,100,?,?, '')`, now, now)
	if err != nil {
		t.Fatalf("duplicate scan state cannot persist byte progress: %v", err)
	}
	var totalBytes, processedBytes int64
	if err = store.db.QueryRow(`SELECT total_bytes,processed_bytes FROM cloud_duplicate_scan_state WHERE account_fingerprint='acct'`).Scan(&totalBytes, &processedBytes); err != nil || totalBytes != 300 || processedBytes != 100 {
		t.Fatalf("byte progress total=%d processed=%d err=%v", totalBytes, processedBytes, err)
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
	estimate, err := store.EstimateCloudDuplicateHashCandidates("acct", 0)
	if err != nil || estimate.Files != 2 || estimate.EstimatedBytes != 84 {
		t.Fatalf("candidate estimate=%+v err=%v", estimate, err)
	}
	remaining, err := store.EstimateCloudDuplicateHashCandidates("acct", 1)
	if err != nil || remaining.Files != 1 || remaining.EstimatedBytes != 42 {
		t.Fatalf("remaining candidate estimate=%+v err=%v", remaining, err)
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
	state := CloudDuplicateScanState{AccountFingerprint: "acct", RunID: "duplicate-run", Status: "running", Phase: "hashing", NextFsID: 11, TotalFiles: 2, ProcessedFiles: 1, TotalBytes: 14, ProcessedBytes: 7, StartedAtUTC: now, UpdatedAtUTC: now.Add(time.Second)}
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
	var evidence struct {
		TotalBytes     int64 `json:"total_bytes"`
		ProcessedBytes int64 `json:"processed_bytes"`
	}
	if err = json.Unmarshal(events[0].Evidence, &evidence); err != nil || evidence.TotalBytes != state.TotalBytes || evidence.ProcessedBytes != state.ProcessedBytes {
		t.Fatalf("duplicate scan byte evidence=%s err=%v", events[0].Evidence, err)
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
	if err != nil || loaded.NextFsID != state.NextFsID || loaded.ProcessedFiles != 1 || loaded.TotalBytes != state.TotalBytes || loaded.ProcessedBytes != state.ProcessedBytes || !loaded.UpdatedAtUTC.Equal(state.UpdatedAtUTC.Truncate(time.Microsecond)) {
		t.Fatalf("duplicate scan state after restart=%+v err=%v", loaded, err)
	}
	var eventCount int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_duplicate_scan_events`).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("scan event table did not survive restart: count=%d err=%v", eventCount, err)
	}
}

func TestVerifiedHashSaveRejectsCachedProviderMD5ChangedAfterCandidateLoad(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account := "acct"
	now := time.Now().UTC()
	items := []CloudInventoryEntry{
		{FsID: 31, Path: "/a.bin", Name: "a.bin", Size: 7, ProviderMtime: 11, ContentMD5: "old-md5"},
		{FsID: 32, Path: "/b.bin", Name: "b.bin", Size: 7, ProviderMtime: 12, ContentMD5: "stable-md5"},
	}
	if err = store.UpsertCloudInventoryPage(account, "initial", "/", items, now, nil); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	for _, item := range items {
		if err = store.SaveVerifiedCloudContentHash(account, item, digest, now); err != nil {
			t.Fatal(err)
		}
	}
	changed := append([]CloudInventoryEntry(nil), items...)
	changed[0].ContentMD5 = "new-md5"
	if err = store.UpsertCloudInventoryPage(account, "refreshed", "/", changed, now.Add(time.Second), nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveVerifiedCloudContentHash(account, items[0], digest, now.Add(2*time.Second)); err == nil {
		t.Fatal("a stale candidate reattached its SHA-256 after provider MD5 changed")
	}
	groups, err := store.ListVerifiedCloudDuplicateGroups(account)
	if err != nil || len(groups) != 0 {
		t.Fatalf("stale hash remained a duplicate group: groups=%+v err=%v", groups, err)
	}
}

func TestRefreshCloudInventoryMD5InvalidatesVerifiedHashAtomically(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	item := CloudInventoryEntry{FsID: 51, Path: "/file.bin", Name: "file.bin", Size: 12, ProviderMtime: 33, ContentMD5: "old"}
	if err = store.UpsertCloudInventoryPage("acct", "scan", "/", []CloudInventoryEntry{item}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveVerifiedCloudContentHash("acct", item, strings.Repeat("b", 64), now); err != nil {
		t.Fatal(err)
	}
	observed := now.Add(time.Minute)
	if err = store.RefreshCloudInventoryMD5("acct", item, "new", observed); err != nil {
		t.Fatal(err)
	}
	var md5 string
	var gotObserved int64
	var hashes int
	if err = store.db.QueryRow(`SELECT content_md5,observed_at_utc FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=51`).Scan(&md5, &gotObserved); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id=51`).Scan(&hashes); err != nil {
		t.Fatal(err)
	}
	if md5 != "new" || gotObserved != observed.UnixMicro() || hashes != 0 {
		t.Fatalf("refreshed md5=%q observed=%d hashes=%d", md5, gotObserved, hashes)
	}
	if err = store.RefreshCloudInventoryMD5("acct", item, "other", observed.Add(time.Minute)); err == nil {
		t.Fatal("accepted refresh against stale MD5 identity")
	}
}

func seedDuplicateDriftCase(t *testing.T, stale bool) (*Store, CloudInventoryEntry, CloudDuplicateScanState, time.Time) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 31, 1, 2, 3, 0, time.UTC)
	old := CloudInventoryEntry{FsID: 71, Path: "/old/a.bin", Name: "a.bin", Size: 12, ProviderMtime: 33, ContentMD5: "old-md5"}
	peer := CloudInventoryEntry{FsID: 73, Path: "/new/peer.bin", Name: "peer.bin", Size: 12, ProviderMtime: 34, ContentMD5: "peer-md5"}
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/", []CloudInventoryEntry{{FsID: 70, Path: "/old", Name: "old", Directory: true}, {FsID: 72, Path: "/new", Name: "new", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/old", []CloudInventoryEntry{old}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/new", []CloudInventoryEntry{peer}, now, nil); err != nil {
		t.Fatal(err)
	}
	for _, item := range []CloudInventoryEntry{old, peer} {
		if err = store.SaveVerifiedCloudContentHash("acct", item, strings.Repeat("c", 64), now); err != nil {
			t.Fatal(err)
		}
	}
	checkpoint := []byte("sha256-state")
	state := CloudDuplicateScanState{AccountFingerprint: "acct", RunID: "run", Status: "running", Phase: "hashing", NextFsID: 70, TotalFiles: 2, ProcessedFiles: 1, TotalBytes: 24, ProcessedBytes: 12, StartedAtUTC: now, UpdatedAtUTC: now, CurrentFsID: old.FsID, CurrentPath: old.Path, CurrentName: old.Name, CurrentSize: old.Size, CurrentProviderMtime: old.ProviderMtime, CurrentContentMD5: old.ContentMD5, CurrentBytes: 8, CurrentSHA256State: checkpoint}
	if err = store.SaveCloudDuplicateScanState(state, false); err != nil {
		t.Fatal(err)
	}
	if stale {
		old.ContentMD5 = "stale-md5"
	}
	return store, old, state, now
}

func TestReconcileCloudDuplicateCandidateChangeUpdatesCacheAndPausesAtomically(t *testing.T) {
	store, expected, state, now := seedDuplicateDriftCase(t, false)
	defer store.Close()
	var generationBefore int64
	if err := store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'`).Scan(&generationBefore); err != nil {
		t.Fatal(err)
	}
	groupsBefore, err := store.ListVerifiedCloudDuplicateGroups("acct")
	if err != nil || len(groupsBefore) != 1 {
		t.Fatalf("test setup lacks a verified duplicate group: groups=%+v err=%v", groupsBefore, err)
	}
	observed := CloudInventoryEntry{FsID: 71, Path: "/new/renamed.bin", Name: "renamed.bin", Size: 20, ProviderMtime: 44, ContentMD5: "new-md5"}
	providerAt := now.Add(5 * time.Second)
	recordedAt := now.Add(6 * time.Second)
	if err := store.ReconcileCloudDuplicateCandidateChange("acct", state, expected, &observed, providerAt, recordedAt); err != nil {
		t.Fatal(err)
	}
	var got CloudInventoryEntry
	var gotParent string
	var gotObserved int64
	if err := store.db.QueryRow(`SELECT fs_id,path,parent_path,name,size_bytes,provider_mtime,content_md5,observed_at_utc FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=71`).Scan(&got.FsID, &got.Path, &gotParent, &got.Name, &got.Size, &got.ProviderMtime, &got.ContentMD5, &gotObserved); err != nil {
		t.Fatal(err)
	}
	if got.Path != observed.Path || gotParent != "/new" || got.Name != observed.Name || got.Size != observed.Size || got.ProviderMtime != observed.ProviderMtime || got.ContentMD5 != observed.ContentMD5 || gotObserved != providerAt.UnixMicro() {
		t.Fatalf("reconciled inventory=%+v observed=%d", got, gotObserved)
	}
	var hashes int
	if err := store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id=71`).Scan(&hashes); err != nil || hashes != 0 {
		t.Fatalf("old verified hash remains: count=%d err=%v", hashes, err)
	}
	for _, parent := range []string{"/old", "/new"} {
		var status string
		if err := store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path=?`, parent).Scan(&status); err != nil || status != "stale" {
			t.Fatalf("parent %s status=%q err=%v", parent, status, err)
		}
	}
	var generationAfter int64
	if err := store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'`).Scan(&generationAfter); err != nil || generationAfter <= generationBefore {
		t.Fatalf("generation did not advance: before=%d after=%d err=%v", generationBefore, generationAfter, err)
	}
	groupsAfter, err := store.ListVerifiedCloudDuplicateGroups("acct")
	if err != nil || len(groupsAfter) != 0 {
		t.Fatalf("stale directories still expose duplicate preview groups: groups=%+v err=%v", groupsAfter, err)
	}
	rootSize, err := store.CloudDirectorySize("acct", "/")
	if err != nil || rootSize.Complete {
		t.Fatalf("stale directory still exposes complete aggregate size: size=%+v err=%v", rootSize, err)
	}
	gotState, err := store.LoadCloudDuplicateScanState("acct")
	if err != nil || gotState.Status != "paused" || gotState.Phase != "inventory_changed" || gotState.NextFsID != state.NextFsID || gotState.ProcessedFiles != state.ProcessedFiles || gotState.ProcessedBytes != state.ProcessedBytes || gotState.CurrentFsID != 0 || len(gotState.CurrentSHA256State) != 0 {
		t.Fatalf("scan state after reconcile=%+v err=%v", gotState, err)
	}
	if gotState.UpdatedAtUTC != recordedAt {
		t.Fatalf("local record time=%s, want %s", gotState.UpdatedAtUTC, recordedAt)
	}
	events, _, _, err := store.CloudDuplicateScanEventPage("acct", 0, 10)
	if err != nil || len(events) != 1 || !strings.Contains(string(events[0].Evidence), observed.Path) || !strings.Contains(string(events[0].Evidence), providerAt.Format(time.RFC3339)) || !strings.Contains(string(events[0].Evidence), recordedAt.Format(time.RFC3339)) {
		t.Fatalf("change event missing snapshots/times: %+v err=%v", events, err)
	}
}

func TestUnavailableDuplicateIdentityPreservesCacheAndInvalidatesCoverage(t *testing.T) {
	store, expected, state, now := seedDuplicateDriftCase(t, false)
	defer store.Close()
	if err := store.ReconcileCloudDuplicateCandidateChange("acct", state, expected, nil, time.Time{}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var pathValue, md5 string
	var hashes int
	if err := store.db.QueryRow(`SELECT path,content_md5 FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=71`).Scan(&pathValue, &md5); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id=71`).Scan(&hashes); err != nil {
		t.Fatal(err)
	}
	var directoryStatus string
	if err := store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/old'`).Scan(&directoryStatus); err != nil {
		t.Fatal(err)
	}
	if pathValue != expected.Path || md5 != expected.ContentMD5 || hashes != 1 || directoryStatus != "stale" {
		t.Fatalf("ambiguous response changed cache: path=%q md5=%q hashes=%d directory=%q", pathValue, md5, hashes, directoryStatus)
	}
}

func TestDuplicateDriftReconciliationRejectsStaleCachedIdentity(t *testing.T) {
	store, expected, state, now := seedDuplicateDriftCase(t, true)
	defer store.Close()
	observed := CloudInventoryEntry{FsID: expected.FsID, Path: "/new/a.bin", Name: "a.bin", Size: 15, ProviderMtime: 44, ContentMD5: "fresh"}
	if err := store.ReconcileCloudDuplicateCandidateChange("acct", state, expected, &observed, now.Add(time.Second), now.Add(2*time.Second)); err == nil {
		t.Fatal("reconciliation accepted a cached row that no longer matches the scan candidate")
	}
	var got string
	if err := store.db.QueryRow(`SELECT path FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=71`).Scan(&got); err != nil || got != "/old/a.bin" {
		t.Fatalf("stale identity partially changed cache: path=%q err=%v", got, err)
	}
}

func TestDuplicateDriftReconciliationDefersToActiveInventoryScan(t *testing.T) {
	store, expected, state, now := seedDuplicateDriftCase(t, false)
	defer store.Close()
	if _, err := store.db.Exec(`INSERT INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES('acct','active-scan',0,'/old')`); err != nil {
		t.Fatal(err)
	}
	observed := CloudInventoryEntry{FsID: expected.FsID, Path: "/new/a.bin", Name: "a.bin", Size: 14, ProviderMtime: 44, ContentMD5: "fresh"}
	if err := store.ReconcileCloudDuplicateCandidateChange("acct", state, expected, &observed, now.Add(time.Second), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	entries, oldDir, _, err := store.ListCloudInventory("acct", "/old", 0, 20)
	if err != nil || len(entries) != 1 || entries[0].Path != expected.Path || oldDir.Status != "complete" {
		t.Fatalf("reconciliation raced the active inventory scan: entries=%+v directory=%+v err=%v", entries, oldDir, err)
	}
	events, _, _, err := store.CloudDuplicateScanEventPage("acct", 0, 10)
	if err != nil || len(events) != 1 || !bytes.Contains(events[0].Evidence, []byte(`"reconciliation_deferred":true`)) {
		t.Fatalf("deferred reconciliation evidence missing: events=%+v err=%v", events, err)
	}
	got, err := store.LoadCloudDuplicateScanState("acct")
	if err != nil || got.Status != "paused" || got.NextFsID != state.NextFsID || got.CurrentFsID != 0 {
		t.Fatalf("deferred reconciliation did not safely pause: state=%+v err=%v", got, err)
	}
}

func TestDuplicateDriftReconciliationDetectsAncestorScanQueue(t *testing.T) {
	store, expected, state, now := seedDuplicateDriftCase(t, false)
	defer store.Close()
	if _, err := store.db.Exec(`INSERT INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES('acct','active-root-scan',0,'/')`); err != nil {
		t.Fatal(err)
	}
	observed := CloudInventoryEntry{FsID: expected.FsID, Path: "/new/a.bin", Name: "a.bin", Size: 14, ProviderMtime: 44, ContentMD5: "fresh"}
	if err := store.ReconcileCloudDuplicateCandidateChange("acct", state, expected, &observed, now.Add(time.Second), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	entries, oldDir, _, err := store.ListCloudInventory("acct", "/old", 0, 20)
	if err != nil || len(entries) != 1 || entries[0].Path != expected.Path || oldDir.Status != "complete" {
		t.Fatalf("ancestor scan queue was not treated as a conflict: entries=%+v directory=%+v err=%v", entries, oldDir, err)
	}
	events, _, _, err := store.CloudDuplicateScanEventPage("acct", 0, 10)
	if err != nil || len(events) != 1 || !bytes.Contains(events[0].Evidence, []byte(`"reconciliation_deferred":true`)) {
		t.Fatalf("ancestor conflict lacks durable deferred evidence: events=%+v err=%v", events, err)
	}
}

func TestDuplicateDriftReconciliationRollsBackWhenEventCannotBeAppended(t *testing.T) {
	store, expected, state, now := seedDuplicateDriftCase(t, false)
	defer store.Close()
	if _, err := store.db.Exec(`CREATE TRIGGER reject_duplicate_drift_event BEFORE INSERT ON cloud_duplicate_scan_events BEGIN SELECT RAISE(ABORT,'injected event failure'); END`); err != nil {
		t.Fatal(err)
	}
	observed := CloudInventoryEntry{FsID: expected.FsID, Path: "/new/a.bin", Name: "a.bin", Size: 14, ProviderMtime: 44, ContentMD5: "fresh"}
	if err := store.ReconcileCloudDuplicateCandidateChange("acct", state, expected, &observed, now.Add(time.Second), now.Add(2*time.Second)); err == nil {
		t.Fatal("expected injected event append failure")
	}
	entries, oldDir, _, err := store.ListCloudInventory("acct", "/old", 0, 20)
	if err != nil || len(entries) != 1 || entries[0].Path != expected.Path || oldDir.Status != "complete" {
		t.Fatalf("failed reconciliation partially changed inventory: entries=%+v directory=%+v err=%v", entries, oldDir, err)
	}
	got, err := store.LoadCloudDuplicateScanState("acct")
	if err != nil || got.Status != "running" || got.CurrentFsID != expected.FsID {
		t.Fatalf("failed reconciliation partially changed scan state: state=%+v err=%v", got, err)
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
	if err = store.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil || version != fmt.Sprint(sqliteSchemaVersion) {
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
	state := CloudDuplicateScanState{AccountFingerprint: "retry-account", RunID: "retry-run", Status: "running", Phase: "backoff", NextFsID: 91, TotalFiles: 4, ProcessedFiles: 2, StartedAtUTC: time.Now().UTC(), UpdatedAtUTC: time.Now().UTC(), LastError: "temporary rate limit", RetryDelayMillis: 30000, RetryBackoffBaseMillis: 60000, RetryAtUTC: &retryAt}
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
	if err != nil || loaded.RetryDelayMillis != state.RetryDelayMillis || loaded.RetryBackoffBaseMillis != state.RetryBackoffBaseMillis || loaded.RetryAtUTC == nil || !loaded.RetryAtUTC.Equal(retryAt) {
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
