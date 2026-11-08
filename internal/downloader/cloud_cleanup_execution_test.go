package downloader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestCleanupExecutionUsesOnlineMutationAndDurableBatch(t *testing.T) {
	for _, scenario := range []string{"success", "identity_changed", "unknown", "stopped", "stop_during_check", "not_empty", "quarantined", "cache_warning"} {
		t.Run(scenario, func(t *testing.T) {
			d, p := setup(t, nil)
			store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			d.queue.store = store
			defer shutdown(t, d)
			if err = store.BindAccount("123"); err != nil {
				t.Fatal(err)
			}
			d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
			scope, err := d.cleanupScope(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			dir := baiduapi.FileInfo{FsID: 91, Path: "/source/empty", ServerFilename: "empty", IsDir: 1, Mtime: 123}
			p.lists["/source"] = []baiduapi.FileInfo{dir}
			p.lists[dir.Path] = nil
			m := taskstore.CleanupBatchManifest{ID: strings.Repeat("a", 32), AccountFingerprint: scope.AccountFingerprint, ApplicationFingerprint: scope.ApplicationFingerprint, CacheNamespace: scope.CacheNamespace, CreatedAtUTC: time.Now().UTC(), Items: []taskstore.CleanupBatchItem{{Kind: "empty", Expected: taskstore.CleanupIdentity{FsID: dir.FsID, Path: dir.Path, Name: dir.ServerFilename, Directory: true, Mtime: dir.Mtime}, PreviewObservedAtUTC: time.Now().UTC()}}}
			if _, err = store.CreateCleanupBatch(context.Background(), m); err != nil {
				t.Fatal(err)
			}
			p.onMutation = func(string, string) {
				operations, readErr := store.LoadRemoteOperations()
				current, batchErr := store.CleanupBatch(context.Background(), m.ID)
				if readErr != nil || batchErr != nil || len(operations) != 1 || operations[0].Status != "prepared" || current.Status != "running" {
					t.Errorf("provider write preceded durable admission: operations=%+v batch=%+v errors=%v/%v", operations, current, readErr, batchErr)
				}
			}
			switch scenario {
			case "cache_warning":
				if err = store.UpsertCloudInventoryPage(scope.CacheNamespace, "seed", "/source", []taskstore.CloudInventoryEntry{{FsID: dir.FsID, Path: dir.Path, Name: dir.ServerFilename, Directory: true, ProviderMtime: dir.Mtime}}, time.Now().UTC(), nil); err != nil {
					t.Fatal(err)
				}
				db, openErr := sql.Open("sqlite", filepath.ToSlash(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue", "queue.sqlite")))
				if openErr != nil {
					t.Fatal(openErr)
				}
				defer db.Close()
				if _, err = db.Exec(`CREATE TRIGGER fail_cleanup_cache BEFORE DELETE ON cloud_inventory BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
					t.Fatal(err)
				}
			case "not_empty":
				p.lists[dir.Path] = []baiduapi.FileInfo{{FsID: 999, Path: dir.Path + "/child", ServerFilename: "child", Size: 1}}
			case "quarantined":
				d.queue.blockedRemotePaths[dir.Path] = true
			case "stop_during_check":
				p.metadataOverride = func(int64, int) *baiduapi.FileInfo {
					if stopErr := store.StopCleanupBatch(context.Background(), m.ID); stopErr != nil {
						t.Error(stopErr)
					}
					return &dir
				}
			case "identity_changed":
				p.lists["/source"][0].Mtime++
			case "unknown":
				p.unknownAction = true
			case "stopped":
				if err = store.StopCleanupBatch(context.Background(), m.ID); err != nil {
					t.Fatal(err)
				}
			}
			_, err = d.executeCleanupItem(context.Background(), m.ID, 0)
			if scenario == "identity_changed" {
				events, _, eventErr := store.CleanupEventPage(context.Background(), m.ID, scope, 0, 100)
				found := false
				for _, event := range events {
					if event.Kind == "preflight_failed" && event.Reason == "source_identity_changed" {
						found = true
					}
				}
				if eventErr != nil || !found {
					t.Fatalf("missing precise source identity reason: %+v %v", events, eventErr)
				}
			}
			batch, readErr := store.CleanupBatch(context.Background(), m.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want, writes := "completed", 1
			switch scenario {
			case "identity_changed", "not_empty", "quarantined":
				want, writes = "failed", 0
			case "cache_warning":
				want = "failed"
			case "unknown":
				want = "unknown"
			case "stopped", "stop_during_check":
				want, writes = "stopped", 0
			}
			if batch.Status != want || len(p.actions) != writes || (scenario == "success" && err != nil) {
				t.Fatalf("batch=%+v writes=%v err=%v", batch, p.actions, err)
			}
			_, _ = d.executeCleanupItem(context.Background(), m.ID, 0)
			if len(p.actions) != writes {
				t.Fatalf("replayed batch: %v", p.actions)
			}
		})
	}
}

func TestCleanupExecutionRechecksRetainedDuplicate(t *testing.T) {
	for _, scenario := range []string{"confirmed", "retained_changed", "retained_quarantined", "retained_parent_quarantined", "download_delete_unknown", "download_rename_unknown", "intake_source_unknown", "intake_target_unknown"} {
		changed := scenario != "confirmed"
		t.Run(scenario, func(t *testing.T) {
			d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
			store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			d.queue.store = store
			defer shutdown(t, d)
			if err = store.BindAccount("123"); err != nil {
				t.Fatal(err)
			}
			d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
			scope, err := d.cleanupScope(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			source := baiduapi.FileInfo{FsID: 91, Path: "/source/copy.bin", ServerFilename: "copy.bin", Size: 7, Mtime: 123}
			keep := baiduapi.FileInfo{FsID: 92, Path: "/source/keep.bin", ServerFilename: "keep.bin", Size: 7, Mtime: 124}
			if scenario == "retained_parent_quarantined" {
				keep.Path = "/archive/keep.bin"
			}
			p.lists["/source"] = []baiduapi.FileInfo{source, keep}
			if scenario == "retained_parent_quarantined" {
				p.lists["/source"] = []baiduapi.FileInfo{source}
				p.lists["/archive"] = []baiduapi.FileInfo{keep}
			}
			keepIdentity := taskstore.CleanupIdentity{FsID: keep.FsID, Path: keep.Path, Name: keep.ServerFilename, Size: keep.Size, Mtime: keep.Mtime}
			digest := sha256.Sum256([]byte("payload"))
			m := taskstore.CleanupBatchManifest{ID: strings.Repeat("b", 32), AccountFingerprint: scope.AccountFingerprint, ApplicationFingerprint: scope.ApplicationFingerprint, CacheNamespace: scope.CacheNamespace, CreatedAtUTC: time.Now().UTC(), Items: []taskstore.CleanupBatchItem{{Kind: "duplicate", Expected: taskstore.CleanupIdentity{FsID: source.FsID, Path: source.Path, Name: source.ServerFilename, Size: source.Size, Mtime: source.Mtime}, Retained: &keepIdentity, ExpectedSHA256: hex.EncodeToString(digest[:]), PreviewObservedAtUTC: time.Now().UTC()}}}
			if _, err = store.CreateCleanupBatch(context.Background(), m); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "download_delete_unknown", "download_rename_unknown":
				mode := DownloadModeDelete
				if scenario == "download_rename_unknown" {
					mode = DownloadModeKeep
				}
				d.queue.jobs[keep.FsID] = &queueJob{Info: keep, Mode: mode, Status: "source_unknown", SourceState: "unknown"}
				d.queue.rebuildSourceBlocks()
			case "intake_source_unknown", "intake_target_unknown":
				source, target := keep, "/queue/keep.bin"
				if scenario == "intake_target_unknown" {
					source.Path = "/original/keep.bin"
					target = keep.Path
				}
				d.intakes["unresolved"] = intakeRecord{Intake: Intake{Items: []IntakeItem{{Source: source, Target: target, State: "unknown"}}}}
				d.refreshIntakeBlocks()
			case "retained_changed":
				p.lists["/source"][1].Mtime++
			case "retained_quarantined":
				d.queue.blockedRemoteIDs[keep.FsID] = true
			case "retained_parent_quarantined":
				d.queue.blockedRemotePaths["/archive"] = true
			}
			_, err = d.executeCleanupItem(context.Background(), m.ID, 0)
			if scenario == "retained_changed" {
				events, _, eventErr := store.CleanupEventPage(context.Background(), m.ID, scope, 0, 100)
				found := false
				for _, event := range events {
					if event.Kind == "preflight_failed" && event.Reason == "retained_identity_changed" {
						found = true
					}
				}
				if eventErr != nil || !found {
					t.Fatalf("missing precise retained identity reason: %+v %v", events, eventErr)
				}
			}

			batch, readErr := store.CleanupBatch(context.Background(), m.ID)
			want, writes := "completed", 1
			if changed {
				want, writes = "failed", 0
			}
			if readErr != nil || batch.Status != want || len(p.actions) != writes || (!changed && err != nil) {
				t.Fatalf("batch=%+v writes=%v errors=%v/%v", batch, p.actions, err, readErr)
			}
		})
	}
}
