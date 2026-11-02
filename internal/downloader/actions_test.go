package downloader

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestTaskActionSnapshotTracksEligibilityAndActiveCancellation(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	info := file(3400000000000000123, "large-id.txt", 3)
	q := d.queue
	q.mu.Lock()
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeDelete, Status: "cancelled"}
	q.jobs[info.FsID] = job
	q.eligible[info.FsID] = &info
	q.reflect(job)
	_, cancel := context.WithCancel(d.ctx)
	q.active[info.FsID] = cancel
	q.mu.Unlock()
	tasks := d.GetTasks()
	if tasks[0].TaskID != "3400000000000000123" || tasks[0].CanRetry || tasks[0].CanCancel {
		t.Fatalf("wrong cancelling snapshot: %+v", tasks[0])
	}
	q.mu.Lock()
	delete(q.active, info.FsID)
	q.mu.Unlock()
	cancel()
	tasks = d.GetTasks()
	if !tasks[0].CanRetry {
		t.Fatal("eligible cancelled root cannot retry")
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(-time.Second))
	// Keep otherwise valid scan eligibility to isolate the expiry condition.
	q.mu.Lock()
	q.eligible[info.FsID] = &info
	q.mu.Unlock()
	if d.GetTasks()[0].CanRetry {
		t.Fatal("expired authorization offers retry")
	}
	if err := d.Retry(info.FsID); err != ErrTaskConflict {
		t.Fatalf("expired retry admitted: %v", err)
	}
	q.mu.Lock()
	job.Status = "done"
	q.reflect(job)
	q.mu.Unlock()
	if d.GetTasks()[0].CanCancel {
		t.Fatal("completed task offers cancellation")
	}
}

func TestTaskActionsCannotChangeQueueDuringManagedPathUpdate(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	q := d.queue
	cancelInfo := file(41, "cancel.txt", 1)
	retryInfo := file(42, "retry.txt", 1)
	cancelJob := &queueJob{Version: 1, Info: cancelInfo, Mode: DownloadModeKeep, Status: "pending"}
	retryJob := &queueJob{Version: 1, Info: retryInfo, Mode: DownloadModeKeep, Status: "failed", Error: "previous failure", Attempts: 1}
	q.mu.Lock()
	q.jobs[cancelInfo.FsID] = cancelJob
	q.jobs[retryInfo.FsID] = retryJob
	q.eligible[retryInfo.FsID] = &retryInfo
	q.mu.Unlock()
	d.pathOperations.Lock()
	defer d.pathOperations.Unlock()

	if err := d.Cancel(cancelInfo.FsID); err != ErrTaskConflict {
		t.Fatalf("cancel during a managed path update = %v, want conflict", err)
	}
	if err := d.Retry(retryInfo.FsID); err != ErrTaskConflict {
		t.Fatalf("retry during a managed path update = %v, want conflict", err)
	}
	if cancelJob.Status != "pending" || retryJob.Status != "failed" || retryJob.Attempts != 1 || retryJob.Error != "previous failure" {
		t.Fatalf("task actions changed the queue during maintenance: cancel=%+v retry=%+v", cancelJob, retryJob)
	}
}
