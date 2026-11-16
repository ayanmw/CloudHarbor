package downloader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestDuplicateHashResumesFromPersistedSHAState(t *testing.T) {
	data := bytes.Repeat([]byte("x"), int(2*duplicateHashCheckpointBytes+1024))
	var rangeHeader string
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		offset := int64(0)
		rangeValue := r.Header.Get("Range")
		if rangeValue != "" {
			rangeHeader = rangeValue
			if _, err := fmt.Sscanf(rangeValue, "bytes=%d-", &offset); err != nil {
				t.Errorf("invalid Range %q: %v", rangeValue, err)
				return
			}
		}
		w.Header().Set("Content-Length", fmt.Sprint(int64(len(data))-offset))
		if rangeValue != "" {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, int64(len(data)-1), len(data)))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_, _ = w.Write(data[offset:])
	})
	item := taskstore.CloudInventoryEntry{FsID: 9901, Path: "/large.bin", Name: "large.bin", Size: int64(len(data)), ProviderMtime: 100}
	p.lists["/"] = []baiduapi.FileInfo{{FsID: item.FsID, Path: item.Path, ServerFilename: item.Name, Size: item.Size, Mtime: item.ProviderMtime}}
	state := taskstore.CloudDuplicateScanState{AccountFingerprint: d.getClient().Fingerprint(), RunID: "resume-test", Status: "running", Phase: "hashing", TotalFiles: 1, TotalBytes: item.Size, StartedAtUTC: time.Now().UTC(), UpdatedAtUTC: time.Now().UTC()}
	if err := initializeDuplicateHashCheckpoint(&state, item); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var firstDurableOffset int64
	firstDigest, firstErr := hashCloudFileResumable(ctx, d.getClient(), item, &state, func() error {
		state.UpdatedAtUTC = time.Now().UTC()
		if firstDurableOffset == 0 {
			firstDurableOffset = state.CurrentBytes
			cancel()
		}
		return nil
	})
	if firstErr == nil || firstDigest != "" || state.CurrentBytes < duplicateHashCheckpointBytes {
		t.Fatalf("first interrupted hash digest=%q err=%v checkpoint=%d", firstDigest, firstErr, state.CurrentBytes)
	}
	resumeOffset := state.CurrentBytes
	if resumeOffset != firstDurableOffset || resumeOffset < duplicateHashCheckpointBytes || resumeOffset >= item.Size {
		t.Fatalf("checkpoint offset=%d first durable=%d, expected an in-file checkpoint after %d", resumeOffset, firstDurableOffset, duplicateHashCheckpointBytes)
	}
	ctx = context.Background()
	digest, err := hashCloudFileResumable(ctx, d.getClient(), item, &state, func() error { return nil })
	want := sha256.Sum256(data)
	if err != nil || digest != hex.EncodeToString(want[:]) {
		t.Fatalf("resumed digest=%q err=%v", digest, err)
	}
	if rangeHeader != fmt.Sprintf("bytes=%d-", resumeOffset) {
		t.Fatalf("resume Range=%q want bytes=%d-", rangeHeader, resumeOffset)
	}
}

