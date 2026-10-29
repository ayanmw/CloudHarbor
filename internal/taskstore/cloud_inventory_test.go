package taskstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func BenchmarkUpsertCloudInventoryPage100(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "queue"), "/queue")
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	entries := make([]CloudInventoryEntry, 100)
	for i := range entries {
		name := fmt.Sprintf("file-%03d.bin", i)
		entries[i] = CloudInventoryEntry{FsID: int64(i + 1), Path: "/benchmark/" + name, Name: name, Size: int64(i + 1)}
	}
	next := 100
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err = store.UpsertCloudInventoryPage("benchmark-account", "benchmark-run", "/benchmark", entries, time.Now().UTC(), &next); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCloudInventoryPage100Strategies(b *testing.B) {
	entries := make([]CloudInventoryEntry, 100)
	for i := range entries {
		name := fmt.Sprintf("file-%03d.bin", i)
		entries[i] = CloudInventoryEntry{FsID: int64(i + 1), Path: "/benchmark/" + name, Name: name, Size: int64(i + 1)}
	}
	for _, strategy := range []string{"batched", "sequential"} {
		b.Run(strategy, func(b *testing.B) {
			store, err := Open(filepath.Join(b.TempDir(), "queue"), "/queue")
			if err != nil {
				b.Fatal(err)
			}
			defer store.Close()
			next := 100
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				now := time.Now().UTC()
				if strategy == "batched" {
					err = store.UpsertCloudInventoryPage("benchmark-account", "benchmark-run", "/benchmark", entries, now, &next)
				} else {
					err = upsertCloudInventoryPageSequentialBenchmark(store, "benchmark-account", "benchmark-run", "/benchmark", entries, now)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func upsertCloudInventoryPageSequentialBenchmark(store *Store, account, runID, directory string, entries []CloudInventoryEntry, observedAt time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	tx, err := store.db.Begin()
	if err != nil {
		return err
	}
	if err = bumpCloudInventoryGeneration(tx, account, observedAt); err != nil {
		_ = tx.Rollback()
		return err
	}
	entryStmt, err := tx.Prepare(upsertCloudInventoryEntrySQL)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	directoryStmt, err := tx.Prepare(upsertCloudInventoryDirectorySQL)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err = tx.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES(?,?,'scanning',NULL,?) ON CONFLICT(account_fingerprint,path) DO UPDATE SET status='scanning',last_run_id=excluded.last_run_id`, account, directory, runID); err != nil {
		_ = tx.Rollback()
		return err
	}
	observed := observedAt.UTC().UnixMicro()
	for _, entry := range entries {
		if _, err = entryStmt.Exec(account, entry.FsID, directory, entry.Path, entry.Name, 0, entry.Size, entry.ProviderMtime, entry.ContentMD5, observed, runID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err = entryStmt.Close(); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = directoryStmt.Close(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func TestBulkCloudInventoryUpsertWritesMoreThanOneBoundedBatch(t *testing.T) {
	if cloudInventoryUpsertBatchSize != 80 || cloudInventoryUpsertBatchSize*11 >= 999 || cloudInventoryUpsertBatchCount(81) != 2 {
		t.Fatalf("inventory batch bound must remain 80 rows and below SQLite's 999 bind variables: size=%d", cloudInventoryUpsertBatchSize)
	}
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries := make([]CloudInventoryEntry, 81)
	for i := range entries {
		name := fmt.Sprintf("file-%03d.bin", i)
		entries[i] = CloudInventoryEntry{FsID: int64(i + 1), Path: "/bulk/" + name, Name: name, Size: int64(i + 10)}
	}
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = bulkUpsertCloudInventoryEntries(tx, "acct", "/bulk", entries, time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC), "run"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	var bytes int64
	if err = store.db.QueryRow(`SELECT count(*),sum(size_bytes) FROM cloud_inventory WHERE account_fingerprint='acct'`).Scan(&count, &bytes); err != nil || count != len(entries) || bytes != 81*10+80*81/2 {
		t.Fatalf("batched inventory count=%d bytes=%d err=%v", count, bytes, err)
	}
}

func TestCloudInventoryPageDuplicateFsIDKeepsLastObservedValue(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries := []CloudInventoryEntry{
		{FsID: 1, Path: "/page/first.bin", Name: "first.bin", Size: 10},
		{FsID: 1, Path: "/page/last.bin", Name: "last.bin", Size: 20},
	}
	if err = store.UpsertCloudInventoryPage("acct", "run", "/page", entries, time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatal(err)
	}
	var gotPath string
	var gotSize int64
	if err = store.db.QueryRow(`SELECT path,size_bytes FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=1`).Scan(&gotPath, &gotSize); err != nil || gotPath != "/page/last.bin" || gotSize != 20 {
		t.Fatalf("duplicate identity did not keep the final row: path=%q size=%d err=%v", gotPath, gotSize, err)
	}
}

func TestCloudInventoryLaterBulkBatchFailureRollsBackWholePage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 29, 8, 30, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/", []CloudInventoryEntry{{FsID: 999, Path: "/stable.bin", Name: "stable.bin", Size: 7}}, now.Add(-time.Second), nil); err != nil {
		t.Fatal(err)
	}
	state := CloudScanState{AccountFingerprint: "acct", RunID: "scan", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fail_second_inventory_batch BEFORE INSERT ON cloud_inventory WHEN NEW.fs_id=81 BEGIN SELECT RAISE(ABORT,'injected second-batch failure'); END`); err != nil {
		t.Fatal(err)
	}
	entries := make([]CloudInventoryEntry, 81)
	for i := range entries {
		name := fmt.Sprintf("file-%03d.bin", i)
		entries[i] = CloudInventoryEntry{FsID: int64(i + 1), Path: "/" + name, Name: name, Size: int64(i + 1)}
	}
	if err = store.UpsertCloudInventoryScanPage(&state, "/", entries, now, nil, nil); err == nil {
		t.Fatal("page unexpectedly committed through a failing second batch")
	}
	if state.Status != "running" || state.PagesProcessed != 0 || state.ItemsProcessed != 0 || state.PendingCount != 1 || state.CurrentDirectory != "/" {
		t.Fatalf("in-memory scan state advanced after rollback: %+v", state)
	}
	var count int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed page changed committed inventory: rows=%d err=%v", count, err)
	}
	persisted, err := store.LoadCloudScanState("acct")
	if err != nil || persisted.Status != "running" || persisted.PagesProcessed != 0 || persisted.ItemsProcessed != 0 || persisted.PendingCount != 1 || persisted.CurrentDirectory != "/" {
		t.Fatalf("persistent scan state advanced after rollback: %+v err=%v", persisted, err)
	}
	if head, headErr := store.CloudScanQueueHead("acct", "scan"); headErr != nil || head != "/" {
		t.Fatalf("queue head changed after rollback: head=%q err=%v", head, headErr)
	}
	var generationCount int
	var generation int64
	if err = store.db.QueryRow(`SELECT count(*),coalesce(max(generation),0) FROM cloud_inventory_generations WHERE account_fingerprint='acct'`).Scan(&generationCount, &generation); err != nil || generationCount != 1 || generation != 1 {
		t.Fatalf("failed multi-batch page changed existing generation: rows=%d generation=%d err=%v", generationCount, generation, err)
	}
}

func TestCloudInventoryBatchedFileRunsPreserveDirectoryPruningOrder(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	initial := []CloudInventoryEntry{{FsID: 1, Path: "/a", Name: "a", Directory: true}, {FsID: 2, Path: "/a", Name: "a", Directory: true}}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/", initial, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/a", nil, now.Add(time.Second), nil); err != nil {
		t.Fatal(err)
	}
	before, err := store.CloudDirectorySize("acct", "/a")
	if err != nil || !before.Complete {
		t.Fatalf("setup directory is not complete: %+v err=%v", before, err)
	}
	changed := []CloudInventoryEntry{
		{FsID: 1, Path: "/a", Name: "a", Directory: true},
		{FsID: 2, Path: "/b", Name: "b", Directory: true},
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-2", "/", changed, now.Add(2*time.Second), nil); err != nil {
		t.Fatal(err)
	}
	var oldPathState int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/a'`).Scan(&oldPathState); err != nil || oldPathState != 0 {
		t.Fatalf("directory move pruning order changed: old path state rows=%d err=%v", oldPathState, err)
	}
}

func BenchmarkCloudScanQueueInsertStrategies100(b *testing.B) {
	paths := make([]string, 100)
	for i := range paths {
		paths[i] = fmt.Sprintf("/child-%03d", i)
	}
	for _, strategy := range []string{"individual-exec", "prepared-exec"} {
		b.Run(strategy, func(b *testing.B) {
			store, err := Open(filepath.Join(b.TempDir(), "queue"), "/queue")
			if err != nil {
				b.Fatal(err)
			}
			defer store.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tx, txErr := store.db.Begin()
				if txErr != nil {
					b.Fatal(txErr)
				}
				runID := fmt.Sprintf("run-%d", i)
				switch strategy {
				case "individual-exec":
					for sequence, child := range paths {
						if _, txErr = tx.Exec(`INSERT OR IGNORE INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES(?,?,?,?)`, "bench", runID, sequence, child); txErr != nil {
							_ = tx.Rollback()
							b.Fatal(txErr)
						}
					}
				case "prepared-exec":
					var stmt *sql.Stmt
					stmt, txErr = tx.Prepare(`INSERT OR IGNORE INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES(?,?,?,?)`)
					if txErr != nil {
						_ = tx.Rollback()
						b.Fatal(txErr)
					}
					for sequence, child := range paths {
						if _, txErr = stmt.Exec("bench", runID, sequence, child); txErr != nil {
							_ = tx.Rollback()
							b.Fatal(txErr)
						}
					}
					if txErr = stmt.Close(); txErr != nil {
						_ = tx.Rollback()
						b.Fatal(txErr)
					}
				}
				if txErr = tx.Commit(); txErr != nil {
					b.Fatal(txErr)
				}
			}
		})
	}
}

