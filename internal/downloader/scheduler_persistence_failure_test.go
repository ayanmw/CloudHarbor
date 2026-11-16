package downloader

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func loadPersistedJob(t *testing.T, d *Downloader, id int64) *queueJob {
	t.Helper()
	store := openEventStore(t, d)
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	body, ok := records[id]
	if !ok {
		t.Fatalf("persisted queue job %d missing", id)
	}
	var job queueJob
	if err := json.Unmarshal(body, &job); err != nil {
		t.Fatalf("decode persisted queue job %d: %v", id, err)
	}
	return &job
}

func TestClaimPersistenceFailureKeepsJobInPersistedState(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	info := file(12601, "claim-persist-failure.bin", 42)
	deadline := time.Now().UTC().Add(-time.Minute)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "retry_wait", Attempts: 2, Failures: 1, AttemptID: "previous-attempt", Error: "previously recorded error", NextAttempt: deadline}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.jobs[info.FsID] = job
	d.queue.eligible[info.FsID] = &info
	d.queue.enqueueCandidate(job)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	claimed, _, _ := d.queue.claimAt(time.Now())
	if claimed != nil {
		t.Fatalf("claim returned a job after its durable start event failed: %+v", claimed)
	}
	if job.Status != "retry_wait" || job.Attempts != 2 || job.Failures != 1 || job.AttemptID != "previous-attempt" || job.Error != "previously recorded error" || !job.NextAttempt.Equal(deadline) || !job.Updated.IsZero() {
		t.Fatalf("failed claim changed the in-memory job projection: %+v", job)
	}
	if d.QueueError() == nil {
		t.Fatal("SQLite failure did not put the queue into its fail-closed state")
	}
	if durable := loadPersistedJob(t, d, info.FsID); durable.Status != "retry_wait" || durable.Attempts != 2 || durable.AttemptID != "previous-attempt" || !durable.NextAttempt.Equal(deadline) {
		t.Fatalf("SQLite record changed despite failed claim event: %+v", durable)
	}
}

func TestAuthenticationWaitPersistenceFailureKeepsJobInPersistedState(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	info := file(12602, "auth-wait-persist-failure.bin", 42)
	deadline := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "retry_wait", Attempts: 3, Failures: 2, Error: "temporary provider error", NextAttempt: deadline}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.jobs[info.FsID] = job
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	d.SetAuthentication(baiduapi.NewClient("", "app"), false, time.Time{})
	if job.Status != "retry_wait" || job.Error != "temporary provider error" || !job.NextAttempt.Equal(deadline) || !job.Updated.IsZero() {
		t.Fatalf("failed auth-wait save changed the in-memory projection: %+v", job)
	}
	if d.QueueError() == nil {
		t.Fatal("SQLite failure did not put the queue into its fail-closed state")
	}
	if durable := loadPersistedJob(t, d, info.FsID); durable.Status != "retry_wait" || durable.Error != "temporary provider error" || !durable.NextAttempt.Equal(deadline) {
		t.Fatalf("SQLite record differs from pre-failure auth-wait state: %+v", durable)
	}
}

func TestCloudIdentityPersistenceFailureRestoresOriginalIdentity(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	oldInfo := file(12603, "old-name.bin", 42)
	job := &queueJob{Version: 1, Info: oldInfo, Mode: DownloadModeKeep, Status: "pending", Error: "previously recorded error"}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(oldInfo.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.jobs[oldInfo.FsID] = job
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	newInfo := oldInfo
	newInfo.Path = "/queue/new-name.bin"
	newInfo.ServerFilename = "new-name.bin"

	err = d.queue.reconcileObserved([]baiduapi.FileInfo{newInfo}, d.authEpoch, time.Now().UTC())
	if err == nil {
		t.Fatal("identity update succeeded with a closed SQLite store")
	}
	if job.Info != oldInfo || job.Status != "pending" || job.Error != "previously recorded error" || !job.Updated.IsZero() {
		t.Fatalf("failed identity update changed the persisted projection: %+v", job)
	}
	if d.QueueError() == nil {
		t.Fatal("SQLite failure did not put the queue into its fail-closed state")
	}
	if durable := loadPersistedJob(t, d, oldInfo.FsID); durable.Info != oldInfo || durable.Status != "pending" || durable.Error != "previously recorded error" {
		t.Fatalf("SQLite record differs from pre-failure cloud identity: %+v", durable)
	}
}

func TestAuthenticationRecoveryPersistenceFailureKeepsAuthWait(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	info := file(12604, "auth-recovery-persist-failure.bin", 42)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "auth_wait", Attempts: 2, Failures: 1, Error: ErrAuthenticationWaiting.Error()}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.jobs[info.FsID] = job
	d.SetAuthentication(baiduapi.NewClient("auth-recovery-token", "app"), true, time.Now().Add(time.Hour))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	err = d.queue.reconcileObserved([]baiduapi.FileInfo{info}, d.authEpoch, time.Now().UTC())
	if err == nil {
		t.Fatal("authentication recovery succeeded with a closed SQLite store")
	}
	if job.Status != "auth_wait" || job.Error != ErrAuthenticationWaiting.Error() || job.Attempts != 2 || job.Failures != 1 || !job.Updated.IsZero() {
		t.Fatalf("failed authentication recovery changed the persisted projection: %+v", job)
	}
	if d.QueueError() == nil {
		t.Fatal("SQLite failure did not put the queue into its fail-closed state")
	}
	if durable := loadPersistedJob(t, d, info.FsID); durable.Status != "auth_wait" || durable.Error != ErrAuthenticationWaiting.Error() {
		t.Fatalf("SQLite record differs from pre-failure auth-wait state: %+v", durable)
	}
}

