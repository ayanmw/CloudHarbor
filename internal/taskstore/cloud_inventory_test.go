package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInventoryRetrySchedulePersistsAcrossStoreReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	retryAt := now.Add(47 * time.Second)
	full := CloudScanState{AccountFingerprint: "acct", RunID: "full-run", Status: "running", Phase: "backoff", CurrentDirectory: "/", StartedAtUTC: now.Add(-time.Minute), UpdatedAtUTC: now, LastError: "rate limited", RetryDelayMillis: 47000, RetryBackoffBaseMillis: 60000, RetryAtUTC: &retryAt}
	if err = store.SaveCloudScanCheckpoint(full); err != nil {
		t.Fatal(err)
	}
	refresh := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "refresh-run", RootPath: "/root", Status: "running", Phase: "backoff", CurrentDirectory: "/root", StartedAtUTC: now.Add(-time.Minute), UpdatedAtUTC: now, LastError: "rate limited", RetryDelayMillis: 47000, RetryBackoffBaseMillis: 60000, RetryAtUTC: &retryAt}
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
	defer store.Close()
	gotFull, err := store.LoadCloudScanState("acct")
	if err != nil || gotFull.RetryAtUTC == nil || !gotFull.RetryAtUTC.Equal(retryAt) || gotFull.RetryDelayMillis != 47000 || gotFull.RetryBackoffBaseMillis != 60000 || gotFull.LastError != "rate limited" {
		t.Fatalf("full scan retry schedule=%+v err=%v", gotFull, err)
	}
	gotRefresh, err := store.LoadCloudInventoryRefreshState("acct")
	if err != nil || gotRefresh.RetryAtUTC == nil || !gotRefresh.RetryAtUTC.Equal(retryAt) || gotRefresh.RetryDelayMillis != 47000 || gotRefresh.RetryBackoffBaseMillis != 60000 || gotRefresh.LastError != "rate limited" {
		t.Fatalf("refresh retry schedule=%+v err=%v", gotRefresh, err)
	}
}

func TestCloudInventoryRefreshRequestBudgetPersistsAndResumeExtensionIsAtomic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "budget-run", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now, RequestBudget: 2}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	for want := int64(1); want <= 2; want++ {
		got, reserveErr := store.ReserveCloudInventoryRefreshRequest("acct", "budget-run", now.Add(time.Duration(want)*time.Second))
		if reserveErr != nil || got != want {
			t.Fatalf("reserved request count=%d want=%d err=%v", got, want, reserveErr)
		}
	}
	if _, err = store.ReserveCloudInventoryRefreshRequest("acct", "budget-run", now.Add(3*time.Second)); !errors.Is(err, ErrCloudInventoryRefreshRequestBudgetReached) {
		t.Fatalf("request above budget was not rejected: %v", err)
	}
	state, err = store.LoadCloudInventoryRefreshState("acct")
	if err != nil || state.RequestBudget != 2 || state.RequestsUsed != 2 {
		t.Fatalf("budget state before reopen=%+v err=%v", state, err)
	}
	state.Status, state.Phase = "paused", "request_budget_reached"
	state.UpdatedAtUTC = now.Add(4 * time.Second)
	if err = store.SaveCloudInventoryRefreshState(state); err != nil {
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
	state, err = store.LoadCloudInventoryRefreshState("acct")
	if err != nil || state.RequestBudget != 2 || state.RequestsUsed != 2 || state.Phase != "request_budget_reached" {
		t.Fatalf("budget checkpoint after reopen=%+v err=%v", state, err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER reject_budget_resume BEFORE INSERT ON cloud_inventory_refresh_events WHEN NEW.event_kind='resumed' BEGIN SELECT RAISE(ABORT,'injected resume event failure'); END`); err != nil {
		t.Fatal(err)
	}
	state.RequestBudget += 1
	state.Status, state.Phase = "running", "listing"
	state.UpdatedAtUTC = now.Add(5 * time.Second)
	if err = store.ResumeCloudInventoryRefreshState(state); err == nil {
		t.Fatal("budget extension succeeded despite rejected resume event")
	}
	state, err = store.LoadCloudInventoryRefreshState("acct")
	if err != nil || state.RequestBudget != 2 || state.RequestsUsed != 2 || state.Status != "paused" {
		t.Fatalf("failed budget extension partially changed state=%+v err=%v", state, err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER reject_budget_resume`); err != nil {
		t.Fatal(err)
	}
	state.RequestBudget += 1
	state.Status, state.Phase = "running", "listing"
	state.UpdatedAtUTC = now.Add(6 * time.Second)
	if err = store.ResumeCloudInventoryRefreshState(state); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", "budget-run", "", 0, 10)
	if err != nil || len(events) != 3 || events[0].Kind != "resumed" || events[0].RequestBudget != 3 || events[0].RequestsUsed != 2 {
		t.Fatalf("resumed budget evidence=%+v err=%v", events, err)
	}
}

func TestCloudScanRequestBudgetPersistsAndStopsReservations(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	state := CloudScanState{AccountFingerprint: "acct", RunID: "full-budget-run", CurrentDirectory: "/", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now, RequestBudget: 2}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	for want := int64(1); want <= 2; want++ {
		got, reserveErr := store.ReserveCloudScanRequest("acct", "full-budget-run", now.Add(time.Duration(want)*time.Second))
		if reserveErr != nil || got != want {
			t.Fatalf("reserved=%d want=%d err=%v", got, want, reserveErr)
		}
	}
	if _, err = store.ReserveCloudScanRequest("acct", "full-budget-run", now.Add(3*time.Second)); !errors.Is(err, ErrCloudInventoryScanRequestBudgetReached) {
		t.Fatalf("third reservation error=%v", err)
	}
	state, err = store.LoadCloudScanState("acct")
	if err != nil || state.RequestBudget != 2 || state.RequestsUsed != 2 {
		t.Fatalf("saved budget state=%+v err=%v", state, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, err = store.LoadCloudScanState("acct")
	if err != nil || state.RequestBudget != 2 || state.RequestsUsed != 2 {
		t.Fatalf("reopened budget state=%+v err=%v", state, err)
	}
}

func TestSuccessfulInventoryPagesClearStaleRetryEvidence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	retryAt := now.Add(time.Minute)
	full := CloudScanState{AccountFingerprint: "acct", RunID: "full", Status: "running", Phase: "backoff", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now, LastError: "rate limited", RetryDelayMillis: 60000, RetryAtUTC: &retryAt}
	if err = store.StartCloudScan(&full); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanCheckpoint(full); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryScanPage(&full, "/", nil, now.Add(time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	if full.LastError != "" || full.RetryDelayMillis != 0 || full.RetryAtUTC != nil {
		t.Fatalf("successful full-scan page retained retry evidence: %+v", full)
	}
	refresh := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "refresh", RootPath: "/root", Status: "running", Phase: "backoff", CurrentDirectory: "/root", StartedAtUTC: now, UpdatedAtUTC: now, LastError: "rate limited", RetryDelayMillis: 60000, RetryAtUTC: &retryAt}
	if err = store.StartCloudInventoryRefresh(&refresh); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudInventoryRefreshState(refresh); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryRefreshPage(&refresh, "/root", nil, now.Add(2*time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	if refresh.LastError != "" || refresh.RetryDelayMillis != 0 || refresh.RetryAtUTC != nil {
		t.Fatalf("successful refresh page retained retry evidence: %+v", refresh)
	}
}

func BenchmarkUpsertCloudInventoryPage100(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "queue"), "/queue")
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = store.Close() }()
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

func TestSearchCloudInventory(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 1, 2, 3, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct-a", "run-a", "/", []CloudInventoryEntry{
		{FsID: 1, Path: "/报告", Name: "报告", Directory: true},
		{FsID: 2, Path: "/root%_report.txt", Name: "root%_report.txt", Size: 11},
	}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct-a", "run-a", "/报告", []CloudInventoryEntry{
		{FsID: 3, Path: "/报告/AI报告.pdf", Name: "AI报告.pdf", Size: 17},
		{FsID: 4, Path: "/报告/archive", Name: "archive", Directory: true},
	}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct-a", "run-a", "/报告/archive", []CloudInventoryEntry{
		{FsID: 5, Path: "/报告/archive/AI报告-old.pdf", Name: "AI报告-old.pdf", Size: 19},
	}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct-b", "run-b", "/", []CloudInventoryEntry{
		{FsID: 1, Path: "/AI报告-private.pdf", Name: "AI报告-private.pdf", Size: 99},
	}, observed, nil); err != nil {
		t.Fatal(err)
	}
	state := CloudScanState{AccountFingerprint: "acct-a", RunID: "run-a", Status: "complete", Phase: "complete", CurrentDirectory: "/", StartedAtUTC: observed, UpdatedAtUTC: observed}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState("acct-a", "run-a", "complete", "complete", "/", 0, 3, 5, observed, observed, ""); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState("acct-b", "run-b", "complete", "complete", "/", 0, 1, 1, observed, observed, ""); err != nil {
		t.Fatal(err)
	}

	entries, total, coverage, err := store.SearchCloudInventory("acct-a", "AI报告", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(entries) != 1 || entries[0].Path != "/报告/AI报告.pdf" || coverage.Status != "complete" || coverage.IncompleteDirectories != 0 || coverage.ScanUpdatedAtUTC == nil || !coverage.ScanUpdatedAtUTC.Equal(observed) {
		t.Fatalf("search page=%+v total=%d coverage=%+v", entries, total, coverage)
	}
	entries, total, _, err = store.SearchCloudInventory("acct-a", "AI报告", 1, 1)
	if err != nil || total != 2 || len(entries) != 1 || entries[0].Path != "/报告/archive/AI报告-old.pdf" {
		t.Fatalf("second page=%+v total=%d err=%v", entries, total, err)
	}
	entries, total, _, err = store.SearchCloudInventory("acct-a", "报告", 0, 10)
	if err != nil || total != 4 {
		t.Fatalf("path substring results=%+v total=%d err=%v", entries, total, err)
	}
	var foundSizedDirectory bool
	for _, entry := range entries {
		if entry.Path == "/报告" && entry.DirectorySizeBytes != nil && *entry.DirectorySizeBytes == 36 && entry.DirectorySizeComplete != nil && *entry.DirectorySizeComplete {
			foundSizedDirectory = true
		}
	}
	if !foundSizedDirectory {
		t.Fatalf("directory search result missing complete local aggregate: %+v", entries)
	}
	entries, total, _, err = store.SearchCloudInventory("acct-a", "%_", 0, 10)
	if err != nil || total != 1 || len(entries) != 1 || entries[0].Path != "/root%_report.txt" {
		t.Fatalf("wildcards were not treated literally: entries=%+v total=%d err=%v", entries, total, err)
	}
	entries, total, _, err = store.SearchCloudInventory("acct-a", "PRIVATE", 0, 10)
	if err != nil || total != 0 || len(entries) != 0 {
		t.Fatalf("search leaked a different account: entries=%+v total=%d err=%v", entries, total, err)
	}
	entries, total, _, err = store.SearchCloudInventory("acct-b", "PRIVATE", 0, 10)
	if err != nil || total != 1 || len(entries) != 1 || entries[0].Path != "/AI报告-private.pdf" {
		t.Fatalf("account-scoped result missing: entries=%+v total=%d err=%v", entries, total, err)
	}

	if _, err = store.db.Exec(`UPDATE cloud_inventory_directories SET status='stale' WHERE account_fingerprint='acct-a' AND path='/报告'`); err != nil {
		t.Fatal(err)
	}
	_, _, coverage, err = store.SearchCloudInventory("acct-a", "not-present", 0, 10)
	if err != nil || coverage.Status != "incomplete" || coverage.IncompleteDirectories != 1 {
		t.Fatalf("stale cache coverage=%+v err=%v", coverage, err)
	}
	if err = store.SaveCloudScanState("acct-a", "run-a", "running", "listing", "/报告", 0, 3, 4, observed, observed.Add(time.Minute), ""); err != nil {
		t.Fatal(err)
	}
	_, _, coverage, err = store.SearchCloudInventory("acct-a", "not-present", 0, 10)
	if err != nil || coverage.Status != "scanning" {
		t.Fatalf("running cache coverage=%+v err=%v", coverage, err)
	}
}

func TestSearchCloudInventoryRejectsMissingDirectoryCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", []CloudInventoryEntry{{FsID: 1, Path: "/child", Name: "child", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run", "/child", []CloudInventoryEntry{{FsID: 2, Path: "/child/file.bin", Name: "file.bin", Size: 4}}, now, nil); err != nil {
		t.Fatal(err)
	}
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run", Status: "complete", Phase: "complete", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState("acct", "run", "complete", "complete", "/", 0, 2, 2, now, now, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DELETE FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/child'`); err != nil {
		t.Fatal(err)
	}
	_, _, coverage, err := store.SearchCloudInventory("acct", "not-present", 0, 10)
	if err != nil || coverage.Status != "incomplete" || coverage.IncompleteDirectories == 0 {
		t.Fatalf("missing directory coverage was reported complete: %+v err=%v", coverage, err)
	}
}

func TestSearchCloudInventoryRejectsFileWithoutParentCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", nil, now, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES('acct',1,'/missing','/missing/file.bin','file.bin',0,7,0,'',?,'old-run')`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run", Status: "complete", Phase: "complete", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState("acct", "run", "complete", "complete", "/", 0, 1, 0, now, now, ""); err != nil {
		t.Fatal(err)
	}
	_, _, coverage, err := store.SearchCloudInventory("acct", "not-present", 0, 10)
	if err != nil || coverage.Status != "incomplete" || coverage.IncompleteDirectories == 0 {
		t.Fatalf("orphan file parent was reported as complete: %+v err=%v", coverage, err)
	}
}

func TestSearchCloudInventoryRequiresCompleteRootCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run", Status: "complete", Phase: "complete", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState("acct", "run", "complete", "complete", "/", 0, 1, 0, now, now, ""); err != nil {
		t.Fatal(err)
	}
	_, _, coverage, err := store.SearchCloudInventory("acct", "not-present", 0, 10)
	if err != nil || coverage.Status != "incomplete" || coverage.IncompleteDirectories == 0 {
		t.Fatalf("search reported complete without a root coverage record: %+v err=%v", coverage, err)
	}
}

func TestCloudInventoryScanOverviewSeparatesRunStatusFromCurrentCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", []CloudInventoryEntry{{FsID: 1, Path: "/folder", Name: "folder", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run", "/folder", nil, now, nil); err != nil {
		t.Fatal(err)
	}
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run", Status: "complete", Phase: "complete", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState("acct", "run", "complete", "complete", "/", 0, 2, 1, now, now, ""); err != nil {
		t.Fatal(err)
	}
	got, coverage, err := store.LoadCloudInventoryScanOverview("acct")
	if err != nil || got.Status != "complete" || coverage.Status != "complete" || coverage.IncompleteDirectories != 0 || coverage.Generation == 0 || coverage.ScanUpdatedAtUTC == nil || !coverage.ScanUpdatedAtUTC.Equal(now) {
		t.Fatalf("complete overview: state=%+v coverage=%+v err=%v", got, coverage, err)
	}
	if err = store.InvalidateCloudInventory("acct", []string{"/folder"}); err != nil {
		t.Fatal(err)
	}
	got, coverage, err = store.LoadCloudInventoryScanOverview("acct")
	if err != nil || got.Status != "complete" || coverage.Status != "incomplete" || coverage.IncompleteDirectories != 1 {
		t.Fatalf("run completion must not imply current cache completeness: state=%+v coverage=%+v err=%v", got, coverage, err)
	}
	if got, coverage, err = store.LoadCloudInventoryScanOverview("other-account"); err != nil || got.Status != "" || coverage.Status != "unknown" || coverage.IncompleteDirectories != 1 || coverage.Generation != 0 {
		t.Fatalf("account without scan must remain unknown and isolated: state=%+v coverage=%+v err=%v", got, coverage, err)
	}
}