func BenchmarkCloudDirectorySizeLargeSubtree(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "queue"), "/queue")
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	tx, err := store.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	entryStmt, err := tx.Prepare(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	directoryStmt, err := tx.Prepare(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES(? ,?,'complete',?,'bench')`)
	if err != nil {
		b.Fatal(err)
	}
	observed := time.Now().UTC().UnixMicro()
	if _, err = directoryStmt.Exec("bench-account", "/", observed); err != nil {
		b.Fatal(err)
	}
	for i := int64(1); i <= 50000; i++ {
		directory := fmt.Sprintf("/d-%05d", i)
		if _, err = entryStmt.Exec("bench-account", i, "/", directory, strings.TrimPrefix(directory, "/"), 1, 0, 1, "", observed, "bench"); err != nil {
			b.Fatal(err)
		}
		if _, err = entryStmt.Exec("bench-account", i+50000, directory, directory+"/file.bin", "file.bin", 0, 1, 1, "", observed, "bench"); err != nil {
			b.Fatal(err)
		}
		if _, err = directoryStmt.Exec("bench-account", directory, observed); err != nil {
			b.Fatal(err)
		}
	}
	if err = entryStmt.Close(); err != nil {
		b.Fatal(err)
	}
	if err = directoryStmt.Close(); err != nil {
		b.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		b.Fatal(err)
	}
	got, err := store.CloudDirectorySize("bench-account", "/")
	if err != nil || !got.Complete || got.Bytes != 50000 || got.Files != 50000 || got.Directories != 50000 {
		b.Fatalf("invalid benchmark fixture result: %+v err=%v", got, err)
	}
	b.Run("aggregate-and-persist", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			tx, txErr := store.db.Begin()
			if txErr != nil {
				b.Fatal(txErr)
			}
			if txErr = bumpCloudInventoryGeneration(tx, "bench-account", time.Now().UTC()); txErr != nil {
				_ = tx.Rollback()
				b.Fatal(txErr)
			}
			if txErr = tx.Commit(); txErr != nil {
				b.Fatal(txErr)
			}
			if _, err = store.CloudDirectorySize("bench-account", "/"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("persisted-cache-hit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err = store.CloudDirectorySize("bench-account", "/"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("legacy-multiple-scans", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err = benchmarkLegacyCloudDirectorySize(store, "bench-account", "/"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func benchmarkLegacyCloudDirectorySize(store *Store, account, directory string) error {
	var status string
	if err := store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint=? AND path=?`, account, directory).Scan(&status); err != nil {
		return err
	}
	prefix, upper := cloudDirectoryPathRange(directory)
	var incomplete, files, directories int64
	var bytes int64
	var observed sql.NullInt64
	if err := store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_directories WHERE account_fingerprint=? AND path>=? AND path<? AND status<>'complete'`, account, prefix, upper).Scan(&incomplete); err != nil {
		return err
	}
	if err := store.db.QueryRow(cloudInventoryFileAggregateSQL, account, prefix, upper).Scan(&bytes, &files); err != nil {
		return err
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_directories WHERE account_fingerprint=? AND path<>? AND path>=? AND path<?`, account, directory, prefix, upper).Scan(&directories); err != nil {
		return err
	}
	if err := store.db.QueryRow(`SELECT min(observed_at_utc) FROM cloud_inventory_directories WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?)) AND status='complete'`, account, directory, prefix, upper).Scan(&observed); err != nil {
		return err
	}
	return nil
}

