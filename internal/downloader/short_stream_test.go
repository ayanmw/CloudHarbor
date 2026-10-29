package downloader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestChunkedShortStreamIsRetryableAndRetainsPartial(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush() // Force chunked framing without Content-Length.
		io.WriteString(w, "abc")
	})
	info := file(1, "a.txt", 6)
	err := d.transfer(context.Background(), info, "a.txt", filepath.Join(d.cfg.LocalDownDir, "a.txt"))
	if !errors.Is(err, io.ErrUnexpectedEOF) || !baiduapi.IsRetryable(err) {
		t.Fatalf("short stream lost transient classification: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d.cfg.LocalDownDir, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("short stream published a final file: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("short stream modified source: %v", p.actions)
	}
}

func TestChunkedShortStreamBackoffResumesAndHonorsBudget(t *testing.T) {
	for _, recover := range []bool{true, false} {
		t.Run(map[bool]string{true: "recover", false: "exhaust"}[recover], func(t *testing.T) {
			var requests atomic.Int32
			ranges := make(chan string, 4)
			original, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				ranges <- r.Header.Get("Range")
				if n == 1 {
					w.(http.Flusher).Flush()
					io.WriteString(w, "abc")
					return
				}
				w.Header().Set("Content-Range", "bytes 3-5/6")
				w.WriteHeader(http.StatusPartialContent)
				w.(http.Flusher).Flush()
				if recover {
					io.WriteString(w, "def")
				}
			})
			original.cfg.MaxRetries = 1
			original.cfg.RetryBackoffSeconds = 1
			d, err := OpenDownloader(original.cfg, original.getClient())
			if err != nil {
				t.Fatal(err)
			}
			defer shutdown(t, d)
			p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 6)}
			if err = d.ScanAndDownload(); err != nil {
				t.Fatal(err)
			}
			waitForStatus(t, d, 1, "retry_wait")
			if d.GetTaskByFsID(1).NextAttempt.IsZero() {
				t.Fatal("missing retry deadline")
			}
			time.Sleep(100 * time.Millisecond)
			if requests.Load() != 1 {
				t.Fatal("short stream bypassed backoff")
			}
			p.mu.Lock()
			actionsBeforeRetry := len(p.actions)
			p.mu.Unlock()
			if actionsBeforeRetry != 0 {
				t.Fatal("incomplete stream processed source")
			}
			wantStatus := "failed"
			if recover {
				wantStatus = "done"
			}
			waitForStatus(t, d, 1, wantStatus)
			shutdown(t, d)
			if requests.Load() != 2 || <-ranges != "" || <-ranges != "bytes=3-" {
				t.Fatal("retry did not resume the bound partial exactly once")
			}
			body, readErr := os.ReadFile(filepath.Join(d.cfg.LocalDownDir, "a.txt"))
			if recover {
				if readErr != nil || string(body) != "abcdef" || len(p.actions) != 1 {
					t.Fatalf("recovery did not publish exact content once: %q %v %v", body, readErr, p.actions)
				}
			} else if !os.IsNotExist(readErr) || len(p.actions) != 0 {
				t.Fatalf("exhausted short stream published or touched source: %v %v", readErr, p.actions)
			}
		})
	}
}

func TestChunkedShortStreamRestartRequiresScanAndKeepsBudget(t *testing.T) {
	var requests atomic.Int32
	ranges := make(chan string, 3)
	original, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		ranges <- r.Header.Get("Range")
		if n == 1 {
			w.(http.Flusher).Flush()
			io.WriteString(w, "abc")
			return
		}
		w.Header().Set("Content-Range", "bytes 3-5/6")
		w.WriteHeader(http.StatusPartialContent)
		w.(http.Flusher).Flush() // Empty suffix: the persisted retry budget must exhaust.
	})
	original.cfg.MaxRetries = 1
	original.cfg.RetryBackoffSeconds = 1
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 6)}
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { shutdown(t, d) }()
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 1, "retry_wait")
	before := d.GetTaskByFsID(1)
	shutdown(t, d)
	reopened, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	d = reopened
	after := d.GetTaskByFsID(1)
	if after.Status != "retry_wait" || after.Attempts != before.Attempts || !after.NextAttempt.Equal(before.NextAttempt) {
		t.Fatalf("restart lost pending retry: before=%+v after=%+v", before, after)
	}
	partials, err := filepath.Glob(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "*", "content.part"))
	if err != nil || len(partials) != 1 {
		t.Fatalf("restart lost bound partial: %v %v", partials, err)
	}
	partial, err := os.ReadFile(partials[0])
	if err != nil || string(partial) != "abc" {
		t.Fatalf("restart changed partial content: %q %v", partial, err)
	}
	// Let the persisted deadline pass. Restart alone must not authorize a request.
	if delay := time.Until(after.NextAttempt) + 150*time.Millisecond; delay > 0 {
		time.Sleep(delay)
	}
	if requests.Load() != 1 {
		t.Fatal("restart retried without a fresh scan")
	}
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 1, "failed")
	shutdown(t, d)
	if requests.Load() != 2 || <-ranges != "" || <-ranges != "bytes=3-" {
		t.Fatal("restart discarded partial or reset the retry budget")
	}
	if len(p.actions) != 0 {
		t.Fatalf("incomplete stream touched source across restart: %v", p.actions)
	}
	if _, err = os.Stat(filepath.Join(d.cfg.LocalDownDir, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("restart published an incomplete file: %v", err)
	}
}
