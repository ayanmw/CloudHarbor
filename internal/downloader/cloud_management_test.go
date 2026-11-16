package downloader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestCloudRenameRechecksIdentityAndPersistsConfirmedOperation(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := baiduapi.FileInfo{FsID: 91, Path: "/source/old.txt", ServerFilename: "old.txt", Size: 7, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{info}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	request := CloudMutationRequest{Operation: "rename", Expected: info, NewName: "new.txt"}
	result, err := d.MutateCloud(context.Background(), request)
	if err != nil || result.Status != "confirmed" || result.DestinationPath != "/source/new.txt" {
		t.Fatalf("mutation=%+v err=%v", result, err)
	}
	operations, err := store.LoadRemoteOperations()
	if err != nil || len(operations) != 1 || operations[0].Status != "confirmed" || operations[0].ResponseObservedAtUTC == nil {
		t.Fatalf("remote evidence=%+v err=%v", operations, err)
	}
	events, _, _, err := store.RemoteOperationEventPage(result.OperationID, taskstore.RemoteOperationEventFilter{}, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	reconciled := 0
	for _, event := range events {
		if string(event.Evidence) == `{"event":"local_reconciliation_complete"}` {
			reconciled++
		}
	}
	if reconciled != 1 {
		t.Fatalf("local reconciliation marker count=%d events=%+v", reconciled, events)
	}
	if len(p.actions) != 1 {
		t.Fatalf("provider mutation calls=%d", len(p.actions))
	}
}

func TestConfirmedCloudRenameRebasesUntouchedQueueJobByFsID(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := file(90, "old-name.bin", 7)
	p.lists["/queue"] = []baiduapi.FileInfo{info}
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeDelete, Status: "pending", Created: time.Date(2026, 10, 30, 1, 2, 3, 0, time.UTC)}
	store := seedEventJob(t, d, job)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	p.onMutation = func(operation, _ string) {
		if operation != "rename" {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		updated := info
		updated.Path = "/queue/new-name.bin"
		updated.ServerFilename = "new-name.bin"
		updated.Mtime++
		p.lists["/queue"] = []baiduapi.FileInfo{updated}
	}
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: info, NewName: "new-name.bin"})
	if err != nil || result.Status != "confirmed" {
		t.Fatalf("confirmed rename=%+v err=%v", result, err)
	}
	if job.Info.Path != "/queue/new-name.bin" || job.Mode != DownloadModeDelete || job.Status != "pending" || job.Created.IsZero() {
		t.Fatalf("confirmed rename did not rebase the original task: %+v", job)
	}
	event, err := latestTaskEvent(t, store, info.FsID, "task_cloud_identity_updated")
	if err != nil {
		t.Fatal(err)
	}
	if event.OperationID != result.OperationID || event.CloudObservedAtUTC == nil || event.ResponseObservedAtUTC == nil || event.RecordedAtUTC.IsZero() {
		t.Fatalf("queue update evidence lacks operation or separated timestamps: %+v", event)
	}
	evidence := decodeEventEvidence(t, event)
	if evidence["previous_info"] == nil || evidence["current_info"] == nil || evidence["reason"] != "confirmed_cloud_mutation" {
		t.Fatalf("queue update evidence lacks before/after identity: %#v", evidence)
	}
}

func TestConfirmedCloudDeleteCancelsUntouchedQueueJobWithRemoteEvidence(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := file(901, "remove.bin", 7)
	p.lists["/queue"] = []baiduapi.FileInfo{info}
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending"}
	store := seedEventJob(t, d, job)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	p.onMutation = func(operation, _ string) {
		if operation != "delete" {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.lists["/queue"] = []baiduapi.FileInfo{}
	}
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: info})
	if err != nil || result.Status != "confirmed" {
		t.Fatalf("confirmed delete=%+v err=%v", result, err)
	}
	if job.Status != "cancelled" || job.Mode != DownloadModeKeep || !strings.Contains(job.Error, "远端") {
		t.Fatalf("confirmed delete did not cancel the existing queue projection: %+v", job)
	}
	event, err := latestTaskEvent(t, store, info.FsID, "task_source_deleted_remotely")
	if err != nil {
		t.Fatal(err)
	}
	if event.OperationID != result.OperationID || event.ResponseObservedAtUTC == nil || event.CloudObservedAtUTC != nil {
		t.Fatalf("delete event has incorrect operation/timestamp evidence: %+v", event)
	}
	evidence := decodeEventEvidence(t, event)
	if evidence["previous_info"] == nil || evidence["reason"] != "confirmed_cloud_delete" {
		t.Fatalf("delete event lacks previous remote identity: %#v", evidence)
	}
}