func TestDuplicateHashResumeChecksProviderMD5WhenCacheDidNotHaveOne(t *testing.T) {
	data := bytes.Repeat([]byte("y"), int(2*duplicateHashCheckpointBytes+1024))
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		offset := int64(0)
		if value := r.Header.Get("Range"); value != "" {
			if _, err := fmt.Sscanf(value, "bytes=%d-", &offset); err != nil {
				t.Errorf("invalid Range %q: %v", value, err)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, int64(len(data)-1), len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Header().Set("Content-Length", fmt.Sprint(int64(len(data))-offset))
		_, _ = w.Write(data[offset:])
	})
	item := taskstore.CloudInventoryEntry{FsID: 9902, Path: "/large-md5.bin", Name: "large-md5.bin", Size: int64(len(data)), ProviderMtime: 100}
	info := baiduapi.FileInfo{FsID: item.FsID, Path: item.Path, ServerFilename: item.Name, Size: item.Size, Mtime: item.ProviderMtime, MD5: "md5-before"}
	p.lists["/"] = []baiduapi.FileInfo{info}
	p.metadataOverride = func(id int64, call int) *baiduapi.FileInfo {
		if id != item.FsID || call < 2 {
			return nil
		}
		changed := info
		changed.MD5 = "md5-after"
		return &changed
	}
	state := taskstore.CloudDuplicateScanState{AccountFingerprint: d.getClient().Fingerprint(), RunID: "md5-resume", Status: "running", Phase: "hashing", TotalFiles: 1, TotalBytes: item.Size, StartedAtUTC: time.Now().UTC(), UpdatedAtUTC: time.Now().UTC()}
	if err := initializeDuplicateHashCheckpoint(&state, item); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, firstErr := hashCloudFileResumable(ctx, d.getClient(), item, &state, func() error {
		if state.CurrentBytes >= duplicateHashCheckpointBytes {
			cancel()
		}
		return nil
	})
	if firstErr == nil || state.CurrentBytes < duplicateHashCheckpointBytes || state.CurrentContentMD5 != "md5-before" {
		t.Fatalf("first checkpoint did not retain provider MD5: bytes=%d md5=%q err=%v", state.CurrentBytes, state.CurrentContentMD5, firstErr)
	}
	var changed *cloudInventoryIdentityChanged
	_, resumeErr := hashCloudFileResumable(context.Background(), d.getClient(), item, &state, func() error { return nil })
	if !errors.As(resumeErr, &changed) || changed.current.MD5 != "md5-after" {
		t.Fatalf("resume accepted changed content MD5: err=%v", resumeErr)
	}
}

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

func TestDuplicateScanIdentityRejectsChangedProviderMD5(t *testing.T) {
	cached := taskstore.CloudInventoryEntry{FsID: 91, Path: "/file.bin", Name: "file.bin", Size: 10, ProviderMtime: 12, ContentMD5: "old-provider-md5"}
	live := baiduapi.FileInfo{FsID: 91, Path: "/file.bin", ServerFilename: "file.bin", Size: 10, Mtime: 12, MD5: "new-provider-md5"}
	if sameInventoryIdentity(cached, live) {
		t.Fatal("duplicate hash scan accepted changed provider MD5 as the same content identity")
	}
}

func prepareDuplicateDriftRun(t *testing.T, content http.HandlerFunc) (*Downloader, *provider, *taskstore.Store, taskstore.CloudInventoryEntry, taskstore.CloudDuplicateScanState) {
	t.Helper()
	d, p := setup(t, content)
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	d.queue.store = store
	account := d.getClient().Fingerprint()
	now := time.Now().UTC()
	old := taskstore.CloudInventoryEntry{FsID: 802, Path: "/old/a.bin", Name: "a.bin", Size: 7, ProviderMtime: 21, ContentMD5: "old-md5"}
	peer := taskstore.CloudInventoryEntry{FsID: 801, Path: "/peer.bin", Name: "peer.bin", Size: 7, ProviderMtime: 20, ContentMD5: "peer-md5"}
	if err = store.UpsertCloudInventoryPage(account, "inventory", "/", []taskstore.CloudInventoryEntry{{FsID: 800, Path: "/old", Name: "old", Directory: true}, peer}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage(account, "inventory", "/old", []taskstore.CloudInventoryEntry{old}, now, nil); err != nil {
		t.Fatal(err)
	}
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 800, Path: "/old", ServerFilename: "old", IsDir: 1}, {FsID: peer.FsID, Path: peer.Path, ServerFilename: peer.Name, Size: peer.Size, Mtime: peer.ProviderMtime, MD5: peer.ContentMD5}}
	p.lists["/old"] = []baiduapi.FileInfo{{FsID: old.FsID, Path: old.Path, ServerFilename: old.Name, Size: old.Size, Mtime: old.ProviderMtime, MD5: old.ContentMD5}}
	if err = store.SaveCloudScanState(account, "inventory", "complete", "complete", "/", 0, 2, 3, now, now, ""); err != nil {
		t.Fatal(err)
	}
	state := taskstore.CloudDuplicateScanState{AccountFingerprint: account, RunID: "drift-run", Status: "running", Phase: "hashing", NextFsID: peer.FsID, TotalFiles: 2, ProcessedFiles: 1, TotalBytes: 14, ProcessedBytes: 7, StartedAtUTC: now, UpdatedAtUTC: now}
	if err = initializeDuplicateHashCheckpoint(&state, old); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudDuplicateScanState(state, false); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	return d, p, store, old, state
}