func TestCloudInventoryScanOverviewExplainsIncompleteCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", []CloudInventoryEntry{
		{FsID: 1, Path: "/stale", Name: "stale", Directory: true},
		{FsID: 2, Path: "/unknown", Name: "unknown", Directory: true},
		{FsID: 3, Path: "/failed", Name: "failed", Directory: true},
		{FsID: 6, Path: "/missing", Name: "missing", Directory: true},
	}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run", "/orphan-parent", []CloudInventoryEntry{{FsID: 4, Path: "/orphan-parent/file", Name: "file", Size: 1}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run", "/wrong", []CloudInventoryEntry{{FsID: 5, Path: "/wrong/actual", Name: "actual", Size: 1}}, now, nil); err != nil {
		t.Fatal(err)
	}
	// Inject a damaged cached path directly; normal cache publication rejects this shape.
	if _, err = store.db.Exec(`UPDATE cloud_inventory SET path='/wrong/name' WHERE account_fingerprint='acct' AND fs_id=5`); err != nil {
		t.Fatal(err)
	}
	for p, status := range map[string]string{"/stale": "stale", "/unknown": "unknown", "/failed": "failed"} {
		if _, err = store.db.Exec(`UPDATE cloud_inventory_directories SET status=? WHERE account_fingerprint='acct' AND path=?`, status, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.Exec(`DELETE FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/orphan-parent'`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DELETE FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/missing'`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,last_run_id) VALUES('acct','/orphan-status','complete','fixture')`); err != nil {
		t.Fatal(err)
	}
	_, coverage, err := store.LoadCloudInventoryScanOverview("acct")
	if err != nil {
		t.Fatal(err)
	}
	if coverage.Reasons == nil {
		t.Fatalf("incomplete coverage reasons missing: %+v", coverage)
	}
	want := CloudInventoryCoverageReasons{StaleDirectories: 1, UnknownDirectories: 1, FailedDirectories: 1, MissingDirectoryStatus: 1, OrphanDirectoryStatus: 2, ParentNotComplete: 2, PathMismatch: 1}
	if *coverage.Reasons != want {
		t.Fatalf("reasons=%+v want=%+v", *coverage.Reasons, want)
	}
	_, isolated, err := store.LoadCloudInventoryScanOverview("other-account")
	if err != nil || isolated.Reasons == nil || *isolated.Reasons != (CloudInventoryCoverageReasons{MissingRoot: 1}) {
		t.Fatalf("account reasons leaked: %+v err=%v", isolated, err)
	}
}

func TestListCloudInventoryIncompleteDirectoriesFiltersPagesAndChecksGeneration(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", []CloudInventoryEntry{
		{FsID: 1, Path: "/alpha", Name: "alpha", Directory: true},
		{FsID: 2, Path: "/beta", Name: "beta", Directory: true},
	}, now, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_directories SET status='stale' WHERE account_fingerprint='acct' AND path='/alpha'`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_directories SET status='unknown',observed_at_utc=? WHERE account_fingerprint='acct' AND path='/beta'`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,last_run_id) VALUES('acct','/orphan','complete','run')`); err != nil {
		t.Fatal(err)
	}
	first, err := store.ListCloudInventoryIncompleteDirectories("acct", "", "stale", "alp", 1, nil)
	if err != nil || len(first.Entries) != 1 || first.Total != 1 || first.Entries[0].Path != "/alpha" || first.Entries[0].Status != "stale" || first.Entries[0].Reason != "stale" || first.Generation == 0 {
		t.Fatalf("first page=%+v err=%v", first, err)
	}
	observed, err := store.ListCloudInventoryIncompleteDirectories("acct", "", "unknown", "beta", 1, nil)
	if err != nil || len(observed.Entries) != 1 || observed.Entries[0].ObservedAtUTC == nil || !observed.Entries[0].ObservedAtUTC.Equal(now) {
		t.Fatalf("observation evidence=%+v err=%v", observed, err)
	}
	all, err := store.ListCloudInventoryIncompleteDirectories("acct", "", "", "", 1, nil)
	if err != nil || !all.HasMore || all.NextBeforePath == "" {
		t.Fatalf("expected more paths: %+v err=%v", all, err)
	}
	orphan, err := store.ListCloudInventoryIncompleteDirectories("acct", "", "orphan_status", "orphan", 10, nil)
	if err != nil || orphan.Total != 1 || len(orphan.Entries) != 1 || orphan.Entries[0].Reason != "orphan_status" || orphan.Entries[0].CachedDirectory {
		t.Fatalf("orphan status evidence=%+v err=%v", orphan, err)
	}
	next, err := store.ListCloudInventoryIncompleteDirectories("acct", all.NextBeforePath, "", "", 1, &all.Generation)
	if err != nil || len(next.Entries) != 1 || next.Entries[0].Path <= all.NextBeforePath || next.Generation != all.Generation {
		t.Fatalf("next page=%+v err=%v", next, err)
	}
	other, err := store.ListCloudInventoryIncompleteDirectories("other-account", "", "", "", 20, nil)
	if err != nil || other.Total != 1 || len(other.Entries) != 1 || other.Entries[0].Path != "/" || other.Entries[0].Reason != "root_incomplete" || other.Entries[0].CachedDirectory {
		t.Fatalf("other account leaked cache: %+v err=%v", other, err)
	}
	staleGeneration := all.Generation + 1
	if _, err = store.ListCloudInventoryIncompleteDirectories("acct", "", "", "", 1, &staleGeneration); !errors.Is(err, ErrCloudInventoryGenerationChanged) {
		t.Fatalf("stale generation err=%v", err)
	}
}

func TestSearchCloudInventoryRejectsInvalidInputAndUnknownCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, _, _, err = store.SearchCloudInventory("acct", "", 0, 10); err == nil {
		t.Fatal("empty query accepted")
	}
	if _, _, _, err = store.SearchCloudInventory("acct", strings.Repeat("界", 129), 0, 10); err == nil {
		t.Fatal("query over 128 Unicode characters accepted")
	}
	if _, _, _, err = store.SearchCloudInventory("acct", "x", -1, 10); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, _, _, err = store.SearchCloudInventory("acct", "x", 0, 101); err == nil {
		t.Fatal("limit above 100 accepted")
	}
	_, total, coverage, err := store.SearchCloudInventory("acct", "missing", 0, 10)
	if err != nil || total != 0 || coverage.Status != "unknown" {
		t.Fatalf("empty inventory coverage=%+v total=%d err=%v", coverage, total, err)
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

func TestCloudDirectorySizeCacheRefreshesObservationTimeWhenInventoryIsUnchanged(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	firstObserved := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	entries := []CloudInventoryEntry{{FsID: 1, Path: "/file.bin", Name: "file.bin", Size: 9}}
	if err = store.UpsertCloudInventoryPage("acct", "initial", "/", entries, firstObserved, nil); err != nil {
		t.Fatal(err)
	}
	first, err := store.CloudDirectorySize("acct", "/")
	if err != nil || !first.Complete || !first.ObservedAtUTC.Equal(firstObserved) {
		t.Fatalf("initial size evidence=%+v err=%v", first, err)
	}
	refreshObserved := firstObserved.Add(time.Minute)
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "unchanged-refresh", RootPath: "/", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: refreshObserved, UpdatedAtUTC: refreshObserved}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/", entries, refreshObserved, nil, nil); err != nil {
		t.Fatal(err)
	}
	updated, err := store.CloudDirectorySize("acct", "/")
	if err != nil || !updated.Complete || updated.Bytes != first.Bytes || !updated.ObservedAtUTC.Equal(refreshObserved) {
		t.Fatalf("unchanged content did not refresh cloud observation evidence: size=%+v err=%v want observed=%v", updated, err, refreshObserved)
	}
}

func TestCloudDirectorySizeRefreshObservationRespectsSubtreeMinimum(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	firstObserved := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	for _, page := range []struct {
		dir     string
		entries []CloudInventoryEntry
	}{
		{dir: "/", entries: []CloudInventoryEntry{{FsID: 1, Path: "/a", Name: "a", Directory: true}, {FsID: 4, Path: "/sibling", Name: "sibling", Directory: true}}},
		{dir: "/a", entries: []CloudInventoryEntry{{FsID: 2, Path: "/a/nested", Name: "nested", Directory: true}}},
		{dir: "/a/nested", entries: []CloudInventoryEntry{{FsID: 3, Path: "/a/nested/file.bin", Name: "file.bin", Size: 9}}},
		{dir: "/sibling", entries: []CloudInventoryEntry{{FsID: 5, Path: "/sibling/keep.bin", Name: "keep.bin", Size: 4}}},
	} {
		if err = store.UpsertCloudInventoryPage("acct", "seed", page.dir, page.entries, firstObserved, nil); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{"/", "/a", "/a/nested", "/sibling"}
	before := make(map[string]CloudDirectorySize, len(paths))
	for _, dir := range paths {
		before[dir], err = store.CloudDirectorySize("acct", dir)
		if err != nil || !before[dir].Complete || before[dir].ObservedAtUTC != firstObserved {
			t.Fatalf("initial directory size for %q=%+v err=%v", dir, before[dir], err)
		}
	}
	refreshedAt := firstObserved.Add(time.Minute)
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "refresh-nested", RootPath: "/a/nested", Status: "running", Phase: "listing", CurrentDirectory: "/a/nested", StartedAtUTC: refreshedAt, UpdatedAtUTC: refreshedAt}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/a/nested", []CloudInventoryEntry{{FsID: 3, Path: "/a/nested/file.bin", Name: "file.bin", Size: 9}}, refreshedAt, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"/", "/a", "/a/nested"} {
		updated, sizeErr := store.CloudDirectorySize("acct", dir)
		wantObserved := firstObserved
		if dir == "/a/nested" {
			wantObserved = refreshedAt
		}
		if sizeErr != nil || !updated.Complete || updated.ObservedAtUTC != wantObserved || updated.CachedAtUTC != before[dir].CachedAtUTC {
			t.Fatalf("subtree evidence for %q=%+v err=%v; want observed=%v and cached=%v", dir, updated, sizeErr, wantObserved, before[dir].CachedAtUTC)
		}
	}
	sibling, err := store.CloudDirectorySize("acct", "/sibling")
	if err != nil || !sibling.Complete || sibling.ObservedAtUTC != firstObserved || sibling.CachedAtUTC != before["/sibling"].CachedAtUTC {
		t.Fatalf("unrelated sibling cache changed: size=%+v err=%v", sibling, err)
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

func TestListCloudInventoryIncludesLocalDirectorySizeAndCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/", []CloudInventoryEntry{{FsID: 1, Path: "/folder", Name: "folder", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/folder", []CloudInventoryEntry{{FsID: 2, Path: "/folder/nested", Name: "nested", Directory: true}, {FsID: 4, Path: "/folder/raw.bin", Name: "raw.bin", Size: 9}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/folder/nested", []CloudInventoryEntry{{FsID: 3, Path: "/folder/nested/file.bin", Name: "file.bin", Size: 42}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	items, _, _, err := store.ListCloudInventory("acct", "/", 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("list cached root: items=%+v err=%v", items, err)
	}
	encoded, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result["directory_size_bytes"] != float64(51) || result["directory_size_complete"] != true {
		t.Fatalf("cached directory row lacks local aggregate evidence: %s", encoded)
	}
	childItems, _, _, err := store.ListCloudInventory("acct", "/folder", 0, 10)
	if err != nil || len(childItems) != 2 || childItems[1].Size != 9 || childItems[1].DirectorySizeBytes != nil {
		t.Fatalf("file size must retain its provider value without directory aggregates: items=%+v err=%v", childItems, err)
	}
	if err = store.InvalidateCloudInventory("acct", []string{"/folder/nested"}); err != nil {
		t.Fatal(err)
	}
	items, _, _, err = store.ListCloudInventory("acct", "/", 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("list stale directory: items=%+v err=%v", items, err)
	}
	encoded, err = json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	result = nil
	if err = json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result["directory_size_bytes"] != float64(51) || result["directory_size_complete"] != false {
		t.Fatalf("stale cached size should retain value but report incomplete coverage: %s", encoded)
	}
}

func TestCloudInventoryRefreshEventsPersistAppendOnlyAndPageByScope(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 30, 2, 3, 4, 0, time.UTC)
	observed := when.Add(-time.Minute)
	for _, event := range []CloudInventoryRefreshEvent{
		{AccountFingerprint: "acct-a", RunID: "run-a", Kind: "started", Status: "running", RecordedAtUTC: when},
		{AccountFingerprint: "acct-a", RunID: "run-a", Kind: "directory_published", Status: "running", CurrentDirectory: "/x", PagesProcessed: 2, ItemsProcessed: 5, PendingCount: 1, RecordedAtUTC: when.Add(time.Second), CloudObservedAtUTC: &observed, LastError: "temporary provider error"},
		{AccountFingerprint: "acct-b", RunID: "run-a", Kind: "started", Status: "running", RecordedAtUTC: when},
		{AccountFingerprint: "acct-a", RunID: "run-b", Kind: "started", Status: "running", RecordedAtUTC: when},
	} {
		tx, txErr := store.db.Begin()
		if txErr != nil {
			t.Fatal(txErr)
		}
		if txErr = appendCloudInventoryRefreshEvent(tx, event); txErr != nil {
			t.Fatal(txErr)
		}
		if txErr = tx.Commit(); txErr != nil {
			t.Fatal(txErr)
		}
	}
	page, err := store.ListCloudInventoryRefreshEvents("acct-a", "run-a", "", 0, 1)
	if err != nil || len(page) != 1 || page[0].Kind != "directory_published" || page[0].CurrentDirectory != "/x" || page[0].CloudObservedAtUTC == nil || !page[0].CloudObservedAtUTC.Equal(observed) || page[0].LastError != "temporary provider error" {
		t.Fatalf("latest scoped page=%+v err=%v", page, err)
	}
	firstSequence := page[0].Sequence
	older, err := store.ListCloudInventoryRefreshEvents("acct-a", "run-a", "", firstSequence, 10)
	if err != nil || len(older) != 1 || older[0].Kind != "started" || older[0].Sequence >= firstSequence {
		t.Fatalf("older page=%+v err=%v", older, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	all, err := store.ListCloudInventoryRefreshEvents("acct-a", "run-a", "", 0, 10)
	if err != nil || len(all) != 2 || !all[0].RecordedAtUTC.Equal(when.Add(time.Second)) {
		t.Fatalf("events after reopen=%+v err=%v", all, err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory_refresh_events SET kind='changed' WHERE sequence=?`, firstSequence); err == nil {
		t.Fatal("refresh history allowed update")
	}
	if _, err = store.db.Exec(`DELETE FROM cloud_inventory_refresh_events WHERE sequence=?`, firstSequence); err == nil {
		t.Fatal("refresh history allowed delete")
	}
}

func TestCloudInventoryRefreshEventsFilterByKindBeforeStablePaging(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	when := time.Date(2026, 10, 30, 4, 5, 6, 0, time.UTC)
	for _, event := range []CloudInventoryRefreshEvent{
		{AccountFingerprint: "acct-a", RunID: "run-a", Kind: "started", Status: "running", RecordedAtUTC: when},
		{AccountFingerprint: "acct-a", RunID: "run-a", Kind: "directory_published", Status: "running", RecordedAtUTC: when.Add(time.Second)},
		{AccountFingerprint: "acct-a", RunID: "run-a", Kind: "paused", Status: "paused", RecordedAtUTC: when.Add(2 * time.Second)},
		{AccountFingerprint: "acct-a", RunID: "run-a", Kind: "directory_published", Status: "running", RecordedAtUTC: when.Add(3 * time.Second)},
		{AccountFingerprint: "acct-a", RunID: "run-a", Kind: "completed", Status: "complete", RecordedAtUTC: when.Add(4 * time.Second)},
		{AccountFingerprint: "acct-a", RunID: "run-b", Kind: "directory_published", Status: "running", RecordedAtUTC: when.Add(5 * time.Second)},
		{AccountFingerprint: "acct-b", RunID: "run-a", Kind: "directory_published", Status: "running", RecordedAtUTC: when.Add(6 * time.Second)},
	} {
		tx, txErr := store.db.Begin()
		if txErr != nil {
			t.Fatal(txErr)
		}
		if txErr = appendCloudInventoryRefreshEvent(tx, event); txErr != nil {
			_ = tx.Rollback()
			t.Fatal(txErr)
		}
		if txErr = tx.Commit(); txErr != nil {
			t.Fatal(txErr)
		}
	}
	page, err := store.ListCloudInventoryRefreshEvents("acct-a", "run-a", "directory_published", 0, 1)
	if err != nil || len(page) != 1 || page[0].Sequence != 4 {
		t.Fatalf("latest filtered page=%+v err=%v", page, err)
	}
	older, err := store.ListCloudInventoryRefreshEvents("acct-a", "run-a", "directory_published", page[0].Sequence, 1)
	if err != nil || len(older) != 1 || older[0].Sequence != 2 {
		t.Fatalf("older filtered page=%+v err=%v", older, err)
	}
	empty, err := store.ListCloudInventoryRefreshEvents("acct-a", "run-a", "failed", 0, 20)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty filtered page=%+v err=%v", empty, err)
	}
	all, err := store.ListCloudInventoryRefreshEvents("acct-a", "run-a", "", 0, 20)
	if err != nil || len(all) != 5 || all[0].Kind != "completed" {
		t.Fatalf("all-event compatibility page=%+v err=%v", all, err)
	}
	if _, err = store.ListCloudInventoryRefreshEvents("acct-a", "run-a", "invented", 0, 20); err == nil {
		t.Fatal("invalid event kind accepted")
	}
}

