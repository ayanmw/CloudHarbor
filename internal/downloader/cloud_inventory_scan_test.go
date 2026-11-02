package downloader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestCloudInventoryScanStatusReportsPersistedWorkerInactiveAfterReopen(t *testing.T) {
	d, _ := setup(t, nil)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	queueDir := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue")
	store, err := taskstore.Open(queueDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	now := time.Now().UTC()
	state := taskstore.CloudScanState{AccountFingerprint: d.getClient().Fingerprint(), RunID: "restart-worker-state", CurrentDirectory: "/", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = taskstore.Open(queueDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	status, err := d.CloudInventoryScanStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.InventoryCoverage == nil || status.InventoryCoverage.Status != "scanning" || status.InventoryCoverage.IncompleteDirectories == 0 {
		t.Fatalf("persisted run status must include independently computed current coverage: %+v", status.InventoryCoverage)
	}
	body, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if active, ok := payload["worker_active"].(bool); !ok || active {
		t.Fatalf("reopened persisted running full-scan checkpoint must report worker_active=false: %s", body)
	}
}

func TestCloudInventoryScanStatusReportsActiveWorker(t *testing.T) {
	d, p := setup(t, nil)
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	entered, release := make(chan struct{}), make(chan struct{})
	p.beforeList = func(string) {
		close(entered)
		<-release
	}
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("full-scan worker did not reach provider listing")
	}
	status, err := d.CloudInventoryScanStatus()
	if err != nil || status.Status != "running" {
		close(release)
		t.Fatalf("active full-scan status=%+v err=%v", status, err)
	}
	body, err := json.Marshal(status)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if active, ok := payload["worker_active"].(bool); !ok || !active {
		t.Fatalf("active full-scan worker must report worker_active=true: %s", body)
	}
	// Several paged requests and bounded boundary restarts may exceed five
	// seconds when the full downloader suite is under load; poll the state
	// transition instead of treating normal API pacing as a scan failure.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		status, err = d.CloudInventoryScanStatus()
		if err == nil && status.Status == "complete" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("full scan did not complete after provider release: state=%+v err=%v", status, err)
}

func TestCloudInventoryRefreshStatusReportsPersistedWorkerInactiveAfterReopen(t *testing.T) {
	d, _ := setup(t, nil)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	queueDir := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue")
	store, err := taskstore.Open(queueDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	now := time.Now().UTC()
	state := taskstore.CloudInventoryRefreshState{AccountFingerprint: d.getClient().Fingerprint(), RunID: "restart-worker-state", RootPath: "/target", CurrentDirectory: "/target", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = taskstore.Open(queueDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	status, err := d.CloudInventoryRefreshStatus()
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if active, ok := payload["worker_active"].(bool); !ok || active {
		t.Fatalf("reopened persisted running checkpoint must report worker_active=false: %s", body)
	}
}

func TestCloudInventoryRefreshStatusReportsActiveWorker(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	entered, release := make(chan struct{}), make(chan struct{})
	p.beforeList = func(string) {
		close(entered)
		<-release
	}
	if _, err = d.StartCloudInventoryRefresh(context.Background(), "/target", 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh worker did not reach provider listing")
	}
	state, err := d.CloudInventoryRefreshStatus()
	if err != nil || state.Status != "running" || !state.WorkerActive {
		t.Fatalf("active refresh worker was not reported: state=%+v err=%v", state, err)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryRefreshStatus()
		if err == nil && state.Status == "complete" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("refresh did not complete after provider release: state=%+v err=%v", state, err)
}

func TestUTCInventoryRetryWaitRechecksDeadlineAgainstWallClock(t *testing.T) {
	now := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	deadline := now.Add(time.Hour)
	if got := utcInventoryRetryWait(deadline, now); got != 30*time.Second {
		t.Fatalf("far deadline wait=%s, want 30s recheck", got)
	}
	if got := utcInventoryRetryWait(deadline, deadline.Add(time.Minute)); got != 0 {
		t.Fatalf("deadline after wall-clock jump wait=%s, want immediate retry", got)
	}
	if got := utcInventoryRetryWait(deadline, deadline.Add(-5*time.Second)); got != 5*time.Second {
		t.Fatalf("backward clock adjustment wait=%s, want remaining deadline", got)
	}
}

func TestInventoryScanAdmissionRechecksMaintenanceAfterInitialCheck(t *testing.T) {
	d, _ := setup(t, nil)
	checked := make(chan struct{})
	resume := make(chan struct{})
	d.inventoryScanAdmissionHook = func() {
		close(checked)
		<-resume
	}
	result := make(chan error, 1)
	go func() {
		_, err := d.StartCloudInventoryScan(context.Background(), 0)
		result <- err
	}()
	select {
	case <-checked:
	case <-time.After(time.Second):
		close(resume)
		t.Fatal("scan did not reach the admission window")
	}
	if err := d.TryBeginMaintenance(); err != nil {
		close(resume)
		t.Fatalf("begin path maintenance: %v", err)
	}
	close(resume)
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "维护") {
			t.Fatalf("scan admission after maintenance=%v, want maintenance rejection", err)
		}
	case <-time.After(time.Second):
		t.Fatal("scan admission did not return")
	}
	d.inventoryScanMutex.Lock()
	started := d.inventoryScanStarting || d.inventoryScanCancel != nil
	d.inventoryScanMutex.Unlock()
	d.EndMaintenance()
	if started {
		t.Fatal("scan was admitted while path maintenance owned the queue")
	}
}

func TestDirectoryRefreshRejectsPersistedActiveDuplicateScan(t *testing.T) {
	d, _ := setup(t, nil)
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	account := d.getClient().Fingerprint()
	now := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage(account, "inventory", "/", nil, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState(account, "inventory", "complete", "complete", "/", 0, 0, 0, now, now, ""); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudDuplicateScanState(taskstore.CloudDuplicateScanState{AccountFingerprint: account, RunID: "hash-active", Status: "running", Phase: "hashing", StartedAtUTC: now, UpdatedAtUTC: now}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = d.StartCloudInventoryRefresh(context.Background(), "/", 0); err == nil {
		t.Fatal("directory cache refresh started while a persisted duplicate hash scan was active")
	}
}

func TestFullInventoryScanRejectsPersistedActiveDuplicateScan(t *testing.T) {
	d, _ := setup(t, nil)
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	account := d.getClient().Fingerprint()
	now := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage(account, "inventory", "/", nil, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState(account, "inventory", "complete", "complete", "/", 0, 0, 0, now, now, ""); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudDuplicateScanState(taskstore.CloudDuplicateScanState{AccountFingerprint: account, RunID: "hash-active", Status: "running", Phase: "hashing", StartedAtUTC: now, UpdatedAtUTC: now}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err == nil {
		t.Fatal("full inventory scan started while a persisted duplicate hash scan was active")
	}
}

func TestCloudInventoryScannerPersistsRecursiveListingAndLocalSize(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	p.lists["/"] = []baiduapi.FileInfo{
		{FsID: 1, Path: "/folder", ServerFilename: "folder", IsDir: 1},
		{FsID: 2, Path: "/root.bin", ServerFilename: "root.bin", Size: 4},
	}
	p.lists["/folder"] = []baiduapi.FileInfo{{FsID: 3, Path: "/folder/file.bin", ServerFilename: "file.bin", Size: 7}}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var state taskstore.CloudScanState
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryScanStatus()
		if err == nil && state.Status == "complete" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || state.Status != "complete" {
		t.Fatalf("scan state=%+v err=%v", state, err)
	}
	size, err := store.CloudDirectorySize(d.getClient().Fingerprint(), "/")
	if err != nil || !size.Complete || size.Bytes != 11 || size.Files != 2 || size.Directories != 1 {
		t.Fatalf("local root size=%+v err=%v", size, err)
	}
}

func TestCloudInventoryScanRequestBudgetPausesAndResumesWithAdditionalQuota(t *testing.T) {
	d, p := setup(t, nil)
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 41, Path: "/sub", ServerFilename: "sub", IsDir: 1}}
	p.lists["/sub"] = []baiduapi.FileInfo{{FsID: 42, Path: "/sub/file.bin", ServerFilename: "file.bin", Size: 3}}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	if _, err = d.StartCloudInventoryScan(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var state taskstore.CloudScanState
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryScanStatus()
		if err == nil && state.Status == "paused" && state.Phase == "request_budget_reached" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "paused" || state.RequestBudget != 1 || state.RequestsUsed != 1 || state.PendingCount != 1 || state.CurrentDirectory != "/sub" {
		t.Fatalf("budget checkpoint=%+v err=%v", state, err)
	}
	p.mu.Lock()
	calls := append([]string(nil), p.listRequests...)
	p.mu.Unlock()
	if !reflect.DeepEqual(calls, []string{"/"}) {
		t.Fatalf("requests exceeded budget: %v", calls)
	}
	if _, err = d.StartCloudInventoryScan(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryScanStatus()
		if err == nil && state.Status == "complete" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "complete" || state.RequestBudget != 2 || state.RequestsUsed != 2 {
		t.Fatalf("resumed scan=%+v err=%v", state, err)
	}
}

func TestCloudInventoryScanResumesPersistedPageBoundaryAfterReopen(t *testing.T) {
	d, p := setup(t, nil)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	items := make([]baiduapi.FileInfo, 105)
	firstPage := make([]taskstore.CloudInventoryEntry, 100)
	for i := range items {
		name := fmt.Sprintf("item-%03d", i)
		item := baiduapi.FileInfo{FsID: int64(1000 + i), Path: "/" + name, ServerFilename: name, Size: int64(i + 1), Mtime: int64(i + 10)}
		items[i] = item
		if i < len(firstPage) {
			firstPage[i] = taskstore.CloudInventoryEntry{FsID: item.FsID, Path: item.Path, Name: item.ServerFilename, Size: item.Size, ProviderMtime: item.Mtime}
		}
	}
	p.lists["/"] = items
	queueDir := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue")
	store, err := taskstore.Open(queueDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	now := time.Now().UTC()
	state := taskstore.CloudScanState{AccountFingerprint: d.getClient().Fingerprint(), RunID: "resume-boundary", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	state.NextPageBoundary = cloudInventoryPageBoundary(items[100])
	nextOffset := 100
	if err = store.UpsertCloudInventoryScanPage(&state, "/", firstPage, now, &nextOffset, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = taskstore.Open(queueDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	resumed, err := d.StartCloudInventoryScan(context.Background(), 0)
	if err != nil || resumed.NextOffset != 100 || !resumed.WorkerActive {
		t.Fatalf("full scan did not resume its persisted boundary: state=%+v err=%v", resumed, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var status taskstore.CloudScanState
	for time.Now().Before(deadline) {
		status, err = d.CloudInventoryScanStatus()
		if err == nil && status.Status == "complete" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || status.Status != "complete" {
		t.Fatalf("full scan did not complete after boundary recovery: state=%+v err=%v", status, err)
	}
	entries, directory, total, err := store.ListCloudInventory(status.AccountFingerprint, "/", 0, 200)
	if err != nil || directory.Status != "complete" || total != len(items) || len(entries) != len(items) {
		t.Fatalf("recovered inventory has wrong coverage: status=%s total=%d entries=%d err=%v", directory.Status, total, len(entries), err)
	}
	p.mu.Lock()
	requests := append([]string(nil), p.listRequests...)
	p.mu.Unlock()
	if !reflect.DeepEqual(requests, []string{"/"}) {
		t.Fatalf("boundary resume made unexpected provider page requests: %v", requests)
	}
}

func TestCloudInventoryScanRejectsChangedPersistedBoundaryAfterReopen(t *testing.T) {
	d, p := setup(t, nil)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	items := make([]baiduapi.FileInfo, 105)
	firstPage := make([]taskstore.CloudInventoryEntry, 100)
	for i := range items {
		name := fmt.Sprintf("item-%03d", i)
		item := baiduapi.FileInfo{FsID: int64(3000 + i), Path: "/" + name, ServerFilename: name, Size: int64(i + 1), Mtime: int64(i + 10)}
		items[i] = item
		if i < len(firstPage) {
			firstPage[i] = taskstore.CloudInventoryEntry{FsID: item.FsID, Path: item.Path, Name: item.ServerFilename, Size: item.Size, ProviderMtime: item.Mtime}
		}
	}
	p.lists["/"] = items
	queueDir := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue")
	store, err := taskstore.Open(queueDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	now := time.Now().UTC()
	state := taskstore.CloudScanState{AccountFingerprint: d.getClient().Fingerprint(), RunID: "resume-drift-boundary", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	state.NextPageBoundary = cloudInventoryPageBoundary(items[100])
	nextOffset := 100
	if err = store.UpsertCloudInventoryScanPage(&state, "/", firstPage, now, &nextOffset, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	// The provider item changes while the process is down; the reopened
	// checkpoint must retain enough identity evidence to reject the next page.
	p.lists["/"][100].Size++
	store, err = taskstore.Open(queueDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var resumed taskstore.CloudScanState
	for time.Now().Before(deadline) {
		resumed, err = d.CloudInventoryScanStatus()
		if err == nil && resumed.Status == "complete" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || resumed.Status != "complete" || resumed.BoundaryRestartCount != 1 {
		t.Fatalf("reopened scan did not recover from the changed persisted boundary: state=%+v err=%v", resumed, err)
	}
	_, directory, count, err := store.ListCloudInventory(resumed.AccountFingerprint, "/", 0, 200)
	if err != nil || directory.Status != "complete" || count != len(p.lists["/"]) {
		t.Fatalf("reopened scan did not publish the rechecked directory: status=%s count=%d err=%v", directory.Status, count, err)
	}
}

func TestInventoryScannersRecheckWhenPageBoundaryMoves(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		for _, drift := range []string{"insert-before-boundary", "delete-boundary", "replace-boundary-identity", "duplicate-within-page", "duplicate-across-pages"} {
			name := "full scan"
			if refresh {
				name = "directory refresh"
			}
			t.Run(name+"/"+drift, func(t *testing.T) {
				d, p := setup(t, nil)
				directory := "/"
				if refresh {
					directory = "/target"
				}
				items := make([]baiduapi.FileInfo, 105)
				for i := range items {
					name := fmt.Sprintf("item-%03d", i)
					items[i] = baiduapi.FileInfo{FsID: int64(1000 + i), Path: path.Join(directory, name), ServerFilename: name, Size: int64(i + 1), Mtime: int64(i + 10)}
				}
				p.lists[directory] = items
				var pageCalls atomic.Int32
				p.beforeList = func(requested string) {
					if requested != directory {
						return
					}
					call := pageCalls.Add(1)
					if call != 2 && call != 4 {
						return
					}
					p.mu.Lock()
					defer p.mu.Unlock()
					if call == 4 {
						// Once the worker restarts at offset zero, model the provider's
						// listing settling before the retried second page is read.
						if drift == "duplicate-within-page" || drift == "duplicate-across-pages" {
							p.lists[directory][101].FsID = int64(1000 + 101)
						}
						return
					}
					switch drift {
					case "insert-before-boundary":
						inserted := baiduapi.FileInfo{FsID: 9999, Path: path.Join(directory, "inserted"), ServerFilename: "inserted", Size: 1, Mtime: 99}
						p.lists[directory] = append([]baiduapi.FileInfo{inserted}, p.lists[directory]...)
					case "delete-boundary":
						p.lists[directory] = append(p.lists[directory][:100], p.lists[directory][101:]...)
					case "replace-boundary-identity":
						// Keep fs_id stable while changing metadata to verify the full
						// persisted boundary identity, not only its primary key.
						p.lists[directory][100].Size++
						p.lists[directory][100].Mtime++
						p.lists[directory][100].MD5 = "changed-content"
					case "duplicate-within-page":
						p.lists[directory][101].FsID = p.lists[directory][100].FsID
					case "duplicate-across-pages":
						p.lists[directory][101].FsID = p.lists[directory][50].FsID
					}
				}
				store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				d.queue.store = store
				if refresh {
					if _, err = d.StartCloudInventoryRefresh(context.Background(), directory, 0); err != nil {
						t.Fatal(err)
					}
				} else if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					var stateStatus string
					var nextOffset int64
					var boundary *taskstore.CloudInventoryPageBoundary
					if refresh {
						state, statusErr := d.CloudInventoryRefreshStatus()
						if statusErr != nil {
							t.Fatal(statusErr)
						}
						stateStatus, nextOffset, boundary = state.Status, state.NextOffset, state.NextPageBoundary
					} else {
						state, statusErr := d.CloudInventoryScanStatus()
						if statusErr != nil {
							t.Fatal(statusErr)
						}
						stateStatus, nextOffset, boundary = state.Status, state.NextOffset, state.NextPageBoundary
					}
					if stateStatus == "complete" {
						_ = nextOffset
						_ = boundary
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if refresh {
					state, statusErr := d.CloudInventoryRefreshStatus()
					if statusErr != nil || state.Status != "complete" || state.BoundaryRestartCount != 1 {
						t.Fatalf("scoped refresh did not recover from page drift: state=%+v err=%v", state, statusErr)
					}
					if state.ItemsProcessed < 100 || state.PagesProcessed < 2 {
						t.Fatalf("refresh did not commit rechecked pages: items=%d pages=%d", state.ItemsProcessed, state.PagesProcessed)
					}
					events, eventErr := store.ListCloudInventoryRefreshEvents(state.AccountFingerprint, state.RunID, "boundary_restart", 0, 10)
					wantReason := "page_boundary_mismatch"
					if drift == "duplicate-within-page" {
						wantReason = "provider_duplicate_page_id"
					} else if drift == "duplicate-across-pages" {
						wantReason = "staged_page_duplicate_id"
					}
					var wantFSID int64
					if drift == "duplicate-within-page" {
						wantFSID = 1100
					} else if drift == "duplicate-across-pages" {
						wantFSID = 1050
					}
					if eventErr != nil || len(events) != 1 || events[0].RestartReason != wantReason || events[0].RestartOffset == nil || *events[0].RestartOffset != 100 || (wantFSID > 0 && (events[0].RestartFSID == nil || *events[0].RestartFSID != wantFSID)) || (wantFSID == 0 && events[0].RestartFSID != nil) {
						t.Fatalf("refresh restart reason=%+v want=%q err=%v", events, wantReason, eventErr)
					}
				} else {
					state, statusErr := d.CloudInventoryScanStatus()
					if statusErr != nil || state.Status != "complete" || state.BoundaryRestartCount != 1 {
						t.Fatalf("full scan did not recover from page drift: state=%+v err=%v", state, statusErr)
					}
					_, directoryState, count, listErr := store.ListCloudInventory(state.AccountFingerprint, directory, 0, 200)
					if listErr != nil || directoryState.Status != "complete" || count != len(p.lists[directory]) {
						t.Fatalf("full scan did not publish rechecked directory: status=%s count=%d err=%v", directoryState.Status, count, listErr)
					}
					events, eventErr := store.ListCloudScanEvents(state.AccountFingerprint, state.RunID, 0, 20)
					wantReason := "page_boundary_mismatch"
					if drift == "duplicate-within-page" {
						wantReason = "provider_duplicate_page_id"
					}
					reasonFound := false
					for _, event := range events {
						if event.Kind == "boundary_restart" && event.RestartReason == wantReason {
							reasonFound = true
						}
					}
					if eventErr != nil || !reasonFound {
						t.Fatalf("full scan restart reason=%+v want=%q err=%v", events, wantReason, eventErr)
					}
				}
				p.mu.Lock()
				requests := len(p.listRequests)
				p.mu.Unlock()
				if requests != 4 {
					t.Fatalf("boundary drift recovery made %d list requests, want four", requests)
				}
			})
		}
	}
}

func TestCloudInventoryScanResetsCheckpointAfterDuplicatePageAndResumes(t *testing.T) {
	d, p := setup(t, nil)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	items := make([]baiduapi.FileInfo, 105)
	for i := range items {
		name := fmt.Sprintf("item-%03d", i)
		items[i] = baiduapi.FileInfo{FsID: int64(2000 + i), Path: "/" + name, ServerFilename: name, Size: int64(i + 1), Mtime: int64(i + 10)}
	}
	p.lists["/"] = append([]baiduapi.FileInfo(nil), items...)
	var pageCalls atomic.Int32
	p.beforeList = func(directory string) {
		if directory != "/" || pageCalls.Add(1) != 2 {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.lists["/"][101].FsID = p.lists["/"][100].FsID
	}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	// Rechecking permits two paced boundary restarts before the third drift fails closed.
	deadline := time.Now().Add(15 * time.Second)
	var state taskstore.CloudScanState
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryScanStatus()
		if err == nil && state.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "failed" {
		t.Fatalf("duplicate page did not fail closed: state=%+v err=%v", state, err)
	}
	if state.BoundaryRestartCount != taskstore.CloudInventoryBoundaryRestartLimit || state.NextOffset != 100 || state.NextPageBoundary == nil || !strings.Contains(state.LastError, "上限") {
		t.Fatalf("duplicate-page recovery was not bounded with evidence: state=%+v", state)
	}
	p.mu.Lock()
	p.lists["/"] = append([]baiduapi.FileInfo(nil), items...)
	p.beforeList = nil
	p.mu.Unlock()
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryScanStatus()
		if err == nil && state.Status == "complete" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "complete" {
		t.Fatalf("scan did not recover after duplicate page was corrected: state=%+v err=%v", state, err)
	}
	_, directory, total, err := store.ListCloudInventory(state.AccountFingerprint, "/", 0, 200)
	if err != nil || directory.Status != "complete" || total != len(items) {
		t.Fatalf("recovered inventory has wrong coverage: status=%s total=%d err=%v", directory.Status, total, err)
	}
}

func TestCloudInventoryScanResetsCheckpointAfterDuplicateAcrossPages(t *testing.T) {
	d, p := setup(t, nil)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	items := make([]baiduapi.FileInfo, 105)
	for i := range items {
		name := fmt.Sprintf("item-%03d", i)
		items[i] = baiduapi.FileInfo{FsID: int64(3000 + i), Path: "/" + name, ServerFilename: name, Size: int64(i + 1), Mtime: int64(i + 10)}
	}
	p.lists["/"] = append([]baiduapi.FileInfo(nil), items...)
	var pageCalls atomic.Int32
	p.beforeList = func(directory string) {
		if directory != "/" || pageCalls.Add(1) != 2 {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.lists["/"][101].FsID = p.lists["/"][50].FsID
	}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var state taskstore.CloudScanState
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryScanStatus()
		if err == nil && state.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "failed" {
		p.mu.Lock()
		requests := append([]string(nil), p.listRequests...)
		p.mu.Unlock()
		t.Fatalf("cross-page duplicate did not fail closed: state=%+v requests=%v err=%v", state, requests, err)
	}
	if state.BoundaryRestartCount != taskstore.CloudInventoryBoundaryRestartLimit || state.NextOffset != 100 || state.NextPageBoundary == nil || !strings.Contains(state.LastError, "上限") {
		t.Fatalf("cross-page duplicate recovery was not bounded with evidence: state=%+v", state)
	}
}

func TestCloudInventoryScanPreservesCheckpointWhenPageCommitFails(t *testing.T) {
	d, p := setup(t, nil)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	items := make([]baiduapi.FileInfo, 105)
	for i := range items {
		name := fmt.Sprintf("item-%03d", i)
		items[i] = baiduapi.FileInfo{FsID: int64(4000 + i), Path: "/" + name, ServerFilename: name, Size: int64(i + 1), Mtime: int64(i + 10)}
	}
	p.lists["/"] = items
	queueDir := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue")
	store, err := taskstore.Open(queueDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	faultDB, err := sql.Open("sqlite", filepath.Join(queueDir, "queue.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer faultDB.Close()
	// Install before the worker starts so status polling cannot race a schema change.
	if _, err = faultDB.Exec(`CREATE TRIGGER fail_inventory_page_insert BEFORE INSERT ON cloud_inventory WHEN NEW.fs_id>=4100 BEGIN SELECT RAISE(ABORT, 'injected page commit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var state taskstore.CloudScanState
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryScanStatus()
		if err == nil && state.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "failed" {
		t.Fatalf("failed page commit left no terminal checkpoint: state=%+v err=%v", state, err)
	}
	if state.NextOffset != 100 || state.NextPageBoundary == nil || state.NextPageBoundary.FsID != items[100].FsID {
		t.Fatalf("failed page commit did not preserve the prior valid cursor: offset=%d boundary=%+v", state.NextOffset, state.NextPageBoundary)
	}
	if _, err = faultDB.Exec(`DROP TRIGGER fail_inventory_page_insert`); err != nil {
		t.Fatal(err)
	}
	d.inventoryPageCommitHook = nil
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryScanStatus()
		if err == nil && state.Status == "complete" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "complete" {
		t.Fatalf("valid checkpoint did not resume after page commit failure: state=%+v err=%v", state, err)
	}
}

func TestInventoryRetryDelayUsesProviderMinimumAndCapsWait(t *testing.T) {
	if got := inventoryRetryDelay(errors.New("temporary"), 12*time.Second); got != 12*time.Second {
		t.Fatalf("base retry delay=%s, want 12s", got)
	}
	provider := &baiduapi.APIError{Status: http.StatusTooManyRequests, RetryAfter: 45 * time.Second}
	if got := inventoryRetryDelay(provider, 12*time.Second); got != 45*time.Second {
		t.Fatalf("provider retry delay=%s, want 45s", got)
	}
	provider.RetryAfter = 2 * time.Hour
	if got := inventoryRetryDelay(provider, 12*time.Second); got != time.Hour {
		t.Fatalf("capped provider retry delay=%s, want 1h", got)
	}
}

func TestWaitForInventoryRetryHonorsDeadlineAndCancellation(t *testing.T) {
	retryAt := time.Now().Add(40 * time.Millisecond)
	started := time.Now()
	if !waitForInventoryRetry(context.Background(), &retryAt) {
		t.Fatal("active retry wait was cancelled")
	}
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
		t.Fatalf("retry wait returned early after %s", elapsed)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	future := time.Now().Add(time.Hour)
	if waitForInventoryRetry(ctx, &future) {
		t.Fatal("cancelled retry wait reported ready")
	}
}

func TestInventoryWorkersDoNotRequestBeforePersistedRetryDeadline(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		name := "full scan"
		if refresh {
			name = "directory refresh"
		}
		t.Run(name, func(t *testing.T) {
			d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
			p.lists["/"] = nil
			p.lists["/target"] = nil
			dir := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue")
			store, err := taskstore.Open(dir, d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			d.queue.store = store
			requestEntered := make(chan struct{}, 1)
			releaseRequest := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releaseRequest) }) }
			defer release()
			p.mu.Lock()
			p.beforeList = func(directory string) {
				if directory == "/target" || (!refresh && directory == "/") {
					requestEntered <- struct{}{}
					<-releaseRequest
				}
			}
			p.mu.Unlock()
			now := time.Now().UTC()
			// Leave enough time for SQLite close/reopen and worker startup before
			// asserting that the persisted backoff is still in effect.
			retryAt := now.Add(2 * time.Second)
			if refresh {
				state := taskstore.CloudInventoryRefreshState{AccountFingerprint: d.getClient().Fingerprint(), RunID: "refresh-restart", RootPath: "/target", Status: "paused", Phase: "paused", CurrentDirectory: "/target", StartedAtUTC: now, UpdatedAtUTC: now, LastError: "HTTP 429: rate limited", RetryDelayMillis: 2000, RetryAtUTC: &retryAt}
				if err = store.StartCloudInventoryRefresh(&state); err != nil {
					t.Fatal(err)
				}
				if err = store.SaveCloudInventoryRefreshState(state); err != nil {
					t.Fatal(err)
				}
			} else {
				state := taskstore.CloudScanState{AccountFingerprint: d.getClient().Fingerprint(), RunID: "full-restart", Status: "paused", Phase: "paused", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now, LastError: "HTTP 429: rate limited", RetryDelayMillis: 2000, RetryAtUTC: &retryAt}
				if err = store.StartCloudScan(&state); err != nil {
					t.Fatal(err)
				}
				if err = store.SaveCloudScanCheckpoint(state); err != nil {
					t.Fatal(err)
				}
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = taskstore.Open(dir, d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			d.queue.store = store
			if refresh {
				if _, err = d.StartCloudInventoryRefresh(context.Background(), "/target", 0); err != nil {
					t.Fatal(err)
				}
			} else if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
			if refresh {
				resumed, statusErr := d.CloudInventoryRefreshStatus()
				if statusErr != nil || resumed.LastError != "HTTP 429: rate limited" || resumed.Phase != "backoff" || resumed.RetryAtUTC == nil || !resumed.RetryAtUTC.After(time.Now().UTC()) {
					t.Fatalf("refresh resume lost the retry cause: state=%+v err=%v", resumed, statusErr)
				}
			} else {
				resumed, statusErr := d.CloudInventoryScanStatus()
				if statusErr != nil || resumed.LastError != "HTTP 429: rate limited" || resumed.Phase != "backoff" || resumed.RetryAtUTC == nil || !resumed.RetryAtUTC.After(time.Now().UTC()) {
					t.Fatalf("full-scan resume lost the retry cause: state=%+v err=%v", resumed, statusErr)
				}
			}
			p.mu.Lock()
			requestsBeforeDeadline := len(p.listRequests)
			p.mu.Unlock()
			if requestsBeforeDeadline != 0 {
				t.Fatalf("provider received %d requests before persisted deadline", requestsBeforeDeadline)
			}
			select {
			case <-requestEntered:
			case <-time.After(3 * time.Second):
				t.Fatal("provider request did not begin after persisted retry deadline")
			}
			if refresh {
				resumed, statusErr := d.CloudInventoryRefreshStatus()
				if statusErr != nil || resumed.Phase != "listing" || resumed.RetryAtUTC != nil {
					t.Fatalf("refresh did not leave backoff while listing: state=%+v err=%v", resumed, statusErr)
				}
			} else {
				resumed, statusErr := d.CloudInventoryScanStatus()
				if statusErr != nil || resumed.Phase != "listing" || resumed.RetryAtUTC != nil {
					t.Fatalf("full scan did not leave backoff while listing: state=%+v err=%v", resumed, statusErr)
				}
			}
			release()
			deadline := time.Now().Add(4 * time.Second)
			for time.Now().Before(deadline) {
				var status string
				if refresh {
					state, statusErr := d.CloudInventoryRefreshStatus()
					if statusErr == nil {
						status = state.Status
					}
				} else {
					state, statusErr := d.CloudInventoryScanStatus()
					if statusErr == nil {
						status = state.Status
					}
				}
				if status == "complete" {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("inventory worker did not resume after persisted deadline")
		})
	}
}

func TestInventoryRetryBackoffBaseSurvivesRestart(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		name := "full scan"
		if refresh {
			name = "directory refresh"
		}
		t.Run(name, func(t *testing.T) {
			d, p := setup(t, nil)
			storeDir := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue")
			store, err := taskstore.Open(storeDir, d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			d.queue.store = store
			now := time.Now().UTC()
			retryAt := now.Add(150 * time.Millisecond)
			account := d.getClient().Fingerprint()
			if refresh {
				state := taskstore.CloudInventoryRefreshState{AccountFingerprint: account, RunID: "refresh-backoff", RootPath: "/target", Status: "paused", Phase: "paused", CurrentDirectory: "/target", StartedAtUTC: now, UpdatedAtUTC: now, LastError: "rate limited", RetryDelayMillis: 300, RetryBackoffBaseMillis: 600, RetryAtUTC: &retryAt}
				if err = store.StartCloudInventoryRefresh(&state); err != nil {
					t.Fatal(err)
				}
				if err = store.SaveCloudInventoryRefreshState(state); err != nil {
					t.Fatal(err)
				}
			} else {
				state := taskstore.CloudScanState{AccountFingerprint: account, RunID: "full-backoff", Status: "paused", Phase: "paused", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now, LastError: "rate limited", RetryDelayMillis: 300, RetryBackoffBaseMillis: 600, RetryAtUTC: &retryAt}
				if err = store.StartCloudScan(&state); err != nil {
					t.Fatal(err)
				}
				if err = store.SaveCloudScanCheckpoint(state); err != nil {
					t.Fatal(err)
				}
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = taskstore.Open(storeDir, d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			d.queue.store = store
			p.mu.Lock()
			p.listFailures[map[bool]string{true: "/target", false: "/"}[refresh]] = 1
			p.mu.Unlock()
			if refresh {
				if _, err = d.StartCloudInventoryRefresh(context.Background(), "/target", 0); err != nil {
					t.Fatal(err)
				}
			} else if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(4 * time.Second)
			for time.Now().Before(deadline) {
				var backoffBase int64
				if refresh {
					state, statusErr := d.CloudInventoryRefreshStatus()
					if statusErr == nil {
						backoffBase = state.RetryBackoffBaseMillis
					}
				} else {
					state, statusErr := d.CloudInventoryScanStatus()
					if statusErr == nil {
						backoffBase = state.RetryBackoffBaseMillis
					}
				}
				if backoffBase == 1200 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			var backoffBase, retryDelay int64
			if refresh {
				state, statusErr := d.CloudInventoryRefreshStatus()
				if statusErr != nil {
					t.Fatal(statusErr)
				}
				backoffBase, retryDelay = state.RetryBackoffBaseMillis, state.RetryDelayMillis
				d.PauseCloudInventoryRefresh()
			} else {
				state, statusErr := d.CloudInventoryScanStatus()
				if statusErr != nil {
					t.Fatal(statusErr)
				}
				backoffBase, retryDelay = state.RetryBackoffBaseMillis, state.RetryDelayMillis
				d.PauseCloudInventoryScan()
			}
			if backoffBase != 1200 || retryDelay != 600 {
				t.Fatalf("retry state after resumed failure: next backoff=%d ms, current delay=%d ms; want 1200/600", backoffBase, retryDelay)
			}
		})
	}
}

func TestInventoryTerminalFailureClearsPersistedBackoffPlan(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		name := "full scan"
		if refresh {
			name = "directory refresh"
		}
		t.Run(name, func(t *testing.T) {
			d, p := setup(t, nil)
			store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			d.queue.store = store
			account := d.getClient().Fingerprint()
			now := time.Now().UTC()
			if refresh {
				state := taskstore.CloudInventoryRefreshState{AccountFingerprint: account, RunID: "refresh-terminal", RootPath: "/target", Status: "paused", Phase: "paused", CurrentDirectory: "/target", StartedAtUTC: now, UpdatedAtUTC: now, RetryDelayMillis: 600, RetryBackoffBaseMillis: 1200}
				if err = store.StartCloudInventoryRefresh(&state); err != nil {
					t.Fatal(err)
				}
				if err = store.SaveCloudInventoryRefreshState(state); err != nil {
					t.Fatal(err)
				}
			} else {
				state := taskstore.CloudScanState{AccountFingerprint: account, RunID: "full-terminal", Status: "paused", Phase: "paused", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now, RetryDelayMillis: 600, RetryBackoffBaseMillis: 1200}
				if err = store.StartCloudScan(&state); err != nil {
					t.Fatal(err)
				}
				if err = store.SaveCloudScanCheckpoint(state); err != nil {
					t.Fatal(err)
				}
			}
			p.listFailures[map[bool]string{true: "/target", false: "/"}[refresh]] = 1
			p.listFailureStatus[map[bool]string{true: "/target", false: "/"}[refresh]] = http.StatusBadRequest
			if refresh {
				if _, err = d.StartCloudInventoryRefresh(context.Background(), "/target", 0); err != nil {
					t.Fatal(err)
				}
			} else if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				var status string
				var delay, backoffBase int64
				if refresh {
					state, statusErr := d.CloudInventoryRefreshStatus()
					if statusErr != nil {
						t.Fatal(statusErr)
					}
					status, delay, backoffBase = state.Status, state.RetryDelayMillis, state.RetryBackoffBaseMillis
				} else {
					state, statusErr := d.CloudInventoryScanStatus()
					if statusErr != nil {
						t.Fatal(statusErr)
					}
					status, delay, backoffBase = state.Status, state.RetryDelayMillis, state.RetryBackoffBaseMillis
				}
				if status == "failed" {
					if delay != 0 || backoffBase != 0 {
						t.Fatalf("terminal failure retained retry plan: delay=%d backoff_base=%d", delay, backoffBase)
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("inventory scan did not reach terminal failure")
		})
	}
}

func TestCloudInventoryRefreshRunsUseAuthenticatedAccount(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	client := d.getClient()
	d.SetAuthentication(client, true, time.Now().Add(time.Hour))
	now := time.Now().UTC()
	state := taskstore.CloudInventoryRefreshState{AccountFingerprint: client.Fingerprint(), RunID: "authorized-run", RootPath: "/root", CurrentDirectory: "/root", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	page, next, more, err := d.CloudInventoryRefreshRuns(context.Background(), 0, 1)
	if err != nil || len(page) != 1 || page[0].RunID != state.RunID || more || next != 0 {
		t.Fatalf("authenticated run list=%+v next=%d more=%v err=%v", page, next, more, err)
	}
	bundle, err := d.CloudInventoryRefreshEvidenceExport(context.Background(), state.RunID)
	if err != nil || bundle.Run.RunID != state.RunID || len(bundle.Events) != 1 || bundle.Events[0].Kind != "started" || bundle.Events[0].AccountFingerprint != "" {
		t.Fatalf("authenticated refresh evidence=%+v err=%v", bundle, err)
	}
	summaries, err := d.CloudInventoryRefreshRunSummariesExport(context.Background(), taskstore.CloudInventoryRefreshRunFilter{})
	if err != nil || len(summaries) != 1 || summaries[0].RunID != state.RunID {
		t.Fatalf("authenticated refresh run summaries=%+v err=%v", summaries, err)
	}
	events, err := store.ListCloudInventoryRefreshEvents(client.Fingerprint(), state.RunID, "", 0, 1)
	if err != nil || len(events) != 1 {
		t.Fatalf("authenticated refresh events=%+v err=%v", events, err)
	}
	eventSequence := events[0].Sequence
	changes, err := d.CloudInventoryRefreshChangePage(context.Background(), state.RunID, eventSequence, 0, 1, "", "")
	if err == nil || len(changes.Changes) != 0 {
		t.Fatalf("authenticated refresh changes=%+v err=%v", changes, err)
	}
}

func TestListCloudUsesCompleteLocalDirectorySizeInsteadOfProviderPlaceholder(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 11, Path: "/folder", ServerFilename: "folder", IsDir: 1, Size: 0}, {FsID: 13, Path: "/z-last.bin", ServerFilename: "z-last.bin", Size: 3, Mtime: 44, MD5: "next-md5"}}
	p.lists["/folder"] = []baiduapi.FileInfo{{FsID: 12, Path: "/folder/file.bin", ServerFilename: "file.bin", Size: 7}}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, stateErr := d.CloudInventoryScanStatus()
		if stateErr == nil && state.Status == "complete" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	listing, err := d.ListCloud(context.Background(), "/", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Size != 7 || !listing.Entries[0].SizeComplete || listing.Entries[0].ProviderSize != 0 {
		t.Fatalf("directory listing did not use complete local aggregate: %+v", listing.Entries)
	}
	observedAt, observedErr := time.Parse(time.RFC3339Nano, listing.ObservedAtUTC)
	if observedErr != nil {
		t.Fatalf("cloud listing omitted its UTC observation time: %q (%v)", listing.ObservedAtUTC, observedErr)
	}
	if _, offset := observedAt.Zone(); offset != 0 {
		t.Fatalf("cloud listing observation time is not UTC: %s", listing.ObservedAtUTC)
	}
	if listing.NextBoundary == nil || listing.NextBoundary.FsID != 13 || listing.NextBoundary.Path != "/z-last.bin" || listing.NextBoundary.ProviderMtime != 44 || listing.NextBoundary.ContentMD5 != "next-md5" {
		t.Fatalf("provider lookahead boundary was not returned: %+v", listing.NextBoundary)
	}
}

func TestListCloudRejectsInvalidLookaheadBoundary(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	p.lists["/"] = []baiduapi.FileInfo{
		{FsID: 21, Path: "/a.bin", ServerFilename: "a.bin", Size: 1},
		{FsID: 22, Path: "/wrong-path.bin", ServerFilename: "b.bin", Size: 2},
	}
	_, err = d.ListCloud(context.Background(), "/", 0, 1)
	if !errors.Is(err, ErrIntakeConflict) {
		t.Fatalf("invalid lookahead identity was accepted: %v", err)
	}
}

func TestCloudInventoryScanOutlivesStartRequestContext(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 901, Path: "/request-context.bin", ServerFilename: "request-context.bin", Size: 4}}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	requestCtx, cancel := context.WithCancel(context.Background())
	if _, err = d.StartCloudInventoryScan(requestCtx, 0); err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, stateErr := d.CloudInventoryScanStatus()
		if stateErr == nil && state.Status == "complete" {
			return
		}
		if stateErr != nil {
			t.Fatal(stateErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, err := d.CloudInventoryScanStatus()
	t.Fatalf("scan followed canceled HTTP request context: state=%+v err=%v", state, err)
}

func TestCloudInventoryScannerResumesPersistedDirectoryQueueAfterPause(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 81, Path: "/folder", ServerFilename: "folder", IsDir: 1}}
	p.lists["/folder"] = []baiduapi.FileInfo{{FsID: 82, Path: "/folder/file.bin", ServerFilename: "file.bin", Size: 9}}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, stateErr := d.CloudInventoryScanStatus()
		if stateErr == nil && state.Status == "running" && state.CurrentDirectory == "/folder" {
			d.PauseCloudInventoryScan()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for time.Now().Before(deadline) {
		state, stateErr := d.CloudInventoryScanStatus()
		if stateErr == nil && state.Status == "paused" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	workerStopped := false
	for time.Now().Before(deadline) {
		d.inventoryScanMutex.Lock()
		workerStopped = d.inventoryScanCancel == nil
		d.inventoryScanMutex.Unlock()
		if workerStopped {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !workerStopped {
		t.Fatal("paused cloud inventory worker did not stop")
	}
	paused, err := d.CloudInventoryScanStatus()
	if err != nil || paused.Status != "paused" || paused.PendingCount != 1 || paused.CurrentDirectory != "/folder" {
		t.Fatalf("persisted paused scan=%+v err=%v", paused, err)
	}
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, stateErr := d.CloudInventoryScanStatus()
		if stateErr == nil && state.Status == "complete" {
			if state.PendingCount != 0 || state.ItemsProcessed != 2 {
				t.Fatalf("resumed scan totals=%+v", state)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, err := d.CloudInventoryScanStatus()
	t.Fatalf("resumed scan did not complete: state=%+v err=%v", state, err)
}

func TestCloudInventoryRefreshRecursivelyScansOnlySelectedSubtree(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	p.lists["/target"] = []baiduapi.FileInfo{
		{FsID: 3002, Path: "/target/new.bin", ServerFilename: "new.bin", Size: 13},
		{FsID: 3003, Path: "/target/sub", ServerFilename: "sub", IsDir: 1},
	}
	p.lists["/target/sub"] = []baiduapi.FileInfo{{FsID: 3004, Path: "/target/sub/child.bin", ServerFilename: "child.bin", Size: 6}}
	dir := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue")
	store, err := taskstore.Open(dir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	account := d.getClient().Fingerprint()
	now := time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)
	for i, page := range []struct {
		dir     string
		entries []taskstore.CloudInventoryEntry
	}{
		{"/", []taskstore.CloudInventoryEntry{{FsID: 3000, Path: "/target", Name: "target", Directory: true}, {FsID: 3001, Path: "/sibling", Name: "sibling", Directory: true}}},
		{"/target", []taskstore.CloudInventoryEntry{{FsID: 3010, Path: "/target/old.bin", Name: "old.bin", Size: 20}}},
		{"/sibling", []taskstore.CloudInventoryEntry{{FsID: 3011, Path: "/sibling/keep.bin", Name: "keep.bin", Size: 9}}},
	} {
		if err = store.UpsertCloudInventoryPage(account, fmt.Sprintf("seed-%d", i), page.dir, page.entries, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.SaveCloudScanState(account, "full-account-run", "complete", "complete", "/", 0, 7, 11, now, now, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = d.StartCloudInventoryRefresh(context.Background(), "/target", 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var state taskstore.CloudInventoryRefreshState
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryRefreshStatus()
		if err == nil && state.Status == "complete" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "complete" || state.RootPath != "/target" || state.ItemsProcessed != 3 {
		t.Fatalf("scoped refresh state=%+v err=%v", state, err)
	}
	events, err := store.ListCloudInventoryRefreshEvents(account, state.RunID, "", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []string{"completed", "directory_published", "directory_published", "started"}
	if len(events) != len(wantKinds) {
		t.Fatalf("refresh event history=%+v", events)
	}
	for i, want := range wantKinds {
		if events[i].Kind != want || events[i].RecordedAtUTC.IsZero() {
			t.Fatalf("refresh event %d=%+v want kind %q with local UTC time", i, events[i], want)
		}
	}
	if events[1].CurrentDirectory != "/target/sub" || events[1].CloudObservedAtUTC == nil || events[1].DirectoryChanged == nil || !*events[1].DirectoryChanged {
		t.Fatalf("directory publish event lacks online observation/change evidence: %+v", events[1])
	}
	size, err := store.CloudDirectorySize(account, "/target")
	if err != nil || !size.Complete || size.Bytes != 19 || size.Files != 2 || size.Directories != 1 {
		t.Fatalf("refreshed target size=%+v err=%v", size, err)
	}
	_, siblingMeta, siblingCount, err := store.ListCloudInventory(account, "/sibling", 0, 10)
	if err != nil || siblingMeta.Status != "complete" || siblingCount != 1 {
		t.Fatalf("sibling cache changed: meta=%+v count=%d err=%v", siblingMeta, siblingCount, err)
	}
	full, err := store.LoadCloudScanState(account)
	if err != nil || full.RunID != "full-account-run" || full.Status != "complete" || full.PagesProcessed != 7 || full.ItemsProcessed != 11 {
		t.Fatalf("full-account state changed: %+v err=%v", full, err)
	}
	p.mu.Lock()
	requests := append([]string(nil), p.listRequests...)
	p.mu.Unlock()
	if !reflect.DeepEqual(requests, []string{"/target", "/target/sub"}) {
		t.Fatalf("scoped refresh listed outside its subtree: %v", requests)
	}
}

func TestCloudInventoryRefreshRejectsInvalidPathsBeforeStarting(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	for _, directory := range []string{"", "relative", "/target/../other", "/target\\child", "/target//child"} {
		if _, err := d.StartCloudInventoryRefresh(context.Background(), directory, 0); err == nil {
			t.Errorf("invalid refresh directory %q was accepted", directory)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.listRequests) != 0 {
		t.Fatalf("invalid paths caused provider requests: %v", p.listRequests)
	}
}

func TestCloudInventoryRefreshPausesAtRequestBudgetAndResumesWithAddition(t *testing.T) {
	d, provider := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	provider.lists["/target"] = []baiduapi.FileInfo{{FsID: 501, Path: "/target/sub", ServerFilename: "sub", IsDir: 1}}
	provider.lists["/target/sub"] = []baiduapi.FileInfo{{FsID: 502, Path: "/target/sub/file.bin", ServerFilename: "file.bin", Size: 3}}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	if err = store.UpsertCloudInventoryPage(d.getClient().Fingerprint(), "old-root", "/", []taskstore.CloudInventoryEntry{{FsID: 497, Path: "/target", Name: "target", Directory: true}}, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage(d.getClient().Fingerprint(), "old-parent", "/target", []taskstore.CloudInventoryEntry{{FsID: 498, Path: "/target/sub", Name: "sub", Directory: true}}, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage(d.getClient().Fingerprint(), "old", "/target/sub", []taskstore.CloudInventoryEntry{{FsID: 499, Path: "/target/sub/old.bin", Name: "old.bin", Size: 1}}, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState(d.getClient().Fingerprint(), "old-full-scan", "complete", "complete", "/", 0, 1, 1, time.Now().UTC(), time.Now().UTC(), ""); err != nil {
		t.Fatal(err)
	}
	started, err := d.StartCloudInventoryRefresh(context.Background(), "/target", 1)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var state taskstore.CloudInventoryRefreshState
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryRefreshStatus()
		if err == nil && state.Phase == "request_budget_reached" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "paused" || state.RequestsUsed != 1 || state.RequestBudget != 1 || state.PendingCount != 1 || state.RunID != started.RunID {
		t.Fatalf("budget exhaustion lost scoped checkpoint: state=%+v err=%v", state, err)
	}
	provider.mu.Lock()
	requests := append([]string(nil), provider.listRequests...)
	provider.mu.Unlock()
	if !reflect.DeepEqual(requests, []string{"/target"}) {
		t.Fatalf("request budget did not bound provider list calls: %v", requests)
	}
	budgetEvents, err := store.ListCloudInventoryRefreshEvents(state.AccountFingerprint, state.RunID, "request_budget_reached", 0, 10)
	if err != nil || len(budgetEvents) != 1 || budgetEvents[0].RequestsUsed != 1 || budgetEvents[0].RequestBudget != 1 {
		t.Fatalf("request budget exhaustion history=%+v err=%v", budgetEvents, err)
	}
	if _, err = d.StartCloudInventoryRefresh(context.Background(), "/target", 1); err != nil {
		t.Fatalf("same-scope resume with added request budget failed: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryRefreshStatus()
		if err == nil && state.Status == "complete" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "complete" || state.RequestsUsed != 2 || state.RequestBudget != 2 {
		t.Fatalf("resumed scoped refresh state=%+v err=%v", state, err)
	}
	events, err := store.ListCloudInventoryRefreshEvents(state.AccountFingerprint, state.RunID, "resumed", 0, 10)
	if err != nil || len(events) != 1 || events[0].RequestBudget != 2 || events[0].RequestsUsed != 1 || events[0].RequestBudgetAdded != 1 {
		t.Fatalf("resumed event request-budget evidence=%+v err=%v", events, err)
	}
}

func TestCloudInventoryRefreshCanSetFutureBudgetOnUnlimitedCheckpoint(t *testing.T) {
	d, provider := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	now := time.Now().UTC()
	state := taskstore.CloudInventoryRefreshState{AccountFingerprint: d.getClient().Fingerprint(), RunID: "unlimited-refresh", RootPath: "/target", CurrentDirectory: "/target", Status: "running", Phase: "listing", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudInventoryRefresh(&state); err != nil {
		t.Fatal(err)
	}
	if used, reserveErr := store.ReserveCloudInventoryRefreshRequest(state.AccountFingerprint, state.RunID, now.Add(time.Second)); reserveErr != nil || used != 1 {
		t.Fatalf("seed unlimited request reservation used=%d err=%v", used, reserveErr)
	}
	state, err = store.LoadCloudInventoryRefreshState(state.AccountFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	state.Status, state.Phase, state.UpdatedAtUTC = "paused", "paused", now.Add(2*time.Second)
	if err = store.SaveCloudInventoryRefreshState(state); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	provider.beforeList = func(string) { close(entered); <-release }
	resumed, err := d.StartCloudInventoryRefresh(context.Background(), "/target", 3)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.RequestBudget != 4 || resumed.RequestsUsed != 1 {
		d.PauseCloudInventoryRefresh()
		close(release)
		t.Fatalf("unlimited checkpoint did not convert to a future budget: %+v", resumed)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		d.PauseCloudInventoryRefresh()
		close(release)
		t.Fatal("resumed refresh did not reach the mock list request")
	}
	events, err := store.ListCloudInventoryRefreshEvents(state.AccountFingerprint, state.RunID, "resumed", 0, 10)
	if err != nil || len(events) != 1 || events[0].RequestBudget != 4 || events[0].RequestsUsed != 1 {
		d.PauseCloudInventoryRefresh()
		close(release)
		t.Fatalf("unlimited-to-limited resume event=%+v err=%v", events, err)
	}
	d.PauseCloudInventoryRefresh()
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err = store.LoadCloudInventoryRefreshState(state.AccountFingerprint)
		if err == nil && (state.Status == "paused" || state.Status == "complete") {
			if state.Status == "complete" && (state.RequestBudget != 4 || state.RequestsUsed != 2) {
				t.Fatalf("completed converted refresh exceeded its new future budget: %+v", state)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("converted refresh did not stop cleanly: state=%+v err=%v", state, err)
}

func TestCloudInventoryRefreshKeepsRetryableFailuresIncompleteAndResumable(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	p.listFailures["/target"] = 1
	p.listFailureStatus["/target"] = http.StatusServiceUnavailable
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	if err = store.UpsertCloudInventoryPage(d.getClient().Fingerprint(), "old", "/target", nil, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	started, err := d.StartCloudInventoryRefresh(context.Background(), "/target", 0)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var state taskstore.CloudInventoryRefreshState
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryRefreshStatus()
		if err == nil && state.Phase == "backoff" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Phase != "backoff" || state.PendingCount != 1 || state.NextOffset != 0 || state.RunID != started.RunID {
		t.Fatalf("retryable refresh failure lost its checkpoint: state=%+v err=%v", state, err)
	}
	retryEvents, err := store.ListCloudInventoryRefreshEvents(state.AccountFingerprint, state.RunID, "retry_scheduled", 0, 10)
	if err != nil || len(retryEvents) != 1 || retryEvents[0].RetryAtUTC == nil || !retryEvents[0].RetryAtUTC.Equal(*state.RetryAtUTC) || retryEvents[0].LastError == "" {
		t.Fatalf("retry schedule history=%+v state=%+v err=%v", retryEvents, state, err)
	}
	_, directory, _, err := store.ListCloudInventory(d.getClient().Fingerprint(), "/target", 0, 10)
	if err != nil || directory.Status != "complete" {
		t.Fatalf("failed refresh hid the previous complete snapshot: directory=%+v err=%v", directory, err)
	}
	d.PauseCloudInventoryRefresh()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryRefreshStatus()
		if err == nil && state.Status == "paused" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "paused" || state.PendingCount != 1 {
		t.Fatalf("retryable refresh did not persist pause: state=%+v err=%v", state, err)
	}
}

func TestCloudInventoryRefreshMarksNonRetryableFailureAndClearsQueue(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	p.listFailures["/target"] = 1
	p.listFailureStatus["/target"] = http.StatusBadRequest
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	if err = store.UpsertCloudInventoryPage(d.getClient().Fingerprint(), "old", "/target", nil, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err = d.StartCloudInventoryRefresh(context.Background(), "/target", 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var state taskstore.CloudInventoryRefreshState
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryRefreshStatus()
		if err == nil && state.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "failed" || state.PendingCount != 0 || state.LastError == "" {
		t.Fatalf("permanent refresh failure state=%+v err=%v", state, err)
	}
	if _, err = store.CloudInventoryRefreshQueueHead(state.AccountFingerprint, state.RunID); err == nil {
		t.Fatal("permanent refresh failure kept a resumable queue")
	}
	events, eventErr := store.ListCloudInventoryRefreshEvents(state.AccountFingerprint, state.RunID, "", 0, 10)
	if eventErr != nil {
		t.Fatal(eventErr)
	}
	kinds := make([]string, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	if !reflect.DeepEqual(kinds, []string{"failed", "started"}) || events[0].Status != "failed" || events[0].RecordedAtUTC.IsZero() {
		t.Fatalf("terminal failure evidence=%+v", events)
	}
	_, directory, _, err := store.ListCloudInventory(state.AccountFingerprint, "/target", 0, 10)
	if err != nil || directory.Status != "complete" {
		t.Fatalf("failed refresh hid the previous complete snapshot: directory=%+v err=%v", directory, err)
	}
	failedRunID := state.RunID
	started, err := d.StartCloudInventoryRefresh(context.Background(), "/target", 0)
	if err != nil || started.RunID == failedRunID {
		t.Fatalf("retry did not create a fresh run for the failed scope: started=%+v previous=%s err=%v", started, failedRunID, err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err = d.CloudInventoryRefreshStatus()
		if err == nil && state.Status == "complete" && state.RunID == started.RunID {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || state.Status != "complete" || state.RunID != started.RunID {
		t.Fatalf("fresh retry for failed scope did not complete: state=%+v err=%v", state, err)
	}
}

func TestCloudInventoryRefreshResumesSameRootAndBlocksOtherScanners(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	p.lists["/target"] = []baiduapi.FileInfo{{FsID: 401, Path: "/target/sub", ServerFilename: "sub", IsDir: 1}}
	p.lists["/target/sub"] = []baiduapi.FileInfo{{FsID: 402, Path: "/target/sub/file.bin", ServerFilename: "file.bin", Size: 3}}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	if _, err = d.StartCloudInventoryRefresh(context.Background(), "/target", 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, stateErr := d.CloudInventoryRefreshStatus()
		if stateErr == nil && state.PagesProcessed > 0 && state.CurrentDirectory == "/target/sub" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	d.PauseCloudInventoryRefresh()
	deadline = time.Now().Add(5 * time.Second)
	var paused taskstore.CloudInventoryRefreshState
	for time.Now().Before(deadline) {
		paused, err = d.CloudInventoryRefreshStatus()
		if err == nil && paused.Status == "paused" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || paused.Status != "paused" || paused.PendingCount != 1 || paused.RootPath != "/target" {
		t.Fatalf("paused scoped refresh=%+v err=%v", paused, err)
	}
	if _, err = d.StartCloudInventoryRefresh(context.Background(), "/other", 0); err == nil {
		t.Fatal("different root replaced a resumable scoped refresh")
	}
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err == nil {
		t.Fatal("full scan started while a scoped refresh queue was resumable")
	}
	if _, err = d.StartCloudInventoryRefresh(context.Background(), "/target", 0); err != nil {
		t.Fatalf("same-root refresh did not resume: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, stateErr := d.CloudInventoryRefreshStatus()
		if stateErr == nil && state.Status == "complete" {
			if state.ItemsProcessed != 2 || state.PagesProcessed != 2 {
				t.Fatalf("resumed refresh counters=%+v", state)
			}
			events, eventErr := store.ListCloudInventoryRefreshEvents(paused.AccountFingerprint, paused.RunID, "", 0, 20)
			if eventErr != nil {
				t.Fatal(eventErr)
			}
			kinds := make([]string, len(events))
			for i, event := range events {
				kinds[i] = event.Kind
			}
			if !reflect.DeepEqual(kinds, []string{"completed", "directory_published", "resumed", "paused", "directory_published", "started"}) {
				t.Fatalf("refresh lifecycle event order=%v", kinds)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, err := d.CloudInventoryRefreshStatus()
	t.Fatalf("resumed scoped refresh did not complete: state=%+v err=%v", state, err)
}

func TestRemoteMutationIsRejectedWhileInventoryPageIsBeingCommitted(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	item := baiduapi.FileInfo{FsID: 31, Path: "/source/old.txt", ServerFilename: "old.txt", Size: 7, Mtime: 123}
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 30, Path: "/source", ServerFilename: "source", IsDir: 1}}
	p.lists["/source"] = []baiduapi.FileInfo{item}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	d.cloudAPIPacer = newCloudAPIPacer(time.Microsecond)
	d.UpdateClient(baiduapi.NewClient("test", "test"))
	pageReady := make(chan struct{})
	continuePage := make(chan struct{})
	var resumePage sync.Once
	defer resumePage.Do(func() { close(continuePage) })
	d.inventoryPageCommitHook = func(directory string) {
		if directory == "/source" {
			close(pageReady)
			<-continuePage
		}
	}
	if _, err = d.StartCloudInventoryScan(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-pageReady:
	case <-time.After(5 * time.Second):
		t.Fatal("inventory scanner did not reach the page commit boundary")
	}
	_, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: item, NewName: "new.txt"})
	if err == nil || !strings.Contains(err.Error(), "扫描") {
		resumePage.Do(func() { close(continuePage) })
		t.Fatalf("remote mutation was not rejected by active inventory scan: %v", err)
	}
	p.mu.Lock()
	mutationStarted := len(p.actions) > 0
	p.mu.Unlock()
	if mutationStarted {
		resumePage.Do(func() { close(continuePage) })
		t.Fatal("provider mutation started while inventory scan was active")
	}
	resumePage.Do(func() { close(continuePage) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, stateErr := d.CloudInventoryScanStatus()
		if stateErr == nil && state.Status == "complete" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	size, err := store.CloudDirectorySize(d.getClient().Fingerprint(), "/source")
	if err != nil || !size.Complete {
		t.Fatalf("rejected mutation changed completed inventory coverage: size=%+v err=%v", size, err)
	}
}
