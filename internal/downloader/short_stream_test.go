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
