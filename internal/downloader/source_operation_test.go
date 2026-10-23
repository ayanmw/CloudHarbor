package downloader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestUncertainSourceOperationSurvivesRestartWithoutReplay(t *testing.T) {
	for _, mode := range []DownloadMode{DownloadModeKeep, DownloadModeDelete} {
		t.Run(string(mode), func(t *testing.T) {
			original, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
			d, err := OpenDownloader(original.cfg, original.getClient())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { shutdown(t, d) }()
			d.SetDownloadMode(mode)
			p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3)}
			base := http.DefaultTransport
			var requests atomic.Int32
			http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("method") == "filemanager" {
					requests.Add(1)
					return nil, io.ErrUnexpectedEOF
				}
				return base.RoundTrip(r)
			})
			if err = d.ScanAndDownload(); err != nil {
				t.Fatal(err)
			}
			waitForStatus(t, d, 1, "source_unknown")
			shutdown(t, d)
			for _, action := range []func(int64) error{d.Cancel, d.Retry} {
				if !errors.Is(action(1), ErrTaskConflict) {
					t.Fatal("uncertain source operation manually replayable")
				}
			}
			d, err = OpenDownloader(original.cfg, original.getClient())
			if err != nil {
				t.Fatal(err)
			}
			if err = d.ScanAndDownload(); err != nil {
				t.Fatal(err)
			}
			assertStatus(t, d, 1, "source_unknown")
			if !errors.Is(d.Retry(1), ErrTaskConflict) {
				t.Fatal("restart permitted retry")
			}
			p.lists["/queue"] = []baiduapi.FileInfo{file(1, "renamed.txt", 3)}
			if err = d.ScanAndDownload(); err != nil {
				t.Fatal(err)
			}
			shutdown(t, d)
			d, err = OpenDownloader(original.cfg, original.getClient())
			if err != nil {
				t.Fatal(err)
			}
			if d.GetTaskByFsID(1).FileInfo.Path != "/queue/a.txt" || !d.queue.blockedSources["/queue/a.txt"] {
				t.Fatal("same ID at a new path erased uncertain source journal")
			}
			p.lists["/queue"] = []baiduapi.FileInfo{file(2, "a.txt", 3)}
			if err = d.ScanAndDownload(); err != nil {
				t.Fatal(err)
			}
			p.lists["/queue"] = []baiduapi.FileInfo{}
			p.lists["/source"] = []baiduapi.FileInfo{{FsID: 3, Path: "/source/a.txt", ServerFilename: "a.txt", Size: 3}}
			if _, err = d.PreviewIntake(context.Background(), []string{"/source/a.txt"}); !errors.Is(err, ErrIntakeConflict) {
				t.Fatal("intake bypassed source quarantine")
			}
			shutdown(t, d)
			if requests.Load() != 1 || d.GetTaskByFsID(2) != nil {
				t.Fatal("new ID bypassed source path quarantine")
			}
		})
	}
}

func TestChangedSourceIdentityPreventsPostprocess(t *testing.T) {
	base, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d, err := OpenDownloader(base.cfg, base.client)
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	original := file(73, "original.txt", 3)
	changed := file(73, "replacement.txt", 3)
	p.lists["/queue"] = []baiduapi.FileInfo{changed}
	job := &queueJob{Version: 1, Info: original, Mode: DownloadModeDelete, Status: "downloading", Created: time.Now()}
	d.queue.mu.Lock()
	d.queue.jobs[original.FsID] = job
	if err = d.queue.persist(job); err != nil {
		d.queue.mu.Unlock()
		t.Fatal(err)
	}
	d.queue.mu.Unlock()
	err = d.runRoot(context.Background(), original, DownloadModeDelete)
	if !errors.Is(err, ErrSourceIdentityChanged) {
		t.Fatalf("runRoot error=%v, want source identity change", err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("changed cloud item received source operation: %v", p.actions)
	}
}

func TestSourceJournalSaveFailureNeverSendsRequest(t *testing.T) {
	original, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected download") })
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	info := file(1, "a.txt", 3)
	d.queue.jobs[1] = &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "downloading"}
	if err = d.queue.store.Close(); err != nil {
		t.Fatal(err)
	}
	base := http.DefaultTransport
	var requests atomic.Int32
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) { requests.Add(1); return base.RoundTrip(r) })
	if err = d.postprocess(context.Background(), info, DownloadModeKeep); err == nil || requests.Load() != 0 {
		t.Fatalf("failed journal sent source request: %v requests=%d", err, requests.Load())
	}
}

func TestSourceReceiptSaveFailureKeepsSendingRecordOnDisk(t *testing.T) {
	original, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { shutdown(t, d) }()
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3)}
	base := http.DefaultTransport
	var requests atomic.Int32
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") == "filemanager" {
			requests.Add(1)
			if err := d.queue.store.Close(); err != nil {
				t.Error(err)
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0,"info":[{"errno":0}]}`)), Request: r}, nil
		}
		return base.RoundTrip(r)
	})
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	// Drain after the response closed the store. It is not enough to observe
	// the transient source_unknown state while the HTTP request is in flight.
	deadline := time.Now().Add(3 * time.Second)
	for d.QueueError() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if d.QueueError() == nil {
		t.Fatal("receipt storage failure was not surfaced")
	}
	shutdown(t, d)
	d, err = OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, d, 1, "source_unknown")
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	shutdown(t, d)
	if requests.Load() != 1 {
		t.Fatal("lost durable receipt replayed source operation")
	}
}

func TestSendingSourceJournalRecoversAsUnknown(t *testing.T) {
	original, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected download") })
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"file":{"fs_id":1,"path":"/queue/a.txt","server_filename":"a.txt","size":3},"mode":"keep","status":"source_unknown","source_state":"sending"}`
	if err = d.queue.store.Save(1, []byte(body)); err != nil {
		t.Fatal(err)
	}
	shutdown(t, d)
	d, err = OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	assertStatus(t, d, 1, "source_unknown")
	if !errors.Is(d.postprocess(context.Background(), file(1, "a.txt", 3), DownloadModeKeep), ErrSourceOperationUnknown) {
		t.Fatal("recovered sending operation can be resent")
	}
	if !strings.Contains(d.GetTaskByFsID(1).Error, "未知") {
		t.Fatal("uncertainty not visible")
	}
}
