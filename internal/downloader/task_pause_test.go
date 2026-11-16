package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestPausePendingTaskPersistsAndResumesIdempotently(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	info := file(901, "pending.bin", 42)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending"}
	body, _ := json.Marshal(job)
	if err := store.Save(info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.mu.Lock()
	d.queue.jobs[info.FsID] = job
	d.queue.eligible[info.FsID] = &info
	d.queue.enqueueCandidate(job)
	d.queue.mu.Unlock()

	if err := d.PauseTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	if got := d.GetTasks()[0]; got.Status != "paused" || got.CanPause || !got.CanResume {
		t.Fatalf("paused task view = %+v", got)
	}
	if err := d.PauseTask(info.FsID); err != nil {
		t.Fatalf("repeated pause should be idempotent: %v", err)
	}
	if err := d.ResumeTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	if got := d.GetTasks()[0]; got.Status != "pending" || !got.CanPause || got.CanResume {
		t.Fatalf("resumed task view = %+v", got)
	}
	if err := d.ResumeTask(info.FsID); err != ErrTaskConflict {
		t.Fatalf("repeated resume = %v, want conflict", err)
	}
}

func TestPausedRetryWaitPreservesItsDeadlineAndTaskIdentity(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	info := file(902, "retry.bin", 42)
	deadline := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "retry_wait", Failures: 2, Attempts: 3, NextAttempt: deadline}
	body, _ := json.Marshal(job)
	if err := store.Save(info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.mu.Lock()
	d.queue.jobs[info.FsID] = job
	d.queue.eligible[info.FsID] = &info
	d.queue.enqueueCandidate(job)
	d.queue.mu.Unlock()
	if err := d.PauseTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	if err := d.ResumeTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	if got := d.queue.jobs[info.FsID]; got.Status != "retry_wait" || !got.NextAttempt.Equal(deadline) || got.Failures != 2 || got.Attempts != 3 {
		t.Fatalf("retry evidence changed while paused: %+v", got)
	}
	events, err := store.Events(info.FsID)
	if err != nil || len(events) != 3 || events[1].Kind != "task_paused" || events[2].Kind != "task_resumed" {
		t.Fatalf("pause/resume history = %+v, err=%v", events, err)
	}
	var resumeEvidence map[string]any
	if err := json.Unmarshal(events[2].Evidence, &resumeEvidence); err != nil || resumeEvidence["resume_from_status"] != "retry_wait" || resumeEvidence["next_attempt_at_utc"] != deadline.Format(time.RFC3339) {
		t.Fatalf("retry resume evidence = %#v, err=%v", resumeEvidence, err)
	}
}

func TestPausePersistenceFailureDoesNotCancelActiveTask(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	info := file(903, "active.bin", 42)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "downloading"}
	d.queue.jobs[info.FsID] = job
	ctx, cancel := context.WithCancel(context.Background())
	d.queue.active[info.FsID] = cancel
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.PauseTask(info.FsID); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("pause without durable storage error=%v, want ErrQueueUnavailable", err)
	}
	if ctx.Err() != nil || job.Status != "downloading" {
		t.Fatalf("failed pause changed active task: status=%s ctx=%v", job.Status, ctx.Err())
	}
	cancel()
}

func TestPauseActiveTaskCancelsOnlyItsAttemptAndCompletionKeepsPaused(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	info := file(906, "active-pause.bin", 42)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "downloading", Attempts: 1, Failures: 1}
	d.queue.jobs[info.FsID] = job
	ctx, cancel := context.WithCancel(d.ctx)
	d.queue.active[info.FsID] = cancel
	if err := d.PauseTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil || job.Status != "paused" || job.PausedFromStatus != "downloading" {
		t.Fatalf("active pause did not persist intent and cancel task: status=%s from=%s ctx=%v", job.Status, job.PausedFromStatus, ctx.Err())
	}
	if err := d.ResumeTask(info.FsID); err != ErrTaskConflict {
		t.Fatalf("resume before active attempt drains = %v, want conflict", err)
	}
	d.queue.complete(&queueJob{Info: info, AttemptID: job.AttemptID}, context.Canceled)
	if job.Status != "paused" || job.PausedFromStatus != "pending" || job.Failures != 1 {
		t.Fatalf("cancelled attempt overwrote pause or spent failure budget: %+v", job)
	}
	if d.GetTasks()[0].CanResume != true {
		t.Fatal("task was not resumable after its active attempt drained")
	}
}

