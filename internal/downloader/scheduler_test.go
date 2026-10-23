package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func waitForStatus(t *testing.T, d *Downloader, id int64, status string) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for {
		v := d.GetTaskByFsID(id)
		if v != nil && v.Status == status {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %d: %+v, want %s", id, v, status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func shutdown(t *testing.T, d *Downloader) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := d.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultModeSurvivesRestart(t *testing.T) {
	original, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	d.SetDownloadMode(DownloadModeDelete)
	shutdown(t, d)
	d, err = OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	if d.GetDownloadMode() != DownloadModeDelete {
		t.Fatal("default mode lost after restart")
	}
}

func TestOpenDownloaderClosesStoreWhenStreamingRestoreRejectsRecord(t *testing.T) {
	base, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	d, err := OpenDownloader(base.cfg, base.getClient())
	if err != nil {
		t.Fatal(err)
	}
	if err := d.queue.store.Save(999, []byte(`{"version":99,"file":{"fs_id":999},"mode":"keep","status":"done"}`)); err != nil {
		t.Fatal(err)
	}
	shutdown(t, d)
	if reopened, err := OpenDownloader(d.cfg, d.getClient()); err == nil {
		shutdown(t, reopened)
		t.Fatal("accepted unsupported persisted job")
	}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatalf("restore failure leaked queue store lock: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerLimitAndCapturedMode(t *testing.T) {
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var active, peak atomic.Int32
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
			io.WriteString(w, "abc")
		case <-r.Context().Done():
		}
	})
	d.cfg.MaxParallel = 1
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3), file(2, "b.txt", 3)}
	if err := d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	<-started
	d.SetDownloadMode(DownloadModeDelete)
	if err := d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		close(release)
		t.Fatal("second download bypassed worker limit")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	waitForStatus(t, d, 1, "done")
	waitForStatus(t, d, 2, "done")
	shutdown(t, d)
	if peak.Load() != 1 {
		t.Fatalf("peak=%d", peak.Load())
	}
	for _, action := range p.actions {
		if action[:6] != "rename" {
			t.Fatalf("queued policy changed: %s", action)
		}
	}
}

func TestPersistentRecoveryRequiresSuccessfulScan(t *testing.T) {
	var allow atomic.Bool
	var requests atomic.Int32
	started := make(chan struct{}, 4)
	original, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		started <- struct{}{}
		if !allow.Load() {
			<-r.Context().Done()
			return
		}
		io.WriteString(w, "abc")
	})
	original.cfg.MaxParallel = 1
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3), file(2, "b.txt", 3)}
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	<-started
	shutdown(t, d)
	allow.Store(true)
	d, err = OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	before := requests.Load()
	time.Sleep(100 * time.Millisecond)
	if requests.Load() != before {
		t.Fatal("recovered stale path ran before scanning")
	}
	if len(d.GetTasks()) != 2 {
		t.Fatalf("lost pending jobs: %+v", d.GetTasks())
	}
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 1, "done")
	waitForStatus(t, d, 2, "done")
	shutdown(t, d)
	d, err = OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	assertStatus(t, d, 1, "done")
	assertStatus(t, d, 2, "done")
	before = requests.Load()
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if requests.Load() != before {
		t.Fatal("completed history was redownloaded")
	}
}

func TestRestartReusesPublishedFileBeforeSourcePostprocess(t *testing.T) {
	var downloadRequests atomic.Int32
	original, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		downloadRequests.Add(1)
		io.WriteString(w, "abc")
	})
	info := file(1, "a.txt", 3)
	p.lists["/queue"] = []baiduapi.FileInfo{info}
	mode := DownloadModeDelete
	job := queueJob{Version: 1, Info: info, Mode: mode, Status: "downloading", Attempts: 1, Created: time.Now()}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	store, err := taskstore.Open(filepath.Join(original.cfg.LocalDownDir, stateDirectory, "queue"), original.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(info.FsID, body); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(original.cfg.LocalDownDir, info.ServerFilename)
	if err = os.WriteFile(local, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte(info.ServerFilename))
	state := filepath.Join(original.cfg.LocalDownDir, stateDirectory, hex.EncodeToString(key[:]))
	if err = os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	receipt := recordFor(info, info.ServerFilename)
	digest := sha256.Sum256([]byte("abc"))
	receipt.SHA256 = hex.EncodeToString(digest[:])
	if err = writeRecord(filepath.Join(state, "complete.json"), receipt); err != nil {
		t.Fatal(err)
	}

	// The durable downloading job plus verified published file represents a
	// crash after publication but before source postprocessing began.
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, info.FsID, "done")
	shutdown(t, d)
	if got := downloadRequests.Load(); got != 0 {
		t.Fatalf("recovery fetched file content again: requests=%d", got)
	}
	p.mu.Lock()
	actions := append([]string(nil), p.actions...)
	p.mu.Unlock()
	if len(actions) != 1 || !strings.HasPrefix(actions[0], "delete:") {
		t.Fatalf("source postprocess did not resume exactly once: %v", actions)
	}
	content, err := os.ReadFile(local)
	if err != nil || string(content) != "abc" {
		t.Fatalf("published file changed during recovery: %q %v", content, err)
	}
}