func TestConfirmedCloudDeleteQueueProjectionRecoversAfterRestart(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := file(906, "delete-recovery.bin", 7)
	p.lists["/queue"] = []baiduapi.FileInfo{info}
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending", Created: time.Now().UTC()}
	store := seedEventJob(t, d, job)
	db, err := sql.Open("sqlite", filepath.ToSlash(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue", "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TRIGGER fail_remote_delete_queue_event BEFORE INSERT ON job_events WHEN NEW.event_type='task_source_deleted_remotely' BEGIN SELECT RAISE(FAIL,'injected local delete event failure'); END`); err != nil {
		t.Fatal(err)
	}
	p.onMutation = func(operation, _ string) {
		if operation != "delete" {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.lists["/queue"] = []baiduapi.FileInfo{}
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: info})
	if err == nil || result.Status != "confirmed" || job.Status != "pending" || len(p.actions) != 1 {
		t.Fatalf("fault injection did not preserve confirmed remote result and old job: result=%+v job=%+v actions=%v err=%v", result, job, p.actions, err)
	}
	operations, err := store.LoadRemoteOperations()
	if err != nil || len(operations) != 1 || operations[0].Status != "confirmed" || operations[0].ResponseObservedAtUTC == nil {
		t.Fatalf("durable delete receipt=%+v err=%v", operations, err)
	}
	if _, err = db.Exec(`DROP TRIGGER fail_remote_delete_queue_event`); err != nil {
		t.Fatal(err)
	}
	shutdown(t, d)

	restarted, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, restarted)
	restarted.SetAuthentication(restarted.getClient(), true, time.Now().Add(time.Hour))
	if err = restarted.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	recovered := restarted.queue.jobs[info.FsID]
	if recovered == nil || recovered.Status != "cancelled" || recovered.Mode != DownloadModeKeep || recovered.Info != info {
		t.Fatalf("restart did not restore confirmed deletion outcome: %+v", recovered)
	}
	events, err := restarted.queue.store.Events(info.FsID)
	if err != nil {
		t.Fatal(err)
	}
	var deletedEvent, missingEvent *taskstore.Event
	for index := range events {
		if events[index].Kind == "task_source_deleted_remotely" {
			deletedEvent = &events[index]
		}
		if events[index].Kind == "task_source_missing" {
			missingEvent = &events[index]
		}
	}
	if deletedEvent == nil || deletedEvent.OperationID != result.OperationID || deletedEvent.ResponseObservedAtUTC == nil || deletedEvent.RecordedAtUTC.IsZero() || missingEvent != nil {
		t.Fatalf("recovered task event lacks exact remote evidence or duplicates source-missing: deleted=%+v missing=%+v all=%+v", deletedEvent, missingEvent, events)
	}
	if len(p.actions) != 1 {
		t.Fatalf("restart repeated remote delete: actions=%v", p.actions)
	}
}

func TestUnknownCloudRenameKeepsPendingQueueProjectionQuarantined(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := file(902, "uncertain.bin", 7)
	p.lists["/queue"] = []baiduapi.FileInfo{info}
	p.unknownAction = true
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeDelete, Status: "pending"}
	store := seedEventJob(t, d, job)
	account := d.getClient().Fingerprint()
	observed := time.Now().UTC()
	if err := store.UpsertCloudInventoryPage(account, "unknown-seed", "/", []taskstore.CloudInventoryEntry{{FsID: 903, Path: "/queue", Name: "queue", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertCloudInventoryPage(account, "unknown-seed", "/queue", []taskstore.CloudInventoryEntry{cloudInventoryEntry(info)}, observed, nil); err != nil {
		t.Fatal(err)
	}
	eventDB, err := sql.Open("sqlite", filepath.ToSlash(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue", "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	defer eventDB.Close()
	if _, err = eventDB.Exec(`CREATE TRIGGER fail_unknown_mutation_cache BEFORE UPDATE ON cloud_inventory_directories BEGIN SELECT RAISE(FAIL,'injected mutation cache failure'); END`); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: info, NewName: "uncertain-new.bin"})
	if err == nil || result.Status != "unknown" || !strings.Contains(result.Warning, "重启后会重试") {
		t.Fatalf("uncertain rename=%+v err=%v", result, err)
	}
	if job.Info != info || job.Status != "pending" || job.Mode != DownloadModeDelete || !d.queue.blockedRemoteIDs[info.FsID] {
		t.Fatalf("unknown result changed or released queued source identity: job=%+v blocked=%v", job, d.queue.blockedRemoteIDs[info.FsID])
	}
	if len(p.actions) != 1 {
		t.Fatalf("uncertain remote operation was retried: %v", p.actions)
	}
	remoteEvents, _, _, err := store.RemoteOperationEventPage(result.OperationID, taskstore.RemoteOperationEventFilter{}, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	cacheRecoveryRecorded := false
	for _, event := range remoteEvents {
		if string(event.Evidence) == `{"event":"local_cache_recovery_complete"}` && event.Status == "unknown" {
			cacheRecoveryRecorded = true
		}
	}
	if cacheRecoveryRecorded {
		t.Fatalf("failed cache invalidation was incorrectly marked complete: %+v", remoteEvents)
	}
	if _, err = eventDB.Exec(`DROP TRIGGER fail_unknown_mutation_cache`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, recoveredDirectory, _, err := store.ListCloudInventory(account, "/queue", 0, 10)
	if err != nil || recoveredDirectory.Status != "stale" {
		t.Fatalf("reopened unknown operation did not stale cache: %+v err=%v", recoveredDirectory, err)
	}
	remoteEvents, _, _, err = store.RemoteOperationEventPage(result.OperationID, taskstore.RemoteOperationEventFilter{}, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	cacheRecoveryRecorded = false
	for _, event := range remoteEvents {
		if string(event.Evidence) == `{"event":"local_cache_recovery_complete"}` && event.Status == "unknown" {
			cacheRecoveryRecorded = true
		}
	}
	if !cacheRecoveryRecorded {
		t.Fatalf("retried unknown operation lacks durable cache-recovery marker: %+v", remoteEvents)
	}
	events, err := store.Events(info.FsID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == "task_cloud_identity_updated" || event.Kind == "task_source_deleted_remotely" {
			t.Fatalf("unknown remote result wrote a confirmed queue transition: %+v", event)
		}
	}
}

func TestUnknownCloudResultPersistenceFailureStillQuarantinesSource(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := file(904, "unpersisted-unknown.bin", 7)
	p.lists["/queue"] = []baiduapi.FileInfo{info}
	p.unknownAction = true
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeDelete, Status: "pending"}
	store := seedEventJob(t, d, job)
	account := d.getClient().Fingerprint()
	observed := time.Now().UTC()
	if err := store.UpsertCloudInventoryPage(account, "persist-failure-seed", "/", []taskstore.CloudInventoryEntry{{FsID: 905, Path: "/queue", Name: "queue", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertCloudInventoryPage(account, "persist-failure-seed", "/queue", []taskstore.CloudInventoryEntry{cloudInventoryEntry(info)}, observed, nil); err != nil {
		t.Fatal(err)
	}
	queueDB, err := sql.Open("sqlite", filepath.ToSlash(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue", "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	defer queueDB.Close()
	if _, err = queueDB.Exec(`CREATE TRIGGER fail_unknown_status_persist BEFORE UPDATE ON remote_operations BEGIN SELECT RAISE(FAIL,'injected unknown persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: info, NewName: "unpersisted-unknown-new.bin"})
	if err == nil || result.Status != "unknown" || !strings.Contains(result.Warning, "本地缓存已标为失效") {
		t.Fatalf("unpersisted unknown result=%+v err=%v", result, err)
	}
	if !d.queue.blockedRemoteIDs[info.FsID] || !d.queue.blockedRemotePaths[info.Path] || !d.queue.blockedRemotePaths[result.DestinationPath] {
		t.Fatalf("unknown source was not quarantined in memory: id=%v source=%v target=%v", d.queue.blockedRemoteIDs[info.FsID], d.queue.blockedRemotePaths[info.Path], d.queue.blockedRemotePaths[result.DestinationPath])
	}
	if job.Info != info || job.Status != "pending" || len(p.actions) != 1 {
		t.Fatalf("persistence failure changed queued identity or retried remote mutation: job=%+v actions=%v", job, p.actions)
	}
	operations, err := store.LoadRemoteOperations()
	if err != nil || len(operations) != 1 || operations[0].Status != "prepared" {
		t.Fatalf("failed unknown persistence should leave a prepared journal row for startup recovery: operations=%+v err=%v", operations, err)
	}
	_, directory, _, err := store.ListCloudInventory(account, "/queue", 0, 10)
	if err != nil || directory.Status != "stale" {
		t.Fatalf("cache was not invalidated after unknown persistence failure: %+v err=%v", directory, err)
	}
	if _, err = queueDB.Exec(`DROP TRIGGER fail_unknown_status_persist`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	operations, err = store.LoadRemoteOperations()
	if err != nil || len(operations) != 1 || operations[0].Status != "unknown" {
		t.Fatalf("prepared journal did not recover as unknown after reopen: operations=%+v err=%v", operations, err)
	}
	remoteEvents, _, _, err := store.RemoteOperationEventPage(operations[0].ID, taskstore.RemoteOperationEventFilter{}, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	persistenceFailureRecorded := false
	for _, event := range remoteEvents {
		var evidence map[string]string
		if json.Unmarshal(event.Evidence, &evidence) == nil && evidence["event"] == "unknown_result_persistence_failed" && event.Status == "prepared" && !event.RecordedAtUTC.IsZero() {
			persistenceFailureRecorded = true
		}
	}
	if !persistenceFailureRecorded {
		t.Fatalf("failed unknown transition was not retained as local evidence: %+v", remoteEvents)
	}
	_, directory, _, err = store.ListCloudInventory(account, "/queue", 0, 10)
	if err != nil || directory.Status != "stale" {
		t.Fatalf("startup recovery did not preserve stale cache: %+v err=%v", directory, err)
	}
}

func TestConfirmedCloudMoveOutAndBackKeepsOneTaskAndAllowsManualRetry(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	current := file(903, "move-back.bin", 7)
	p.lists["/queue"] = []baiduapi.FileInfo{current}
	p.lists["/archive"] = []baiduapi.FileInfo{}
	job := &queueJob{Version: 1, Info: current, Mode: DownloadModeDelete, Status: "pending"}
	store := seedEventJob(t, d, job)
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	moveCount := 0
	p.onMutation = func(operation, _ string) {
		if operation != "move" {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		current.Mtime++
		if moveCount == 0 {
			current.Path = "/archive/move-back.bin"
			p.lists["/queue"] = []baiduapi.FileInfo{}
			p.lists["/archive"] = []baiduapi.FileInfo{current}
		} else {
			current.Path = "/queue/move-back.bin"
			p.lists["/archive"] = []baiduapi.FileInfo{}
			p.lists["/queue"] = []baiduapi.FileInfo{current}
		}
		moveCount++
	}
	first, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "move", Expected: current, DestinationDirectory: "/archive"})
	if err != nil || first.Status != "confirmed" || job.Status != "failed" || job.Info.Path != "/archive/move-back.bin" || job.Mode != DownloadModeDelete {
		t.Fatalf("move out of queue did not preserve the same task identity/history: result=%+v job=%+v err=%v", first, job, err)
	}
	second, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "move", Expected: current, DestinationDirectory: "/queue"})
	if err != nil || second.Status != "confirmed" || job.Status != "failed" || job.Info.Path != "/queue/move-back.bin" || job.Mode != DownloadModeDelete || !strings.Contains(job.Error, "可手动重试") {
		t.Fatalf("move back did not reconcile the existing failed task for manual retry: result=%+v job=%+v err=%v", second, job, err)
	}
	events, err := store.Events(current.FsID)
	if err != nil {
		t.Fatal(err)
	}
	var linkedFirst, linkedSecond bool
	for _, event := range events {
		if event.Kind != "task_cloud_identity_updated" {
			continue
		}
		if event.OperationID == first.OperationID && event.ResponseObservedAtUTC != nil && event.CloudObservedAtUTC != nil {
			linkedFirst = true
		}
		if event.OperationID == second.OperationID && event.ResponseObservedAtUTC != nil && event.CloudObservedAtUTC != nil && event.EvidenceType == "source_returned_after_confirmed_mutation" {
			linkedSecond = true
		}
	}
	if !linkedFirst || !linkedSecond {
		t.Fatalf("move identity evidence was not linked to both confirmations: %+v", events)
	}
	if err = d.Retry(current.FsID); err != nil || job.Status != "pending" || job.Info.Path != "/queue/move-back.bin" || job.Mode != DownloadModeDelete {
		t.Fatalf("returned source was not manually retryable on its original task: job=%+v err=%v", job, err)
	}
}

func TestConfirmedCloudRenameUpdatesCompleteInventoryWithoutStalingIt(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := baiduapi.FileInfo{FsID: 97, Path: "/source/old.txt", ServerFilename: "old.txt", Size: 7, Mtime: 123}
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 96, Path: "/source", ServerFilename: "source", IsDir: 1}}
	p.lists["/source"] = []baiduapi.FileInfo{info}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	account := d.getClient().Fingerprint()
	observed := time.Date(2026, 10, 30, 6, 0, 0, 0, time.UTC)
	if err = store.UpsertCloudInventoryPage(account, "seed", "/", []taskstore.CloudInventoryEntry{{FsID: 96, Path: "/source", Name: "source", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage(account, "seed", "/source", []taskstore.CloudInventoryEntry{{FsID: 97, Path: info.Path, Name: info.ServerFilename, Size: info.Size, ProviderMtime: info.Mtime}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	p.onMutation = func(operation, _ string) {
		if operation != "rename" {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.lists["/source"] = []baiduapi.FileInfo{{FsID: 97, Path: "/source/new.txt", ServerFilename: "new.txt", Size: 7, Mtime: 124}}
	}
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: info, NewName: "new.txt"})
	if err != nil || result.Status != "confirmed" {
		t.Fatalf("confirmed rename=%+v err=%v", result, err)
	}
	items, directory, total, err := store.ListCloudInventory(account, "/source", 0, 10)
	if err != nil || directory.Status != "complete" || total != 1 || len(items) != 1 || items[0].Path != "/source/new.txt" {
		t.Fatalf("inventory after confirmed rename=%+v directory=%+v total=%d err=%v", items, directory, total, err)
	}
	size, err := store.CloudDirectorySize(account, "/source")
	if err != nil || !size.Complete || size.Bytes != 7 || size.Files != 1 {
		t.Fatalf("size after confirmed rename=%+v err=%v", size, err)
	}
}

func TestCloudMutationRejectsScanQueueOverlapBeforeJournalOrProviderWrite(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := baiduapi.FileInfo{FsID: 98, Path: "/source/old.txt", ServerFilename: "old.txt", Size: 7, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{info}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	now := time.Now().UTC()
	if err = store.StartCloudScan(&taskstore.CloudScanState{
		AccountFingerprint: d.getClient().Fingerprint(), RunID: "scan", Status: "running", Phase: "listing",
		CurrentDirectory: "/source", StartedAtUTC: now, UpdatedAtUTC: now,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: info, NewName: "new.txt"})
	if !errors.Is(err, ErrIntakeConflict) {
		t.Fatalf("mutation overlapping queued scan path was not rejected: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("provider write ran despite scan queue overlap: %v", p.actions)
	}
	operations, err := store.LoadRemoteOperations()
	if err != nil || len(operations) != 0 {
		t.Fatalf("rejected mutation left an operation journal: %+v err=%v", operations, err)
	}
}

func TestCloudMutationRejectsActiveDuplicateScanBeforeProviderWrite(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := baiduapi.FileInfo{FsID: 982, Path: "/source/old.txt", ServerFilename: "old.txt", Size: 7, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{info}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	now := time.Now().UTC()
	if err = store.SaveCloudDuplicateScanState(taskstore.CloudDuplicateScanState{
		AccountFingerprint: d.getClient().Fingerprint(), RunID: "hash-run", Status: "running", Phase: "hashing",
		StartedAtUTC: now, UpdatedAtUTC: now,
	}, true); err != nil {
		t.Fatal(err)
	}
	// A persisted running row can survive restart without a worker. This test
	// protects an actual live worker, which must still reject namespace writes.
	d.inventoryScanMutex.Lock()
	d.inventoryScanKind = "duplicate"
	d.inventoryScanCancel = func() {}
	d.inventoryScanMutex.Unlock()
	defer func() {
		d.inventoryScanMutex.Lock()
		d.inventoryScanCancel = nil
		d.inventoryScanKind = ""
		d.inventoryScanMutex.Unlock()
	}()
	_, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: info, NewName: "new.txt"})
	if err == nil || !strings.Contains(err.Error(), "网盘扫描已经在运行") {
		t.Fatalf("mutation overlapped duplicate hash scan without conflict: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("provider write ran while duplicate scan was active: %v", p.actions)
	}
	checkpoint, err := store.LoadCloudDuplicateScanState(d.getClient().Fingerprint())
	if err != nil || checkpoint.Phase != "hashing" {
		t.Fatalf("active worker checkpoint was invalidated: %+v %v", checkpoint, err)
	}
	operations, err := store.LoadRemoteOperations()
	if err != nil || len(operations) != 0 {
		t.Fatalf("rejected mutation left operation evidence: %+v err=%v", operations, err)
	}
}

func TestCloudMutationRejectsScanAdmissionWindowBeforeProviderWrite(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := baiduapi.FileInfo{FsID: 983, Path: "/source/old.txt", ServerFilename: "old.txt", Size: 7, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{info}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	d.inventoryScanMutex.Lock()
	d.inventoryScanStarting = true
	d.inventoryScanMutex.Unlock()
	_, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: info, NewName: "new.txt"})
	if err == nil || !strings.Contains(err.Error(), "扫描") {
		t.Fatalf("mutation was admitted while scan held its start reservation: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("provider write ran during scan admission: %v", p.actions)
	}
	operations, err := store.LoadRemoteOperations()
	if err != nil || len(operations) != 0 {
		t.Fatalf("rejected mutation left operation evidence: %+v err=%v", operations, err)
	}
	d.inventoryScanMutex.Lock()
	d.inventoryScanStarting = false
	d.inventoryScanMutex.Unlock()
}

func TestCloudMutationRejectsScopedRefreshQueueOverlapBeforeJournalOrProviderWrite(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := baiduapi.FileInfo{FsID: 981, Path: "/source/old.txt", ServerFilename: "old.txt", Size: 7, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{info}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	now := time.Now().UTC()
	if err = store.StartCloudInventoryRefresh(&taskstore.CloudInventoryRefreshState{
		AccountFingerprint: d.getClient().Fingerprint(), RunID: "refresh", RootPath: "/source/child",
		Status: "paused", Phase: "paused", CurrentDirectory: "/source/child",
		StartedAtUTC: now, UpdatedAtUTC: now,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: info, NewName: "new.txt"})
	if !errors.Is(err, ErrIntakeConflict) {
		t.Fatalf("mutation overlapping scoped refresh queue was not rejected: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("provider write ran despite scoped refresh queue overlap: %v", p.actions)
	}
	operations, err := store.LoadRemoteOperations()
	if err != nil || len(operations) != 0 {
		t.Fatalf("rejected mutation left an operation journal: %+v err=%v", operations, err)
	}
}

func TestConfirmedCloudRenameWithMismatchedProviderObservationStalesCache(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := baiduapi.FileInfo{FsID: 99, Path: "/source/old.txt", ServerFilename: "old.txt", Size: 7, Mtime: 123}
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 100, Path: "/source", ServerFilename: "source", IsDir: 1}}
	p.lists["/source"] = []baiduapi.FileInfo{info}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	account := d.getClient().Fingerprint()
	observed := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage(account, "seed", "/", []taskstore.CloudInventoryEntry{{FsID: 100, Path: "/source", Name: "source", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage(account, "seed", "/source", []taskstore.CloudInventoryEntry{{FsID: 99, Path: info.Path, Name: info.ServerFilename, Size: info.Size, ProviderMtime: info.Mtime}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	p.onMutation = func(operation, _ string) {
		if operation != "rename" {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.lists["/source"] = []baiduapi.FileInfo{{FsID: 99, Path: "/source/not-the-requested-name.txt", ServerFilename: "not-the-requested-name.txt", Size: 7, Mtime: 124}}
	}
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: info, NewName: "new.txt"})
	if err != nil || result.Status != "confirmed" || result.Warning == "" {
		t.Fatalf("confirmed but unverified result=%+v err=%v", result, err)
	}
	_, directory, _, err := store.ListCloudInventory(account, "/source", 0, 10)
	if err != nil || directory.Status != "stale" {
		t.Fatalf("unverified remote mutation did not stale local cache: %+v err=%v", directory, err)
	}
	operations, err := store.LoadRemoteOperations()
	if err != nil || len(operations) != 1 || operations[0].Status != "confirmed" || operations[0].ResponseObservedAtUTC == nil {
		t.Fatalf("cache verification changed remote evidence: %+v err=%v", operations, err)
	}
	var eventStatus string
	var recordedAt int64
	var evidence string
	eventDB, err := sql.Open("sqlite", filepath.ToSlash(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue", "queue.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	defer eventDB.Close()
	if err = eventDB.QueryRow(`SELECT status,recorded_at_utc,evidence_json FROM remote_operation_events WHERE operation_id=? AND CAST(evidence_json AS TEXT) LIKE '%cache_refresh_failed%' ORDER BY sequence DESC LIMIT 1`, result.OperationID).Scan(&eventStatus, &recordedAt, &evidence); err != nil {
		t.Fatal(err)
	}
	if eventStatus != "confirmed" || recordedAt == 0 || !strings.Contains(evidence, `"event":"cache_refresh_failed"`) || !strings.Contains(evidence, "对象身份") {
		t.Fatalf("cache-refresh failure was not durably diagnosed: status=%q recorded=%d evidence=%s", eventStatus, recordedAt, evidence)
	}
}

func TestConfirmedMkdirWithoutUniqueProviderIdentityReturnsRefreshWarning(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 101, Path: "/source", ServerFilename: "source", IsDir: 1}}
	p.lists["/source"] = []baiduapi.FileInfo{}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	account := d.getClient().Fingerprint()
	observed := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage(account, "seed", "/", []taskstore.CloudInventoryEntry{{FsID: 101, Path: "/source", Name: "source", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage(account, "seed", "/source", nil, observed, nil); err != nil {
		t.Fatal(err)
	}
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "mkdir", DestinationDirectory: "/source", NewName: "created"})
	if err != nil || result.Status != "confirmed" || result.Warning == "" {
		t.Fatalf("confirmed mkdir without identity=%+v err=%v", result, err)
	}
	if len(p.actions) != 1 || p.actions[0] != "create:/source/created" {
		t.Fatalf("expected one confirmed remote create: %v", p.actions)
	}
	_, directory, _, err := store.ListCloudInventory(account, "/source", 0, 10)
	if err != nil || directory.Status != "stale" {
		t.Fatalf("unverified mkdir did not stale cache: %+v err=%v", directory, err)
	}
}

func TestConfirmedMkdirWithUniqueProviderIdentityUpdatesCompleteInventory(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 111, Path: "/source", ServerFilename: "source", IsDir: 1}}
	p.lists["/source"] = []baiduapi.FileInfo{}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	account := d.getClient().Fingerprint()
	observed := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage(account, "seed", "/", []taskstore.CloudInventoryEntry{{FsID: 111, Path: "/source", Name: "source", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage(account, "seed", "/source", nil, observed, nil); err != nil {
		t.Fatal(err)
	}
	p.onCreate = func(createdPath string) {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.lists["/source"] = []baiduapi.FileInfo{{FsID: 112, Path: createdPath, ServerFilename: "created", IsDir: 1}}
	}
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "mkdir", DestinationDirectory: "/source", NewName: "created"})
	if err != nil || result.Status != "confirmed" || result.Warning != "" {
		t.Fatalf("confirmed mkdir=%+v err=%v", result, err)
	}
	items, directory, total, err := store.ListCloudInventory(account, "/source", 0, 10)
	if err != nil || directory.Status != "complete" || total != 1 || len(items) != 1 || items[0].FsID != 112 || !items[0].Directory {
		t.Fatalf("inventory after mkdir=%+v directory=%+v total=%d err=%v", items, directory, total, err)
	}
	size, err := store.CloudDirectorySize(account, "/source")
	if err != nil || !size.Complete || size.Directories != 1 || size.Bytes != 0 {
		t.Fatalf("size after mkdir=%+v err=%v", size, err)
	}
}

func TestConfirmedCloudMoveUpdatesCompleteInventoryAndSize(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := baiduapi.FileInfo{FsID: 113, Path: "/source/file.bin", ServerFilename: "file.bin", Size: 13, Mtime: 7}
	p.lists["/"] = []baiduapi.FileInfo{
		{FsID: 114, Path: "/source", ServerFilename: "source", IsDir: 1},
		{FsID: 115, Path: "/archive", ServerFilename: "archive", IsDir: 1},
	}
	p.lists["/source"] = []baiduapi.FileInfo{info}
	p.lists["/archive"] = []baiduapi.FileInfo{}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	account := d.getClient().Fingerprint()
	observed := time.Now().UTC()
	for _, page := range []struct {
		path    string
		entries []taskstore.CloudInventoryEntry
	}{
		{path: "/", entries: []taskstore.CloudInventoryEntry{
			{FsID: 114, Path: "/source", Name: "source", Directory: true},
			{FsID: 115, Path: "/archive", Name: "archive", Directory: true},
		}},
		{path: "/source", entries: []taskstore.CloudInventoryEntry{{FsID: 113, Path: info.Path, Name: info.ServerFilename, Size: info.Size, ProviderMtime: info.Mtime}}},
		{path: "/archive"},
	} {
		if err = store.UpsertCloudInventoryPage(account, "seed", page.path, page.entries, observed, nil); err != nil {
			t.Fatal(err)
		}
	}
	p.onMutation = func(operation, _ string) {
		if operation != "move" {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.lists["/source"] = []baiduapi.FileInfo{}
		p.lists["/archive"] = []baiduapi.FileInfo{{FsID: 113, Path: "/archive/file.bin", ServerFilename: "file.bin", Size: 13, Mtime: 8}}
	}
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "move", Expected: info, DestinationDirectory: "/archive"})
	if err != nil || result.Status != "confirmed" || result.Warning != "" {
		t.Fatalf("confirmed move=%+v err=%v", result, err)
	}
	for _, want := range []struct {
		path     string
		bytes    int64
		complete bool
	}{
		{path: "/source", bytes: 0, complete: true},
		{path: "/archive", bytes: 13, complete: true},
	} {
		size, sizeErr := store.CloudDirectorySize(account, want.path)
		if sizeErr != nil || size.Bytes != want.bytes || size.Complete != want.complete {
			t.Fatalf("size after move for %s=%+v err=%v", want.path, size, sizeErr)
		}
	}
	items, directory, total, err := store.ListCloudInventory(account, "/archive", 0, 10)
	if err != nil || directory.Status != "complete" || total != 1 || len(items) != 1 || items[0].Path != "/archive/file.bin" {
		t.Fatalf("inventory after move=%+v directory=%+v total=%d err=%v", items, directory, total, err)
	}
}

func TestConfirmedCloudDeleteUpdatesCompleteInventoryAndSize(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := baiduapi.FileInfo{FsID: 116, Path: "/source/file.bin", ServerFilename: "file.bin", Size: 13, Mtime: 7}
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 117, Path: "/source", ServerFilename: "source", IsDir: 1}}
	p.lists["/source"] = []baiduapi.FileInfo{info}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	account := d.getClient().Fingerprint()
	observed := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage(account, "seed", "/", []taskstore.CloudInventoryEntry{{FsID: 117, Path: "/source", Name: "source", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage(account, "seed", "/source", []taskstore.CloudInventoryEntry{{FsID: 116, Path: info.Path, Name: info.ServerFilename, Size: info.Size, ProviderMtime: info.Mtime}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: info})
	if err != nil || result.Status != "confirmed" || result.Warning != "" {
		t.Fatalf("confirmed delete=%+v err=%v", result, err)
	}
	items, directory, total, err := store.ListCloudInventory(account, "/source", 0, 10)
	if err != nil || directory.Status != "complete" || total != 0 || len(items) != 0 {
		t.Fatalf("inventory after delete=%+v directory=%+v total=%d err=%v", items, directory, total, err)
	}
	size, err := store.CloudDirectorySize(account, "/source")
	if err != nil || !size.Complete || size.Files != 0 || size.Bytes != 0 {
		t.Fatalf("size after delete=%+v err=%v", size, err)
	}
}

func TestUnknownCloudMutationKeepsQuarantineAndNeverAppliesDelta(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	info := baiduapi.FileInfo{FsID: 102, Path: "/source/old.txt", ServerFilename: "old.txt", Size: 7, Mtime: 123}
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 103, Path: "/source", ServerFilename: "source", IsDir: 1}}
	p.lists["/source"] = []baiduapi.FileInfo{info}
	p.unknownAction = true
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	account := d.getClient().Fingerprint()
	observed := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage(account, "seed", "/", []taskstore.CloudInventoryEntry{{FsID: 103, Path: "/source", Name: "source", Directory: true}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage(account, "seed", "/source", []taskstore.CloudInventoryEntry{{FsID: 102, Path: info.Path, Name: info.ServerFilename, Size: info.Size, ProviderMtime: info.Mtime}}, observed, nil); err != nil {
		t.Fatal(err)
	}
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: info, NewName: "new.txt"})
	if err == nil || result.Status != "unknown" {
		t.Fatalf("uncertain provider result=%+v err=%v", result, err)
	}
	if !d.queue.blockedRemoteIDs[info.FsID] || !d.queue.blockedRemotePaths[info.Path] {
		t.Fatal("uncertain provider result did not quarantine the source")
	}
	_, directory, _, err := store.ListCloudInventory(account, "/source", 0, 10)
	if err != nil || directory.Status != "stale" {
		t.Fatalf("uncertain provider result did not stale inventory: %+v err=%v", directory, err)
	}
	operations, err := store.LoadRemoteOperations()
	if err != nil || len(operations) != 1 || operations[0].Status != "unknown" {
		t.Fatalf("uncertain remote operation evidence=%+v err=%v", operations, err)
	}
}

func TestCloudRenameRejectsChangedSourceIdentityBeforeMutation(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	current := baiduapi.FileInfo{FsID: 92, Path: "/source/new.txt", ServerFilename: "new.txt", Size: 11, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{current}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	expected := current
	expected.Path = "/source/old.txt"
	expected.ServerFilename = "old.txt"
	_, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "rename", Expected: expected, NewName: "renamed.txt"})
	if err == nil {
		t.Fatal("changed object identity accepted")
	}
	if len(p.actions) != 0 {
		t.Fatalf("cloud mutation sent despite identity mismatch: %v", p.actions)
	}
}

func TestEmptyDirectoryDeleteRechecksLiveChildrenBeforeMutation(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	dir := baiduapi.FileInfo{FsID: 93, Path: "/source/empty", ServerFilename: "empty", IsDir: 1, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{dir}
	p.lists[dir.Path] = []baiduapi.FileInfo{{FsID: 94, Path: "/source/empty/new-child", ServerFilename: "new-child", Size: 1}}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	_, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: dir, RequireEmptyDirectory: true})
	if !errors.Is(err, ErrIntakeConflict) {
		t.Fatalf("non-empty remote directory was not rejected: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("non-empty directory delete was sent: %v", p.actions)
	}
}

func TestMutateCloudUsesQueuePathAfterPathChangeBeforeAdmission(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	monitor := baiduapi.FileInfo{FsID: 811, Path: "/new-queue", ServerFilename: "new-queue", IsDir: 1, Mtime: 42}
	p.lists["/"] = []baiduapi.FileInfo{monitor}
	p.lists[monitor.Path] = []baiduapi.FileInfo{}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	d.cloudMutationAdmissionHook = func() {
		close(entered)
		<-release
	}
	mutation := make(chan struct {
		result CloudMutationResult
		err    error
	}, 1)
	go func() {
		result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: monitor})
		mutation <- struct {
			result CloudMutationResult
			err    error
		}{result: result, err: err}
	}()
	defer releaseOnce.Do(func() { close(release) })
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("mutation did not reach the controlled admission point")
	}
	if _, err := d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/new-queue", LocalDownDir: d.LocalDownloadDirectory()}, false); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	result := <-mutation
	if !errors.Is(result.err, ErrIntakeInvalid) {
		t.Fatalf("mutation was not rejected against the updated monitor directory: result=%+v err=%v", result.result, result.err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("protected monitor directory reached remote mutation: %v", p.actions)
	}
}

func TestEmptyDirectoryPreviewExcludesManagedQueueSubtree(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	account := d.getClient().Fingerprint()
	now := time.Now().UTC()
	for _, page := range []struct {
		dir     string
		entries []taskstore.CloudInventoryEntry
	}{
		{dir: "/", entries: []taskstore.CloudInventoryEntry{
			{FsID: 301, Path: "/queue", Name: "queue", Directory: true},
			{FsID: 302, Path: "/outside", Name: "outside", Directory: true},
		}},
		{dir: "/queue", entries: []taskstore.CloudInventoryEntry{{FsID: 303, Path: "/queue/nested", Name: "nested", Directory: true}}},
		{dir: "/queue/nested"},
		{dir: "/outside"},
	} {
		if err = store.UpsertCloudInventoryPage(account, "empty-preview", page.dir, page.entries, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.StartCloudScan(&taskstore.CloudScanState{AccountFingerprint: account, RunID: "empty-preview", Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCloudScanState(account, "empty-preview", "complete", "complete", "/", 0, 4, 4, now, now, ""); err != nil {
		t.Fatal(err)
	}
	items, err := d.PreviewEmptyCloudDirectories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Path != "/outside" {
		t.Fatalf("preview included managed queue directories: %+v", items)
	}
}

func TestDuplicateDeleteRehashesContentBeforeRemoteMutation(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	item := baiduapi.FileInfo{FsID: 95, Path: "/source/copy.bin", ServerFilename: "copy.bin", Size: 7, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{item}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	digest := sha256.Sum256([]byte("payload"))
	result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: item, ExpectedContentSHA256: hex.EncodeToString(digest[:])})
	if err != nil || result.Status != "confirmed" {
		t.Fatalf("verified delete=%+v err=%v", result, err)
	}
	if len(p.actions) != 1 {
		t.Fatalf("expected one provider delete after digest recheck, got %v", p.actions)
	}
}

func TestDuplicateDeleteRejectsChangedContentBeforeRemoteMutation(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("payload")) })
	item := baiduapi.FileInfo{FsID: 96, Path: "/source/copy.bin", ServerFilename: "copy.bin", Size: 7, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{item}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	_, err = d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: item, ExpectedContentSHA256: strings.Repeat("0", 64)})
	if !errors.Is(err, ErrIntakeStale) {
		t.Fatalf("content change was not rejected: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("delete sent for unverified content: %v", p.actions)
	}
}