func TestCloudInventorySizeUsesOnlyCompleteCachedSubtreeAndPersists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Date(2026, 10, 29, 2, 0, 0, 0, time.UTC)
	more := 1
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/", []CloudInventoryEntry{{FsID: 1, Path: "/docs", Name: "docs", Directory: true}, {FsID: 2, Path: "/root.bin", Name: "root.bin", Size: 4}}, observed, &more); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/", nil, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/docs", []CloudInventoryEntry{{FsID: 3, Path: "/docs/a.bin", Name: "a.bin", Size: 3}, {FsID: 4, Path: "/docs/sub", Name: "sub", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/docs/sub", []CloudInventoryEntry{{FsID: 5, Path: "/docs/sub/b.bin", Name: "b.bin", Size: 7}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	size, err := store.CloudDirectorySize("acct", "/docs")
	if err != nil || !size.Complete || size.Bytes != 10 || size.Files != 2 || size.Directories != 1 || !size.ObservedAtUTC.Equal(observed) {
		t.Fatalf("size=%+v err=%v", size, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	size, err = store.CloudDirectorySize("acct", "/docs")
	if err != nil || size.Bytes != 10 || !size.Complete {
		t.Fatalf("reopened size=%+v err=%v", size, err)
	}
	if _, err = store.CloudDirectorySize("another-account", "/docs"); err != nil {
		t.Fatal(err)
	}
	other, err := store.CloudDirectorySize("another-account", "/docs")
	if err != nil || other.Complete || other.Bytes != 0 {
		t.Fatalf("cross-account cache leak: %+v err=%v", other, err)
	}
}

func TestCloudDirectorySizePersistentCache(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Date(2026, 10, 29, 2, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/", []CloudInventoryEntry{{FsID: 1, Path: "/child", Name: "child", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/child", []CloudInventoryEntry{{FsID: 2, Path: "/child/a.bin", Name: "a.bin", Size: 40}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	first, err := store.CloudDirectorySize("acct", "/")
	if err != nil || !first.Complete || first.Bytes != 40 || first.Files != 1 || first.Directories != 1 || !first.ObservedAtUTC.Equal(observed) || first.CachedAtUTC.IsZero() {
		t.Fatalf("first size=%+v err=%v", first, err)
	}
	var oldCachedAt int64
	if err = store.db.QueryRow(`UPDATE cloud_directory_size_cache SET cached_at_utc=1 WHERE account_fingerprint='acct' AND path='/' RETURNING cached_at_utc`).Scan(&oldCachedAt); err != nil {
		t.Fatalf("set deterministic cache timestamp: %v", err)
	}
	second, err := store.CloudDirectorySize("acct", "/")
	if err != nil || !second.CachedAtUTC.Equal(time.UnixMicro(oldCachedAt).UTC()) {
		t.Fatalf("same-generation size did not reuse cache: %+v err=%v", second, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	updatedObserved := observed.Add(time.Minute)
	if err = store.UpsertCloudInventoryPage("acct", "run-2", "/child", []CloudInventoryEntry{{FsID: 2, Path: "/child/a.bin", Name: "a.bin", Size: 90}}, updatedObserved, nil); err != nil {
		t.Fatal(err)
	}
	updated, err := store.CloudDirectorySize("acct", "/")
	if err != nil || !updated.Complete || updated.Bytes != 90 || updated.Files != 1 || updated.Directories != 1 || !updated.ObservedAtUTC.Equal(observed) || updated.CachedAtUTC.UnixMicro() <= oldCachedAt {
		t.Fatalf("updated generation size=%+v err=%v", updated, err)
	}
	if err = store.InvalidateCloudInventory("acct", []string{"/child"}); err != nil {
		t.Fatal(err)
	}
	invalidated, err := store.CloudDirectorySize("acct", "/")
	if err != nil || invalidated.Complete || invalidated.Bytes != 90 || invalidated.CachedAtUTC.UnixMicro() <= updated.CachedAtUTC.UnixMicro() {
		t.Fatalf("invalidated size=%+v err=%v", invalidated, err)
	}
	other, err := store.CloudDirectorySize("other-account", "/")
	if err != nil || other.Complete || other.Bytes != 0 {
		t.Fatalf("account cache isolation failed: %+v err=%v", other, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloudInventoryGenerationRollsBackWithFailedPage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/", []CloudInventoryEntry{{FsID: 1, Path: "/stable.bin", Name: "stable.bin", Size: 12}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	before, err := store.CloudDirectorySize("acct", "/")
	if err != nil {
		t.Fatal(err)
	}
	var generationBefore int64
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'`).Scan(&generationBefore); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fail_inventory_insert BEFORE INSERT ON cloud_inventory WHEN NEW.fs_id=99 BEGIN SELECT RAISE(ABORT,'injected page failure'); END`); err != nil {
		t.Fatal(err)
	}
	err = store.UpsertCloudInventoryPage("acct", "run-2", "/", []CloudInventoryEntry{{FsID: 99, Path: "/failed.bin", Name: "failed.bin", Size: 88}}, observed.Add(time.Minute), nil)
	if err == nil {
		t.Fatal("inventory page unexpectedly committed through a failing trigger")
	}
	if _, err = store.db.Exec(`DROP TRIGGER fail_inventory_insert`); err != nil {
		t.Fatal(err)
	}
	var generationAfter int64
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'`).Scan(&generationAfter); err != nil || generationAfter != generationBefore {
		t.Fatalf("failed page changed generation from %d to %d: %v", generationBefore, generationAfter, err)
	}
	after, err := store.CloudDirectorySize("acct", "/")
	if err != nil || after.Bytes != before.Bytes || !after.CachedAtUTC.Equal(before.CachedAtUTC) {
		t.Fatalf("failed page invalidated committed size cache: before=%+v after=%+v err=%v", before, after, err)
	}
	if err = store.UpsertCloudInventoryPage("other-account", "run-1", "/", []CloudInventoryEntry{{FsID: 1, Path: "/other.bin", Name: "other.bin", Size: 99}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'`).Scan(&generationAfter); err != nil || generationAfter != generationBefore {
		t.Fatalf("another account changed generation from %d to %d: %v", generationBefore, generationAfter, err)
	}
}

func TestCloudInventoryDirectoryMovePrunesOldSubtree(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 29, 5, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/", []CloudInventoryEntry{{FsID: 1, Path: "/old", Name: "old", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/old", []CloudInventoryEntry{{FsID: 2, Path: "/old/removed.bin", Name: "removed.bin", Size: 100}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	initial, err := store.CloudDirectorySize("acct", "/")
	if err != nil || !initial.Complete || initial.Bytes != 100 || initial.Directories != 1 {
		t.Fatalf("initial size=%+v err=%v", initial, err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-2", "/", []CloudInventoryEntry{{FsID: 1, Path: "/new", Name: "new", Directory: true}}, observed.Add(time.Minute), nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-2", "/new", nil, observed.Add(time.Minute), nil); err != nil {
		t.Fatal(err)
	}
	updated, err := store.CloudDirectorySize("acct", "/")
	if err != nil || !updated.Complete || updated.Bytes != 0 || updated.Files != 0 || updated.Directories != 1 {
		t.Fatalf("size after remote directory move=%+v err=%v", updated, err)
	}
	var oldInventory, oldDirectoryMetadata int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct' AND (path='/old' OR path LIKE '/old/%')`).Scan(&oldInventory); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND (path='/old' OR path LIKE '/old/%')`).Scan(&oldDirectoryMetadata); err != nil {
		t.Fatal(err)
	}
	if oldInventory != 0 || oldDirectoryMetadata != 0 {
		t.Fatalf("old directory subtree survived refresh: entries=%d directories=%d", oldInventory, oldDirectoryMetadata)
	}
}

func TestCloudInventoryReplacedDirectoryAtSamePathRetainsNewIdentity(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 29, 6, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/", []CloudInventoryEntry{{FsID: 1, Path: "/folder", Name: "folder", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/folder", []CloudInventoryEntry{{FsID: 2, Path: "/folder/old.bin", Name: "old.bin", Size: 100}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-2", "/", []CloudInventoryEntry{{FsID: 3, Path: "/folder", Name: "folder", Directory: true}}, observed.Add(time.Minute), nil); err != nil {
		t.Fatal(err)
	}
	items, metadata, total, err := store.ListCloudInventory("acct", "/", 0, 10)
	if err != nil || total != 1 || len(items) != 1 || items[0].FsID != 3 || metadata.Status != "complete" {
		t.Fatalf("replacement directory disappeared: items=%+v metadata=%+v total=%d err=%v", items, metadata, total, err)
	}
	size, err := store.CloudDirectorySize("acct", "/")
	if err != nil || size.Bytes != 0 || size.Files != 0 || size.Directories != 1 || size.Complete {
		t.Fatalf("new directory should be awaiting its own scan: %+v err=%v", size, err)
	}
}

func TestCloudInventoryDirectoryMovePruningRollsBackWithFailedPage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 29, 7, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/", []CloudInventoryEntry{{FsID: 1, Path: "/old", Name: "old", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/old", []CloudInventoryEntry{{FsID: 2, Path: "/old/kept.bin", Name: "kept.bin", Size: 100}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fail_moved_directory BEFORE INSERT ON cloud_inventory WHEN NEW.fs_id=1 AND NEW.path='/new' BEGIN SELECT RAISE(ABORT,'injected move failure'); END`); err != nil {
		t.Fatal(err)
	}
	err = store.UpsertCloudInventoryPage("acct", "run-2", "/", []CloudInventoryEntry{{FsID: 1, Path: "/new", Name: "new", Directory: true}}, observed.Add(time.Minute), nil)
	if err == nil {
		t.Fatal("directory move unexpectedly committed through a failing trigger")
	}
	if _, err = store.db.Exec(`DROP TRIGGER fail_moved_directory`); err != nil {
		t.Fatal(err)
	}
	var oldChildCount int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct' AND path='/old/kept.bin'`).Scan(&oldChildCount); err != nil || oldChildCount != 1 {
		t.Fatalf("failed move did not roll back old subtree pruning: count=%d err=%v", oldChildCount, err)
	}
	var currentPath string
	if err = store.db.QueryRow(`SELECT path FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=1 AND is_dir=1`).Scan(&currentPath); err != nil || currentPath != "/old" {
		t.Fatalf("failed move changed directory identity: path=%q err=%v", currentPath, err)
	}
	size, err := store.CloudDirectorySize("acct", "/")
	if err != nil || size.Bytes != 100 || size.Files != 1 || !size.Complete {
		t.Fatalf("failed move changed committed size evidence: %+v err=%v", size, err)
	}
}

func TestCloudInventoryIncompleteDirectoryIsNotReportedComplete(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	more := 100
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", []CloudInventoryEntry{{FsID: 1, Path: "/partial.bin", Name: "partial.bin", Size: 12}}, time.Now().UTC(), &more); err != nil {
		t.Fatal(err)
	}
	size, err := store.CloudDirectorySize("acct", "/")
	if err != nil || size.Complete || size.Bytes != 12 {
		t.Fatalf("partial scan size=%+v err=%v", size, err)
	}
}

func TestCloudDirectorySizeUsesIndexedPathRange(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.db.Query(`EXPLAIN QUERY PLAN `+cloudInventoryFileAggregateSQL, "acct", "/nested/", "/nested0")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(details, " | ")
	if !strings.Contains(plan, "cloud_inventory_path") || !strings.Contains(plan, "path>? AND path<?") {
		t.Fatalf("directory size query does not seek the path range index: %s", plan)
	}
}

func TestCloudDirectoryMetaAggregateUsesIndexedPathRange(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prefix, upper := cloudDirectoryPathRange("/nested")
	rows, err := store.db.Query(`EXPLAIN QUERY PLAN `+cloudInventoryDirectoryAggregateSQL, "/nested", "/nested", "acct", "/nested", prefix, upper)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(details, " | ")
	if !strings.Contains(plan, "cloud_inventory_directories") || !strings.Contains(plan, "path>? AND path<?") || strings.Contains(plan, "SCAN cloud_inventory_directories") {
		t.Fatalf("directory metadata aggregate does not use a bounded path index: %s", plan)
	}
}

func TestCloudDirectorySizeRangeExcludesSiblingPrefix(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for _, page := range []struct {
		path    string
		entries []CloudInventoryEntry
	}{
		{path: "/", entries: []CloudInventoryEntry{{FsID: 1, Path: "/docs", Name: "docs", Directory: true}, {FsID: 2, Path: "/docs0", Name: "docs0", Directory: true}}},
		{path: "/docs", entries: []CloudInventoryEntry{{FsID: 3, Path: "/docs/a.bin", Name: "a.bin", Size: 3}}},
		{path: "/docs0", entries: []CloudInventoryEntry{{FsID: 4, Path: "/docs0/b.bin", Name: "b.bin", Size: 5}}},
	} {
		if err = store.UpsertCloudInventoryPage("acct", "run", page.path, page.entries, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	size, err := store.CloudDirectorySize("acct", "/docs")
	if err != nil || !size.Complete || size.Bytes != 3 || size.Files != 1 {
		t.Fatalf("sibling prefix leaked into directory size: %+v err=%v", size, err)
	}
	if err = store.InvalidateCloudInventory("acct", []string{"/docs"}); err != nil {
		t.Fatal(err)
	}
	sibling, err := store.CloudDirectorySize("acct", "/docs0")
	if err != nil || !sibling.Complete || sibling.Bytes != 5 {
		t.Fatalf("invalidating /docs changed sibling /docs0: %+v err=%v", sibling, err)
	}
}

func TestCloudInventoryFileSizeCountsTrackUpsertsAndDeletes(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	initial := []CloudInventoryEntry{
		{FsID: 21, Path: "/one.bin", Name: "one.bin", Size: 7},
		{FsID: 22, Path: "/two.bin", Name: "two.bin", Size: 7},
		{FsID: 23, Path: "/folder", Name: "folder", Directory: true, Size: 7},
	}
	if err = store.UpsertCloudInventoryPage("acct", "first", "/", initial, now, nil); err != nil {
		t.Fatal(err)
	}
	countSize := func(account string, size int64) int {
		t.Helper()
		var count int
		if queryErr := store.db.QueryRow(`SELECT file_count FROM cloud_inventory_file_sizes WHERE account_fingerprint=? AND size_bytes=?`, account, size).Scan(&count); queryErr != nil {
			t.Fatal(queryErr)
		}
		return count
	}
	if got := countSize("acct", 7); got != 2 {
		t.Fatalf("initial file size count=%d, want 2 (directories excluded)", got)
	}
	if err = store.UpsertCloudInventoryPage("acct", "second", "/", []CloudInventoryEntry{{FsID: 21, Path: "/one.bin", Name: "one.bin", Size: 9, ProviderMtime: 1}, initial[2]}, now.Add(time.Second), nil); err != nil {
		t.Fatal(err)
	}
	var oldCount, newCount int
	if err = store.db.QueryRow(`SELECT COALESCE((SELECT file_count FROM cloud_inventory_file_sizes WHERE account_fingerprint='acct' AND size_bytes=7),0),COALESCE((SELECT file_count FROM cloud_inventory_file_sizes WHERE account_fingerprint='acct' AND size_bytes=9),0)`).Scan(&oldCount, &newCount); err != nil {
		t.Fatal(err)
	}
	if oldCount != 0 || newCount != 1 {
		t.Fatalf("updated/deleted size counts old=%d new=%d, want 0/1", oldCount, newCount)
	}
	if err = store.UpsertCloudInventoryPage("acct", "second", "/", []CloudInventoryEntry{{FsID: 23, Path: "/folder", Name: "folder", Size: 7}}, now.Add(2*time.Second), nil); err != nil {
		t.Fatal(err)
	}
	if got := countSize("acct", 7); got != 1 {
		t.Fatalf("directory-to-file size count=%d, want 1", got)
	}
	if err = store.UpsertCloudInventoryPage("acct", "second", "/", []CloudInventoryEntry{{FsID: 23, Path: "/folder", Name: "folder", Directory: true, Size: 7}}, now.Add(3*time.Second), nil); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_file_sizes WHERE account_fingerprint='acct' AND size_bytes=7`).Scan(&oldCount); err != nil || oldCount != 0 {
		t.Fatalf("file-to-directory transition left size count=%d err=%v", oldCount, err)
	}
	if err = store.UpsertCloudInventoryPage("other", "first", "/", []CloudInventoryEntry{{FsID: 31, Path: "/other.bin", Name: "other.bin", Size: 7}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if got := countSize("other", 7); got != 1 {
		t.Fatalf("second account size count=%d, want 1", got)
	}
}

func TestCompleteEmptyDirectoryCandidatesIncludeEmptyAncestorsLeafFirst(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	pages := []struct {
		path    string
		entries []CloudInventoryEntry
	}{
		{path: "/", entries: []CloudInventoryEntry{{FsID: 1, Path: "/a", Name: "a", Directory: true}, {FsID: 4, Path: "/c", Name: "c", Directory: true}}},
		{path: "/", entries: []CloudInventoryEntry{{FsID: 6, Path: "/a0", Name: "a0", Directory: true}}},
		{path: "/a", entries: []CloudInventoryEntry{{FsID: 2, Path: "/a/b", Name: "b", Directory: true}}},
		{path: "/a/b"},
		{path: "/a0", entries: []CloudInventoryEntry{{FsID: 7, Path: "/a0/file", Name: "file", Size: 10}}},
		{path: "/c", entries: []CloudInventoryEntry{{FsID: 5, Path: "/c/file", Name: "file", Size: 10}}},
	}
	for _, page := range pages {
		if err = store.UpsertCloudInventoryPage("acct", "run", page.path, page.entries, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.ListCompleteEmptyCloudDirectories("acct")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Path != "/a/b" || items[1].Path != "/a" {
		t.Fatalf("empty directory candidates not leaf-first or included non-empty subtree: %+v", items)
	}
}

func TestCompleteEmptyDirectoryCandidatesRejectIncompleteOrUntrackedDescendants(t *testing.T) {
	for _, missingMetadata := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing-metadata-%t", missingMetadata), func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			now := time.Now().UTC()
			for _, page := range []struct {
				path    string
				entries []CloudInventoryEntry
			}{
				{path: "/", entries: []CloudInventoryEntry{{FsID: 11, Path: "/a", Name: "a", Directory: true}}},
				{path: "/a", entries: []CloudInventoryEntry{{FsID: 12, Path: "/a/b", Name: "b", Directory: true}}},
				{path: "/a/b"},
			} {
				if err = store.UpsertCloudInventoryPage("acct", "run", page.path, page.entries, now, nil); err != nil {
					t.Fatal(err)
				}
			}
			if missingMetadata {
				if _, err = store.db.Exec(`DELETE FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/a/b'`); err != nil {
					t.Fatal(err)
				}
			} else if err = store.InvalidateCloudInventory("acct", []string{"/a/b"}); err != nil {
				t.Fatal(err)
			}
			items, err := store.ListCompleteEmptyCloudDirectories("acct")
			if err != nil || len(items) != 0 {
				t.Fatalf("incomplete descendant produced empty candidates: %+v err=%v", items, err)
			}
		})
	}
}

func TestCompleteEmptyDirectoryQueryUsesIndexedSubtreeRanges(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.db.Query(`EXPLAIN QUERY PLAN `+listCompleteEmptyCloudDirectoriesSQL, "acct")
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
	if strings.Contains(queryPlan, "CO-ROUTINE subtree") || !strings.Contains(queryPlan, "cloud_inventory_path") {
		t.Fatalf("empty directory query did not use indexed subtree ranges: %s", queryPlan)
	}
}

func TestCloudScanCursorPersistsForRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run", Status: "running", Phase: "listing", CurrentDirectory: "/", NextOffset: 100, PagesProcessed: 2, ItemsProcessed: 200, StartedAtUTC: now.Add(-time.Minute), UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES('acct','run',1,'/next'); UPDATE cloud_inventory_scan_state SET pending_count=2 WHERE account_fingerprint='acct'`); err != nil {
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
	got, err := store.LoadCloudScanState("acct")
	if err != nil || got.RunID != "run" || got.NextOffset != 100 || got.PagesProcessed != 2 || got.PendingCount != 2 || !got.UpdatedAtUTC.Equal(now.Truncate(time.Microsecond)) {
		t.Fatalf("scan cursor=%+v err=%v", got, err)
	}
	if head, headErr := store.CloudScanQueueHead("acct", "run"); headErr != nil || head != "/" {
		t.Fatalf("scan queue head=%q err=%v", head, headErr)
	}
}

func TestCloudScanStatusReadsPendingCountFromSingleStateRow(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.db.Query(`EXPLAIN QUERY PLAN `+loadCloudScanStateSQL, "acct")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(details, " | ")
	if !strings.Contains(plan, "SEARCH cloud_inventory_scan_state") || strings.Contains(plan, "cloud_inventory_scan_queue") || strings.Contains(plan, "SCAN cloud_inventory_scan_state") {
		t.Fatalf("scan status query is not a state-row lookup: %s", plan)
	}
}

func TestCloudScanFinalPageCommitsQueueAndProgressAtomically(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "acct", RunID: "scan", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	entries := []CloudInventoryEntry{{FsID: 1, Path: "/child", Name: "child", Directory: true}, {FsID: 2, Path: "/file.bin", Name: "file.bin", Size: 12}}
	if err = store.UpsertCloudInventoryScanPage(&state, "/", entries, now, nil, []string{"/child"}); err != nil {
		t.Fatal(err)
	}
	if state.CurrentDirectory != "/child" || state.PendingCount != 1 || state.PagesProcessed != 1 || state.ItemsProcessed != 2 || state.Status != "running" {
		t.Fatalf("state after root page=%+v", state)
	}
	if head, headErr := store.CloudScanQueueHead("acct", "scan"); headErr != nil || head != "/child" {
		t.Fatalf("next queue head=%q err=%v", head, headErr)
	}
	if err = store.UpsertCloudInventoryScanPage(&state, "/child", nil, now.Add(time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	if state.Status != "complete" || state.PendingCount != 0 || state.PagesProcessed != 2 || state.ItemsProcessed != 2 {
		t.Fatalf("state after final page=%+v", state)
	}
}

func TestCloudScanQueueEnqueueDeduplicatesAndPreservesSequence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "acct", RunID: "scan", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	entries := make([]CloudInventoryEntry, 101)
	children := make([]string, len(entries)+1)
	for i := range entries {
		name := fmt.Sprintf("child-%03d", i)
		children[i] = "/" + name
		entries[i] = CloudInventoryEntry{FsID: int64(i + 1), Path: children[i], Name: name, Directory: true}
	}
	children[len(entries)] = children[0]
	if err = store.UpsertCloudInventoryScanPage(&state, "/", entries, now, nil, children); err != nil {
		t.Fatal(err)
	}
	if state.PendingCount != int64(len(entries)) || state.CurrentDirectory != children[0] {
		t.Fatalf("scan state after chunked enqueue=%+v", state)
	}
	var count, maxSequence int64
	if err = store.db.QueryRow(`SELECT count(*),max(sequence) FROM cloud_inventory_scan_queue WHERE account_fingerprint='acct' AND run_id='scan'`).Scan(&count, &maxSequence); err != nil || count != int64(len(entries)) || maxSequence != int64(len(entries)-1) {
		t.Fatalf("queued children count=%d max sequence=%d err=%v", count, maxSequence, err)
	}
}

func TestCloudScanQueueAndInventoryRollbackTogetherOnEnqueueFailure(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "acct", RunID: "scan", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER reject_cloud_scan_child BEFORE INSERT ON cloud_inventory_scan_queue WHEN NEW.path='/blocked' BEGIN SELECT RAISE(ABORT,'test enqueue failure'); END`); err != nil {
		t.Fatal(err)
	}
	entries := []CloudInventoryEntry{{FsID: 1, Path: "/blocked", Name: "blocked", Directory: true}}
	if err = store.UpsertCloudInventoryScanPage(&state, "/", entries, now, nil, []string{"/blocked"}); err == nil {
		t.Fatal("expected child queue insert failure")
	}
	if state.PagesProcessed != 0 || state.ItemsProcessed != 0 || state.CurrentDirectory != "/" || state.PendingCount != 1 {
		t.Fatalf("in-memory progress advanced after rollback: %+v", state)
	}
	var inventoryCount int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct'`).Scan(&inventoryCount); err != nil || inventoryCount != 0 {
		t.Fatalf("inventory count after rollback=%d err=%v", inventoryCount, err)
	}
	if head, headErr := store.CloudScanQueueHead("acct", "scan"); headErr != nil || head != "/" {
		t.Fatalf("queue head after rollback=%q err=%v", head, headErr)
	}
}

func TestCloudScanPendingCountMismatchCannotCompleteScan(t *testing.T) {
	for _, pendingCount := range []int64{0, 2} {
		t.Run(fmt.Sprintf("count_%d", pendingCount), func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			now := time.Now().UTC()
			state := CloudScanState{AccountFingerprint: "acct", RunID: "scan", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
			if err = store.StartCloudScan(&state); err != nil {
				t.Fatal(err)
			}
			state.PendingCount = pendingCount
			if _, err = store.db.Exec(`UPDATE cloud_inventory_scan_state SET pending_count=? WHERE account_fingerprint='acct'`, pendingCount); err != nil {
				t.Fatal(err)
			}
			if err = store.UpsertCloudInventoryScanPage(&state, "/", nil, now, nil, nil); err == nil {
				t.Fatal("accepted inconsistent persisted queue count")
			}
			if state.Status == "complete" || state.PagesProcessed != 0 {
				t.Fatalf("scan advanced despite count mismatch: %+v", state)
			}
			if head, headErr := store.CloudScanQueueHead("acct", "scan"); headErr != nil || head != "/" {
				t.Fatalf("queue head after rejected page=%q err=%v", head, headErr)
			}
		})
	}
}

func TestCloudScanStatusCountsLargeQueueWithoutReturningPaths(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "acct", RunID: "scan", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES('acct','scan',?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10000; i++ {
		if _, err = stmt.Exec(i, fmt.Sprintf("/directory-%05d", i)); err != nil {
			stmt.Close()
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(`UPDATE cloud_inventory_scan_state SET pending_count=10001 WHERE account_fingerprint='acct'`); err != nil {
		stmt.Close()
		tx.Rollback()
		t.Fatal(err)
	}
	if err = stmt.Close(); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadCloudScanState("acct")
	if err != nil || got.PendingCount != 10001 {
		t.Fatalf("large scan queue count=%d err=%v", got.PendingCount, err)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 1024 || strings.Contains(string(body), "/directory-") {
		t.Fatalf("scan status response contains the worklist or is unexpectedly large: %d bytes", len(body))
	}
}
