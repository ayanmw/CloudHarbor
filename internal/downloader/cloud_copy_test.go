package downloader

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestCloudCopyConfirmedCacheFailureRecoversWithoutSourceRebase(t *testing.T) {
	d, p := setup(t, func(http.ResponseWriter, *http.Request) {})
	source := file(91, "original.bin", 7)
	target := source
	target.FsID = 92
	target.Path = "/archive/original.bin"
	p.lists["/queue"] = []baiduapi.FileInfo{source}
	p.lists["/archive"] = []baiduapi.FileInfo{}
	job := &queueJob{Version: 1, Info: source, Mode: DownloadModeKeep, Status: "pending", Created: time.Now().UTC()}
	store := seedEventJob(t, d, job)
	account := d.getClient().Fingerprint()
	observed := time.Now().UTC()
	if err := store.UpsertCloudInventoryPage(account, "copy-seed", "/archive", nil, observed, nil); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.ToSlash(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue", "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TRIGGER fail_copy_cache BEFORE UPDATE ON cloud_inventory_directories BEGIN SELECT RAISE(FAIL,'injected copy cache failure'); END`); err != nil {
		t.Fatal(err)
	}
	p.onMutation = func(operation, _ string) {
		if operation == "copy" {
			p.mu.Lock()
			p.lists["/archive"] = []baiduapi.FileInfo{target}
			p.mu.Unlock()
		}
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "copy", Expected: source, DestinationDirectory: "/archive"})
	if err == nil || result.Status != "confirmed" || len(p.actions) != 1 {
		t.Fatalf("fault not exercised: result=%+v err=%v", result, err)
	}
	if _, err = db.Exec(`DROP TRIGGER fail_copy_cache`); err != nil {
		t.Fatal(err)
	}
	shutdown(t, d)
	restarted, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, restarted)
	if got := restarted.queue.jobs[91]; got == nil || got.Info != source || got.Status != "pending" {
		t.Fatalf("copy source rebased after restart: %+v", got)
	}
	_, metadata, _, err := restarted.queue.store.ListCloudInventory(account, "/archive", 0, 10)
	if err != nil || metadata.Status != "stale" || len(p.actions) != 1 {
		t.Fatalf("cache recovery=%+v actions=%v err=%v", metadata, p.actions, err)
	}
}

func TestCloudCopyPreservesSourceTaskAndRecordsDistinctTarget(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	source := file(91, "original.bin", 7)
	p.lists["/queue"] = []baiduapi.FileInfo{source}
	p.lists["/archive"] = []baiduapi.FileInfo{}
	job := &queueJob{Version: 1, Info: source, Mode: DownloadModeKeep, Status: "pending", Created: time.Now().UTC()}
	store := seedEventJob(t, d, job)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	target := source
	target.FsID = 9007199254740995
	target.Path = "/archive/copy.bin"
	target.ServerFilename = "copy.bin"
	p.onMutation = func(operation, _ string) {
		if operation == "copy" {
			p.mu.Lock()
			p.lists["/archive"] = []baiduapi.FileInfo{target}
			p.mu.Unlock()
		}
	}
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "copy", Expected: source, DestinationDirectory: "/archive", NewName: "copy.bin"})
	if err != nil || result.Status != "confirmed" || result.ObjectID != 91 || result.DestinationPath != target.Path {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if job.Info != source || job.Status != "pending" || job.Mode != DownloadModeKeep {
		t.Fatalf("source task mutated: %+v", job)
	}
	events, _, _, err := store.RemoteOperationEventPage(result.OperationID, taskstore.RemoteOperationEventFilter{}, 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	observed := false
	for _, event := range events {
		var evidence map[string]json.RawMessage
		json.Unmarshal(event.Evidence, &evidence)
		if string(evidence["event"]) == `"copy_identity_observed"` {
			observed = true
			var got taskstore.CloudInventoryEntry
			json.Unmarshal(evidence["target"], &got)
			if got.FsID != 9007199254740995 || string(evidence["target_fs_id"]) != `"9007199254740995"` || got.Path != target.Path || event.ResponseObservedAtUTC == nil || len(evidence["source_observed_at_utc"]) == 0 || len(evidence["target_observed_at_utc"]) == 0 {
				t.Fatalf("missing precise identity/time: %s", event.Evidence)
			}
		}
	}
	if !observed || len(p.actions) != 1 || !strings.HasPrefix(p.actions[0], "copy:") {
		t.Fatalf("observation=%v actions=%v", observed, p.actions)
	}
}

func TestCloudCopyReceiptPersistenceFailureQuarantinesBeforeRestart(t *testing.T) {
	d, p := setup(t, func(http.ResponseWriter, *http.Request) {})
	source := file(91, "original.bin", 7)
	p.lists["/queue"] = []baiduapi.FileInfo{source}
	p.lists["/archive"] = []baiduapi.FileInfo{}
	job := &queueJob{Version: 1, Info: source, Mode: DownloadModeKeep, Status: "pending"}
	seedEventJob(t, d, job)
	db, err := sql.Open("sqlite", filepath.ToSlash(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue", "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TRIGGER fail_copy_receipt BEFORE UPDATE ON remote_operations WHEN NEW.status='confirmed' BEGIN SELECT RAISE(FAIL,'injected copy receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	request := CloudMutationRequest{Operation: "copy", Expected: source, DestinationDirectory: "/archive"}
	result, err := d.MutateCloud(context.Background(), request)
	if err == nil || result.Status != "unknown" || !d.queue.blockedRemoteIDs[91] || job.Info != source {
		t.Fatalf("receipt persistence window unprotected: %+v err=%v", result, err)
	}
	if _, err = d.MutateCloud(context.Background(), request); err == nil || len(p.actions) != 1 {
		t.Fatal("copy repeated after persistence failure")
	}
	if _, err = db.Exec(`DROP TRIGGER fail_copy_receipt`); err != nil {
		t.Fatal(err)
	}
	shutdown(t, d)
	restarted, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, restarted)
	operations, err := restarted.queue.store.LoadRemoteOperations()
	if err != nil || len(operations) != 1 || operations[0].Status != "unknown" || !restarted.queue.blockedRemoteIDs[91] {
		t.Fatalf("prepared copy not quarantined on restart: %+v err=%v", operations, err)
	}
}

func TestCloudCopyUnknownPersistsWithoutReplay(t *testing.T) {
	d, p := setup(t, func(http.ResponseWriter, *http.Request) {})
	source := baiduapi.FileInfo{FsID: 91, Path: "/source/a", ServerFilename: "a", Size: 7, Mtime: 1}
	p.lists["/source"] = []baiduapi.FileInfo{source}
	p.lists["/archive"] = []baiduapi.FileInfo{}
	p.unknownAction = true
	state := filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue")
	store, err := taskstore.Open(state, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "copy", Expected: source, DestinationDirectory: "/archive"})
	if err == nil || result.Status != "unknown" || !d.queue.blockedRemoteIDs[91] || !d.queue.blockedRemotePaths["/archive/a"] {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "copy", Expected: source, DestinationDirectory: "/archive"}); err == nil || len(p.actions) != 1 {
		t.Fatalf("unknown copy replayed: actions=%v err=%v", p.actions, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := taskstore.Open(state, d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	operations, err := reopened.LoadRemoteOperations()
	if err != nil || len(operations) != 1 || operations[0].Kind != "copy" || operations[0].Status != "unknown" || len(p.actions) != 1 {
		t.Fatalf("operations=%+v actions=%v err=%v", operations, p.actions, err)
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	d.queue.store = nil
	// Reopen the downloader itself, not only SQLite: startup must restore the
	// source/target quarantine before any user can submit another copy.
	restarted, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, restarted)
	restarted.SetAuthentication(restarted.getClient(), true, time.Now().Add(time.Hour))
	if !restarted.queue.blockedRemoteIDs[91] || !restarted.queue.blockedRemotePaths["/archive/a"] {
		t.Fatal("copy quarantine lost on restart")
	}
	if _, err = restarted.MutateCloud(context.Background(), CloudMutationRequest{Operation: "copy", Expected: source, DestinationDirectory: "/archive"}); err == nil || len(p.actions) != 1 {
		t.Fatalf("restart replayed unknown copy: actions=%v err=%v", p.actions, err)
	}
}

func TestCloudCopyConflictAndSelfDescendantSendNoWrite(t *testing.T) {
	for _, destination := range []string{"/source/folder/sub", "/archive"} {
		t.Run(destination, func(t *testing.T) {
			d, p := setup(t, func(http.ResponseWriter, *http.Request) {})
			source := baiduapi.FileInfo{FsID: 91, Path: "/source/folder", ServerFilename: "folder", IsDir: 1, Mtime: 1}
			p.lists["/source"] = []baiduapi.FileInfo{source}
			p.lists["/archive"] = []baiduapi.FileInfo{{FsID: 92, Path: "/archive/folder", ServerFilename: "folder", IsDir: 1}}
			store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			d.queue.store = store
			d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
			if _, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "copy", Expected: source, DestinationDirectory: destination}); err == nil {
				t.Fatal("accepted conflicting copy")
			}
			operations, _ := store.LoadRemoteOperations()
			if len(p.actions) != 0 || len(operations) != 0 {
				t.Fatalf("conflict reached write/journal: %v %+v", p.actions, operations)
			}
		})
	}
}

func TestCloudCopyReceiptDoesNotInventTargetIdentity(t *testing.T) {
	d, p := setup(t, func(http.ResponseWriter, *http.Request) {})
	source := file(91, "original.bin", 7)
	p.lists["/queue"] = []baiduapi.FileInfo{source}
	p.lists["/archive"] = []baiduapi.FileInfo{}
	job := &queueJob{Version: 1, Info: source, Mode: DownloadModeKeep, Status: "pending"}
	store := seedEventJob(t, d, job)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "copy", Expected: source, DestinationDirectory: "/archive"})
	if err != nil || result.Status != "confirmed" || !strings.Contains(result.Warning, "新身份尚未核实") || job.Info != source {
		t.Fatalf("result=%+v job=%+v err=%v", result, job, err)
	}
	events, _, _, err := store.RemoteOperationEventPage(result.OperationID, taskstore.RemoteOperationEventFilter{}, 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if strings.Contains(string(event.Evidence), `"event":"copy_identity_observed"`) || string(event.Evidence) == `{"event":"local_reconciliation_complete"}` {
			t.Fatalf("invented identity completion: %s", event.Evidence)
		}
	}
	if _, _, err = store.RemoteOperationPageFiltered(taskstore.RemoteOperationFilter{Kind: "copy"}, 0, 10); err != nil {
		t.Fatal(err)
	}
}
