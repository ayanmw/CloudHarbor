package downloader

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestClearDownloadRecordResetsOnlySelectedHistoryWithoutProviderRequests(t *testing.T) {
	d, p := setup(t, nil)
	s, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = s
	defer shutdown(t, d)
	info := file(42, "old", 0)
	info.IsDir = 1
	job := &queueJob{Version: 1, Info: info, Status: "source_unknown", SourceState: "unknown", Mode: DownloadModeDelete}
	other := &queueJob{Version: 1, Info: file(43, "keep", 0), Status: "failed", Mode: DownloadModeKeep}
	q := d.queue
	q.jobs[42] = job
	q.jobs[43] = other
	if err = q.persist(job); err != nil {
		t.Fatal(err)
	}
	if err = q.persist(other); err != nil {
		t.Fatal(err)
	}
	q.rebuildSourceBlocks()
	q.eligible[42] = &info
	d.tasks[44] = &Task{FileInfo: file(44, "old/child", 0)}
	d.GetTaskPage(0, 50, "name")
	d.GetTaskPage(0, 50, "size")
	token := downloadClearToken(job)
	if err = d.ClearDownloadRecord(context.Background(), 42, "stale"); err != ErrTaskConflict {
		t.Fatalf("old token %v", err)
	}
	q.active[42] = func() {}
	if err = d.ClearDownloadRecord(context.Background(), 42, token); err != ErrTaskConflict {
		t.Fatal("active task cleared")
	}
	delete(q.active, 42)
	if err = d.ClearDownloadRecord(context.Background(), 42, token); err != nil {
		t.Fatal(err)
	}
	if q.jobs[42] != nil || q.jobs[43] == nil || q.blockedSources[info.Path] || q.eligible[42] != nil || d.tasks[44] != nil {
		t.Fatal("selected projection or isolation survived / other task lost")
	}
	for _, order := range []string{"name", "size", "progress"} {
		page := d.GetTaskPage(0, 50, order)
		if page.Total != 1 || len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 43 {
			t.Fatalf("stale %s page: %+v", order, page)
		}
	}
	records, err := s.Load()
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%v err=%v", records, err)
	}
	p.mu.Lock()
	requests := len(p.actions) + len(p.listRequests)
	p.mu.Unlock()
	if requests != 0 {
		t.Fatal("cleanup accessed provider")
	}
	// Feed a fresh monitor observation through the ordinary registration path.
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	d.SetDownloadMode(DownloadModeKeep)
	d.clientMutex.RLock()
	epoch := d.authEpoch
	d.clientMutex.RUnlock()
	if err = q.reconcileMonitorObserved([]baiduapi.FileInfo{info}, epoch, time.Now().UTC(), ""); err != nil {
		t.Fatal(err)
	}
	records, err = s.Load()
	if err != nil {
		t.Fatal(err)
	}
	var loaded queueJob
	if err = json.Unmarshal(records[42], &loaded); err != nil || loaded.SourceState != "" || loaded.Mode != DownloadModeKeep || loaded.TransferStats != nil {
		t.Fatalf("old execution inherited %+v err=%v", loaded, err)
	}
	if err = d.ClearDownloadRecord(context.Background(), 42, token); err != ErrTaskConflict {
		t.Fatal("old confirmation cleared a future task")
	}
}

func TestClearDownloadRecordCancellationDoesNotPoisonQueue(t *testing.T) {
	d, _ := setup(t, nil)
	s, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = s
	defer shutdown(t, d)
	job := &queueJob{Version: 1, Info: file(42, "old", 0), Status: "done"}
	d.queue.jobs[42] = job
	if err = d.queue.persist(job); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = d.ClearDownloadRecord(ctx, 42, downloadClearToken(job)); err != context.Canceled {
		t.Fatalf("cancelled cleanup=%v", err)
	}
	if d.queue.err != nil || d.queue.jobs[42] == nil {
		t.Fatal("cancelled request changed queue")
	}
	if err = d.ClearDownloadRecord(context.Background(), 42, downloadClearToken(job)); err != nil {
		t.Fatal(err)
	}
}
