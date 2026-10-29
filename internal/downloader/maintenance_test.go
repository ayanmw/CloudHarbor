package downloader

import (
	"context"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
)

func TestMaintenanceStopsClaimsAndWaitsForActiveWork(t *testing.T) {
	d := NewDownloader(&config.Config{QueueDir: "/queue", MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	defer d.cancel()
	q := d.queue
	q.mu.Lock()
	job := &queueJob{Info: baiduapi.FileInfo{FsID: 7}, Status: "pending"}
	q.jobs[7] = job
	q.eligible[7] = &job.Info
	q.enqueueCandidate(job)
	_, activeCancel := context.WithCancel(context.Background())
	q.active[8] = activeCancel
	q.mu.Unlock()

	result := make(chan error, 1)
	go func() { result <- d.BeginMaintenance(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for {
		q.mu.Lock()
		blocked := q.maintenance
		q.mu.Unlock()
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("maintenance was not enabled")
		}
		time.Sleep(time.Millisecond)
	}
	claimed, _, _ := q.claimAt(time.Now())
	if claimed != nil || len(q.ready) != 1 {
		t.Fatalf("maintenance claimed work: job=%v ready=%d", claimed, len(q.ready))
	}
	select {
	case err := <-result:
		t.Fatalf("maintenance returned before drain: %v", err)
	default:
	}

	q.mu.Lock()
	delete(q.active, 8)
	close(q.activeChanged)
	q.activeChanged = make(chan struct{})
	q.mu.Unlock()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("maintenance did not finish after active work drained")
	}
	d.EndMaintenance()
}

func TestMaintenanceTimeoutReleasesClaimGate(t *testing.T) {
	d := NewDownloader(&config.Config{QueueDir: "/queue", MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	defer d.cancel()
	d.queue.mu.Lock()
	d.queue.active[9] = func() {}
	d.queue.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := d.BeginMaintenance(ctx); err == nil {
		t.Fatal("maintenance unexpectedly drained active work")
	}
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	if d.queue.maintenance {
		t.Fatal("maintenance gate stayed enabled after timeout")
	}
}
