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
	q.eligible[info.FsID] = info
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
	q.eligible[info.FsID] = info
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