func TestCloudInventoryRefreshPageOverlapReturnsTypedDuplicateFSID(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_refresh_entries(account_fingerprint,run_id,directory,fs_id,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc) VALUES('acct','run','/root',81,'/root/a.bin','a.bin',0,1,1,'',1)`); err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = rejectCloudInventoryRefreshPageOverlap(tx, "acct", "run", "/root", []CloudInventoryEntry{{FsID: 81, Path: "/root/a.bin", Name: "a.bin"}})
	_ = tx.Rollback()
	var duplicate *CloudInventoryDuplicateAcrossPagesError
	if !errors.Is(err, ErrCloudInventoryDuplicateAcrossPages) || !errors.As(err, &duplicate) || duplicate.FsID != 81 {
		t.Fatalf("overlap error=%v typed=%+v", err, duplicate)
	}
}

func TestCloudInventoryRefreshRunsPageByImmutableStartSequence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 30, 4, 5, 6, 0, time.UTC)
	for _, item := range []struct {
		account, run string
		status, kind string
		at           time.Time
	}{
		{"acct-a", "run-old", "running", "started", when},
		{"acct-b", "other-account", "running", "started", when.Add(time.Second)},
		{"acct-a", "run-old", "paused", "paused", when.Add(2 * time.Second)},
		{"acct-a", "run-new", "running", "started", when.Add(3 * time.Second)},
		{"acct-a", "run-old", "running", "resumed", when.Add(4 * time.Second)},
	} {
		tx, txErr := store.db.Begin()
		if txErr != nil {
			t.Fatal(txErr)
		}
		event := CloudInventoryRefreshEvent{AccountFingerprint: item.account, RunID: item.run, Kind: item.kind, Status: item.status, Phase: item.status, CurrentDirectory: "/root", PagesProcessed: int64(item.at.Sub(when) / time.Second), RecordedAtUTC: item.at}
		if txErr = appendCloudInventoryRefreshEvent(tx, event); txErr != nil {
			_ = tx.Rollback()
			t.Fatal(txErr)
		}
		if txErr = tx.Commit(); txErr != nil {
			t.Fatal(txErr)
		}
	}
	page, err := store.ListCloudInventoryRefreshRuns("acct-a", 0, 1)
	if err != nil || len(page) != 1 || page[0].RunID != "run-new" || page[0].LatestKind != "started" || page[0].StartSequence <= 0 {
		t.Fatalf("latest refresh run page=%+v err=%v", page, err)
	}
	newStartSequence := page[0].StartSequence
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = appendCloudInventoryRefreshEvent(tx, CloudInventoryRefreshEvent{AccountFingerprint: "acct-a", RunID: "run-after-page", Kind: "started", Status: "running", Phase: "listing", CurrentDirectory: "/root", PendingCount: 1, RecordedAtUTC: when.Add(5 * time.Second)}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	older, err := store.ListCloudInventoryRefreshRuns("acct-a", newStartSequence, 10)
	if err != nil || len(older) != 1 || older[0].RunID != "run-old" || older[0].LatestKind != "resumed" || older[0].Status != "running" || older[0].PagesProcessed != 4 || older[0].StartSequence >= newStartSequence {
		t.Fatalf("older run summary=%+v err=%v", older, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	all, err := store.ListCloudInventoryRefreshRuns("acct-a", 0, 10)
	if err != nil || len(all) != 3 || all[0].RunID != "run-after-page" || all[2].RunID != "run-old" || !all[2].StartedAtUTC.Equal(when) {
		t.Fatalf("refresh runs after reopen=%+v err=%v", all, err)
	}
}

func TestCloudInventoryRefreshRunSummaryTracksDurableRequestReservations(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	state := CloudInventoryRefreshState{AccountFingerprint: "acct-a", RunID: "refresh-budget-run", RootPath: "/root/scope", Status: "running", Phase: "listing", CurrentDirectory: "/root/scope", RequestBudget: 3, StartedAtUTC: started, UpdatedAtUTC: started}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if used, reserveErr := store.ReserveCloudInventoryRefreshRequest("acct-a", state.RunID, started.Add(time.Duration(i)*time.Second)); reserveErr != nil || used != int64(i) {
			t.Fatalf("reservation %d: used=%d err=%v", i, used, reserveErr)
		}
	}
	runs, err := store.ListCloudInventoryRefreshRuns("acct-a", 0, 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("run summary=%+v err=%v", runs, err)
	}
	if runs[0].RootPath != state.RootPath || runs[0].RequestBudget != 3 || runs[0].RequestsUsed != 2 || !runs[0].LastRecordedAtUTC.Equal(started.Add(2*time.Second)) {
		t.Fatalf("run summary missed durable reservation or scope: %+v", runs[0])
	}
	exported, err := store.ExportCloudInventoryRefreshRunSummaries(context.Background(), "acct-a", CloudInventoryRefreshRunFilter{})
	if err != nil || len(exported) != 1 || exported[0].RootPath != state.RootPath || exported[0].RequestsUsed != 2 || exported[0].RequestBudget != 3 {
		t.Fatalf("exported run summary=%+v err=%v", exported, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runs, err = store.ListCloudInventoryRefreshRuns("acct-a", 0, 10)
	if err != nil || len(runs) != 1 || runs[0].RequestsUsed != 2 || runs[0].RequestBudget != 3 || runs[0].RootPath != state.RootPath {
		t.Fatalf("run summary after reopen=%+v err=%v", runs, err)
	}
}

func TestCloudInventoryRefreshRequestReservationRollsBackWithRunSummaryFailure(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudInventoryRefreshState{AccountFingerprint: "acct-atomic", RunID: "run-atomic", RootPath: "/scope", CurrentDirectory: "/scope", Status: "running", Phase: "listing", RequestBudget: 1, StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fail_refresh_run_update BEFORE UPDATE ON cloud_inventory_refresh_runs BEGIN SELECT RAISE(ABORT,'injected run projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReserveCloudInventoryRefreshRequest(state.AccountFingerprint, state.RunID, now.Add(time.Second)); err == nil {
		t.Fatal("reservation succeeded despite projection update failure")
	}
	loaded, err := store.LoadCloudInventoryRefreshState(state.AccountFingerprint)
	if err != nil || loaded.RequestsUsed != 0 {
		t.Fatalf("failed reservation was not rolled back with state: %+v err=%v", loaded, err)
	}
}

func TestCloudInventoryRefreshRunsFilterByLatestStatusAndUTCStartRange(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	start := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		account, run, kind, status string
		at                         time.Time
	}{{"acct-a", "failed-run", "started", "running", start}, {"acct-a", "failed-run", "failed", "failed", start.Add(time.Minute)}, {"acct-a", "complete-run", "started", "running", start.Add(time.Hour)}, {"acct-a", "complete-run", "completed", "complete", start.Add(time.Hour + time.Minute)}, {"acct-a", "complete-run-later", "started", "running", start.Add(2 * time.Hour)}, {"acct-a", "complete-run-later", "completed", "complete", start.Add(2*time.Hour + time.Minute)}, {"acct-a", "other-day", "started", "running", start.Add(24 * time.Hour)}, {"acct-b", "other-account", "started", "running", start.Add(time.Hour)}} {
		tx, txErr := store.db.Begin()
		if txErr != nil {
			t.Fatal(txErr)
		}
		if txErr = appendCloudInventoryRefreshEvent(tx, CloudInventoryRefreshEvent{AccountFingerprint: item.account, RunID: item.run, Kind: item.kind, Status: item.status, Phase: item.status, CurrentDirectory: "/root", RecordedAtUTC: item.at}); txErr != nil {
			_ = tx.Rollback()
			t.Fatal(txErr)
		}
		if txErr = tx.Commit(); txErr != nil {
			t.Fatal(txErr)
		}
	}
	filter := CloudInventoryRefreshRunFilter{Status: "complete", StartedFromUTC: "2026-10-30", StartedThroughUTC: "2026-10-30"}
	page, err := store.ListCloudInventoryRefreshRunsFiltered("acct-a", 0, filter, 1)
	if err != nil || len(page) != 1 || page[0].RunID != "complete-run-later" {
		t.Fatalf("filtered run page=%+v err=%v", page, err)
	}
	older, err := store.ListCloudInventoryRefreshRunsFiltered("acct-a", page[0].StartSequence, filter, 10)
	if err != nil || len(older) != 1 || older[0].RunID != "complete-run" {
		t.Fatalf("filtered older run page=%+v err=%v", older, err)
	}
	failed, err := store.ListCloudInventoryRefreshRunsFiltered("acct-a", 0, CloudInventoryRefreshRunFilter{Status: "failed", StartedFromUTC: "2026-10-30", StartedThroughUTC: "2026-10-30"}, 10)
	if err != nil || len(failed) != 1 || failed[0].RunID != "failed-run" {
		t.Fatalf("failed runs=%+v err=%v", failed, err)
	}
	if _, err = store.ListCloudInventoryRefreshRunsFiltered("acct-a", 0, CloudInventoryRefreshRunFilter{Status: "invalid"}, 10); err == nil {
		t.Fatal("invalid status filter accepted")
	}
	if _, err = store.ListCloudInventoryRefreshRunsFiltered("acct-a", 0, CloudInventoryRefreshRunFilter{StartedFromUTC: "2026-10-30", StartedThroughUTC: "2026-10-29"}, 10); err == nil {
		t.Fatal("reversed UTC start range accepted")
	}
}

func TestCloudInventoryRefreshEvidenceExportIsAccountBoundAndAllOrNothing(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	when := time.Date(2026, 10, 30, 5, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		account, run, kind string
		status             string
		at                 time.Time
	}{{"acct-a", "0123456789abcdef0123456789abcdef", "started", "running", when}, {"acct-a", "0123456789abcdef0123456789abcdef", "completed", "complete", when.Add(time.Second)}, {"acct-b", "0123456789abcdef0123456789abcdef", "started", "running", when}} {
		tx, txErr := store.db.Begin()
		if txErr != nil {
			t.Fatal(txErr)
		}
		event := CloudInventoryRefreshEvent{AccountFingerprint: item.account, RunID: item.run, Kind: item.kind, Status: item.status, Phase: item.status, CurrentDirectory: "/root", RecordedAtUTC: item.at}
		if txErr = appendCloudInventoryRefreshEvent(tx, event); txErr != nil {
			_ = tx.Rollback()
			t.Fatal(txErr)
		}
		if txErr = tx.Commit(); txErr != nil {
			t.Fatal(txErr)
		}
	}
	restartTx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	offset, fsID := int64(100), int64(7788)
	if err = appendCloudInventoryRefreshEvent(restartTx, CloudInventoryRefreshEvent{AccountFingerprint: "acct-a", RunID: "0123456789abcdef0123456789abcdef", Kind: "boundary_restart", Status: "running", Phase: "listing", CurrentDirectory: "/root", RecordedAtUTC: when.Add(2 * time.Second), RestartReason: "provider_duplicate_page_id", RestartOffset: &offset, RestartFSID: &fsID}); err != nil {
		_ = restartTx.Rollback()
		t.Fatal(err)
	}
	if err = restartTx.Commit(); err != nil {
		t.Fatal(err)
	}
	bundle, err := store.CloudInventoryRefreshEvidenceExport(context.Background(), "acct-a", "0123456789abcdef0123456789abcdef")
	if err != nil || bundle.Run.RunID != "0123456789abcdef0123456789abcdef" || bundle.Run.Status != "running" || len(bundle.Events) != 3 || bundle.Events[0].Kind != "started" || bundle.Events[2].Kind != "boundary_restart" || bundle.Events[2].RestartOffset == nil || *bundle.Events[2].RestartOffset != offset || bundle.Events[2].RestartFSID == nil || *bundle.Events[2].RestartFSID != fsID || bundle.Events[0].AccountFingerprint != "" {
		t.Fatalf("export=%+v err=%v", bundle, err)
	}
	filtered, err := store.ListCloudInventoryRefreshEventsFiltered("acct-a", "0123456789abcdef0123456789abcdef", "boundary_restart", "provider_duplicate_page_id", 0, 10)
	if err != nil || len(filtered) != 1 || filtered[0].RestartOffset == nil || *filtered[0].RestartOffset != offset || filtered[0].RestartFSID == nil || *filtered[0].RestartFSID != fsID {
		t.Fatalf("filtered restart evidence=%+v err=%v", filtered, err)
	}
	if _, err = store.CloudInventoryRefreshEvidenceExport(context.Background(), "acct-a", "ffffffffffffffffffffffffffffffff"); !errors.Is(err, ErrCloudInventoryRefreshNotFound) {
		t.Fatalf("missing run error=%v", err)
	}
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = appendCloudInventoryRefreshEvent(tx, CloudInventoryRefreshEvent{AccountFingerprint: "acct-a", RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: "started", Status: "running", Phase: "listing", CurrentDirectory: "/root", RecordedAtUTC: when}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < CloudInventoryRefreshEvidenceExportLimit; i++ {
		tx, txErr := store.db.Begin()
		if txErr != nil {
			t.Fatal(txErr)
		}
		event := CloudInventoryRefreshEvent{AccountFingerprint: "acct-a", RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: "directory_published", Status: "running", Phase: "listing", CurrentDirectory: "/root", RecordedAtUTC: when.Add(time.Duration(i) * time.Second)}
		if txErr = appendCloudInventoryRefreshEvent(tx, event); txErr != nil {
			_ = tx.Rollback()
			t.Fatal(txErr)
		}
		if txErr = tx.Commit(); txErr != nil {
			t.Fatal(txErr)
		}
	}
	tx, err = store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = appendCloudInventoryRefreshEvent(tx, CloudInventoryRefreshEvent{AccountFingerprint: "acct-a", RunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: "directory_published", Status: "running", Phase: "listing", CurrentDirectory: "/root", RecordedAtUTC: when.Add(time.Duration(CloudInventoryRefreshEvidenceExportLimit) * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CloudInventoryRefreshEvidenceExport(context.Background(), "acct-a", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); !errors.Is(err, ErrCloudInventoryRefreshEvidenceExportTooLarge) {
		t.Fatalf("oversized run export error=%v", err)
	}
}

func TestCloudInventoryRefreshRunSummaryExportUsesFiltersAndRejectsPartialResults(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	start := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		account, run, kind, status string
		at                         time.Time
	}{{"acct-a", "paused-run", "started", "running", start}, {"acct-a", "paused-run", "paused", "paused", start.Add(time.Minute)}, {"acct-a", "failed-run", "started", "running", start.Add(time.Hour)}, {"acct-a", "failed-run", "failed", "failed", start.Add(time.Hour + time.Minute)}, {"acct-b", "other-account", "started", "paused", start}} {
		tx, txErr := store.db.Begin()
		if txErr != nil {
			t.Fatal(txErr)
		}
		if txErr = appendCloudInventoryRefreshEvent(tx, CloudInventoryRefreshEvent{AccountFingerprint: item.account, RunID: item.run, Kind: item.kind, Status: item.status, Phase: item.status, CurrentDirectory: "/root", RecordedAtUTC: item.at}); txErr != nil {
			_ = tx.Rollback()
			t.Fatal(txErr)
		}
		if txErr = tx.Commit(); txErr != nil {
			t.Fatal(txErr)
		}
	}
	filter := CloudInventoryRefreshRunFilter{Status: "paused", StartedFromUTC: "2026-10-30", StartedThroughUTC: "2026-10-30"}
	items, err := store.ExportCloudInventoryRefreshRunSummaries(context.Background(), "acct-a", filter)
	if err != nil || len(items) != 1 || items[0].RunID != "paused-run" || items[0].LatestKind != "paused" {
		t.Fatalf("filtered summaries=%+v err=%v", items, err)
	}
	if _, err = store.ExportCloudInventoryRefreshRunSummaries(context.Background(), "acct-b", CloudInventoryRefreshRunFilter{Status: "paused"}); err != nil {
		t.Fatalf("account-scoped export failed: %v", err)
	}
	if _, err = store.ExportCloudInventoryRefreshRunSummaries(context.Background(), "acct-a", filter); err != nil {
		t.Fatalf("repeated same-snapshot export failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = store.ExportCloudInventoryRefreshRunSummaries(ctx, "acct-a", filter); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export error=%v", err)
	}

	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO cloud_inventory_refresh_events(account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,pending_count,recorded_at_utc) VALUES(?,?,'started','running','listing','/root',0,0,0,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= CloudInventoryRefreshRunSummaryExportLimit; i++ {
		if _, err = stmt.Exec("bulk-account", fmt.Sprintf("run-%05d", i), start.Add(time.Duration(i)*time.Second).UnixMicro()); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_refresh_runs(start_sequence,account_fingerprint,run_id,root_path,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error,latest_sequence,latest_kind) SELECT started.sequence,started.account_fingerprint,started.run_id,started.current_directory,latest.status,latest.phase,latest.current_directory,latest.pages_processed,latest.items_processed,latest.request_budget,latest.requests_used,latest.pending_count,started.recorded_at_utc,latest.recorded_at_utc,'',latest.sequence,latest.event_kind FROM cloud_inventory_refresh_events started JOIN cloud_inventory_refresh_events latest ON latest.account_fingerprint=started.account_fingerprint AND latest.run_id=started.run_id AND latest.sequence=(SELECT max(newest.sequence) FROM cloud_inventory_refresh_events newest WHERE newest.account_fingerprint=started.account_fingerprint AND newest.run_id=started.run_id) WHERE started.event_kind='started' AND started.account_fingerprint='bulk-account'`); err != nil {
		t.Fatal(err)
	}
	bulk, err := store.ExportCloudInventoryRefreshRunSummaries(context.Background(), "bulk-account", CloudInventoryRefreshRunFilter{})
	if err != nil || len(bulk) != CloudInventoryRefreshRunSummaryExportLimit || bulk[0].RunID != fmt.Sprintf("run-%05d", CloudInventoryRefreshRunSummaryExportLimit) {
		t.Fatalf("boundary export count=%d first=%+v err=%v", len(bulk), bulk[0], err)
	}
	tx, err = store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO cloud_inventory_refresh_events(account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,pending_count,recorded_at_utc) VALUES(?,?,'started','running','listing','/root',0,0,0,?)`, "bulk-account", "run-05001", start.Add(time.Duration(CloudInventoryRefreshRunSummaryExportLimit+1)*time.Second).UnixMicro()); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_refresh_runs(start_sequence,account_fingerprint,run_id,root_path,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error,latest_sequence,latest_kind) SELECT started.sequence,started.account_fingerprint,started.run_id,started.current_directory,started.status,started.phase,started.current_directory,started.pages_processed,started.items_processed,started.request_budget,started.requests_used,started.pending_count,started.recorded_at_utc,started.recorded_at_utc,'',started.sequence,started.event_kind FROM cloud_inventory_refresh_events started WHERE started.event_kind='started' AND started.account_fingerprint='bulk-account' AND started.run_id='run-05001'`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ExportCloudInventoryRefreshRunSummaries(context.Background(), "bulk-account", CloudInventoryRefreshRunFilter{}); !errors.Is(err, ErrCloudInventoryRefreshRunSummaryExportTooLarge) {
		t.Fatalf("oversized summary export error=%v", err)
	}
}