func TestAttemptFinishPersistenceFailurePreservesDurablePause(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	info := file(12605, "finish-pause-persist-failure.bin", 42)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "downloading", Attempts: 1}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.jobs[info.FsID] = job
	canceled := false
	d.queue.active[info.FsID] = func() { canceled = true }
	d.taskMutex.Lock()
	d.inFlight[info.FsID] = true
	d.taskMutex.Unlock()
	if err := d.PauseTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	persistedPauseUpdated := job.Updated
	if !canceled || job.Status != "paused" || job.PausedFromStatus != "downloading" {
		t.Fatalf("pause fixture was not persisted: job=%+v canceled=%t", job, canceled)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	d.queue.complete(&queueJob{Info: info, AttemptID: "finish-pause-attempt"}, context.Canceled)
	if job.Status != "paused" || job.PausedFromStatus != "downloading" || job.Error != "" || !job.Updated.Equal(persistedPauseUpdated) {
		t.Fatalf("failed attempt finish overwrote the durable pause: %+v", job)
	}
	if d.QueueError() == nil {
		t.Fatal("SQLite failure did not put the queue into its fail-closed state")
	}
	if durable := loadPersistedJob(t, d, info.FsID); durable.Status != "paused" || durable.PausedFromStatus != "downloading" || !durable.Updated.Equal(persistedPauseUpdated) {
		t.Fatalf("SQLite record differs from the committed pause: %+v", durable)
	}
}

func TestSourceMissingPersistenceFailureKeepsPriorQueueProjection(t *testing.T) {
	original, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	info := file(92, "missing.txt", 3)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "retry_wait", Error: "previous retry", Attempts: 2, Failures: 1, NextAttempt: time.Now().Add(time.Minute)}
	d.queue.mu.Lock()
	d.queue.jobs[info.FsID] = job
	if err = d.queue.persist(job); err != nil {
		d.queue.mu.Unlock()
		t.Fatal(err)
	}
	d.queue.mu.Unlock()
	if err = d.queue.store.Close(); err != nil {
		t.Fatal(err)
	}
	err = d.queue.reconcileObserved(nil, d.authEpoch, time.Now().UTC())
	if err == nil {
		t.Fatal("expected source-missing event persistence failure")
	}
	if job.Status != "retry_wait" || job.Error != "previous retry" || job.Failures != 1 || job.NextAttempt.IsZero() {
		t.Fatalf("failed event write changed source-missing projection: %+v", job)
	}
	shutdown(t, d)
}

func TestCommittedNewJobBatchRemainsVisibleWhenFollowingBatchFails(t *testing.T) {
	original, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	triggerDB, err := sql.Open("sqlite", filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue", "queue.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer triggerDB.Close()
	lastID := int64(maxNewJobPersistenceBatch + 1)
	if _, err = triggerDB.Exec(`CREATE TRIGGER fail_later_batch_job BEFORE INSERT ON jobs WHEN NEW.id=` + fmt.Sprint(lastID) + ` BEGIN SELECT RAISE(FAIL,'injected later batch failure'); END`); err != nil {
		t.Fatal(err)
	}
	files := make([]baiduapi.FileInfo, lastID)
	for i := range files {
		id := int64(i + 1)
		files[i] = baiduapi.FileInfo{FsID: id, Path: fmt.Sprintf("/queue/%04d.txt", id), ServerFilename: fmt.Sprintf("%04d.txt", id), Size: 1}
	}
	if err = d.queue.reconcileObserved(files, d.authEpoch, time.Now().UTC()); err == nil {
		t.Fatal("expected second persistence batch to fail")
	}
	if len(d.queue.jobs) != maxNewJobPersistenceBatch {
		t.Fatalf("visible committed jobs=%d, want %d", len(d.queue.jobs), maxNewJobPersistenceBatch)
	}
	if task := d.GetTaskByFsID(1); task == nil || task.FileInfo.Path != files[0].Path {
		t.Fatalf("committed first-batch task is not visible: %+v", task)
	}
	if d.GetTaskByFsID(lastID) != nil {
		t.Fatal("failed later-batch job was published without a durable record")
	}
}
