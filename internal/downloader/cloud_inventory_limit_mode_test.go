package downloader

import (
	"context"
	"path/filepath"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestInventoryExplicitUnlimitedPreservesLegacyAndUsedRequests(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "scoped"}[scoped], func(t *testing.T) {
			d, p := setup(t, nil)
			root := "/"
			if scoped {
				root = "/target"
			}
			sub := root + "sub"
			if scoped {
				sub = root + "/sub"
			}
			p.lists[root] = []baiduapi.FileInfo{{FsID: 41, Path: sub, ServerFilename: "sub", IsDir: 1}}
			p.lists[sub] = []baiduapi.FileInfo{}
			store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			d.queue.store = store
			defer shutdown(t, d)
			start := func(mode string, budget int64) error {
				if scoped {
					_, e := d.StartCloudInventoryRefreshWithLimit(context.Background(), root, budget, mode)
					return e
				}
				_, e := d.StartCloudInventoryScanWithLimit(context.Background(), budget, mode)
				return e
			}
			status := func() (string, string, int64, int64, string, error) {
				if scoped {
					s, e := d.CloudInventoryRefreshStatus()
					return s.Status, s.Phase, s.RequestBudget, s.RequestsUsed, s.RunID, e
				}
				s, e := d.CloudInventoryScanStatus()
				return s.Status, s.Phase, s.RequestBudget, s.RequestsUsed, s.RunID, e
			}
			if err = start("limited", 1); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			var run string
			for {
				_, phase, budget, used, id, e := status()
				if e != nil {
					t.Fatal(e)
				}
				if phase == "request_budget_reached" {
					if budget != 1 || used != 1 {
						t.Fatalf("checkpoint budget=%d used=%d", budget, used)
					}
					run = id
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("scan did not reach local limit")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err = start("", 0); err == nil {
				t.Fatal("legacy zero unexpectedly removed finite cap")
			}
			if err = start("unlimited", 0); err != nil {
				t.Fatal(err)
			}
			deadline = time.Now().Add(5 * time.Second)
			for {
				state, _, budget, used, id, e := status()
				if e != nil {
					t.Fatal(e)
				}
				if state == "complete" {
					if budget != 0 || used != 2 || id != run {
						t.Fatalf("unlimited budget=%d used=%d run=%s", budget, used, id)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("unlimited scan did not complete")
				}
				time.Sleep(10 * time.Millisecond)
			}
			account := d.getClient().Fingerprint()
			found := false
			if scoped {
				events, e := store.ListCloudInventoryRefreshEvents(account, run, "resumed", 0, 20)
				if e != nil {
					t.Fatal(e)
				}
				for _, e := range events {
					found = found || (e.RequestBudget == 0 && e.RequestsUsed == 1)
				}
			} else {
				events, e := store.ListCloudScanEvents(account, run, 0, 20)
				if e != nil {
					t.Fatal(e)
				}
				for _, e := range events {
					found = found || (e.Kind == "resumed" && e.RequestBudget == 0 && e.RequestsUsed == 1)
				}
			}
			if !found {
				t.Fatal("unlimited resume evidence missing")
			}
			// Reopen the durable database after worker completion; never infer a new cap.
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			d.queue.store = nil
			reopened, e := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			if scoped {
				s, e := reopened.LoadCloudInventoryRefreshState(account)
				if e != nil || s.RequestBudget != 0 || s.RequestsUsed != 2 {
					t.Fatalf("reopened refresh=%+v err=%v", s, e)
				}
			} else {
				s, e := reopened.LoadCloudScanState(account)
				if e != nil || s.RequestBudget != 0 || s.RequestsUsed != 2 {
					t.Fatalf("reopened full=%+v err=%v", s, e)
				}
			}
		})
	}
}

func TestInventoryRequestLimitModeValidation(t *testing.T) {
	for _, c := range []struct {
		mode   string
		budget int64
	}{{"unlimited", 1}, {"limited", 0}, {"other", 0}, {"", -1}, {"limited", 100001}} {
		if ValidateInventoryRequestLimit(c.budget, c.mode) == nil {
			t.Fatalf("accepted invalid mode=%s budget=%d", c.mode, c.budget)
		}
	}
}

func TestInventoryStartRejectsStaleConfirmedRun(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		d, _ := setup(t, nil)
		store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
		if err != nil {
			t.Fatal(err)
		}
		d.queue.store = store
		defer shutdown(t, d)
		expected := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		ctx := WithInventoryExpectedRun(context.Background(), &expected)
		if scoped {
			_, err = d.StartCloudInventoryRefreshWithLimit(ctx, "/", 0, "unlimited")
		} else {
			_, err = d.StartCloudInventoryScanWithLimit(ctx, 0, "unlimited")
		}
		if err == nil {
			t.Fatal("stale confirmation started a new scan")
		}
	}
}

func TestInventoryPauseRejectsStaleRunWithoutCancellingWorker(t *testing.T) {
	d, _ := setup(t, nil)
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	defer shutdown(t, d)
	client, _, err := d.authorizedClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := taskstore.CloudScanState{AccountFingerprint: client.Fingerprint(), RunID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.StartCloudScan(&state); err != nil {
		t.Fatal(err)
	}
	cancelled := false
	d.inventoryScanKind = "full"
	d.inventoryScanCancel = func() { cancelled = true }
	if err = d.PauseCloudInventoryRun(context.Background(), "scan", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err == nil || cancelled {
		t.Fatal("stale pause cancelled current worker")
	}
	if err = d.PauseCloudInventoryRun(context.Background(), "scan", state.RunID); err != nil || !cancelled {
		t.Fatalf("matching pause failed: %v", err)
	}
	d.inventoryScanCancel = nil
}