func TestCloudInventoryRefreshStartAndStateEventsAreAtomic(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "atomic", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: time.Now().UTC(), UpdatedAtUTC: time.Now().UTC()}
	if _, err = store.db.Exec(`CREATE TRIGGER reject_started_refresh BEFORE INSERT ON cloud_inventory_refresh_events WHEN NEW.event_kind='started' BEGIN SELECT RAISE(ABORT,'injected event failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = store.StartCloudInventoryRefresh(&state); err == nil {
		t.Fatal("start succeeded despite rejected event")
	}
	if _, err = store.LoadCloudInventoryRefreshState("acct"); err == nil {
		t.Fatal("refresh state committed without its start event")
	}
	if _, err = store.db.Exec(`DROP TRIGGER reject_started_refresh`); err != nil {
		t.Fatal(err)
	}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER reject_paused_refresh BEFORE INSERT ON cloud_inventory_refresh_events WHEN NEW.event_kind='paused' BEGIN SELECT RAISE(ABORT,'injected event failure'); END`); err != nil {
		t.Fatal(err)
	}
	state.Status, state.Phase = "paused", "paused"
	state.UpdatedAtUTC = time.Now().UTC()
	if err = store.SaveCloudInventoryRefreshState(state); err == nil {
		t.Fatal("pause state succeeded despite rejected event")
	}
	loaded, err := store.LoadCloudInventoryRefreshState("acct")
	if err != nil || loaded.Status != "running" {
		t.Fatalf("state changed despite failed event: state=%+v err=%v", loaded, err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", "atomic", "", 0, 10)
	if err != nil || len(events) != 1 || events[0].Kind != "started" {
		t.Fatalf("event history changed despite failed transaction: events=%+v err=%v", events, err)
	}
}

func TestCloudInventoryRefreshResumeAfterRestartRecordsRunningCheckpoint(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "restart-running", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
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
	state, err = store.LoadCloudInventoryRefreshState("acct")
	if err != nil || state.Status != "running" || state.PendingCount != 1 {
		t.Fatalf("persisted running checkpoint=%+v err=%v", state, err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER reject_resume_event BEFORE INSERT ON cloud_inventory_refresh_events WHEN NEW.event_kind='resumed' BEGIN SELECT RAISE(ABORT,'injected event failure'); END`); err != nil {
		t.Fatal(err)
	}
	state.UpdatedAtUTC = time.Now().UTC()
	if err = store.ResumeCloudInventoryRefreshState(state); err == nil {
		t.Fatal("resume succeeded despite rejected event")
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", "restart-running", "", 0, 10)
	if err != nil || len(events) != 1 || events[0].Kind != "started" {
		t.Fatalf("failed resume changed history: events=%+v err=%v", events, err)
	}
	loaded, err := store.LoadCloudInventoryRefreshState("acct")
	if err != nil || loaded.Status != "running" || loaded.UpdatedAtUTC.Equal(state.UpdatedAtUTC) {
		t.Fatalf("failed resume changed checkpoint: loaded=%+v err=%v", loaded, err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER reject_resume_event`); err != nil {
		t.Fatal(err)
	}
	state, err = store.LoadCloudInventoryRefreshState("acct")
	if err != nil {
		t.Fatal(err)
	}
	state.UpdatedAtUTC = time.Now().UTC()
	if err = store.ResumeCloudInventoryRefreshState(state); err != nil {
		t.Fatal(err)
	}
	events, err = store.ListCloudInventoryRefreshEvents("acct", "restart-running", "", 0, 10)
	if err != nil || len(events) != 2 || events[0].Kind != "resumed" || events[0].Status != "running" {
		t.Fatalf("resumed running checkpoint evidence=%+v err=%v", events, err)
	}
}

func TestCloudInventoryRefreshPublishAndEventAreAtomic(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 3, 4, 5, 0, time.UTC)
	old := CloudInventoryEntry{FsID: 10, Path: "/root/old.bin", Name: "old.bin", Size: 4}
	if err = store.UpsertCloudInventoryPage("acct", "prior", "/root", []CloudInventoryEntry{old}, observed, nil); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "publish-atomic", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER reject_directory_event BEFORE INSERT ON cloud_inventory_refresh_events WHEN NEW.event_kind='directory_published' BEGIN SELECT RAISE(ABORT,'injected event failure'); END`); err != nil {
		t.Fatal(err)
	}
	newEntry := CloudInventoryEntry{FsID: 11, Path: "/root/new.bin", Name: "new.bin", Size: 5}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", []CloudInventoryEntry{newEntry}, observed.Add(time.Minute), nil, nil); err == nil {
		t.Fatal("directory publish succeeded despite rejected event")
	}
	entries, metadata, _, err := store.ListCloudInventory("acct", "/root", 0, 10)
	if err != nil || metadata.Status != "complete" || len(entries) != 1 || entries[0].FsID != old.FsID {
		t.Fatalf("formal snapshot changed after event failure: entries=%+v metadata=%+v err=%v", entries, metadata, err)
	}
	loaded, err := store.LoadCloudInventoryRefreshState("acct")
	if err != nil || loaded.Status != "running" || loaded.PendingCount != 1 {
		t.Fatalf("refresh checkpoint changed after publish rollback: state=%+v err=%v", loaded, err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", state.RunID, "", 0, 10)
	if err != nil || len(events) != 1 || events[0].Kind != "started" {
		t.Fatalf("directory event committed without publish: events=%+v err=%v", events, err)
	}
}

func TestCloudInventoryRefreshChangeInsertFailureRollsBackDirectoryPublish(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 30, 6, 0, 0, 0, time.UTC)
	old := []CloudInventoryEntry{{FsID: 1, Path: "/root/a", Name: "a", Size: 1}, {FsID: 2, Path: "/root/b", Name: "b", Size: 2}}
	if err = store.UpsertCloudInventoryPage("acct", "prior", "/root", old, now, nil); err != nil {
		t.Fatal(err)
	}
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "detail-atomic", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER reject_second_refresh_change BEFORE INSERT ON cloud_inventory_refresh_changes WHEN NEW.ordinal=2 BEGIN SELECT RAISE(ABORT,'injected detail failure'); END`); err != nil {
		t.Fatal(err)
	}
	current := []CloudInventoryEntry{{FsID: 3, Path: "/root/c", Name: "c", Size: 3}, {FsID: 4, Path: "/root/d", Name: "d", Size: 4}}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", current, now.Add(time.Minute), nil, nil); err == nil {
		t.Fatal("directory publish succeeded despite rejected second detail")
	}
	entries, metadata, _, err := store.ListCloudInventory("acct", "/root", 0, 10)
	if err != nil || metadata.Status != "complete" || len(entries) != 2 || entries[0].FsID == 3 || entries[1].FsID == 4 {
		t.Fatalf("formal snapshot changed after detail failure: entries=%+v metadata=%+v err=%v", entries, metadata, err)
	}
	loaded, err := store.LoadCloudInventoryRefreshState("acct")
	if err != nil || loaded.Status != "running" || loaded.PendingCount != 1 || loaded.CurrentDirectory != "/root" {
		t.Fatalf("refresh checkpoint changed after detail failure: state=%+v err=%v", loaded, err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", state.RunID, "", 0, 10)
	if err != nil || len(events) != 1 || events[0].Kind != "started" {
		t.Fatalf("publish event committed despite detail failure: events=%+v err=%v", events, err)
	}
	var details int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_refresh_changes`).Scan(&details); err != nil || details != 0 {
		t.Fatalf("partial change rows committed: count=%d err=%v", details, err)
	}
}

func TestCloudInventoryRefreshEventsPersistDirectChildDeltaCounts(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 3, 4, 5, 0, time.UTC)
	prior := []CloudInventoryEntry{
		{FsID: 10, Path: "/root/stable.bin", Name: "stable.bin", Size: 4},
		{FsID: 20, Path: "/root/removed.bin", Name: "removed.bin", Size: 5},
		{FsID: 30, Path: "/root/before.bin", Name: "before.bin", Size: 6, ProviderMtime: 100, ContentMD5: "old-md5"},
	}
	if err = store.UpsertCloudInventoryPage("acct", "prior", "/root", prior, observed, nil); err != nil {
		t.Fatal(err)
	}
	now := observed.Add(time.Minute)
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "delta-counts", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	current := []CloudInventoryEntry{
		{FsID: 10, Path: "/root/stable.bin", Name: "stable.bin", Size: 4},
		{FsID: 30, Path: "/root/after.bin", Name: "after.bin", Size: 8, ProviderMtime: 101, ContentMD5: "new-md5"},
		{FsID: 40, Path: "/root/added.bin", Name: "added.bin", Size: 9},
	}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", current, observed.Add(2*time.Minute), nil, nil); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", state.RunID, "", 0, 10)
	if err != nil || len(events) != 3 {
		t.Fatalf("refresh events=%+v err=%v", events, err)
	}
	delta := events[1]
	if delta.Kind != "directory_published" || delta.AddedCount == nil || *delta.AddedCount != 1 || delta.RemovedCount == nil || *delta.RemovedCount != 1 || delta.ChangedCount == nil || *delta.ChangedCount != 1 {
		t.Fatalf("directory delta summary=%+v", delta)
	}
	if delta.DirectoryChanged == nil || !*delta.DirectoryChanged || delta.CloudObservedAtUTC == nil || !delta.CloudObservedAtUTC.Equal(observed.Add(2*time.Minute)) {
		t.Fatalf("directory event lost change or cloud observation evidence: %+v", delta)
	}
	bundle, err := store.CloudInventoryRefreshEvidenceExport(context.Background(), "acct", state.RunID)
	if err != nil || len(bundle.Events) != 3 || bundle.Events[1].AddedCount == nil || *bundle.Events[1].AddedCount != 1 || bundle.Events[1].RemovedCount == nil || *bundle.Events[1].RemovedCount != 1 || bundle.Events[1].ChangedCount == nil || *bundle.Events[1].ChangedCount != 1 {
		t.Fatalf("evidence export omitted directory delta: %+v err=%v", bundle, err)
	}
	if events[0].AddedCount != nil || events[0].RemovedCount != nil || events[0].ChangedCount != nil {
		t.Fatalf("non-directory event unexpectedly has delta counts: %+v", events[0])
	}
	unchangedState := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "unchanged-delta", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: now.Add(3 * time.Minute), UpdatedAtUTC: now.Add(3 * time.Minute)}
	if err = store.StartCloudInventoryRefresh(&unchangedState); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryRefreshPage(&unchangedState, "/root", current, observed.Add(4*time.Minute), nil, nil); err != nil {
		t.Fatal(err)
	}
	unchangedEvents, err := store.ListCloudInventoryRefreshEvents("acct", unchangedState.RunID, "", 0, 10)
	if err != nil || len(unchangedEvents) != 3 {
		t.Fatalf("unchanged refresh events=%+v err=%v", unchangedEvents, err)
	}
	unchanged := unchangedEvents[1]
	if unchanged.DirectoryChanged == nil || *unchanged.DirectoryChanged || unchanged.AddedCount == nil || *unchanged.AddedCount != 0 || unchanged.RemovedCount == nil || *unchanged.RemovedCount != 0 || unchanged.ChangedCount == nil || *unchanged.ChangedCount != 0 {
		t.Fatalf("unchanged directory must persist explicit zero counts: %+v", unchanged)
	}
}

func TestCloudInventoryRefreshChangeItemsKeepPageObservationTimesAcrossRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 31, 5, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "prior", "/root", nil, base, nil); err != nil {
		t.Fatal(err)
	}
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "page-times", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: base, UpdatedAtUTC: base}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	page1At, page2At := base.Add(time.Minute), base.Add(2*time.Minute)
	next := 100
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 2, Path: "/root/second.bin", Name: "second.bin", Size: 2}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", []CloudInventoryEntry{{FsID: 1, Path: "/root/first.bin", Name: "first.bin", Size: 1}}, page1At, &next, nil); err != nil {
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
	state, err = store.LoadCloudInventoryRefreshState("acct")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", []CloudInventoryEntry{{FsID: 2, Path: "/root/second.bin", Name: "second.bin", Size: 2}}, page2At, nil, nil); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", state.RunID, "directory_published", 0, 10)
	if err != nil || len(events) != 1 || events[0].CloudObservedAtUTC == nil || !events[0].CloudObservedAtUTC.Equal(page2At) {
		t.Fatalf("directory completion time=%+v err=%v", events, err)
	}
	changes, err := store.ListCloudInventoryRefreshChanges("acct", state.RunID, events[0].Sequence, 0, 10)
	if err != nil || len(changes) != 2 {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
	observed := map[string]time.Time{}
	for _, change := range changes {
		if change.New == nil || change.New.ObservedAtUTC == nil {
			t.Fatalf("item observation time missing: %+v", change)
		}
		observed[change.New.Path] = *change.New.ObservedAtUTC
	}
	if !observed["/root/first.bin"].Equal(page1At) || !observed["/root/second.bin"].Equal(page2At) {
		t.Fatalf("per-page observation times=%v want first=%v second=%v", observed, page1At, page2At)
	}
	for fsID, want := range map[int64]time.Time{1: page1At, 2: page2At} {
		var got int64
		if err = store.db.QueryRow(`SELECT observed_at_utc FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=?`, fsID).Scan(&got); err != nil || got != want.UnixMicro() {
			t.Fatalf("cached fs_id=%d observed=%d want=%d err=%v", fsID, got, want.UnixMicro(), err)
		}
	}
}

func TestCloudInventoryRefreshRejectsDuplicateFsIDAcrossPages(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Date(2026, 10, 31, 6, 0, 0, 0, time.UTC)
	old := CloudInventoryEntry{FsID: 1, Path: "/root/old.bin", Name: "old.bin", Size: 1}
	if err = store.UpsertCloudInventoryPage("acct", "baseline", "/root", []CloudInventoryEntry{old}, base, nil); err != nil {
		t.Fatal(err)
	}
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "duplicate-pages", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: base, UpdatedAtUTC: base}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	next := 100
	first := CloudInventoryEntry{FsID: 2, Path: "/root/first.bin", Name: "first.bin", Size: 2}
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 2, Path: "/root/replaced.bin", Name: "replaced.bin", Size: 9}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", []CloudInventoryEntry{first}, base.Add(time.Minute), &next, nil); err != nil {
		t.Fatal(err)
	}
	duplicate := CloudInventoryEntry{FsID: 2, Path: "/root/replaced.bin", Name: "replaced.bin", Size: 9}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", []CloudInventoryEntry{duplicate}, base.Add(2*time.Minute), nil, nil); err == nil {
		t.Fatal("duplicate fs_id across provider pages was accepted and published")
	}
	var path string
	if err = store.db.QueryRow(`SELECT path FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=1`).Scan(&path); err != nil || path != old.Path {
		t.Fatalf("previous complete inventory changed: path=%q err=%v", path, err)
	}
	if err = store.db.QueryRow(`SELECT path FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=2`).Scan(&path); err != sql.ErrNoRows {
		t.Fatalf("incomplete refresh leaked into published inventory: path=%q err=%v", path, err)
	}
}

func TestCloudInventoryRefreshChangeDetailsPersistBeforeAfterIdentityAndTimes(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observedBefore := time.Date(2026, 10, 30, 5, 0, 0, 0, time.UTC)
	old := []CloudInventoryEntry{
		{FsID: 10, Path: "/root/stable.bin", Name: "stable.bin", Size: 4, ProviderMtime: 10, ContentMD5: "stable-md5"},
		{FsID: 11, Path: "/root/changed.bin", Name: "changed.bin", Size: 5, ProviderMtime: 11, ContentMD5: "old-md5"},
		{FsID: 12, Path: "/root/deleted.bin", Name: "deleted.bin", Size: 6, ProviderMtime: 12, ContentMD5: "deleted-md5"},
		{FsID: 13, Path: "/root/replaced.bin", Name: "replaced.bin", Size: 7, ProviderMtime: 13, ContentMD5: "replaced-md5"},
	}
	if err = store.UpsertCloudInventoryPage("acct", "prior-run", "/root", old, observedBefore, nil); err != nil {
		t.Fatal(err)
	}
	verifiedAt := observedBefore.Add(time.Minute)
	if err = store.SaveVerifiedCloudContentHash("acct", old[1], strings.Repeat("a", 64), verifiedAt); err != nil {
		t.Fatal(err)
	}
	recordedAt := observedBefore.Add(2 * time.Minute)
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "detail-run", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: recordedAt, UpdatedAtUTC: recordedAt}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	observedAfter := observedBefore.Add(3 * time.Minute)
	current := []CloudInventoryEntry{
		old[0],
		{FsID: 11, Path: "/root/changed.bin", Name: "changed.bin", Size: 8, ProviderMtime: 14, ContentMD5: "new-md5"},
		{FsID: 14, Path: "/root/replaced.bin", Name: "replaced.bin", Size: 9, ProviderMtime: 15, ContentMD5: "new-identity-md5"},
		{FsID: 15, Path: "/root/new.bin", Name: "new.bin", Size: 10, ProviderMtime: 16, ContentMD5: "new-file-md5"},
	}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", current, observedAfter, nil, nil); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", state.RunID, "directory_published", 0, 10)
	if err != nil || len(events) != 1 || events[0].AddedCount == nil || *events[0].AddedCount != 2 || events[0].RemovedCount == nil || *events[0].RemovedCount != 2 || events[0].ChangedCount == nil || *events[0].ChangedCount != 1 {
		t.Fatalf("published summary=%+v err=%v", events, err)
	}
	changes, err := store.ListCloudInventoryRefreshChanges("acct", state.RunID, events[0].Sequence, 0, 50)
	if err != nil || len(changes) != 4 {
		t.Fatalf("published details=%+v err=%v", changes, err)
	}
	byKind := make(map[string]CloudInventoryRefreshChange)
	for _, change := range changes {
		byKind[change.Kind+":"+func() string {
			if change.New != nil {
				return change.New.Path
			}
			return change.Old.Path
		}()] = change
		if !change.RecordedAtUTC.Equal(events[0].RecordedAtUTC) || !change.CloudObservedAtUTC.Equal(observedAfter) {
			t.Fatalf("detail timestamps changed: %+v", change)
		}
	}
	changed := byKind["metadata_changed:/root/changed.bin"]
	if changed.Old == nil || changed.New == nil || changed.Old.FsID != 11 || changed.New.FsID != 11 || changed.Old.ContentSHA256 != strings.Repeat("a", 64) || changed.Old.ContentSHA256VerifiedAtUTC == nil || !changed.Old.ContentSHA256VerifiedAtUTC.Equal(verifiedAt) || changed.New.ContentSHA256 != "" {
		t.Fatalf("metadata before/after or verified hash evidence=%+v", changed)
	}
	identity := byKind["identity_changed:/root/replaced.bin"]
	if identity.Old == nil || identity.New == nil || identity.Old.FsID != 13 || identity.New.FsID != 14 {
		t.Fatalf("same-path identity replacement=%+v", identity)
	}
	if _, exists := byKind["removed:/root/deleted.bin"]; !exists {
		t.Fatalf("deleted item detail missing: %+v", changes)
	}
	if _, exists := byKind["added:/root/new.bin"]; !exists {
		t.Fatalf("added item detail missing: %+v", changes)
	}
	page, err := store.ListCloudInventoryRefreshChanges("acct", state.RunID, events[0].Sequence, changes[0].Sequence, 1)
	if err != nil || len(page) != 1 || page[0].Sequence >= changes[0].Sequence {
		t.Fatalf("stable detail cursor page=%+v err=%v", page, err)
	}
	if _, err = store.ListCloudInventoryRefreshChanges("other-account", state.RunID, events[0].Sequence, 0, 50); !errors.Is(err, ErrCloudInventoryRefreshNotFound) {
		t.Fatalf("cross-account event query err=%v", err)
	}
}

func TestCloudInventoryRefreshChangeQueryFiltersPagesAndKeepsGlobalCounts(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 5, 0, 0, 0, time.UTC)
	old := []CloudInventoryEntry{
		{FsID: 10, Path: "/root/same.bin", Name: "same.bin", Size: 1, ProviderMtime: 1},
		{FsID: 11, Path: "/root/changed-name.bin", Name: "changed-name.bin", Size: 2, ProviderMtime: 2},
		{FsID: 12, Path: "/root/remove.bin", Name: "remove.bin", Size: 3, ProviderMtime: 3},
		{FsID: 13, Path: "/root/replaced.bin", Name: "replaced.bin", Size: 4, ProviderMtime: 4},
	}
	if err = store.UpsertCloudInventoryPage("acct", "prior", "/root", old, observed, nil); err != nil {
		t.Fatal(err)
	}
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "query-run", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: observed.Add(time.Minute), UpdatedAtUTC: observed.Add(time.Minute)}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	current := []CloudInventoryEntry{
		old[0],
		{FsID: 11, Path: "/root/changed-name.bin", Name: "changed-name.bin", Size: 7, ProviderMtime: 8},
		{FsID: 14, Path: "/root/replaced.bin", Name: "replaced.bin", Size: 4, ProviderMtime: 4},
		{FsID: 15, Path: "/root/Added Special.bin", Name: "Added Special.bin", Size: 5, ProviderMtime: 5},
	}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", current, observed.Add(2*time.Minute), nil, nil); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", state.RunID, "directory_published", 0, 2)
	if err != nil || len(events) != 1 {
		t.Fatalf("published events=%+v err=%v", events, err)
	}
	all, err := store.QueryCloudInventoryRefreshChanges("acct", state.RunID, events[0].Sequence, 0, 2, "", "")
	if err != nil || len(all.Changes) != 2 || !all.HasMore || all.MatchedCount != 4 || all.Counts.Total != 4 || all.Counts.Added != 1 || all.Counts.Removed != 1 || all.Counts.MetadataChanged != 1 || all.Counts.IdentityChanged != 1 {
		t.Fatalf("all page=%+v err=%v", all, err)
	}
	filtered, err := store.QueryCloudInventoryRefreshChanges("acct", state.RunID, events[0].Sequence, 0, 10, "metadata_changed", "CHANGED-NAME")
	if err != nil || len(filtered.Changes) != 1 || filtered.MatchedCount != 1 || filtered.Changes[0].Kind != "metadata_changed" || filtered.Counts.Total != 4 {
		t.Fatalf("filtered path/name query=%+v err=%v", filtered, err)
	}
	fsid, err := store.QueryCloudInventoryRefreshChanges("acct", state.RunID, events[0].Sequence, 0, 10, "identity_changed", "14")
	if err != nil || len(fsid.Changes) != 1 || fsid.Changes[0].New == nil || fsid.Changes[0].New.FsID != 14 {
		t.Fatalf("fs_id query=%+v err=%v", fsid, err)
	}
	wildcard, err := store.QueryCloudInventoryRefreshChanges("acct", state.RunID, events[0].Sequence, 0, 10, "", "%")
	if err != nil || len(wildcard.Changes) != 0 || wildcard.MatchedCount != 0 {
		t.Fatalf("wildcard query should be literal: %+v err=%v", wildcard, err)
	}
	exported, err := store.CloudInventoryRefreshChangesExport(context.Background(), "acct", state.RunID, events[0].Sequence, "identity_changed", "14")
	if err != nil || exported.MatchedCount != 1 || len(exported.Changes) != 1 || exported.Counts.Total != 4 || exported.Changes[0].New == nil || exported.Changes[0].New.FsID != 14 || !exported.CloudObservedAtUTC.Equal(observed.Add(2*time.Minute)) {
		t.Fatalf("filtered detail export=%+v err=%v", exported, err)
	}
	if _, err = store.db.Exec(`WITH RECURSIVE seq(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM seq WHERE x<4996) INSERT INTO cloud_inventory_refresh_changes(account_fingerprint,run_id,event_sequence,ordinal,change_kind,new_snapshot,recorded_at_utc,cloud_observed_at_utc) SELECT 'acct',?, ?,1000+x,'added',json_object('fs_id',x,'path','/root/extra-'||x,'name','extra-'||x),?,? FROM seq`, state.RunID, events[0].Sequence, observed.UnixMicro(), observed.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	boundary, err := store.CloudInventoryRefreshChangesExport(context.Background(), "acct", state.RunID, events[0].Sequence, "", "")
	if err != nil || boundary.MatchedCount != CloudInventoryRefreshChangesExportLimit || len(boundary.Changes) != CloudInventoryRefreshChangesExportLimit {
		t.Fatalf("export at the 5,000-row limit should succeed: rows=%d matched=%d err=%v", len(boundary.Changes), boundary.MatchedCount, err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_refresh_changes(account_fingerprint,run_id,event_sequence,ordinal,change_kind,new_snapshot,recorded_at_utc,cloud_observed_at_utc) VALUES('acct',?,?,999999,'added',json_object('fs_id',999999,'path','/root/over-limit','name','over-limit'),?,?)`, state.RunID, events[0].Sequence, observed.UnixMicro(), observed.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	tooLarge, err := store.CloudInventoryRefreshChangesExport(context.Background(), "acct", state.RunID, events[0].Sequence, "", "")
	if !errors.Is(err, ErrCloudInventoryRefreshChangesExportTooLarge) || len(tooLarge.Changes) != 0 {
		t.Fatalf("oversized detail export returned partial data: rows=%d err=%v", len(tooLarge.Changes), err)
	}
	if _, err = store.QueryCloudInventoryRefreshChanges("other", state.RunID, events[0].Sequence, 0, 10, "", ""); !errors.Is(err, ErrCloudInventoryRefreshNotFound) {
		t.Fatalf("cross-account query err=%v", err)
	}
	if _, err = store.QueryCloudInventoryRefreshChanges("acct", state.RunID, events[0].Sequence, 0, 10, "invalid", ""); err == nil {
		t.Fatal("invalid change kind was accepted")
	}
}

func TestCloudInventoryRefreshDeltaCountsAreUnknownWithoutCompleteBaseline(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "first-observation", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", []CloudInventoryEntry{{FsID: 1, Path: "/root/new.bin", Name: "new.bin", Size: 7}}, now.Add(time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", state.RunID, "", 0, 10)
	if err != nil || len(events) != 3 {
		t.Fatalf("refresh events=%+v err=%v", events, err)
	}
	delta := events[1]
	if delta.DirectoryChanged == nil || !*delta.DirectoryChanged || delta.AddedCount != nil || delta.RemovedCount != nil || delta.ChangedCount != nil {
		t.Fatalf("first observation must not infer delta counts: %+v", delta)
	}
	changes, err := store.ListCloudInventoryRefreshChanges("acct", state.RunID, delta.Sequence, 0, 50)
	if err != nil || len(changes) != 0 {
		t.Fatalf("first observation must not create speculative details: changes=%+v err=%v", changes, err)
	}
}