func TestCancelQueuedJobPreventsSourceAction(t *testing.T) {
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
			io.WriteString(w, "abc")
		case <-r.Context().Done():
		}
	})
	d.cfg.MaxParallel = 1
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3), file(2, "b.txt", 3)}
	if err := d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	<-started
	// The first job is deterministic FIFO; the other remains pending.
	if err := d.Cancel(2); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitForStatus(t, d, 1, "done")
	assertStatus(t, d, 2, "cancelled")
	shutdown(t, d)
	if len(p.actions) != 1 {
		t.Fatalf("cancelled job modified cloud: %v", p.actions)
	}
}

func TestActiveCancellationRetainsPartialAndDoesNotTouchSource(t *testing.T) {
	started := make(chan struct{})
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "6")
		io.WriteString(w, "abc")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 6)}
	if err := d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := d.Cancel(1); err != nil {
		t.Fatal(err)
	}
	shutdown(t, d)
	assertStatus(t, d, 1, "cancelled")
	if len(p.actions) != 0 {
		t.Fatalf("cancelled download touched source: %v", p.actions)
	}
	if _, err := os.Stat(filepath.Join(d.cfg.LocalDownDir, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("published partial: %v", err)
	}
}

func TestTransientFailureBacksOffAndExhaustionNeedsManualRetry(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var requests atomic.Int32
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		io.WriteString(w, "abc")
	})
	d.cfg.MaxRetries = 1
	d.cfg.RetryBackoffSeconds = 1
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3)}
	if err := d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 1, "retry_wait")
	job := d.GetTaskByFsID(1)
	if job.NextAttempt.IsZero() || job.Attempts != 1 {
		t.Fatalf("missing retry schedule: %+v", job)
	}
	time.Sleep(100 * time.Millisecond)
	if requests.Load() != 1 {
		t.Fatal("retry ignored backoff")
	}
	waitForStatus(t, d, 1, "failed")
	if requests.Load() != 2 {
		t.Fatalf("retry budget not enforced: %d", requests.Load())
	}
	fail.Store(false)
	if err := d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if requests.Load() != 2 {
		t.Fatal("scan reset exhausted retry budget")
	}
	if err := d.Retry(1); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 1, "done")
	shutdown(t, d)
	if requests.Load() != 3 || len(p.actions) != 1 {
		t.Fatalf("manual retry failed: requests=%d actions=%v", requests.Load(), p.actions)
	}
}

func TestShutdownDeadlineDoesNotWaitForStateLock(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	d.queue.mu.Lock()
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	go func() { done <- d.Shutdown(ctx) }()
	select {
	case err := <-done:
		d.queue.mu.Unlock()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown result=%v", err)
		}
	case <-time.After(150 * time.Millisecond):
		d.queue.mu.Unlock()
		<-done
		t.Fatal("state lock prevented shutdown deadline")
	}
}

func TestInterruptedAttemptsDoNotConsumeFailureRetryBudget(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	original, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		io.WriteString(w, "abc")
	})
	original.cfg.MaxRetries = 1
	original.cfg.RetryBackoffSeconds = 1
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3)}
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	shutdown(t, d)
	record := `{"version":1,"file":{"fs_id":1,"path":"/queue/a.txt","server_filename":"a.txt","size":3,"server_mtime":123},"mode":"keep","status":"pending","attempts":4,"failures":0,"created":"2026-10-21T00:00:00Z"}`
	if err = os.WriteFile(filepath.Join(original.cfg.LocalDownDir, stateDirectory, "queue", "jobs", "1.json"), []byte(record), 0600); err != nil {
		t.Fatal(err)
	}
	d, err = OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 1, "retry_wait")
	fail.Store(false)
	waitForStatus(t, d, 1, "done")
}

func TestLargeFolderFailureDoesNotPoisonPersistentQueue(t *testing.T) {
	original, p := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid filename should not download") })
	root := file(1, "folder", 0)
	root.IsDir = 1
	p.lists["/queue"] = []baiduapi.FileInfo{root}
	for id := int64(2); id < 3002; id++ {
		child := file(id, "invalid/"+strings.Repeat("x", 300), 3)
		child.Path = root.Path + "/" + child.ServerFilename
		p.lists[root.Path] = append(p.lists[root.Path], child)
	}
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 1, "failed")
	if d.QueueError() != nil {
		t.Fatalf("large error stopped queue: %v", d.QueueError())
	}
	if len(d.GetTaskByFsID(1).Error) > 8400 {
		t.Fatal("error summary not bounded")
	}
	shutdown(t, d)
	d, err = OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	assertStatus(t, d, 1, "failed")
}