func TestDuplicateScanReconcilesIdentityChangeBeforeDownloading(t *testing.T) {
	var contentCalls atomic.Int32
	d, p, store, old, state := prepareDuplicateDriftRun(t, func(w http.ResponseWriter, r *http.Request) {
		contentCalls.Add(1)
		_, _ = w.Write([]byte("payload"))
	})
	changed := baiduapi.FileInfo{FsID: old.FsID, Path: "/new/renamed.bin", ServerFilename: "renamed.bin", Size: 8, Mtime: 22, MD5: "new-md5"}
	p.metadataOverride = func(id int64, _ int) *baiduapi.FileInfo {
		if id == old.FsID {
			return &changed
		}
		return nil
	}
	d.runCloudDuplicateScan(context.Background(), state, func() {})
	got, err := store.LoadCloudDuplicateScanState(state.AccountFingerprint)
	if err != nil || got.Status != "paused" || got.Phase != "inventory_changed" || got.NextFsID != old.FsID-1 || got.ProcessedFiles != state.ProcessedFiles || got.ProcessedBytes != state.ProcessedBytes || got.CurrentFsID != 0 {
		t.Fatalf("identity drift did not pause at the unprocessed candidate: %+v err=%v", got, err)
	}
	p.mu.Lock()
	metadataCalls := p.metadataCalls[old.FsID]
	p.mu.Unlock()
	if contentCalls.Load() != 0 || metadataCalls != 1 {
		t.Fatalf("candidate drift unexpectedly downloaded content or made extra metadata calls: content=%d metadata=%d", contentCalls.Load(), metadataCalls)
	}
	entries, directory, _, err := store.ListCloudInventory(state.AccountFingerprint, "/new", 0, 50)
	if err != nil || len(entries) != 1 || entries[0].FsID != old.FsID || entries[0].Path != changed.Path || entries[0].ContentMD5 != changed.MD5 || directory.Status != "stale" {
		oldEntries, oldDirectory, _, oldErr := store.ListCloudInventory(state.AccountFingerprint, "/old", 0, 50)
		t.Fatalf("live identity was not reconciled: entries=%+v directory=%+v err=%v old=%+v old-directory=%+v old-err=%v", entries, directory, err, oldEntries, oldDirectory, oldErr)
	}
}

func TestDuplicateScanDiscardsDigestWhenIdentityChangesAfterContentRead(t *testing.T) {
	var contentCalls atomic.Int32
	d, p, store, old, state := prepareDuplicateDriftRun(t, func(w http.ResponseWriter, r *http.Request) {
		contentCalls.Add(1)
		_, _ = w.Write([]byte("payload"))
	})
	changed := baiduapi.FileInfo{FsID: old.FsID, Path: old.Path, ServerFilename: old.Name, Size: old.Size, Mtime: old.ProviderMtime + 1, MD5: "new-md5"}
	p.metadataOverride = func(id int64, call int) *baiduapi.FileInfo {
		if id == old.FsID && call >= 2 {
			return &changed
		}
		return nil
	}
	d.runCloudDuplicateScan(context.Background(), state, func() {})
	got, err := store.LoadCloudDuplicateScanState(state.AccountFingerprint)
	if err != nil || got.Status != "paused" || got.ProcessedFiles != state.ProcessedFiles || got.ProcessedBytes != state.ProcessedBytes || contentCalls.Load() != 1 {
		t.Fatalf("post-read drift state=%+v content calls=%d err=%v", got, contentCalls.Load(), err)
	}
	entries, directory, _, listErr := store.ListCloudInventory(state.AccountFingerprint, "/old", 0, 50)
	if listErr != nil || len(entries) != 1 || entries[0].ProviderMtime != changed.Mtime || directory.Status != "stale" {
		t.Fatalf("live metadata was not applied/invalidated: entries=%+v directory=%+v err=%v", entries, directory, listErr)
	}
}