func TestCloudInventoryRefreshIdentityPairingRequiresUniqueSamePath(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	old := []CloudInventoryEntry{{FsID: 1, Path: "/root/same.bin", Name: "same.bin", Size: 1}, {FsID: 2, Path: "/root/same.bin", Name: "same.bin", Size: 2}, {FsID: 3, Path: "/root/gone.bin", Name: "gone.bin", Size: 3}}
	if err = store.UpsertCloudInventoryPage("acct", "prior", "/root", old, now, nil); err != nil {
		t.Fatal(err)
	}
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "ambiguous-identity", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	current := []CloudInventoryEntry{{FsID: 4, Path: "/root/same.bin", Name: "same.bin", Size: 4}, {FsID: 5, Path: "/root/new.bin", Name: "new.bin", Size: 5}}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/root", current, now.Add(time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListCloudInventoryRefreshEvents("acct", state.RunID, "directory_published", 0, 10)
	if err != nil || len(events) != 1 || events[0].AddedCount == nil || *events[0].AddedCount != 2 || events[0].RemovedCount == nil || *events[0].RemovedCount != 3 {
		t.Fatalf("ambiguous identity counts=%+v err=%v", events, err)
	}
	changes, err := store.ListCloudInventoryRefreshChanges("acct", state.RunID, events[0].Sequence, 0, 50)
	if err != nil || len(changes) != 5 {
		t.Fatalf("ambiguous identity details=%+v err=%v", changes, err)
	}
	for _, change := range changes {
		if change.Kind == "identity_changed" {
			t.Fatalf("ambiguous same-path IDs were paired as identity change: %+v", changes)
		}
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

func TestCloudDirectorySizeRejectsMissingChildCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", []CloudInventoryEntry{{FsID: 1, Path: "/child", Name: "child", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run", "/child", []CloudInventoryEntry{{FsID: 2, Path: "/child/file.bin", Name: "file.bin", Size: 12}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DELETE FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/child'`); err != nil {
		t.Fatal(err)
	}
	size, err := store.CloudDirectorySize("acct", "/")
	if err != nil || size.Complete || size.Bytes != 12 {
		t.Fatalf("directory with missing child coverage=%+v err=%v; want size retained and completeness false", size, err)
	}
}

func TestCloudDirectorySizeRejectsFileWithoutParentDirectoryCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", nil, now, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES('acct',1,'/missing','/missing/file.bin','file.bin',0,7,0,'',?,'old-run')`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	size, err := store.CloudDirectorySize("acct", "/")
	if err != nil || size.Complete || size.Bytes != 7 {
		t.Fatalf("directory with file lacking parent coverage=%+v err=%v; want size retained and completeness false", size, err)
	}
}

func TestCloudDirectorySizeRejectsDirectoryWithoutParentCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage("acct", "run", "/", nil, now, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES('acct',1,'/missing','/missing/child','child',1,0,0,'',?,'old-run')`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES('acct','/missing/child','complete',?,'old-run')`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	size, err := store.CloudDirectorySize("acct", "/")
	if err != nil || size.Complete {
		t.Fatalf("directory with missing parent node was reported complete: %+v err=%v", size, err)
	}
}

func TestCloudInventoryRejectsPathParentMismatch(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "root", "/", []CloudInventoryEntry{
		{FsID: 1, Path: "/a", Name: "a", Directory: true},
		{FsID: 2, Path: "/b", Name: "b", Directory: true},
	}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "a", "/a", []CloudInventoryEntry{{FsID: 3, Path: "/a/file.bin", Name: "file.bin", Size: 10}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "b", "/b", nil, now, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory SET parent_path='/b' WHERE account_fingerprint='acct' AND fs_id=3`); err != nil {
		t.Fatal(err)
	}

	size, err := store.CloudDirectorySize("acct", "/a")
	if err != nil || size.Complete {
		t.Fatalf("directory size accepted inconsistent path/parent identity: size=%+v err=%v", size, err)
	}
	var incomplete int64
	if err = store.db.QueryRow(cloudInventoryIncompleteDirectoriesSQL, "acct", "acct", "acct", "acct", "acct", "acct").Scan(&incomplete); err != nil || incomplete == 0 {
		t.Fatalf("full inventory search accepted inconsistent path/parent identity: incomplete=%d err=%v", incomplete, err)
	}
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	complete, err := cloudInventorySubtreeComplete(tx, "acct", "/")
	_ = tx.Rollback()
	if err != nil || complete {
		t.Fatalf("full scan completion accepted inconsistent path/parent identity: complete=%t err=%v", complete, err)
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
	rows, err := store.db.Query(`EXPLAIN QUERY PLAN `+cloudInventoryDirectoryAggregateSQL, "/nested", "/nested", "acct", "/nested", prefix, upper, "acct", "/nested", prefix, upper, "acct", "/nested", prefix, upper, "acct", "/nested", prefix, upper, "acct", "/nested", prefix, upper)
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

func TestCloudMutationScanConflictDetectsAffectedQueuedDirectory(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{
		AccountFingerprint: "acct",
		RunID:              "scan",
		Status:             "running",
		Phase:              "listing",
		CurrentDirectory:   "/source",
		StartedAtUTC:       now,
		UpdatedAtUTC:       now,
	}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	conflict, err := store.CloudMutationScanConflict("acct", []string{"/source"})
	if err != nil || !conflict {
		t.Fatalf("active scan touching affected directory must conflict: conflict=%t err=%v", conflict, err)
	}
	conflict, err = store.CloudMutationScanConflict("acct", []string{"/other"})
	if err != nil || conflict {
		t.Fatalf("unrelated path must not conflict: conflict=%t err=%v", conflict, err)
	}
}

func TestCloudMutationScanConflictIncludesActiveDuplicateHashScan(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err = store.SaveCloudDuplicateScanState(CloudDuplicateScanState{AccountFingerprint: "acct", RunID: "hash-run", Status: "running", Phase: "hashing", StartedAtUTC: now, UpdatedAtUTC: now}, true); err != nil {
		t.Fatal(err)
	}
	conflict, err := store.CloudMutationScanConflict("acct", []string{"/unrelated"})
	if err != nil || !conflict {
		t.Fatalf("mutation should conflict with account-wide duplicate hashing: conflict=%t err=%v", conflict, err)
	}
}

func TestApplyConfirmedCloudMutationRenamesCachedFileAndUpdatesSize(t *testing.T) {
	queueDir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(queueDir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	observed := time.Date(2026, 10, 30, 4, 0, 0, 0, time.UTC)
	for _, page := range []struct {
		path    string
		entries []CloudInventoryEntry
	}{
		{path: "/", entries: []CloudInventoryEntry{
			{FsID: 1, Path: "/docs", Name: "docs", Directory: true},
			{FsID: 2, Path: "/archive", Name: "archive", Directory: true},
		}},
		{path: "/docs", entries: []CloudInventoryEntry{
			{FsID: 3, Path: "/docs/old.txt", Name: "old.txt", Size: 7, ContentMD5: "known-md5"},
		}},
		{path: "/archive"},
	} {
		if err = store.UpsertCloudInventoryPage("acct", "seed", page.path, page.entries, observed, nil); err != nil {
			t.Fatal(err)
		}
	}
	var generationBefore int64
	if err = store.db.QueryRow("SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'").Scan(&generationBefore); err != nil {
		t.Fatal(err)
	}
	changeObserved := observed.Add(time.Minute)
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "rename",
		Before:                CloudInventoryEntry{FsID: 3, Path: "/docs/old.txt", Name: "old.txt", Size: 7},
		After:                 CloudInventoryEntry{FsID: 3, Path: "/docs/new.txt", Name: "new.txt", Size: 7},
		ResponseObservedAtUTC: changeObserved,
	})
	if err != nil || !applied {
		t.Fatalf("confirmed rename not applied: applied=%t err=%v", applied, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(queueDir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	items, directory, total, err := store.ListCloudInventory("acct", "/docs", 0, 20)
	if err != nil || directory.Status != "complete" || total != 1 || len(items) != 1 || items[0].FsID != 3 || items[0].Path != "/docs/new.txt" {
		t.Fatalf("renamed cached listing=%+v directory=%+v total=%d err=%v", items, directory, total, err)
	}
	size, err := store.CloudDirectorySize("acct", "/docs")
	if err != nil || !size.Complete || size.Bytes != 7 || size.Files != 1 || !size.ObservedAtUTC.Equal(changeObserved) {
		t.Fatalf("renamed directory size=%+v err=%v", size, err)
	}
	var entryObserved int64
	if err = store.db.QueryRow("SELECT observed_at_utc FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=3").Scan(&entryObserved); err != nil {
		t.Fatal(err)
	}
	if !time.UnixMicro(entryObserved).UTC().Equal(changeObserved) {
		t.Fatalf("file observation time=%s want %s", time.UnixMicro(entryObserved).UTC(), changeObserved)
	}
	var generationAfter int64
	if err = store.db.QueryRow("SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'").Scan(&generationAfter); err != nil || generationAfter != generationBefore+1 {
		t.Fatalf("confirmed mutation generation must advance once: before=%d after=%d err=%v", generationBefore, generationAfter, err)
	}
	var contentMD5 string
	if err = store.db.QueryRow("SELECT content_md5 FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=3").Scan(&contentMD5); err != nil || contentMD5 != "known-md5" {
		t.Fatalf("provider metadata omission erased the cached content identity: md5=%q err=%v", contentMD5, err)
	}
}

func TestApplyConfirmedCloudMutationRollsBackRowsCoverageAndGeneration(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 4, 0, 0, 0, time.UTC)
	for _, page := range []struct {
		path    string
		entries []CloudInventoryEntry
	}{
		{path: "/", entries: []CloudInventoryEntry{
			{FsID: 61, Path: "/from", Name: "from", Directory: true},
			{FsID: 62, Path: "/to", Name: "to", Directory: true},
		}},
		{path: "/from", entries: []CloudInventoryEntry{{FsID: 63, Path: "/from/old.bin", Name: "old.bin", Size: 7}}},
		{path: "/to"},
	} {
		if err = store.UpsertCloudInventoryPage("acct", "seed", page.path, page.entries, observed, nil); err != nil {
			t.Fatal(err)
		}
	}
	var generationBefore int64
	if err = store.db.QueryRow("SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'").Scan(&generationBefore); err != nil {
		t.Fatal(err)
	}
	var fromObservedBefore, toObservedBefore int64
	if err = store.db.QueryRow("SELECT observed_at_utc FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/from'").Scan(&fromObservedBefore); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow("SELECT observed_at_utc FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/to'").Scan(&toObservedBefore); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fail_confirmed_cloud_mutation BEFORE UPDATE ON cloud_inventory WHEN OLD.fs_id=63 BEGIN SELECT RAISE(ABORT,'injected confirmed-mutation failure'); END`); err != nil {
		t.Fatal(err)
	}
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "move",
		Before:                CloudInventoryEntry{FsID: 63, Path: "/from/old.bin", Name: "old.bin", Size: 7},
		After:                 CloudInventoryEntry{FsID: 63, Path: "/to/new.bin", Name: "new.bin", Size: 7},
		ResponseObservedAtUTC: observed.Add(time.Minute),
	})
	if err == nil || applied {
		t.Fatalf("injected storage failure should abort mutation: applied=%t err=%v", applied, err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER fail_confirmed_cloud_mutation`); err != nil {
		t.Fatal(err)
	}
	var oldPathCount, newPathCount int
	if err = store.db.QueryRow("SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct' AND path='/from/old.bin'").Scan(&oldPathCount); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow("SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct' AND path='/to/new.bin'").Scan(&newPathCount); err != nil {
		t.Fatal(err)
	}
	var generationAfter, fromObservedAfter, toObservedAfter int64
	if err = store.db.QueryRow("SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'").Scan(&generationAfter); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow("SELECT observed_at_utc FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/from'").Scan(&fromObservedAfter); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow("SELECT observed_at_utc FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/to'").Scan(&toObservedAfter); err != nil {
		t.Fatal(err)
	}
	if oldPathCount != 1 || newPathCount != 0 || generationAfter != generationBefore || fromObservedAfter != fromObservedBefore || toObservedAfter != toObservedBefore {
		t.Fatalf("failed mutation partially committed: old=%d new=%d generation=%d/%d parents=%d,%d want %d,%d", oldPathCount, newPathCount, generationAfter, generationBefore, fromObservedAfter, toObservedAfter, fromObservedBefore, toObservedBefore)
	}
}

func TestApplyConfirmedCloudMutationRejectsIdentityMismatchAndPathCollision(t *testing.T) {
	for _, test := range []struct {
		name   string
		before CloudInventoryEntry
		after  CloudInventoryEntry
	}{
		{name: "identity-mismatch", before: CloudInventoryEntry{FsID: 73, Path: "/docs/old.bin", Name: "changed.bin", Size: 5}, after: CloudInventoryEntry{FsID: 73, Path: "/docs/new.bin", Name: "new.bin", Size: 5}},
		{name: "destination-collision", before: CloudInventoryEntry{FsID: 73, Path: "/docs/old.bin", Name: "old.bin", Size: 5}, after: CloudInventoryEntry{FsID: 73, Path: "/docs/occupied.bin", Name: "occupied.bin", Size: 5}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			observed := time.Date(2026, 10, 30, 5, 0, 0, 0, time.UTC)
			if err = store.UpsertCloudInventoryPage("acct", "seed", "/", []CloudInventoryEntry{{FsID: 71, Path: "/docs", Name: "docs", Directory: true}}, observed, nil); err != nil {
				t.Fatal(err)
			}
			if err = store.UpsertCloudInventoryPage("acct", "seed", "/docs", []CloudInventoryEntry{
				{FsID: 73, Path: "/docs/old.bin", Name: "old.bin", Size: 5},
				{FsID: 74, Path: "/docs/occupied.bin", Name: "occupied.bin", Size: 9},
			}, observed, nil); err != nil {
				t.Fatal(err)
			}
			var generationBefore int64
			if err = store.db.QueryRow("SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'").Scan(&generationBefore); err != nil {
				t.Fatal(err)
			}
			applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{Operation: "rename", Before: test.before, After: test.after, ResponseObservedAtUTC: observed.Add(time.Minute)})
			if err != nil || applied {
				t.Fatalf("unsafe mutation accepted: applied=%t err=%v", applied, err)
			}
			items, directory, total, err := store.ListCloudInventory("acct", "/docs", 0, 10)
			paths := make(map[string]bool, len(items))
			for _, item := range items {
				paths[item.Path] = true
			}
			if err != nil || directory.Status != "complete" || total != 2 || len(items) != 2 || !paths["/docs/old.bin"] || !paths["/docs/occupied.bin"] {
				t.Fatalf("rejected mutation changed listing or coverage: items=%+v directory=%+v total=%d err=%v", items, directory, total, err)
			}
			var generationAfter int64
			if err = store.db.QueryRow("SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'").Scan(&generationAfter); err != nil || generationAfter != generationBefore {
				t.Fatalf("rejected mutation changed generation: before=%d after=%d err=%v", generationBefore, generationAfter, err)
			}
		})
	}
}

func TestApplyConfirmedCloudMutationReconcilesOnlineIdentityOverStaleCache(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 30, 6, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "root", "/", []CloudInventoryEntry{{FsID: 71, Path: "/docs", Name: "docs", Directory: true}, {FsID: 72, Path: "/archive", Name: "archive", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	before := CloudInventoryEntry{FsID: 73, Path: "/docs/old.bin", Name: "old.bin", Size: 5, ProviderMtime: 1}
	if err = store.UpsertCloudInventoryPage("acct", "docs", "/docs", []CloudInventoryEntry{before}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "archive", "/archive", nil, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveVerifiedCloudContentHash("acct", before, strings.Repeat("a", 64), now); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE cloud_inventory SET path='/docs/stale.bin',name='stale.bin' WHERE account_fingerprint='acct' AND fs_id=73`); err != nil {
		t.Fatal(err)
	}
	opID := strings.Repeat("b", 32)
	op := RemoteOperation{ID: opID, AccountFingerprint: "acct", Kind: "move", ObjectID: 73, SourcePath: before.Path, DestinationPath: "/archive/new.bin", Status: "prepared", CreatedAtUTC: now, Evidence: json.RawMessage(`{"test":true}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateRemoteOperation(opID, "confirmed", json.RawMessage(`{"provider_response":"success"}`), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	after := CloudInventoryEntry{FsID: 73, Path: "/archive/new.bin", Name: "new.bin", Size: 6, ProviderMtime: 2}
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{OperationID: opID, Operation: "move", Before: before, After: after, ResponseObservedAtUTC: now.Add(time.Second)})
	if err != nil || !applied {
		t.Fatalf("confirmed online identity was not reconciled: applied=%t err=%v", applied, err)
	}
	items, meta, total, err := store.ListCloudInventory("acct", "/archive", 0, 10)
	if err != nil || total != 1 || len(items) != 1 || items[0].FsID != 73 || items[0].Path != after.Path || meta.Status != "stale" {
		t.Fatalf("destination cache does not reflect confirmed online object: items=%+v meta=%+v total=%d err=%v", items, meta, total, err)
	}
	var oldRows, hashes int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=73 AND path='/docs/stale.bin'`).Scan(&oldRows); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id=73`).Scan(&hashes); err != nil {
		t.Fatal(err)
	}
	var eventBody []byte
	if err = store.db.QueryRow(`SELECT evidence_json FROM remote_operation_events WHERE operation_id=? ORDER BY sequence DESC LIMIT 1`, opID).Scan(&eventBody); err != nil {
		t.Fatal(err)
	}
	var event struct {
		Kind string `json:"event"`
	}
	if err = json.Unmarshal(eventBody, &event); err != nil || event.Kind != "cache_refresh_reconciled" || oldRows != 0 || hashes != 0 {
		t.Fatalf("stale cache/hash/evidence not reconciled: old=%d hashes=%d event=%s err=%v", oldRows, hashes, eventBody, err)
	}
}

func TestApplyConfirmedCloudDeleteRemovesFileByAuthoritativeIDWhenCachePathDrifts(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "queue", "/queue", []CloudInventoryEntry{{FsID: 75, Path: "/queue/stale.bin", Name: "stale.bin", Size: 8, ProviderMtime: 1}}, now, nil); err != nil {
		t.Fatal(err)
	}
	stale := CloudInventoryEntry{FsID: 75, Path: "/queue/stale.bin", Name: "stale.bin", Size: 8, ProviderMtime: 1}
	if err = store.SaveVerifiedCloudContentHash("acct", stale, strings.Repeat("c", 64), now); err != nil {
		t.Fatal(err)
	}
	opID := strings.Repeat("d", 32)
	op := RemoteOperation{ID: opID, AccountFingerprint: "acct", Kind: "delete", ObjectID: 75, SourcePath: "/queue/remote.bin", Status: "prepared", CreatedAtUTC: now, Evidence: json.RawMessage(`{"test":true}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateRemoteOperation(opID, "confirmed", json.RawMessage(`{"provider_response":"success"}`), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	before := CloudInventoryEntry{FsID: 75, Path: "/queue/remote.bin", Name: "remote.bin", Size: 8, ProviderMtime: 1}
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{OperationID: opID, Operation: "delete", Before: before, ResponseObservedAtUTC: now.Add(time.Second)})
	if err != nil || !applied {
		t.Fatalf("confirmed delete did not repair the cache by fs_id: applied=%t err=%v", applied, err)
	}
	var inventoryRows, hashRows int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=75`).Scan(&inventoryRows); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id=75`).Scan(&hashRows); err != nil {
		t.Fatal(err)
	}
	if inventoryRows != 0 || hashRows != 0 {
		t.Fatalf("deleted remote file remained in cache: inventory=%d hashes=%d", inventoryRows, hashRows)
	}
}

