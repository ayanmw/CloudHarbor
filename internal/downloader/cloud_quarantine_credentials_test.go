package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestUnknownRemoteQuarantineSurvivesEmptyStartupAndCredentialRenewal(t *testing.T) {
	for _, startupToken := range []string{"", "renewed-credential"} {
		t.Run(map[bool]string{true: "empty_startup", false: "renewed_startup"}[startupToken == ""], func(t *testing.T) {
			d, p := setup(t, nil)
			store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.BindAccount("123"); err != nil {
				_ = store.Close()
				t.Fatal(err)
			}
			old := baiduapi.NewClient("old-credential", "app")
			source := baiduapi.FileInfo{FsID: 91, Path: "/source/a.bin", ServerFilename: "a.bin", Size: 7, Mtime: 123}
			op := taskstore.RemoteOperation{ID: strings.Repeat("a", 32), AccountFingerprint: old.Fingerprint(), Kind: "copy", ObjectID: source.FsID, SourcePath: source.Path, DestinationPath: "/archive/a.bin", Status: "prepared", CreatedAtUTC: time.Now().UTC(), Evidence: json.RawMessage(`{}`)}
			if err = store.CreateRemoteOperation(op); err != nil {
				_ = store.Close()
				t.Fatal(err)
			}
			if err = store.UpdateRemoteOperation(op.ID, "unknown", json.RawMessage(`{"reason":"unconfirmed"}`), time.Now().UTC()); err != nil {
				_ = store.Close()
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			restarted, err := OpenDownloader(d.cfg, baiduapi.NewClient(startupToken, "app"))
			if err != nil {
				t.Fatal(err)
			}
			defer shutdown(t, restarted)
			restarted.queue.mu.Lock()
			blocked := restarted.queue.isRemoteBlocked(source.FsID, source.Path) && restarted.queue.isRemoteBlocked(0, op.DestinationPath)
			restarted.queue.mu.Unlock()
			if !blocked {
				t.Fatal("unknown remote operation lost quarantine when startup credential changed")
			}
			restarted.SetAuthentication(baiduapi.NewClient("ready-renewed-credential", "app"), true, time.Now().Add(time.Hour))
			_, err = restarted.MutateCloud(context.Background(), CloudMutationRequest{Operation: "copy", Expected: source, DestinationDirectory: "/archive"})
			if !errors.Is(err, ErrIntakeConflict) || len(p.actions) != 0 {
				t.Fatalf("unknown copy could be replayed after renewal: actions=%v err=%v", p.actions, err)
			}
			operations, err := restarted.queue.store.LoadRemoteOperations()
			if err != nil || len(operations) != 1 || operations[0].Status != "unknown" || operations[0].AccountFingerprint != old.Fingerprint() {
				t.Fatalf("historical evidence changed: %+v err=%v", operations, err)
			}
		})
	}
}

func TestCloudMutationsRejectQuarantinedObjectsAndAncestors(t *testing.T) {
	for _, operation := range []string{"rename", "move", "delete", "copy"} {
		for _, scope := range []string{"source", "destination", "descendant", "unrelated"} {
			if operation == "delete" && scope == "destination" {
				continue
			}
			t.Run(operation+"/"+scope, func(t *testing.T) {
				d, p := setup(t, nil)
				source := baiduapi.FileInfo{FsID: 91, Path: "/source/a.bin", ServerFilename: "a.bin", Size: 7, Mtime: 123}
				if scope == "descendant" {
					source = baiduapi.FileInfo{FsID: 92, Path: "/source", ServerFilename: "source", IsDir: 1, Mtime: 123}
				}
				store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				d.queue.store = store
				d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
				request := CloudMutationRequest{Operation: operation, Expected: source, NewName: "renamed.bin", DestinationDirectory: "/archive"}
				target, err := validateCloudMutation(request)
				if err != nil {
					t.Fatal(err)
				}
				blockedPath := source.Path
				if scope == "destination" {
					blockedPath = target
				}
				if scope == "descendant" {
					blockedPath = "/source/uncertain.bin"
				}
				if scope == "unrelated" {
					blockedPath = "/source-other/uncertain.bin"
				}
				d.queue.blockedRemotePaths[blockedPath] = true
				p.metadataOverride = func(int64, int) *baiduapi.FileInfo { return &source }
				p.lists["/source"] = []baiduapi.FileInfo{source}
				p.lists["/"] = []baiduapi.FileInfo{source}
				p.lists["/archive"] = nil
				_, err = d.MutateCloud(context.Background(), request)
				if scope == "unrelated" {
					if err != nil || len(p.actions) != 1 {
						t.Fatalf("unrelated quarantine blocked operation: actions=%v err=%v", p.actions, err)
					}
					return
				}
				if !errors.Is(err, ErrIntakeConflict) || len(p.actions) != 0 {
					t.Fatalf("quarantine bypass: actions=%v err=%v", p.actions, err)
				}
			})
		}
	}
}

func TestCloudMutationQuarantineRestoresAllUncertainJournals(t *testing.T) {
	for _, kind := range []string{"download_delete", "download_rename", "intake_source", "intake_target"} {
		t.Run(kind, func(t *testing.T) {
			d, p := setup(t, nil)
			defer d.cancel()
			store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			d.queue.store = store
			d.intakeJournal = store
			source := baiduapi.FileInfo{FsID: 91, Path: "/source/keep.bin", ServerFilename: "keep.bin", Size: 7, Mtime: 123}
			checkPath := source.Path
			if strings.HasPrefix(kind, "download") {
				mode := DownloadModeDelete
				if kind == "download_rename" {
					mode = DownloadModeKeep
					checkPath = source.Path + ".可删除"
				}
				job := queueJob{Version: 1, Info: source, Mode: mode, Status: "source_unknown", SourceState: "unknown", Created: time.Now().UTC()}
				body, _ := json.Marshal(job)
				if err = store.Save(source.FsID, body); err != nil {
					t.Fatal(err)
				}
			} else {
				target := "/queue/keep.bin"
				if kind == "intake_target" {
					checkPath = target
				}
				record := intakeRecord{Version: 1, Intake: Intake{ID: strings.Repeat("e", 32), State: "finished", QueueDir: d.cfg.QueueDir, Mode: DownloadModeKeep, Created: time.Now().UTC(), Expires: time.Now().Add(time.Hour), Items: []IntakeItem{{Source: source, SourceID: "91", Target: target, State: "unknown"}}}}
				if err = d.saveIntake(record); err != nil {
					t.Fatal(err)
				}
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			d.queue.store = nil
			restarted, err := OpenDownloader(d.cfg, d.getClient())
			if err != nil {
				t.Fatal(err)
			}
			defer shutdown(t, restarted)
			restarted.queue.mu.Lock()
			blocked := restarted.queue.isCloudMutationBlocked(source.FsID, "/renamed/identity.bin") && restarted.queue.isCloudMutationBlocked(0, checkPath)
			unrelated := restarted.queue.isCloudMutationBlocked(992, "/source-other/keep.bin")
			restarted.queue.mu.Unlock()
			if !blocked || unrelated || len(p.actions) != 0 {
				t.Fatalf("recovered quarantine blocked=%v unrelated=%v actions=%v", blocked, unrelated, p.actions)
			}
		})
	}
}
