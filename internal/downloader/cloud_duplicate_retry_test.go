package downloader

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestDuplicateScanLoadsCandidatesInBoundedPages(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	entries := make([]taskstore.CloudInventoryEntry, 235)
	for i := range entries {
		name := fmt.Sprintf("duplicate-%03d.bin", i)
		entries[i] = taskstore.CloudInventoryEntry{FsID: int64(i + 1), Path: "/" + name, Name: name, Size: 9}
	}
	if err = store.UpsertCloudInventoryPage("account", "inventory", "/", entries, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	first, err := loadDuplicateCandidateBatch(store, "account", 0)
	if err != nil || len(first) != duplicateCandidateBatchSize || first[0].FsID != 1 || first[len(first)-1].FsID != 100 {
		t.Fatalf("first batch len=%d err=%v", len(first), err)
	}
	second, err := loadDuplicateCandidateBatch(store, "account", first[len(first)-1].FsID)
	if err != nil || len(second) != duplicateCandidateBatchSize || second[0].FsID != 101 || second[len(second)-1].FsID != 200 {
		t.Fatalf("second batch len=%d err=%v", len(second), err)
	}
	last, err := loadDuplicateCandidateBatch(store, "account", second[len(second)-1].FsID)
	if err != nil || len(last) != 35 || last[0].FsID != 201 || last[len(last)-1].FsID != 235 {
		t.Fatalf("last batch len=%d err=%v", len(last), err)
	}
}

func TestDuplicateScanRetriesTemporaryRateLimitAndPersistsTransition(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	d.duplicateScanRetryBase = 10 * time.Millisecond
	files := []baiduapi.FileInfo{
		{FsID: 701, Path: "/a.bin", ServerFilename: "a.bin", Size: 7, Mtime: 11},
		{FsID: 702, Path: "/b.bin", ServerFilename: "b.bin", Size: 7, Mtime: 12},
	}
	p.lists["/"] = files
	p.mu.Lock()
	p.metadataFailures[701] = 1
	p.mu.Unlock()
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	now := time.Now().UTC()
	entries := []taskstore.CloudInventoryEntry{{FsID: 701, Path: "/a.bin", Name: "a.bin", Size: 7, ProviderMtime: 11}, {FsID: 702, Path: "/b.bin", Name: "b.bin", Size: 7, ProviderMtime: 12}}
	if err = store.UpsertCloudInventoryPage(baiduapi.NewClient("test", "test").Fingerprint(), "inventory", "/", entries, now, nil); err != nil {
		t.Fatal(err)
	}
	account := d.getClient().Fingerprint()
	if err = store.SaveCloudScanState(account, "inventory", "complete", "complete", "/", 0, 1, 2, now, now, ""); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	if _, err = d.StartCloudDuplicateScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state, e := d.CloudDuplicateScanStatus()
		if e != nil {
			t.Fatal(e)
		}
		if state.Status == "complete" {
			break
		}
		if state.Status == "failed" {
			t.Fatalf("transient provider response permanently failed scan: %+v", state)
		}
		time.Sleep(20 * time.Millisecond)
	}
	state, err := d.CloudDuplicateScanStatus()
	if err != nil || state.Status != "complete" || state.ProcessedFiles != 2 {
		t.Fatalf("scan state=%+v err=%v", state, err)
	}
	events, err := store.CloudDuplicateScanEvents(account, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	backoff, resumed := false, false
	for _, event := range events {
		backoff = backoff || event.Phase == "backoff"
		resumed = resumed || (event.Status == "running" && event.Phase == "hashing" && event.RecordedAtUTC.After(state.StartedAtUTC))
	}
	if !backoff || !resumed {
		t.Fatalf("missing persisted retry transitions: %+v", events)
	}
	groups, err := store.ListVerifiedCloudDuplicateGroups(account)
	if err != nil || len(groups) != 1 || len(groups[0].Delete) != 1 {
		t.Fatalf("duplicate groups=%+v err=%v", groups, err)
	}
}

func TestDuplicateScanHonorsPersistedRetryDeadlineAfterStoreReopen(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	files := []baiduapi.FileInfo{{FsID: 711, Path: "/a.bin", ServerFilename: "a.bin", Size: 7, Mtime: 21}, {FsID: 712, Path: "/b.bin", ServerFilename: "b.bin", Size: 7, Mtime: 22}}
	p.lists["/"] = files
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	account := d.getClient().Fingerprint()
	now := time.Now().UTC()
	entries := []taskstore.CloudInventoryEntry{{FsID: 711, Path: "/a.bin", Name: "a.bin", Size: 7, ProviderMtime: 21}, {FsID: 712, Path: "/b.bin", Name: "b.bin", Size: 7, ProviderMtime: 22}}
	if err = store.UpsertCloudInventoryPage(account, "inventory", "/", entries, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState(account, "inventory", "complete", "complete", "/", 0, 1, 2, now, now, ""); err != nil {
		t.Fatal(err)
	}
	retryAt := time.Now().UTC().Add(400 * time.Millisecond)
	state := taskstore.CloudDuplicateScanState{AccountFingerprint: account, RunID: "persisted-retry", Status: "paused", Phase: "backoff", RetryDelayMillis: 200, RetryAtUTC: &retryAt, TotalFiles: 2, StartedAtUTC: now, UpdatedAtUTC: now, LastError: "temporary rate limit"}
	if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	if _, err = d.StartCloudDuplicateScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	p.mu.Lock()
	early := p.metadataCalls[711]
	p.mu.Unlock()
	if early != 0 {
		t.Fatalf("provider was called before persisted retry deadline: calls=%d", early)
	}
	// The restored retry wait and two per-file hash intervals are close to the
	// old limit, which flakes when the full downloader suite is under load.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s, e := d.CloudDuplicateScanStatus()
		if e != nil {
			t.Fatal(e)
		}
		if s.Status == "complete" {
			return
		}
		if s.Status == "failed" {
			t.Fatalf("resumed retry failed: %+v", s)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("resumed duplicate scan did not complete")
}