func TestDuplicateScanMissingIdentityPreservesCacheAndVerifiedHash(t *testing.T) {
	d, p, store, old, state := prepareDuplicateDriftRun(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	if err := store.SaveVerifiedCloudContentHash(state.AccountFingerprint, old, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.metadataMissing[old.FsID] = 1
	p.mu.Unlock()
	d.runCloudDuplicateScan(context.Background(), state, func() {})
	got, err := store.LoadCloudDuplicateScanState(state.AccountFingerprint)
	if err != nil || got.Status != "paused" || got.ProcessedFiles != state.ProcessedFiles || got.NextFsID != state.NextFsID {
		t.Fatalf("missing identity did not preserve/stop scan: %+v err=%v", got, err)
	}
	entries, directory, _, err := store.ListCloudInventory(state.AccountFingerprint, "/old", 0, 50)
	if err != nil || len(entries) != 1 || entries[0].Path != old.Path || entries[0].ContentMD5 != old.ContentMD5 || directory.Status != "stale" {
		t.Fatalf("ambiguous identity changed cache: entries=%+v directory=%+v err=%v", entries, directory, err)
	}
}

func TestDuplicateScanMissingDownloadLinkPausesWithoutTreatingFileAsDeleted(t *testing.T) {
	var contentCalls atomic.Int32
	d, p, store, old, state := prepareDuplicateDriftRun(t, func(w http.ResponseWriter, r *http.Request) {
		contentCalls.Add(1)
		_, _ = w.Write([]byte("payload"))
	})
	p.mu.Lock()
	p.downloadLinkMissing[old.FsID] = 1
	p.mu.Unlock()
	d.runCloudDuplicateScan(context.Background(), state, func() {})
	got, err := store.LoadCloudDuplicateScanState(state.AccountFingerprint)
	if err != nil || got.Status != "paused" || got.ProcessedFiles != state.ProcessedFiles || got.NextFsID != state.NextFsID || contentCalls.Load() != 0 {
		t.Fatalf("missing download link did not pause before content: state=%+v content=%d err=%v", got, contentCalls.Load(), err)
	}
	entries, directory, _, err := store.ListCloudInventory(state.AccountFingerprint, "/old", 0, 20)
	if err != nil || len(entries) != 1 || entries[0].Path != old.Path || directory.Status != "stale" {
		t.Fatalf("missing download link was treated as deletion: entries=%+v directory=%+v err=%v", entries, directory, err)
	}
}

func TestDuplicateScanResumeAfterInventoryRefreshReestimatesCandidates(t *testing.T) {
	d, p, store, old, state := prepareDuplicateDriftRun(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	changed := baiduapi.FileInfo{FsID: old.FsID, Path: "/old/renamed.bin", ServerFilename: "renamed.bin", Size: 8, Mtime: old.ProviderMtime + 1, MD5: "new-md5"}
	p.metadataOverride = func(id int64, _ int) *baiduapi.FileInfo {
		if id == old.FsID {
			return &changed
		}
		return nil
	}
	d.runCloudDuplicateScan(context.Background(), state, func() {})
	if _, err := d.StartCloudDuplicateScan(context.Background()); err == nil {
		t.Fatal("resumed duplicate scan while the observed directory was still stale")
	}
	refreshed := taskstore.CloudInventoryEntry{FsID: old.FsID, Path: changed.Path, Name: changed.ServerFilename, Size: changed.Size, ProviderMtime: changed.Mtime, ContentMD5: changed.MD5}
	if err := store.UpsertCloudInventoryPage(state.AccountFingerprint, "directory-refresh", "/old", []taskstore.CloudInventoryEntry{refreshed}, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	resumed, err := d.StartCloudDuplicateScan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.PauseCloudDuplicateScan()
	if resumed.TotalFiles != state.ProcessedFiles || resumed.TotalBytes != state.ProcessedBytes || resumed.ProcessedFiles != state.ProcessedFiles || resumed.ProcessedBytes != state.ProcessedBytes || resumed.NextFsID != state.NextFsID {
		t.Fatalf("resume retained stale candidate totals or advanced the changed item: %+v", resumed)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, statusErr := d.CloudDuplicateScanStatus()
		if statusErr != nil {
			t.Fatal(statusErr)
		}
		if current.Status == "complete" || current.Status == "paused" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("resumed duplicate scan did not reach a stable state")
}

func TestDuplicateScanStatusIsIdleBeforeFirstRun(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))

	state, err := d.CloudDuplicateScanStatus()
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "idle" || state.Phase != "idle" || state.AccountFingerprint != d.getClient().Fingerprint() {
		t.Fatalf("unexpected initial duplicate scan state: %+v", state)
	}
}

func TestDuplicateScanResumeKeepsCumulativeFileAndByteProgress(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	files := []baiduapi.FileInfo{{FsID: 711, Path: "/a.bin", ServerFilename: "a.bin", Size: 7, Mtime: 21}, {FsID: 712, Path: "/b.bin", ServerFilename: "b.bin", Size: 7, Mtime: 22}}
	p.lists["/"] = files
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
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
	state := taskstore.CloudDuplicateScanState{AccountFingerprint: account, RunID: "resume-run", Status: "paused", Phase: "paused", NextFsID: 711, TotalFiles: 2, ProcessedFiles: 1, TotalBytes: 999, ProcessedBytes: 7, StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	resumed, err := d.StartCloudDuplicateScan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.PauseCloudDuplicateScan()
	if resumed.TotalFiles != 2 || resumed.ProcessedFiles != 1 || resumed.TotalBytes != 14 || resumed.ProcessedBytes != 7 {
		t.Fatalf("resume reset cumulative progress: %+v", resumed)
	}
}

func TestDuplicateScanEstimateAndStartRejectInvalidatedInventory(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	now := time.Now().UTC()
	account := d.getClient().Fingerprint()
	if err = store.SaveCloudScanState(account, "inventory", "complete", "complete", "/", 0, 1, 10, now, now, ""); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	if _, err = d.CloudDuplicateScanEstimate(); err == nil {
		t.Fatal("duplicate scan estimate accepted an invalidated inventory")
	}
	if _, err = d.StartCloudDuplicateScan(context.Background()); err == nil {
		t.Fatal("duplicate content scan started against an invalidated inventory")
	}
}

func TestDuplicateScanRejectsActiveDirectoryRefresh(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	now := time.Now().UTC()
	account := d.getClient().Fingerprint()
	if err = store.UpsertCloudInventoryPage(account, "inventory", "/", []taskstore.CloudInventoryEntry{{FsID: 1, Path: "/file.bin", Name: "file.bin", Size: 4}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState(account, "inventory", "complete", "complete", "/", 0, 1, 1, now, now, ""); err != nil {
		t.Fatal(err)
	}
	refresh := taskstore.CloudInventoryRefreshState{AccountFingerprint: account, RunID: "refresh-active", RootPath: "/", CurrentDirectory: "/", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&refresh); err != nil {
		t.Fatal(err)
	}
	if _, err = d.StartCloudDuplicateScan(context.Background()); err == nil {
		d.PauseCloudDuplicateScan()
		t.Fatal("duplicate hashing started while a directory cache refresh was active")
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
	if err != nil || state.Status != "complete" || state.ProcessedFiles != 2 || state.TotalFiles != 2 || state.ProcessedBytes != 14 || state.TotalBytes != 14 {
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

func TestDuplicateTerminalFailureClearsRetryEvidence(t *testing.T) {
	d, _ := setup(t, nil)
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	now := time.Now().UTC()
	retryAt := now.Add(time.Minute)
	state := taskstore.CloudDuplicateScanState{
		AccountFingerprint:     d.getClient().Fingerprint(),
		RunID:                  "failed-after-retry",
		Status:                 "running",
		Phase:                  "hashing",
		TotalFiles:             1,
		StartedAtUTC:           now,
		UpdatedAtUTC:           now,
		RetryDelayMillis:       10_000,
		RetryBackoffBaseMillis: 20_000,
		RetryAtUTC:             &retryAt,
	}
	if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
		t.Fatal(err)
	}
	d.failDuplicateScan(&state, errors.New("permanent listing error"))

	got, err := store.LoadCloudDuplicateScanState(state.AccountFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" || got.RetryDelayMillis != 0 || got.RetryBackoffBaseMillis != 0 || got.RetryAtUTC != nil {
		t.Fatalf("terminal failure retained retry evidence: %+v", got)
	}
}

func TestDuplicateScanPersistsRetryAfterDeadlineBeforeWaiting(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	d.duplicateScanRetryBase = 5 * time.Second
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 701, Path: "/a.bin", ServerFilename: "a.bin", Size: 7, Mtime: 11}, {FsID: 702, Path: "/b.bin", ServerFilename: "b.bin", Size: 7, Mtime: 12}}
	p.mu.Lock()
	p.metadataFailures[701] = 1
	p.metadataRetryAfter[701] = "10"
	p.mu.Unlock()
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	now := time.Now().UTC()
	account := d.getClient().Fingerprint()
	entries := []taskstore.CloudInventoryEntry{{FsID: 701, Path: "/a.bin", Name: "a.bin", Size: 7, ProviderMtime: 11}, {FsID: 702, Path: "/b.bin", Name: "b.bin", Size: 7, ProviderMtime: 12}}
	if err = store.UpsertCloudInventoryPage(account, "inventory", "/", entries, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState(account, "inventory", "complete", "complete", "/", 0, 1, 2, now, now, ""); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	if _, err = d.StartCloudDuplicateScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var state taskstore.CloudDuplicateScanState
	for time.Now().Before(deadline) {
		state, err = d.CloudDuplicateScanStatus()
		if err != nil {
			t.Fatal(err)
		}
		if state.Phase == "backoff" && state.RetryAtUTC != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if state.Phase != "backoff" || state.RetryAtUTC == nil {
		d.PauseCloudDuplicateScan()
		t.Fatalf("duplicate scan did not persist a retry wait: %+v", state)
	}
	d.PauseCloudDuplicateScan()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err = d.CloudDuplicateScanStatus()
		if err == nil && state.Status == "paused" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "paused" || state.RetryDelayMillis < 10_000 || state.RetryAtUTC == nil || state.RetryAtUTC.Before(time.Now().UTC().Add(8*time.Second)) {
		t.Fatalf("duplicate scan ignored/prematurely cleared Retry-After: state=%+v err=%v", state, err)
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
	// Keep enough margin for closing/reopening SQLite and running under the
	// full downloader suite; a sub-second deadline can expire before Start.
	retryAt := time.Now().UTC().Add(5 * time.Second)
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
	deadline := time.Now().Add(20 * time.Second)
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

func TestDuplicateRetryBackoffBaseSurvivesRestart(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	d.duplicateScanRetryBase = 10 * time.Millisecond
	files := []baiduapi.FileInfo{{FsID: 711, Path: "/a.bin", ServerFilename: "a.bin", Size: 7, Mtime: 21}, {FsID: 712, Path: "/b.bin", ServerFilename: "b.bin", Size: 7, Mtime: 22}}
	p.lists["/"] = files
	storeDir := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue")
	store, err := taskstore.Open(storeDir, d.cfg.QueueDir)
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
	retryAt := time.Now().UTC().Add(150 * time.Millisecond)
	state := taskstore.CloudDuplicateScanState{AccountFingerprint: account, RunID: "persisted-backoff", Status: "paused", Phase: "backoff", RetryDelayMillis: 5, RetryBackoffBaseMillis: 20, RetryAtUTC: &retryAt, TotalFiles: 2, StartedAtUTC: now, UpdatedAtUTC: now, LastError: "temporary rate limit"}
	if err = store.SaveCloudDuplicateScanState(state, true); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = taskstore.Open(storeDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	p.mu.Lock()
	p.metadataFailures[711] = 1
	p.metadataRetryAfter[711] = "1"
	p.mu.Unlock()
	if _, err = d.StartCloudDuplicateScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		loaded, loadErr := d.CloudDuplicateScanStatus()
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if loaded.RetryBackoffBaseMillis == 40 && loaded.RetryDelayMillis == 1000 && loaded.RetryAtUTC != nil {
			d.PauseCloudDuplicateScan()
			pauseDeadline := time.Now().Add(time.Second)
			for time.Now().Before(pauseDeadline) {
				paused, pauseErr := d.CloudDuplicateScanStatus()
				if pauseErr != nil {
					t.Fatal(pauseErr)
				}
				if paused.Status == "paused" {
					if paused.RetryBackoffBaseMillis != 40 || paused.RetryDelayMillis != 1000 || paused.RetryAtUTC == nil {
						t.Fatalf("pause lost persisted retry backoff: %+v", paused)
					}
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			t.Fatal("duplicate scan did not pause during persisted retry wait")
		}
		time.Sleep(5 * time.Millisecond)
	}
	loaded, loadErr := d.CloudDuplicateScanStatus()
	t.Fatalf("duplicate scan did not persist next backoff after resumed failure: state=%+v err=%v", loaded, loadErr)
}