func TestApplyConfirmedCloudMutationMovesCachedFileAndHash(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 4, 0, 0, 0, time.UTC)
	for _, page := range []struct {
		path    string
		entries []CloudInventoryEntry
	}{
		{path: "/", entries: []CloudInventoryEntry{
			{FsID: 11, Path: "/docs", Name: "docs", Directory: true},
			{FsID: 12, Path: "/archive", Name: "archive", Directory: true},
		}},
		{path: "/docs", entries: []CloudInventoryEntry{
			{FsID: 13, Path: "/docs/file.bin", Name: "file.bin", Size: 9, ProviderMtime: 55},
		}},
		{path: "/archive"},
	} {
		if err = store.UpsertCloudInventoryPage("acct", "seed", page.path, page.entries, observed, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.Exec("INSERT INTO cloud_content_hashes(account_fingerprint,fs_id,path,size_bytes,provider_mtime,sha256,verified_at_utc) VALUES('acct',13,'/docs/file.bin',9,55,?,?)", strings.Repeat("a", 64), observed.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	changeObserved := observed.Add(time.Minute)
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "move",
		Before:                CloudInventoryEntry{FsID: 13, Path: "/docs/file.bin", Name: "file.bin", Size: 9, ProviderMtime: 55},
		After:                 CloudInventoryEntry{FsID: 13, Path: "/archive/file.bin", Name: "file.bin", Size: 9, ProviderMtime: 55},
		ResponseObservedAtUTC: changeObserved,
	})
	if err != nil || !applied {
		t.Fatalf("confirmed move not applied: applied=%t err=%v", applied, err)
	}
	for _, want := range []struct {
		path     string
		bytes    int64
		files    int64
		complete bool
	}{
		{path: "/docs", bytes: 0, files: 0, complete: true},
		{path: "/archive", bytes: 9, files: 1, complete: true},
		{path: "/", bytes: 9, files: 1, complete: true},
	} {
		size, sizeErr := store.CloudDirectorySize("acct", want.path)
		if sizeErr != nil || size.Bytes != want.bytes || size.Files != want.files || size.Complete != want.complete {
			t.Fatalf("size for %s=%+v err=%v want bytes=%d files=%d complete=%t", want.path, size, sizeErr, want.bytes, want.files, want.complete)
		}
	}
	items, _, total, err := store.ListCloudInventory("acct", "/archive", 0, 10)
	if err != nil || total != 1 || len(items) != 1 || items[0].Path != "/archive/file.bin" {
		t.Fatalf("moved listing=%+v total=%d err=%v", items, total, err)
	}
	var hashPath string
	if err = store.db.QueryRow("SELECT path FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id=13").Scan(&hashPath); err != nil || hashPath != "/archive/file.bin" {
		t.Fatalf("moved hash path=%q err=%v", hashPath, err)
	}
}

func TestApplyConfirmedCloudMutationDoesNotCarryOldMD5WhenFileMetadataChanged(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 31, 5, 0, 0, 0, time.UTC)
	oldMD5 := strings.Repeat("a", 32)
	for _, page := range []struct {
		path    string
		entries []CloudInventoryEntry
	}{
		{path: "/", entries: []CloudInventoryEntry{{FsID: 113, Path: "/docs", Name: "docs", Directory: true}, {FsID: 114, Path: "/archive", Name: "archive", Directory: true}}},
		{path: "/docs", entries: []CloudInventoryEntry{{FsID: 115, Path: "/docs/file.bin", Name: "file.bin", Size: 9, ProviderMtime: 55, ContentMD5: oldMD5}}},
		{path: "/archive"},
	} {
		if err = store.UpsertCloudInventoryPage("acct", "seed", page.path, page.entries, observed, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_content_hashes(account_fingerprint,fs_id,path,size_bytes,provider_mtime,sha256,verified_at_utc) VALUES('acct',115,'/docs/file.bin',9,55,?,?)`, strings.Repeat("b", 64), observed.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "move",
		Before:                CloudInventoryEntry{FsID: 115, Path: "/docs/file.bin", Name: "file.bin", Size: 9, ProviderMtime: 55, ContentMD5: oldMD5},
		After:                 CloudInventoryEntry{FsID: 115, Path: "/archive/file.bin", Name: "file.bin", Size: 10, ProviderMtime: 56},
		ResponseObservedAtUTC: observed.Add(time.Minute),
	})
	if err != nil || !applied {
		t.Fatalf("confirmed move not applied: applied=%t err=%v", applied, err)
	}
	items, _, total, err := store.ListCloudInventory("acct", "/archive", 0, 10)
	if err != nil || total != 1 || len(items) != 1 || items[0].Size != 10 || items[0].ProviderMtime != 56 || items[0].ContentMD5 != "" {
		t.Fatalf("changed file metadata retained an unverified provider MD5: items=%+v total=%d err=%v", items, total, err)
	}
	var hashes int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id=115`).Scan(&hashes); err != nil || hashes != 0 {
		t.Fatalf("changed file retained its old SHA-256 evidence: hashes=%d err=%v", hashes, err)
	}
}

func TestApplyConfirmedCloudMutationAddsVerifiedNewDirectory(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 5, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/", nil, observed, nil); err != nil {
		t.Fatal(err)
	}
	changeObserved := observed.Add(time.Minute)
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "mkdir",
		After:                 CloudInventoryEntry{FsID: 21, Path: "/created", Name: "created", Directory: true},
		ResponseObservedAtUTC: changeObserved,
	})
	if err != nil || !applied {
		t.Fatalf("confirmed mkdir not applied: applied=%t err=%v", applied, err)
	}
	items, root, total, err := store.ListCloudInventory("acct", "/", 0, 10)
	if err != nil || root.Status != "complete" || total != 1 || len(items) != 1 || items[0].Path != "/created" || !items[0].Directory {
		t.Fatalf("root listing after mkdir=%+v root=%+v total=%d err=%v", items, root, total, err)
	}
	size, err := store.CloudDirectorySize("acct", "/created")
	if err != nil || !size.Complete || size.Bytes != 0 || size.Files != 0 || size.Directories != 0 || !size.ObservedAtUTC.Equal(changeObserved) {
		t.Fatalf("created directory size=%+v err=%v", size, err)
	}
}

func TestApplyConfirmedCloudMutationDeletesCachedFile(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 6, 0, 0, 0, time.UTC)
	for _, page := range []struct {
		path    string
		entries []CloudInventoryEntry
	}{
		{path: "/", entries: []CloudInventoryEntry{{FsID: 31, Path: "/docs", Name: "docs", Directory: true}}},
		{path: "/docs", entries: []CloudInventoryEntry{{FsID: 32, Path: "/docs/gone.bin", Name: "gone.bin", Size: 14, ProviderMtime: 8}}},
	} {
		if err = store.UpsertCloudInventoryPage("acct", "seed", page.path, page.entries, observed, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.Exec("INSERT INTO cloud_content_hashes(account_fingerprint,fs_id,path,size_bytes,provider_mtime,sha256,verified_at_utc) VALUES('acct',32,'/docs/gone.bin',14,8,?,?)", strings.Repeat("b", 64), observed.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	changeObserved := observed.Add(time.Minute)
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "delete",
		Before:                CloudInventoryEntry{FsID: 32, Path: "/docs/gone.bin", Name: "gone.bin", Size: 14, ProviderMtime: 8},
		ResponseObservedAtUTC: changeObserved,
	})
	if err != nil || !applied {
		t.Fatalf("confirmed delete not applied: applied=%t err=%v", applied, err)
	}
	items, directory, total, err := store.ListCloudInventory("acct", "/docs", 0, 10)
	if err != nil || directory.Status != "complete" || total != 0 || len(items) != 0 {
		t.Fatalf("deleted listing=%+v directory=%+v total=%d err=%v", items, directory, total, err)
	}
	size, err := store.CloudDirectorySize("acct", "/docs")
	if err != nil || !size.Complete || size.Bytes != 0 || size.Files != 0 {
		t.Fatalf("size after delete=%+v err=%v", size, err)
	}
	var hashes int
	if err = store.db.QueryRow("SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id=32").Scan(&hashes); err != nil || hashes != 0 {
		t.Fatalf("deleted file retained hash row count=%d err=%v", hashes, err)
	}
}

func TestApplyConfirmedCloudMutationDeletesCompleteDirectorySubtree(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	for _, page := range []struct {
		path    string
		entries []CloudInventoryEntry
	}{
		{path: "/", entries: []CloudInventoryEntry{{FsID: 41, Path: "/docs", Name: "docs", Directory: true}}},
		{path: "/docs", entries: []CloudInventoryEntry{
			{FsID: 42, Path: "/docs/a.bin", Name: "a.bin", Size: 5},
			{FsID: 43, Path: "/docs/sub", Name: "sub", Directory: true},
		}},
		{path: "/docs/sub", entries: []CloudInventoryEntry{{FsID: 44, Path: "/docs/sub/b.bin", Name: "b.bin", Size: 8}}},
	} {
		if err = store.UpsertCloudInventoryPage("acct", "seed", page.path, page.entries, observed, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.Exec("INSERT INTO cloud_content_hashes(account_fingerprint,fs_id,path,size_bytes,provider_mtime,sha256,verified_at_utc) VALUES('acct',44,'/docs/sub/b.bin',8,0,?,?)", strings.Repeat("c", 64), observed.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	changeObserved := observed.Add(time.Minute)
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "delete",
		Before:                CloudInventoryEntry{FsID: 41, Path: "/docs", Name: "docs", Directory: true},
		ResponseObservedAtUTC: changeObserved,
	})
	if err != nil || !applied {
		t.Fatalf("confirmed directory delete not applied: applied=%t err=%v", applied, err)
	}
	items, root, total, err := store.ListCloudInventory("acct", "/", 0, 10)
	if err != nil || root.Status != "complete" || total != 0 || len(items) != 0 {
		t.Fatalf("root after subtree delete=%+v root=%+v total=%d err=%v", items, root, total, err)
	}
	size, err := store.CloudDirectorySize("acct", "/")
	if err != nil || !size.Complete || size.Bytes != 0 || size.Files != 0 || size.Directories != 0 || !size.ObservedAtUTC.Equal(changeObserved) {
		t.Fatalf("root size after subtree delete=%+v err=%v", size, err)
	}
	var entries, directories, hashes int
	if err = store.db.QueryRow("SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct'").Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow("SELECT count(*) FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path LIKE '/docs%'").Scan(&directories); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow("SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct' AND path LIKE '/docs%'").Scan(&hashes); err != nil {
		t.Fatal(err)
	}
	if entries != 0 || directories != 0 || hashes != 0 {
		t.Fatalf("subtree deletion left rows: inventory=%d directories=%d hashes=%d", entries, directories, hashes)
	}
}

func TestApplyConfirmedCloudMutationDeletesCompleteEmptyDirectory(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 7, 30, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/", []CloudInventoryEntry{{FsID: 46, Path: "/empty", Name: "empty", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/empty", nil, observed, nil); err != nil {
		t.Fatal(err)
	}
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "delete",
		Before:                CloudInventoryEntry{FsID: 46, Path: "/empty", Name: "empty", Directory: true},
		ResponseObservedAtUTC: observed.Add(time.Minute),
	})
	if err != nil || !applied {
		t.Fatalf("confirmed empty-directory deletion not applied: applied=%t err=%v", applied, err)
	}
	items, root, total, err := store.ListCloudInventory("acct", "/", 0, 10)
	if err != nil || root.Status != "complete" || total != 0 || len(items) != 0 {
		t.Fatalf("root listing after empty-directory delete=%+v root=%+v total=%d err=%v", items, root, total, err)
	}
	size, err := store.CloudDirectorySize("acct", "/")
	if err != nil || !size.Complete || size.Bytes != 0 || size.Directories != 0 {
		t.Fatalf("root size after empty-directory delete=%+v err=%v", size, err)
	}
}