func TestPausedTaskRestoresAfterDownloaderRestart(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	info := file(907, "restart-pause.bin", 42)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending"}
	body, _ := json.Marshal(job)
	if err := store.Save(info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.jobs[info.FsID] = job
	if err := d.PauseTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	d.queue.store = nil
	restored, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, restored)
	tasks := restored.GetTasks()
	if len(tasks) != 1 || tasks[0].Status != "paused" || !tasks[0].CanResume {
		t.Fatalf("paused task after restart = %+v", tasks)
	}
}

func TestPauseActiveDownloadPreservesPartialAndResumeUsesRange(t *testing.T) {
	payload := strings.Repeat("resume-data-", 4096)
	firstRequest := make(chan struct{})
	var firstOnce sync.Once
	d, provider := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, payload[:4096])
			w.(http.Flusher).Flush()
			firstOnce.Do(func() { close(firstRequest) })
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		var offset int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &offset); err != nil || offset <= 0 || offset >= len(payload) {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(payload)-1, len(payload)))
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)-offset))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, payload[offset:])
	})
	d.cfg.MaxParallel = 1
	d.transferSlots = make(chan struct{}, 1)
	openEventStore(t, d)
	info := file(908, "active-range-pause.bin", int64(len(payload)))
	provider.mu.Lock()
	provider.lists["/queue"] = []baiduapi.FileInfo{info}
	provider.mu.Unlock()
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	if err := d.queue.reconcileObserved([]baiduapi.FileInfo{info}, d.authEpoch, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstRequest:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}
	// Flushing the provider response does not prove the client has written it.
	// Pause only after the intended prefix is local, including under SQLite load.
	prefixDeadline := time.Now().Add(5 * time.Second)
	for {
		if task := d.GetTaskByFsID(info.FsID); task != nil && task.Progress == 4096 {
			break
		}
		if time.Now().After(prefixDeadline) {
			t.Fatal("client did not write the expected prefix before pause")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := d.PauseTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d.queue.mu.Lock()
		active := d.queue.active[info.FsID] != nil
		status := d.queue.jobs[info.FsID].Status
		d.queue.mu.Unlock()
		if !active && status == "paused" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	sum := sha256.Sum256([]byte(info.ServerFilename))
	partial := filepath.Join(d.LocalDownloadDirectory(), stateDirectory, fmt.Sprintf("%x", sum), "content.part")
	stat, err := os.Stat(partial)
	if err != nil || stat.Size() != 4096 {
		t.Fatalf("paused partial size = %v, err=%v; want 4096 bytes retained", stat, err)
	}
	provider.mu.Lock()
	actionsBeforeResume := len(provider.actions)
	provider.mu.Unlock()
	if actionsBeforeResume != 0 {
		t.Fatalf("remote source operation ran while task was paused")
	}
	if err := d.ResumeTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d.queue.mu.Lock()
		status := d.queue.jobs[info.FsID].Status
		d.queue.mu.Unlock()
		if status == "done" || status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := d.GetTaskByFsID(info.FsID); got == nil || got.Status != "done" {
		t.Fatalf("resumed download status = %+v", got)
	}
	content, err := os.ReadFile(filepath.Join(d.LocalDownloadDirectory(), info.ServerFilename))
	if err != nil || string(content) != payload {
		t.Fatalf("resumed file length=%d err=%v", len(content), err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.actions) != 1 || !strings.HasPrefix(provider.actions[0], "rename:") {
		t.Fatalf("postprocessing after resumed download = %v", provider.actions)
	}
}

func TestPausingDirectoryMarksOnlyCanceledDescendantsPaused(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	root := file(909, "paused-folder", 0)
	root.IsDir = 1
	job := &queueJob{Version: 1, Info: root, Mode: DownloadModeKeep, Status: "paused", PausedFromStatus: "pending"}
	d.queue.jobs[root.FsID] = job
	d.queue.active[root.FsID] = func() {}
	d.taskMutex.Lock()
	d.inFlight[root.FsID] = true
	d.tasks[910] = &Task{FileInfo: baiduapi.FileInfo{FsID: 910, Path: root.Path + "/interrupted.bin"}, Status: "failed", Error: "context canceled", Speed: 128}
	d.tasks[911] = &Task{FileInfo: baiduapi.FileInfo{FsID: 911, Path: root.Path + "/nested/interrupted.bin"}, Status: "failed", Error: "context canceled"}
	d.tasks[912] = &Task{FileInfo: baiduapi.FileInfo{FsID: 912, Path: root.Path + "/real-failure.bin"}, Status: "failed", Error: "connection reset"}
	d.tasks[913] = &Task{FileInfo: baiduapi.FileInfo{FsID: 913, Path: root.Path + "/done.bin"}, Status: "done"}
	d.tasks[914] = &Task{FileInfo: baiduapi.FileInfo{FsID: 914, Path: root.Path + "-other/interrupted.bin"}, Status: "failed", Error: "context canceled"}
	d.taskMutex.Unlock()

	d.queue.complete(&queueJob{Info: root, AttemptID: "pause-folder-attempt"}, context.Canceled)

	for id, want := range map[int64]string{910: "paused", 911: "paused", 912: "failed", 913: "done", 914: "failed"} {
		task := d.GetTaskByFsID(id)
		if task == nil || task.Status != want {
			t.Errorf("descendant %d = %+v, want status %q", id, task, want)
		}
	}
	if task := d.GetTaskByFsID(910); task != nil && (task.Error != "" || task.Speed != 0) {
		t.Errorf("canceled descendant retained transient failure details: %+v", task)
	}
}

func TestCancelingPausedDirectoryDoesNotLeaveDescendantsPaused(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	root := file(915, "cancel-paused-folder", 0)
	root.IsDir = 1
	d.queue.jobs[root.FsID] = &queueJob{Version: 1, Info: root, Mode: DownloadModeKeep, Status: "paused", PausedFromStatus: "pending"}
	d.tasks[916] = &Task{FileInfo: baiduapi.FileInfo{FsID: 916, Path: root.Path + "/child.bin"}, Status: "paused"}
	if err := d.Cancel(root.FsID); err != nil {
		t.Fatal(err)
	}
	if got := d.GetTaskByFsID(916); got == nil || got.Status != "cancelled" {
		t.Fatalf("child after canceling paused directory = %+v, want cancelled", got)
	}
}

func TestCancelPersistenceFailureDoesNotChangeTaskOrCancelAttempt(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	info := file(919, "cancel-persist-failure.bin", 42)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "downloading", Error: "", PausedFromStatus: ""}
	d.queue.jobs[info.FsID] = job
	ctx, cancel := context.WithCancel(context.Background())
	d.queue.active[info.FsID] = cancel
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Cancel(info.FsID); err == nil {
		t.Fatal("cancel with closed SQLite store unexpectedly succeeded")
	}
	if job.Status != "downloading" || job.Error != "" || ctx.Err() != nil {
		t.Fatalf("failed cancel changed in-memory state or stopped transfer: job=%+v context=%v", job, ctx.Err())
	}
	cancel()
}

func TestActiveDirectoryCancelFinishesInterruptedDescendantsAsCancelled(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	root := file(917, "active-cancel-folder", 0)
	root.IsDir = 1
	job := &queueJob{Version: 1, Info: root, Mode: DownloadModeKeep, Status: "downloading"}
	d.queue.jobs[root.FsID] = job
	d.queue.active[root.FsID] = func() {}
	d.taskMutex.Lock()
	d.inFlight[root.FsID] = true
	d.tasks[918] = &Task{FileInfo: baiduapi.FileInfo{FsID: 918, Path: root.Path + "/child.bin"}, Status: "downloading"}
	d.taskMutex.Unlock()
	if err := d.Cancel(root.FsID); err != nil {
		t.Fatal(err)
	}
	d.finish(918, context.Canceled)
	d.queue.complete(&queueJob{Info: root, AttemptID: "cancel-folder-attempt"}, context.Canceled)
	if got := d.GetTaskByFsID(918); got == nil || got.Status != "cancelled" {
		t.Fatalf("child after active directory cancellation = %+v, want cancelled", got)
	}
}

func TestTaskPauseDoesNotOverwriteRemoteSourceOperationState(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	info := file(904, "source-operation.bin", 42)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeDelete, Status: "source_unknown", SourceState: "unknown"}
	d.queue.jobs[info.FsID] = job
	if err := d.PauseTask(info.FsID); err != ErrTaskConflict {
		t.Fatalf("pause unknown remote operation = %v, want conflict", err)
	}
	if job.Status != "source_unknown" || job.SourceState != "unknown" {
		t.Fatalf("pause changed source operation quarantine: %+v", job)
	}
	sending := &queueJob{Version: 1, Info: info, Mode: DownloadModeDelete, Status: "downloading"}
	d.queue.jobs[info.FsID] = sending
	tracked, err := d.beginSourceOperation(d.ctx, info)
	if err != nil || !tracked || sending.SourceState != "sending" {
		t.Fatalf("prepare remote operation: tracked=%v job=%+v err=%v", tracked, sending, err)
	}
	if err := d.PauseTask(info.FsID); err != ErrTaskConflict {
		t.Fatalf("pause sending remote operation = %v, want conflict", err)
	}
	if sending.Status != "source_unknown" || sending.SourceState != "sending" {
		t.Fatalf("pause changed sending operation: %+v", sending)
	}
	d.queue.jobs[info.FsID] = &queueJob{Version: 1, Info: info, Mode: DownloadModeDelete, Status: "pending"}
	d.queue.rebuildSourceBlocks()
	if err := d.PauseTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	if err := d.queue.reconcileObserved([]baiduapi.FileInfo{info}, d.authEpoch, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	pausedJob := d.queue.jobs[info.FsID]
	if pausedJob.Status != "paused" || pausedJob.candidate != nil {
		t.Fatalf("monitoring made a paused task eligible: %+v", pausedJob)
	}
	if tracked, err := d.beginSourceOperation(d.ctx, info); err != ErrTaskConflict || tracked {
		t.Fatalf("remote operation began after durable pause: tracked=%v err=%v", tracked, err)
	}
}

func TestTaskPauseRemainsIndependentFromGlobalQueueAndMaintenanceGates(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	d.SetAuthentication(baiduapi.NewClient("token", "app"), true, time.Now().Add(time.Hour))
	info := file(905, "queue-gates.bin", 42)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending"}
	d.queue.jobs[info.FsID] = job
	d.queue.eligible[info.FsID] = &info
	if err := d.PauseTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SetQueuePaused(true); err != nil {
		t.Fatal(err)
	}
	d.queue.mu.Lock()
	d.queue.maintenance = true
	d.queue.mu.Unlock()
	if err := d.ResumeTask(info.FsID); err != nil {
		t.Fatal(err)
	}
	if got := d.QueuePauseStatus(); !got.Paused {
		t.Fatal("resuming a task cleared the global queue pause")
	}
	if job.Status != "pending" {
		t.Fatalf("per-task resume did not restore pending: %+v", job)
	}
	if claimed, _, _ := d.queue.claimAt(time.Now()); claimed != nil {
		t.Fatal("task resumed despite independent global or maintenance gate")
	}
	d.queue.mu.Lock()
	d.queue.maintenance = false
	d.queue.mu.Unlock()
	if claimed, _, _ := d.queue.claimAt(time.Now()); claimed != nil {
		t.Fatal("task resumed despite global pause")
	}
}

func TestPausingOneTaskLeavesAnotherReadyTaskSchedulable(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	d.SetAuthentication(baiduapi.NewClient("token", "app"), true, time.Now().Add(time.Hour))
	pausedInfo := file(909, "one-paused.bin", 42)
	readyInfo := file(910, "another-ready.bin", 42)
	pausedJob := &queueJob{Version: 1, Info: pausedInfo, Mode: DownloadModeKeep, Status: "pending"}
	readyJob := &queueJob{Version: 1, Info: readyInfo, Mode: DownloadModeKeep, Status: "pending"}
	d.queue.jobs[pausedInfo.FsID] = pausedJob
	d.queue.jobs[readyInfo.FsID] = readyJob
	d.queue.eligible[pausedInfo.FsID] = &pausedInfo
	d.queue.eligible[readyInfo.FsID] = &readyInfo
	d.queue.enqueueCandidate(pausedJob)
	d.queue.enqueueCandidate(readyJob)
	if err := d.PauseTask(pausedInfo.FsID); err != nil {
		t.Fatal(err)
	}
	claimed, ctx, _ := d.queue.claimAt(time.Now())
	if claimed == nil || claimed.Info.FsID != readyInfo.FsID || ctx == nil {
		t.Fatalf("pausing one task blocked another ready task: job=%+v", claimed)
	}
	d.queue.complete(claimed, context.Canceled)
}
