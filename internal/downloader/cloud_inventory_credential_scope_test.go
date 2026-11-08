package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

// Verified renewal must retain visibility without changing credential identity.
// Different applications remain isolated; persisted observations keep their original namespace.
func TestCloudInventoryCredentialRotationPreservesVisibleCacheAcrossReopen(t *testing.T) {
	d, _ := setup(t, nil)
	queueDir := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue")
	store, err := taskstore.Open(queueDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	t.Cleanup(func() { _ = store.Close() })
	if err = d.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	stable, err := store.BoundAccountFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	old := baiduapi.NewClient("scope-before-refresh", "same-app")
	d.SetAuthentication(old, true, time.Now().Add(time.Hour))
	entries := []taskstore.CloudInventoryEntry{{FsID: 701, Path: "/kept.bin", Name: "kept.bin", Size: 7}}
	if err = store.UpsertCloudInventoryPage(old.Fingerprint(), "before-refresh", "/", entries, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	visible, _, count, err := d.ListCachedCloud(context.Background(), "/", 0, 100, nil)
	if err != nil || count != 1 || len(visible) != 1 || visible[0].FsID != 701 {
		t.Fatalf("initial view: entries=%+v count=%d err=%v", visible, count, err)
	}
	now := time.Now().UTC()
	if err = store.SaveCloudScanState(old.Fingerprint(), "historical-inventory", "complete", "complete", "/", 0, 1, 1, now, now, ""); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudDuplicateScanState(taskstore.CloudDuplicateScanState{AccountFingerprint: old.Fingerprint(), RunID: "historical-hash", Status: "complete", Phase: "complete", StartedAtUTC: now, UpdatedAtUTC: now}, true); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(baiduapi.NewClient("scope-after-refresh", "same-app"), true, time.Now().Add(time.Hour))
	visible, _, count, err = d.ListCachedCloud(context.Background(), "/", 0, 100, nil)
	if err != nil || count != 1 || len(visible) != 1 || visible[0].Path != "/kept.bin" {
		t.Fatalf("renewal lost cache: entries=%+v count=%d err=%v", visible, count, err)
	}
	inventory, err := d.CloudInventoryScanStatus()
	if err != nil || inventory.RunID != "historical-inventory" || inventory.WorkerActive {
		t.Fatalf("renewal lost historical inventory: %+v err=%v", inventory, err)
	}
	duplicate, err := d.CloudDuplicateScanStatus()
	if err != nil || duplicate.RunID != "historical-hash" || duplicate.Status != "complete" {
		t.Fatalf("renewal lost historical hash: %+v err=%v", duplicate, err)
	}
	size, err := d.GetCachedCloudDirectorySize(context.Background(), "/")
	if err != nil || size.Bytes != 7 || !size.Complete {
		t.Fatalf("renewal size=%+v err=%v", size, err)
	}
	matches, total, _, err := d.SearchCachedCloud(context.Background(), "kept", 0, 10, nil)
	if err != nil || total != 1 || len(matches) != 1 {
		t.Fatalf("renewal search=%+v total=%d err=%v", matches, total, err)
	}
	d.SetAuthentication(baiduapi.NewClient("scope-after-refresh", "different-app"), true, time.Now().Add(time.Hour))
	visible, _, count, err = d.ListCachedCloud(context.Background(), "/", 0, 100, nil)
	if err != nil || count != 0 || len(visible) != 0 {
		t.Fatalf("application cache leaked count=%d err=%v", count, err)
	}
	d.SetAuthentication(baiduapi.NewClient("scope-after-refresh", "same-app"), true, time.Now().Add(time.Hour))
	after, err := store.BoundAccountFingerprint()
	if err != nil || stable != after {
		t.Fatalf("account binding changed on credential rotation: %v", err)
	}
	if err = d.BindAccount("456"); !errors.Is(err, taskstore.ErrAccountMismatch) {
		t.Fatalf("different account accepted: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = taskstore.Open(queueDir, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	visible, _, count, err = d.ListCachedCloud(context.Background(), "/", 0, 100, nil)
	if err != nil || count != 1 || len(visible) != 1 || visible[0].FsID != 701 {
		t.Fatalf("reopen lost cache count=%d err=%v", count, err)
	}
	retained, _, count, err := store.ListCloudInventory(old.Fingerprint(), "/", 0, 100)
	if err != nil || count != 1 || len(retained) != 1 || retained[0].FsID != 701 || retained[0].Size != 7 {
		t.Fatalf("old inventory was not durable: entries=%+v count=%d err=%v", retained, count, err)
	}
}

func TestCloudListingRejectsResponseFromCredentialBeforeRotation(t *testing.T) {
	d, p := setup(t, nil)
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	t.Cleanup(func() { _ = store.Close() })
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 702, Path: "/late.bin", ServerFilename: "late.bin", Size: 3}}
	d.SetAuthentication(baiduapi.NewClient("listing-before-refresh", "app"), true, time.Now().Add(time.Hour))
	rotated := false
	p.beforeList = func(string) {
		d.SetAuthentication(baiduapi.NewClient("listing-after-refresh", "app"), true, time.Now().Add(time.Hour))
		rotated = true
	}
	listing, err := d.ListCloud(context.Background(), "/", 0, 100)
	if !rotated || len(p.listRequests) != 1 || !errors.Is(err, ErrIntakeStale) || len(listing.Entries) != 0 {
		t.Fatalf("old response accepted after rotation: entries=%+v err=%v", listing.Entries, err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("read-only listing mutated provider: %v", p.actions)
	}
}

func TestCloudInventoryWorkersDoNotPublishPageFromCredentialBeforeRotation(t *testing.T) {
	for _, scenario := range []string{"full", "refresh", "full_commit", "refresh_commit"} {
		t.Run(scenario, func(t *testing.T) {
			kind := scenario
			atCommit := strings.HasSuffix(scenario, "_commit")
			if atCommit {
				kind = strings.TrimSuffix(scenario, "_commit")
			}
			d, p := setup(t, nil)
			store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			d.queue.store = store
			t.Cleanup(func() { shutdown(t, d) })
			if err = d.BindAccount("123"); err != nil {
				t.Fatal(err)
			}
			old := baiduapi.NewClient("worker-before-refresh", "app")
			d.SetAuthentication(old, true, time.Now().Add(time.Hour))
			directory := "/"
			if kind == "refresh" {
				directory = "/folder"
			}
			p.lists[directory] = []baiduapi.FileInfo{{FsID: 703, Path: directory + "/late.bin", ServerFilename: "late.bin", Size: 3}}
			if directory == "/" {
				p.lists[directory][0].Path = "/late.bin"
			}
			rotate := func(string) {
				d.SetAuthentication(baiduapi.NewClient("worker-after-refresh", "app"), true, time.Now().Add(time.Hour))
			}
			if atCommit {
				d.inventoryPageCommitHook = rotate
			} else {
				p.beforeList = rotate
			}
			if kind == "full" {
				_, err = d.StartCloudInventoryScan(context.Background(), 1)
			} else {
				_, err = d.StartCloudInventoryRefresh(context.Background(), directory, 1)
			}
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			phase := ""
			for time.Now().Before(deadline) {
				if kind == "full" {
					state, stateErr := store.LoadCloudScanState(old.Fingerprint())
					err = stateErr
					phase = state.Phase
				} else {
					state, stateErr := store.LoadCloudInventoryRefreshState(old.Fingerprint())
					err = stateErr
					phase = state.Phase
				}
				if err == nil && (phase == "backoff" || phase == "complete" || phase == "failed" || phase == "paused") {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			expectedPhase := "backoff"
			if atCommit {
				expectedPhase = "paused"
			}
			if err != nil || phase != expectedPhase {
				t.Fatalf("late page not rejected phase=%s err=%v", phase, err)
			}
			rows, _, count, err := store.ListCloudInventory(old.Fingerprint(), directory, 0, 100)
			if err != nil || count != 0 || len(rows) != 0 {
				t.Fatalf("old credential page published rows=%+v count=%d err=%v", rows, count, err)
			}
		})
	}
}

func TestCloudMutationAfterRenewalUsesCacheNamespaceAndGuardsLatePublication(t *testing.T) {
	for _, rotateDuringResponse := range []bool{false, true} {
		t.Run(fmt.Sprint(rotateDuringResponse), func(t *testing.T) {
			d, p := setup(t, nil)
			store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			d.queue.store = store
			defer store.Close()
			if err = d.BindAccount("123"); err != nil {
				t.Fatal(err)
			}
			old := baiduapi.NewClient("mutation-original", "app")
			d.SetAuthentication(old, true, time.Now().Add(time.Hour))
			info := baiduapi.FileInfo{FsID: 704, Path: "/source/kept.bin", ServerFilename: "kept.bin", Size: 7, Mtime: 123}
			p.lists["/source"] = []baiduapi.FileInfo{info}
			if err = store.UpsertCloudInventoryPage(old.Fingerprint(), "seed", "/source", []taskstore.CloudInventoryEntry{cloudInventoryEntry(info)}, time.Now().UTC(), nil); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err = d.ListCachedCloud(context.Background(), "/source", 0, 10, nil); err != nil {
				t.Fatal(err)
			}
			sending := baiduapi.NewClient("mutation-renewed", "app")
			d.SetAuthentication(sending, true, time.Now().Add(time.Hour))
			p.onMutation = func(string, string) {
				p.mu.Lock()
				p.lists["/source"] = nil
				p.mu.Unlock()
				if rotateDuringResponse {
					d.SetAuthentication(baiduapi.NewClient("mutation-during-response", "app"), true, time.Now().Add(time.Hour))
				}
			}
			result, _ := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: info})
			if result.Status != "confirmed" || len(p.actions) != 1 {
				t.Fatalf("confirmed response lost: %+v actions=%v", result, p.actions)
			}
			rows, directory, count, err := store.ListCloudInventory(old.Fingerprint(), "/source", 0, 10)
			if err != nil {
				t.Fatal(err)
			}
			if rotateDuringResponse {
				if count != 1 || len(rows) != 1 || directory.Status != "stale" {
					t.Fatalf("late observation published count=%d directory=%+v", count, directory)
				}
			} else if count != 0 {
				t.Fatalf("renewed mutation did not update original cache count=%d", count)
			}
			operations, err := store.LoadRemoteOperations()
			if err != nil || len(operations) != 1 || operations[0].AccountFingerprint != sending.Fingerprint() || operations[0].Status != "confirmed" {
				t.Fatalf("credential journal changed: %+v err=%v", operations, err)
			}
			events, _, _, err := store.RemoteOperationEventPage(result.OperationID, taskstore.RemoteOperationEventFilter{}, 0, 20)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, event := range events {
				if event.Status == "prepared" {
					var evidence struct {
						Namespace string `json:"cache_namespace"`
					}
					if json.Unmarshal(event.Evidence, &evidence) != nil || evidence.Namespace != old.Fingerprint() {
						t.Fatal("prepared cache scope missing")
					}
					found = true
				}
			}
			if !found {
				t.Fatal("prepared evidence missing")
			}
		})
	}
}