func TestApplyConfirmedCloudMutationAcceptsChangedDirectoryMetadata(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/", []CloudInventoryEntry{{FsID: 93, Path: "/parent", Name: "parent", Directory: true, Size: 500, ProviderMtime: 10}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/parent", nil, observed, nil); err != nil {
		t.Fatal(err)
	}
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "delete",
		Before:                CloudInventoryEntry{FsID: 93, Path: "/parent", Name: "parent", Directory: true, Size: 0, ProviderMtime: 11},
		ResponseObservedAtUTC: observed.Add(time.Minute),
	})
	if err != nil || !applied {
		t.Fatalf("confirmed directory delete rejected after child metadata changed: applied=%t err=%v", applied, err)
	}
	items, root, total, err := store.ListCloudInventory("acct", "/", 0, 10)
	if err != nil || root.Status != "complete" || total != 0 || len(items) != 0 {
		t.Fatalf("root listing after changed-metadata delete=%+v root=%+v total=%d err=%v", items, root, total, err)
	}
}

func TestApplyConfirmedCloudMutationSupportsCompleteDirectoryLifecycle(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/", nil, observed, nil); err != nil {
		t.Fatal(err)
	}
	mutationIndex := 0
	apply := func(mutation CloudInventoryMutation) {
		t.Helper()
		mutation.ResponseObservedAtUTC = observed.Add(time.Duration(mutationIndex+1) * time.Minute)
		applied, applyErr := store.ApplyConfirmedCloudMutation("acct", mutation)
		if applyErr != nil || !applied {
			t.Fatalf("confirmed %s not applied: applied=%t err=%v", mutation.Operation, applied, applyErr)
		}
		mutationIndex++
	}
	apply(CloudInventoryMutation{Operation: "mkdir", After: CloudInventoryEntry{FsID: 91, Path: "/source", Name: "source", Directory: true}})
	apply(CloudInventoryMutation{Operation: "rename", Before: CloudInventoryEntry{FsID: 91, Path: "/source", Name: "source", Directory: true}, After: CloudInventoryEntry{FsID: 91, Path: "/renamed", Name: "renamed", Directory: true, ProviderMtime: 1}})
	apply(CloudInventoryMutation{Operation: "mkdir", After: CloudInventoryEntry{FsID: 92, Path: "/target", Name: "target", Directory: true}})
	apply(CloudInventoryMutation{Operation: "move", Before: CloudInventoryEntry{FsID: 91, Path: "/renamed", Name: "renamed", Directory: true, ProviderMtime: 1}, After: CloudInventoryEntry{FsID: 91, Path: "/target/renamed", Name: "renamed", Directory: true, ProviderMtime: 2}})
	apply(CloudInventoryMutation{Operation: "delete", Before: CloudInventoryEntry{FsID: 91, Path: "/target/renamed", Name: "renamed", Directory: true, ProviderMtime: 2}})
	apply(CloudInventoryMutation{Operation: "delete", Before: CloudInventoryEntry{FsID: 92, Path: "/target", Name: "target", Directory: true}})
	items, root, total, err := store.ListCloudInventory("acct", "/", 0, 10)
	if err != nil || root.Status != "complete" || total != 0 || len(items) != 0 {
		t.Fatalf("root listing after lifecycle=%+v root=%+v total=%d err=%v", items, root, total, err)
	}
}

func TestApplyConfirmedCloudMutationRejectsSubtreeWithMissingDirectoryCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	for _, page := range []struct {
		path    string
		entries []CloudInventoryEntry
	}{
		{path: "/", entries: []CloudInventoryEntry{{FsID: 81, Path: "/docs", Name: "docs", Directory: true}}},
		{path: "/docs", entries: []CloudInventoryEntry{{FsID: 82, Path: "/docs/sub", Name: "sub", Directory: true}}},
		{path: "/docs/sub", entries: []CloudInventoryEntry{{FsID: 83, Path: "/docs/sub/file.bin", Name: "file.bin", Size: 1}}},
	} {
		if err = store.UpsertCloudInventoryPage("acct", "seed", page.path, page.entries, observed, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.Exec("DELETE FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/docs/sub'"); err != nil {
		t.Fatal(err)
	}
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "delete",
		Before:                CloudInventoryEntry{FsID: 81, Path: "/docs", Name: "docs", Directory: true},
		ResponseObservedAtUTC: observed.Add(time.Minute),
	})
	if err != nil || applied {
		t.Fatalf("subtree with missing directory coverage was treated complete: applied=%t err=%v", applied, err)
	}
	var rootPresent int
	if err = store.db.QueryRow("SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=81").Scan(&rootPresent); err != nil || rootPresent != 1 {
		t.Fatalf("incomplete subtree was partially deleted: root count=%d err=%v", rootPresent, err)
	}
}

func TestApplyConfirmedCloudMutationRebasesCompleteDirectorySubtree(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	for _, page := range []struct {
		path    string
		entries []CloudInventoryEntry
	}{
		{path: "/", entries: []CloudInventoryEntry{
			{FsID: 51, Path: "/docs", Name: "docs", Directory: true},
			{FsID: 52, Path: "/archive", Name: "archive", Directory: true},
		}},
		{path: "/docs", entries: []CloudInventoryEntry{
			{FsID: 53, Path: "/docs/root.bin", Name: "root.bin", Size: 4},
			{FsID: 54, Path: "/docs/sub", Name: "sub", Directory: true},
		}},
		{path: "/docs/sub", entries: []CloudInventoryEntry{{FsID: 55, Path: "/docs/sub/child.bin", Name: "child.bin", Size: 6}}},
		{path: "/archive"},
	} {
		if err = store.UpsertCloudInventoryPage("acct", "seed", page.path, page.entries, observed, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.Exec("INSERT INTO cloud_content_hashes(account_fingerprint,fs_id,path,size_bytes,provider_mtime,sha256,verified_at_utc) VALUES('acct',55,'/docs/sub/child.bin',6,0,?,?)", strings.Repeat("d", 64), observed.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	changeObserved := observed.Add(time.Minute)
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "move",
		Before:                CloudInventoryEntry{FsID: 51, Path: "/docs", Name: "docs", Directory: true},
		After:                 CloudInventoryEntry{FsID: 51, Path: "/archive/docs2", Name: "docs2", Directory: true},
		ResponseObservedAtUTC: changeObserved,
	})
	if err != nil || !applied {
		t.Fatalf("confirmed directory move not applied: applied=%t err=%v", applied, err)
	}
	items, movedRoot, total, err := store.ListCloudInventory("acct", "/archive", 0, 10)
	if err != nil || movedRoot.Status != "complete" || total != 1 || len(items) != 1 || items[0].Path != "/archive/docs2" {
		t.Fatalf("destination listing=%+v directory=%+v total=%d err=%v", items, movedRoot, total, err)
	}
	childItems, childDir, childTotal, err := store.ListCloudInventory("acct", "/archive/docs2/sub", 0, 10)
	if err != nil || childDir.Status != "complete" || childTotal != 1 || len(childItems) != 1 || childItems[0].Path != "/archive/docs2/sub/child.bin" {
		t.Fatalf("rebased child listing=%+v directory=%+v total=%d err=%v", childItems, childDir, childTotal, err)
	}
	sourceSize, err := store.CloudDirectorySize("acct", "/docs")
	if err != nil || sourceSize.Complete || sourceSize.Bytes != 0 || sourceSize.Directories != 0 {
		t.Fatalf("source size after move=%+v err=%v", sourceSize, err)
	}
	destinationSize, err := store.CloudDirectorySize("acct", "/archive")
	if err != nil || !destinationSize.Complete || destinationSize.Bytes != 10 || destinationSize.Files != 2 || destinationSize.Directories != 2 {
		t.Fatalf("destination size after move=%+v err=%v", destinationSize, err)
	}
	var observedChild int64
	if err = store.db.QueryRow("SELECT observed_at_utc FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=55").Scan(&observedChild); err != nil {
		t.Fatal(err)
	}
	if !time.UnixMicro(observedChild).UTC().Equal(observed) {
		t.Fatalf("descendant observation changed to %s want original %s", time.UnixMicro(observedChild).UTC(), observed)
	}
	var hashPath string
	if err = store.db.QueryRow("SELECT path FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id=55").Scan(&hashPath); err != nil || hashPath != "/archive/docs2/sub/child.bin" {
		t.Fatalf("descendant hash path=%q err=%v", hashPath, err)
	}
}

func TestApplyConfirmedCloudMutationRenamesCompleteDirectorySubtree(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/", []CloudInventoryEntry{{FsID: 91, Path: "/before", Name: "before", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/before", []CloudInventoryEntry{{FsID: 92, Path: "/before/child.bin", Name: "child.bin", Size: 8}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	applied, err := store.ApplyConfirmedCloudMutation("acct", CloudInventoryMutation{
		Operation:             "rename",
		Before:                CloudInventoryEntry{FsID: 91, Path: "/before", Name: "before", Directory: true},
		After:                 CloudInventoryEntry{FsID: 91, Path: "/after", Name: "after", Directory: true},
		ResponseObservedAtUTC: observed.Add(time.Minute),
	})
	if err != nil || !applied {
		t.Fatalf("confirmed directory rename not applied: applied=%t err=%v", applied, err)
	}
	items, directory, total, err := store.ListCloudInventory("acct", "/after", 0, 10)
	if err != nil || directory.Status != "complete" || total != 1 || len(items) != 1 || items[0].Path != "/after/child.bin" {
		t.Fatalf("renamed directory listing=%+v directory=%+v total=%d err=%v", items, directory, total, err)
	}
	size, err := store.CloudDirectorySize("acct", "/")
	if err != nil || !size.Complete || size.Bytes != 8 || size.Files != 1 || size.Directories != 1 {
		t.Fatalf("root size after directory rename=%+v err=%v", size, err)
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

func TestFailedCloudScanMarksOnlyItsPartialDirectoryFailed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	state := CloudScanState{AccountFingerprint: "acct", RunID: "failed-directory", Status: "running", Phase: "listing", CurrentDirectory: "/root", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	next := 1
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 2, Path: "/root/next.bin", Name: "next.bin", Size: 2}
	if err = store.UpsertCloudInventoryScanPage(&state, "/root", []CloudInventoryEntry{{FsID: 1, Path: "/root/kept.bin", Name: "kept.bin", Size: 1}}, now.Add(time.Second), &next, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES('acct','/other','scanning',NULL,'another-run')`); err != nil {
		t.Fatal(err)
	}
	state.Status, state.Phase, state.LastError = "failed", "failed", "simulated terminal listing error"
	state.UpdatedAtUTC = now.Add(2 * time.Second)
	if err = store.SaveCloudScanCheckpoint(state); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen the same database directory to prove the failed checkpoint and
	// its cached entries survive a process restart.
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	items, meta, total, err := store.ListCloudInventory("acct", "/root", 0, 10)
	if err != nil || total != 1 || len(items) != 1 || items[0].Path != "/root/kept.bin" || meta.Status != "failed" {
		t.Fatalf("failed directory coverage=%+v items=%+v total=%d err=%v", meta, items, total, err)
	}
	var scanStatus, directoryStatus, otherDirectoryStatus string
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_scan_state WHERE account_fingerprint='acct'`).Scan(&scanStatus); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/root'`).Scan(&directoryStatus); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/other'`).Scan(&otherDirectoryStatus); err != nil {
		t.Fatal(err)
	}
	if scanStatus != "failed" || directoryStatus != "failed" {
		t.Fatalf("scan/directory statuses=%q/%q want failed/failed", scanStatus, directoryStatus)
	}
	if otherDirectoryStatus != "scanning" {
		t.Fatalf("unrelated directory status=%q want scanning", otherDirectoryStatus)
	}
}

func TestCloudScanCursorPersistsForRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	boundary := &CloudInventoryPageBoundary{FsID: 101, Path: "/item-101", Name: "item-101"}
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run", Status: "running", Phase: "listing", CurrentDirectory: "/", NextOffset: 100, PagesProcessed: 2, ItemsProcessed: 200, StartedAtUTC: now.Add(-time.Minute), UpdatedAtUTC: now, NextPageBoundary: boundary}
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
	if err != nil || got.RunID != "run" || got.NextOffset != 100 || got.PagesProcessed != 2 || got.PendingCount != 2 || !reflect.DeepEqual(got.NextPageBoundary, boundary) || !got.UpdatedAtUTC.Equal(now.Truncate(time.Microsecond)) {
		t.Fatalf("scan cursor=%+v err=%v", got, err)
	}
	if head, headErr := store.CloudScanQueueHead("acct", "run"); headErr != nil || head != "/" {
		t.Fatalf("scan queue head=%q err=%v", head, headErr)
	}
}

func TestCloudScanDoesNotCompleteWithOrphanDirectoryCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES('acct','/orphan','complete',?, 'old-run')`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryScanPage(&state, "/", nil, now.Add(time.Second), nil, nil); err == nil {
		t.Fatal("full scan completed with a directory coverage row that has no cached directory identity")
	}
	state, err = store.LoadCloudScanState("acct")
	if err != nil || state.Status != "running" || state.PendingCount != 1 {
		t.Fatalf("rejected final page changed scan checkpoint: state=%+v err=%v", state, err)
	}
	var rootRows int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/'`).Scan(&rootRows); err != nil {
		t.Fatal(err)
	}
	if rootRows != 0 {
		t.Fatalf("rejected final page published root coverage; rows=%d", rootRows)
	}
}

func TestCloudScanDoesNotCompleteWithOrphanFileParent(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES('acct',1,'/missing','/missing/file.bin','file.bin',0,7,0,'',?,'old-run')`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryScanPage(&state, "/", nil, now.Add(time.Second), nil, nil); err == nil {
		t.Fatal("full scan completed with a cached file whose parent directory has no identity or coverage")
	}
	state, err = store.LoadCloudScanState("acct")
	if err != nil || state.Status != "running" || state.PendingCount != 1 {
		t.Fatalf("rejected final page changed scan checkpoint: state=%+v err=%v", state, err)
	}
}

func TestCloudScanDoesNotCompleteWithOrphanDirectoryParent(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	state := CloudScanState{AccountFingerprint: "acct", RunID: "run", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES('acct',1,'/missing','/missing/child','child',1,0,0,'',?,'old-run')`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES('acct','/missing/child','complete',?,'old-run')`, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryScanPage(&state, "/", nil, now.Add(time.Second), nil, nil); err == nil {
		t.Fatal("full scan completed with a cached subdirectory that has no cached parent directory")
	}
	state, err = store.LoadCloudScanState("acct")
	if err != nil || state.Status != "running" || state.PendingCount != 1 {
		t.Fatalf("rejected final page changed scan checkpoint: state=%+v err=%v", state, err)
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
	if !strings.Contains(plan, "SEARCH s USING INDEX sqlite_autoindex_cloud_inventory_scan_state_1") || !strings.Contains(plan, "cloud_inventory_scan_boundary_restart_count") || strings.Contains(plan, "cloud_inventory_scan_queue") || strings.Contains(plan, "SCAN s") {
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

func TestCloudScanFinalPageQueuesDirectoriesSeenOnEarlierPages(t *testing.T) {
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
	firstPage := []CloudInventoryEntry{
		{FsID: 1, Path: "/first-page-dir", Name: "first-page-dir", Directory: true},
		{FsID: 2, Path: "/first-page.bin", Name: "first-page.bin", Size: 4},
	}
	nextOffset := 2
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 3, Path: "/last-page-dir", Name: "last-page-dir", Directory: true}
	if err = store.UpsertCloudInventoryScanPage(&state, "/", firstPage, now, &nextOffset, nil); err != nil {
		t.Fatal(err)
	}
	lastPage := []CloudInventoryEntry{
		{FsID: 3, Path: "/last-page-dir", Name: "last-page-dir", Directory: true},
		{FsID: 4, Path: "/last-page.bin", Name: "last-page.bin", Size: 7},
	}
	if err = store.UpsertCloudInventoryScanPage(&state, "/", lastPage, now.Add(time.Second), nil, []string{"/last-page-dir"}); err != nil {
		t.Fatal(err)
	}
	if state.PendingCount != 2 || state.CurrentDirectory != "/first-page-dir" {
		t.Fatalf("final page did not enqueue every directory from the full listing: %+v", state)
	}
	for _, directory := range []string{"/first-page-dir", "/last-page-dir"} {
		if head, headErr := store.CloudScanQueueHead("acct", "scan"); headErr != nil || head != directory {
			t.Fatalf("next queue head=%q, want %q, err=%v", head, directory, headErr)
		}
		if err = store.UpsertCloudInventoryScanPage(&state, directory, nil, now.Add(2*time.Second), nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if state.Status != "complete" || state.PendingCount != 0 {
		t.Fatalf("scan did not complete after visiting all listed directories: %+v", state)
	}
	size, err := store.CloudDirectorySize("acct", "/")
	if err != nil || !size.Complete || size.Files != 2 || size.Directories != 2 || size.Bytes != 11 {
		t.Fatalf("root size after paginated scan=%+v err=%v", size, err)
	}
}

func TestCloudScanRejectsFsIDRepeatedOnLaterPage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	state := CloudScanState{AccountFingerprint: "acct", RunID: "overlap", Status: "running", Phase: "listing", CurrentDirectory: "/root", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	next := 100
	first := CloudInventoryEntry{FsID: 7, Path: "/root/first.bin", Name: "first.bin", Size: 1}
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 7, Path: "/root/second.bin", Name: "second.bin", Size: 2}
	if err = store.UpsertCloudInventoryScanPage(&state, "/root", []CloudInventoryEntry{first}, now, &next, nil); err != nil {
		t.Fatal(err)
	}
	duplicate := CloudInventoryEntry{FsID: 7, Path: "/root/second.bin", Name: "second.bin", Size: 2}
	if err = store.UpsertCloudInventoryScanPage(&state, "/root", []CloudInventoryEntry{duplicate}, now.Add(time.Minute), nil, nil); err == nil {
		t.Fatal("full scan accepted an fs_id repeated on a later provider page")
	}
	var path, status string
	if err = store.db.QueryRow(`SELECT i.path,d.status FROM cloud_inventory AS i JOIN cloud_inventory_directories AS d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path WHERE i.account_fingerprint='acct' AND i.fs_id=7`).Scan(&path, &status); err != nil || path != first.Path || status == "complete" {
		t.Fatalf("overlapping page changed or completed the directory: path=%q status=%q err=%v", path, status, err)
	}
}

func TestCloudScanRejectsConflictingFsIDWithinOnePage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	state := CloudScanState{AccountFingerprint: "acct", RunID: "same-page-duplicate", Status: "running", Phase: "listing", CurrentDirectory: "/root", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	entries := []CloudInventoryEntry{
		{FsID: 17, Path: "/root/first.bin", Name: "first.bin", Size: 3},
		{FsID: 17, Path: "/root/second.bin", Name: "second.bin", Size: 99},
	}
	if err = store.UpsertCloudInventoryScanPage(&state, "/root", entries, now, nil, nil); err == nil {
		t.Fatal("full scan accepted conflicting identities for one fs_id within a provider page")
	}
	var count int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=17`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("same-page duplicate was partially published: rows=%d err=%v", count, err)
	}
	var status string
	if err = store.db.QueryRow(`SELECT status FROM cloud_inventory_scan_state WHERE account_fingerprint='acct'`).Scan(&status); err != nil || status != "running" {
		t.Fatalf("same-page duplicate changed scan state: scan=%q err=%v", status, err)
	}
	var completeDirectories int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_directories WHERE account_fingerprint='acct' AND path='/root' AND status='complete'`).Scan(&completeDirectories); err != nil || completeDirectories != 0 {
		t.Fatalf("same-page duplicate completed the scan directory: count=%d err=%v", completeDirectories, err)
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

func TestCloudInventoryRefreshUpdatesOnlySelectedSubtreeAndKeepsFullScanStateSeparate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	full := CloudScanState{AccountFingerprint: "acct", RunID: "full-run", Status: "complete", Phase: "complete", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.SaveCloudScanState("acct", full.RunID, full.Status, full.Phase, full.CurrentDirectory, 0, 4, 20, now, now, ""); err != nil {
		t.Fatal(err)
	}
	pages := []struct {
		dir     string
		entries []CloudInventoryEntry
	}{
		{"/", []CloudInventoryEntry{{FsID: 1, Path: "/target", Name: "target", Directory: true}, {FsID: 2, Path: "/sibling", Name: "sibling", Directory: true}}},
		{"/target", []CloudInventoryEntry{{FsID: 3, Path: "/target/old.bin", Name: "old.bin", Size: 3}, {FsID: 4, Path: "/target/old-sub", Name: "old-sub", Directory: true}}},
		{"/target/old-sub", []CloudInventoryEntry{{FsID: 5, Path: "/target/old-sub/old-child.bin", Name: "old-child.bin", Size: 5}}},
		{"/sibling", []CloudInventoryEntry{{FsID: 6, Path: "/sibling/keep.bin", Name: "keep.bin", Size: 4}}},
	}
	for i, page := range pages {
		if err = store.UpsertCloudInventoryPage("acct", fmt.Sprintf("seed-%d", i), page.dir, page.entries, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []CloudInventoryEntry{pages[1].entries[0], pages[2].entries[0]} {
		if err = store.SaveVerifiedCloudContentHash("acct", item, strings.Repeat("e", 64), now); err != nil {
			t.Fatal(err)
		}
	}
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "refresh-1", RootPath: "/target", Status: "running", Phase: "listing", CurrentDirectory: "/target", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	meta, err := store.LoadCloudInventoryRefreshState("acct")
	if err != nil || meta.RootPath != "/target" || meta.PendingCount != 1 {
		t.Fatalf("started refresh=%+v err=%v", meta, err)
	}
	queued, err := store.CloudInventoryRefreshQueueHead("acct", state.RunID)
	if err != nil || queued != "/target" {
		t.Fatalf("refresh queue head=%q err=%v", queued, err)
	}
	next := 1
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 8, Path: "/target/sub", Name: "sub", Directory: true}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/target", []CloudInventoryEntry{{FsID: 7, Path: "/target/new.bin", Name: "new.bin", Size: 10}}, now.Add(time.Second), &next, nil); err != nil {
		t.Fatal(err)
	}
	if state.NextOffset != 1 || state.PagesProcessed != 1 || state.Status != "running" {
		t.Fatalf("first page checkpoint=%+v", state)
	}
	pendingItems, pendingMeta, pendingTotal, err := store.ListCloudInventory("acct", "/target", 0, 20)
	hasOldFile := false
	for _, item := range pendingItems {
		hasOldFile = hasOldFile || item.Path == "/target/old.bin"
	}
	if err != nil || pendingMeta.Status != "complete" || pendingTotal != 2 || len(pendingItems) != 2 || !hasOldFile {
		t.Fatalf("published cache changed before its final page: items=%+v meta=%+v total=%d err=%v", pendingItems, pendingMeta, pendingTotal, err)
	}
	pendingSize, err := store.CloudDirectorySize("acct", "/target")
	if err != nil || !pendingSize.Complete || pendingSize.Bytes != 8 {
		t.Fatalf("published cache size changed before its final page: size=%+v err=%v", pendingSize, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	state, err = store.LoadCloudInventoryRefreshState("acct")
	if err != nil || state.NextOffset != 1 || state.RootPath != "/target" || state.Status != "running" {
		t.Fatalf("partial refresh did not resume from persisted cursor: state=%+v err=%v", state, err)
	}
	var stagedRows int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_refresh_entries WHERE account_fingerprint='acct' AND run_id='refresh-1' AND directory='/target'`).Scan(&stagedRows); err != nil || stagedRows != 1 {
		t.Fatalf("staged first page did not survive reopen: rows=%d err=%v", stagedRows, err)
	}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/target", []CloudInventoryEntry{{FsID: 8, Path: "/target/sub", Name: "sub", Directory: true}}, now.Add(2*time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	if state.CurrentDirectory != "/target/sub" || state.PendingCount != 1 || state.NextOffset != 0 {
		t.Fatalf("child directory was not queued: %+v", state)
	}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/target/sub", []CloudInventoryEntry{{FsID: 9, Path: "/target/sub/child.bin", Name: "child.bin", Size: 6}}, now.Add(3*time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	if state.Status != "complete" || state.PendingCount != 0 || state.ItemsProcessed != 3 {
		t.Fatalf("completed refresh=%+v", state)
	}
	items, targetMeta, total, err := store.ListCloudInventory("acct", "/target", 0, 20)
	if err != nil || total != 2 || len(items) != 2 || targetMeta.Status != "complete" {
		t.Fatalf("target listing=%+v meta=%+v total=%d err=%v", items, targetMeta, total, err)
	}
	if items[0].Path == "/target/old.bin" || items[1].Path == "/target/old-sub" {
		t.Fatalf("unseen old subtree remains: %+v", items)
	}
	if _, oldMeta, oldTotal, oldErr := store.ListCloudInventory("acct", "/target/old-sub", 0, 20); oldErr != nil || oldTotal != 0 || oldMeta.Status != "" {
		t.Fatalf("removed old subtree remains cached: meta=%+v total=%d err=%v", oldMeta, oldTotal, oldErr)
	}
	var removedHashes int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct' AND fs_id IN (3,5)`).Scan(&removedHashes); err != nil || removedHashes != 0 {
		t.Fatalf("removed remote subtree retained verified hashes: count=%d err=%v", removedHashes, err)
	}
	_, siblingMeta, siblingTotal, err := store.ListCloudInventory("acct", "/sibling", 0, 20)
	if err != nil || siblingMeta.Status != "complete" || siblingTotal != 1 {
		t.Fatalf("unrelated sibling changed: meta=%+v total=%d err=%v", siblingMeta, siblingTotal, err)
	}
	size, err := store.CloudDirectorySize("acct", "/target")
	if err != nil || !size.Complete || size.Bytes != 16 || size.Files != 2 || size.Directories != 1 {
		t.Fatalf("refreshed subtree size=%+v err=%v", size, err)
	}
	fullAfter, err := store.LoadCloudScanState("acct")
	if err != nil || fullAfter.RunID != full.RunID || fullAfter.Status != full.Status || fullAfter.PagesProcessed != 4 || fullAfter.ItemsProcessed != 20 {
		t.Fatalf("account-wide scan state was changed: %+v err=%v", fullAfter, err)
	}
}

