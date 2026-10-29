package downloader

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

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
	if _, err = d.StartCloudInventoryScan(context.Background()); err != nil {
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

func TestCloudInventoryScanOutlivesStartRequestContext(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 901, Path: "/request-context.bin", ServerFilename: "request-context.bin", Size: 4}}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	requestCtx, cancel := context.WithCancel(context.Background())
	if _, err = d.StartCloudInventoryScan(requestCtx); err != nil {
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
	if _, err = d.StartCloudInventoryScan(context.Background()); err != nil {
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
	if _, err = d.StartCloudInventoryScan(context.Background()); err != nil {
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

func TestRemoteMutationInvalidationWinsOverInventoryPageBeingCommitted(t *testing.T) {
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
	if _, err = d.StartCloudInventoryScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-pageReady:
	case <-time.After(5 * time.Second):
		t.Fatal("inventory scanner did not reach the page commit boundary")
	}
	mutationDone := make(chan error, 1)
	go func() {
		_, mutationErr := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: item, NewName: "new.txt"})
		mutationDone <- mutationErr
	}()
	select {
	case err = <-mutationDone:
		resumePage.Do(func() { close(continuePage) })
		t.Fatalf("remote mutation passed the in-progress inventory commit: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	p.mu.Lock()
	mutationStarted := len(p.actions) > 0
	p.mu.Unlock()
	if mutationStarted {
		resumePage.Do(func() { close(continuePage) })
		t.Fatal("provider mutation started before the inventory page was persisted")
	}
	resumePage.Do(func() { close(continuePage) })
	select {
	case err = <-mutationDone:
	case <-time.After(5 * time.Second):
		t.Fatal("remote mutation did not resume after the inventory page committed")
	}
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, stateErr := d.CloudInventoryScanStatus()
		if stateErr == nil && state.Status == "complete" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	size, err := store.CloudDirectorySize(d.getClient().Fingerprint(), "/source")
	if err != nil || size.Complete {
		t.Fatalf("pre-mutation inventory page remained complete after remote mutation: size=%+v err=%v", size, err)
	}
}
