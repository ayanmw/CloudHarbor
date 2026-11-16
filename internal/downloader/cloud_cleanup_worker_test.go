package downloader

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestCleanupWorkerRunsSeriallyAndStopsUnknownBatch(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "unknown"}[unknown], func(t *testing.T) {
			d, p := setup(t, nil)
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
			var items []taskstore.CleanupBatchItem
			for i, name := range []string{"one", "two"} {
				f := baiduapi.FileInfo{FsID: int64(91 + i), Path: "/source/" + name, ServerFilename: name, IsDir: 1, Mtime: 123}
				p.lists["/source"] = append(p.lists["/source"], f)
				items = append(items, taskstore.CleanupBatchItem{Kind: "empty", Expected: taskstore.CleanupIdentity{FsID: f.FsID, Path: f.Path, Name: name, Directory: true, Mtime: f.Mtime}, PreviewObservedAtUTC: time.Now().UTC()})
			}
			p.unknownAction = unknown
			record, err := d.CreateCloudCleanupBatch(context.Background(), strings.Repeat("c", 32), items)
			if err != nil {
				t.Fatal(err)
			}
			d.startCleanupWorker() // Calling start twice cannot add a second executor.
			want, writes := "completed", 2
			if unknown {
				want, writes = "unknown", 1
			}
			deadline := time.Now().Add(12 * time.Second)
			for {
				got, readErr := s.CleanupBatch(context.Background(), record.ID)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if got.Status == want {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("worker did not settle: %+v", got)
				}
				time.Sleep(20 * time.Millisecond)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err = d.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			p.mu.Lock()
			actual := len(p.actions)
			p.mu.Unlock()
			if actual != writes {
				t.Fatalf("writes=%d expected=%d", actual, writes)
			}
		})
	}
}

func TestCleanupRestartWaitsForAuthenticationBeforeExecuting(t *testing.T) {
	d, p := setup(t, nil)
	defer d.cancel()
	s, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = s
	if err = s.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	scope, err := d.cleanupScope(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := baiduapi.FileInfo{FsID: 91, Path: "/source/empty", ServerFilename: "empty", IsDir: 1, Mtime: 123}
	p.lists["/source"] = []baiduapi.FileInfo{dir}
	m := taskstore.CleanupBatchManifest{ID: strings.Repeat("d", 32), AccountFingerprint: scope.AccountFingerprint, ApplicationFingerprint: scope.ApplicationFingerprint, CacheNamespace: scope.CacheNamespace, CreatedAtUTC: time.Now().UTC(), Items: []taskstore.CleanupBatchItem{{Kind: "empty", Expected: taskstore.CleanupIdentity{FsID: dir.FsID, Path: dir.Path, Name: dir.ServerFilename, Directory: true, Mtime: dir.Mtime}, PreviewObservedAtUTC: time.Now().UTC()}}}
	if _, err = s.CreateCleanupBatch(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	d.queue.store = nil
	restarted, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, restarted)
	restarted.SetAuthentication(d.getClient(), false, time.Time{})
	if err = restarted.ResumeCloudCleanup(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	p.mu.Lock()
	writes := len(p.actions)
	p.mu.Unlock()
	if writes != 0 {
		t.Fatal("cleanup sent before authorization ready")
	}
	restarted.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	deadline := time.Now().Add(6 * time.Second)
	for {
		got, readErr := restarted.queue.store.CleanupBatch(context.Background(), m.ID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if got.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart failed to resume: %+v", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