func TestCloudInventoryRefreshStagingKeepsVerifiedDuplicateHashesAndGeneration(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "parent", "/", []CloudInventoryEntry{{FsID: 1, Path: "/target", Name: "target", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	items := []CloudInventoryEntry{{FsID: 2, Path: "/target/a.bin", Name: "a.bin", Size: 4, ProviderMtime: 1, ContentMD5: "old-content-md5"}, {FsID: 3, Path: "/target/b.bin", Name: "b.bin", Size: 4, ProviderMtime: 1, ContentMD5: "old-content-md5"}}
	if err = store.UpsertCloudInventoryPage("acct", "target", "/target", items, now, nil); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	for _, item := range items {
		if err = store.SaveVerifiedCloudContentHash("acct", item, digest, now); err != nil {
			t.Fatal(err)
		}
	}
	var generation int64
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "refresh", RootPath: "/target", Status: "running", Phase: "listing", CurrentDirectory: "/target", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	next := 1
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 3, Path: "/target/b.bin", Name: "b.bin", Size: 4, ProviderMtime: 1, ContentMD5: "old-content-md5"}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/target", items[:1], now.Add(time.Second), &next, nil); err != nil {
		t.Fatal(err)
	}
	listed, meta, total, err := store.ListCloudInventory("acct", "/target", 0, 10)
	if err != nil || meta.Status != "complete" || total != 2 || len(listed) != 2 {
		t.Fatalf("published cache changed while staging: items=%+v meta=%+v total=%d err=%v", listed, meta, total, err)
	}
	groups, err := store.ListVerifiedCloudDuplicateGroups("acct")
	if err != nil || len(groups) != 1 || len(groups[0].Delete) != 1 {
		t.Fatalf("verified duplicate cache unavailable during refresh: groups=%+v err=%v", groups, err)
	}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/target", items[1:], now.Add(2*time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	var afterGeneration, hashCount int64
	if err = store.db.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint='acct'`).Scan(&afterGeneration); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct'`).Scan(&hashCount); err != nil {
		t.Fatal(err)
	}
	if afterGeneration != generation || hashCount != 2 {
		t.Fatalf("unchanged refresh invalidated cache: generation %d->%d hashes=%d", generation, afterGeneration, hashCount)
	}
	state = CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "refresh-changed-md5", RootPath: "/target", Status: "running", Phase: "listing", CurrentDirectory: "/target", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	changed := []CloudInventoryEntry{items[0], items[1]}
	changed[0].ContentMD5 = "changed-content-md5"
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/target", changed, now.Add(3*time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_content_hashes WHERE account_fingerprint='acct'`).Scan(&hashCount); err != nil || hashCount != 1 {
		t.Fatalf("content metadata change did not invalidate only the changed file hash: hashes=%d err=%v", hashCount, err)
	}
}

func TestFailedCloudInventoryRefreshDiscardsStagedPagesAndKeepsPublishedCache(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/", []CloudInventoryEntry{{FsID: 1, Path: "/target", Name: "target", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "seed", "/target", []CloudInventoryEntry{{FsID: 2, Path: "/target/published.bin", Name: "published.bin", Size: 5}}, now, nil); err != nil {
		t.Fatal(err)
	}
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "failed-refresh", RootPath: "/target", Status: "running", Phase: "listing", CurrentDirectory: "/target", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	next := 1
	state.NextPageBoundary = &CloudInventoryPageBoundary{FsID: 4, Path: "/target/next.bin", Name: "next.bin", Size: 7}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/target", []CloudInventoryEntry{{FsID: 3, Path: "/target/uncommitted.bin", Name: "uncommitted.bin", Size: 9}}, now.Add(time.Second), &next, nil); err != nil {
		t.Fatal(err)
	}
	var staged int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_refresh_entries WHERE account_fingerprint='acct'`).Scan(&staged); err != nil || staged != 1 {
		t.Fatalf("failed to prepare staged page: rows=%d err=%v", staged, err)
	}
	state.Status, state.Phase, state.LastError = "failed", "failed", "injected terminal provider error"
	state.UpdatedAtUTC = now.Add(2 * time.Second)
	if err = store.SaveCloudInventoryRefreshState(state); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory_refresh_entries WHERE account_fingerprint='acct'`).Scan(&staged); err != nil || staged != 0 {
		t.Fatalf("terminal failure retained unpublishable staged pages: rows=%d err=%v", staged, err)
	}
	items, meta, total, err := store.ListCloudInventory("acct", "/target", 0, 10)
	if err != nil || meta.Status != "complete" || total != 1 || len(items) != 1 || items[0].Path != "/target/published.bin" {
		t.Fatalf("failed refresh changed the last published cache: items=%+v meta=%+v total=%d err=%v", items, meta, total, err)
	}
	queueHead, queueErr := store.CloudInventoryRefreshQueueHead("acct", state.RunID)
	if queueErr != sql.ErrNoRows || queueHead != "" {
		t.Fatalf("failed refresh remained resumable: head=%q err=%v", queueHead, queueErr)
	}
}

func TestCloudInventoryRefreshPageAndCheckpointRollbackTogether(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := CloudInventoryRefreshState{AccountFingerprint: "acct", RunID: "refresh-rollback", RootPath: "/target", Status: "running", Phase: "listing", CurrentDirectory: "/target", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fail_refresh_checkpoint BEFORE UPDATE ON cloud_inventory_refresh_state BEGIN SELECT RAISE(ABORT,'injected refresh checkpoint failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryRefreshPage(&state, "/target", []CloudInventoryEntry{{FsID: 10, Path: "/target/first.bin", Name: "first.bin", Size: 8}}, now, nil, nil); err == nil {
		t.Fatal("checkpoint failure did not abort the inventory page transaction")
	}
	var rows int
	if err = store.db.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=10`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("inventory page survived failed checkpoint: rows=%d err=%v", rows, err)
	}
	persisted, err := store.LoadCloudInventoryRefreshState("acct")
	if err != nil || persisted.PagesProcessed != 0 || persisted.NextOffset != 0 || persisted.PendingCount != 1 {
		t.Fatalf("checkpoint advanced despite rollback: state=%+v err=%v", persisted, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	persisted, err = store.LoadCloudInventoryRefreshState("acct")
	if err != nil || persisted.PagesProcessed != 0 || persisted.PendingCount != 1 {
		t.Fatalf("refresh checkpoint did not survive reopen: state=%+v err=%v", persisted, err)
	}
}

func TestCloudInventoryBrowseRejectsStaleGeneration(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	emptyPage, emptyMetadata, emptyTotal, err := store.ListCloudInventoryAtGeneration("acct", "/", 0, 100, nil)
	if err != nil || len(emptyPage) != 0 || emptyTotal != 0 || emptyMetadata.Generation != 0 {
		t.Fatalf("uninitialized cache page=%+v metadata=%+v total=%d err=%v", emptyPage, emptyMetadata, emptyTotal, err)
	}
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/", []CloudInventoryEntry{
		{FsID: 1, Path: "/a", Name: "a"}, {FsID: 2, Path: "/b", Name: "b"}, {FsID: 3, Path: "/folder", Name: "folder", Directory: true},
	}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-1", "/folder", []CloudInventoryEntry{
		{FsID: 4, Path: "/folder/content.bin", Name: "content.bin", Size: 5},
	}, now, nil); err != nil {
		t.Fatal(err)
	}
	first, metadata, total, err := store.ListCloudInventoryAtGeneration("acct", "/", 0, 10, nil)
	if err != nil || len(first) != 3 || total != 3 || metadata.Generation == 0 {
		t.Fatalf("first cached page=%+v metadata=%+v total=%d err=%v", first, metadata, total, err)
	}
	var sizedFolder bool
	for _, entry := range first {
		if entry.Path == "/folder" && entry.DirectorySizeBytes != nil && *entry.DirectorySizeBytes == 5 && entry.DirectorySizeComplete != nil && *entry.DirectorySizeComplete {
			sizedFolder = true
		}
	}
	if !sizedFolder {
		t.Fatalf("folder size was not included in the consistent cache snapshot: %+v", first)
	}
	expected := metadata.Generation
	second, nextMeta, nextTotal, err := store.ListCloudInventoryAtGeneration("acct", "/", 1, 1, &expected)
	if err != nil || len(second) != 1 || nextMeta.Generation != expected || nextTotal != total {
		t.Fatalf("same-generation second page=%+v metadata=%+v total=%d err=%v", second, nextMeta, nextTotal, err)
	}
	if err = store.UpsertCloudInventoryPage("acct", "run-2", "/", []CloudInventoryEntry{
		{FsID: 3, Path: "/c", Name: "c"},
	}, now.Add(time.Second), nil); err != nil {
		t.Fatal(err)
	}
	stale, staleMeta, _, err := store.ListCloudInventoryAtGeneration("acct", "/", 1, 1, &expected)
	if !errors.Is(err, ErrCloudInventoryGenerationChanged) || len(stale) != 0 || staleMeta.Generation <= expected {
		t.Fatalf("stale page was not rejected atomically: items=%+v metadata=%+v err=%v", stale, staleMeta, err)
	}
	_, _, staleCoverage, err := store.SearchCloudInventoryAtGeneration("acct", "a", 0, 10, &expected)
	if !errors.Is(err, ErrCloudInventoryGenerationChanged) || staleCoverage.Generation <= expected {
		t.Fatalf("stale search was not rejected: coverage=%+v err=%v", staleCoverage, err)
	}
	_, _, freshCoverage, err := store.SearchCloudInventoryAtGeneration("acct", "a", 0, 10, nil)
	if err != nil || freshCoverage.Generation != staleMeta.Generation {
		t.Fatalf("fresh search generation=%+v list=%+v err=%v", freshCoverage, staleMeta, err)
	}
}
