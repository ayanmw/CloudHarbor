package downloader

import (
	"context"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestCleanupRevisionFollowsWorkerSuccessWithoutRemoteReads(t *testing.T) {
	d, p := setup(t, nil)
	baseTransport := http.DefaultTransport
	var calls atomic.Int64
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) { calls.Add(1); return baseTransport.RoundTrip(r) })
	t.Cleanup(func() { http.DefaultTransport = baseTransport })
	s, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = s
	defer shutdown(t, d)
	if err = s.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	ctx := context.Background()
	before, err := d.CloudCleanupRevision(ctx)
	if err != nil || len(before) != 64 {
		t.Fatalf("revision %q %v", before, err)
	}
	again, err := d.CloudCleanupRevision(ctx)
	if err != nil || again != before {
		t.Fatalf("unstable revision %q %v", again, err)
	}
	p.mu.Lock()
	actions := len(p.actions)
	p.mu.Unlock()
	if calls.Load() != 0 {
		t.Fatal("revision observation contacted provider")
	}
	if actions != 0 {
		t.Fatal("observation sent mutation")
	}
	f := baiduapi.FileInfo{FsID: 91, Path: "/source/one", ServerFilename: "one", IsDir: 1, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{f}
	batch, err := d.CreateCloudCleanupBatch(ctx, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", []taskstore.CleanupBatchItem{{Kind: "empty", Expected: taskstore.CleanupIdentity{FsID: 91, Path: f.Path, Name: f.ServerFilename, Directory: true, Mtime: 123}, PreviewObservedAtUTC: time.Now().UTC()}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		record, e := d.CloudCleanupBatch(ctx, batch.ID)
		if e != nil {
			t.Fatal(e)
		}
		if record.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker did not complete: %+v", record)
		}
		time.Sleep(20 * time.Millisecond)
	}
	callsBeforeObservation := calls.Load()
	after, err := d.CloudCleanupRevision(ctx)
	if err != nil || after == before {
		t.Fatalf("success did not change revision %q %v", after, err)
	}
	again, err = d.CloudCleanupRevision(ctx)
	if err != nil || again != after {
		t.Fatal("success revision unstable")
	}
	p.mu.Lock()
	actions = len(p.actions)
	p.mu.Unlock()
	if calls.Load() != callsBeforeObservation {
		t.Fatal("post-success observation contacted provider")
	}
	if actions != 1 {
		t.Fatalf("writes=%d", actions)
	}
}
