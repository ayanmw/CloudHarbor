package downloader

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestProviderTokenErrnosParkJobsWithoutSpendingRetryBudget(t *testing.T) {
	for _, code := range []int{20016, 20017, 31045} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			original, p := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("download with rejected token") })
			d, err := OpenDownloader(original.cfg, original.getClient())
			if err != nil {
				t.Fatal(err)
			}
			defer shutdown(t, d)
			p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3)}
			base := http.DefaultTransport
			http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("method") == "filemetas" {
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"errno":%d}`, code))), Request: r}, nil
				}
				return base.RoundTrip(r)
			})
			client := baiduapi.NewClient("rejected-fixture", "")
			var notified atomic.Bool
			d.SetAuthenticationFailureHandler(func(fp string) {
				if fp != client.Fingerprint() {
					t.Error("wrong credential rejected")
				}
				notified.Store(true)
			})
			d.SetAuthentication(client, true, time.Now().Add(time.Hour))
			if err = d.ScanAndDownload(); err != nil {
				t.Fatal(err)
			}
			waitForStatus(t, d, 1, "auth_wait")
			d.queue.mu.Lock()
			failures := d.queue.jobs[1].Failures
			d.queue.mu.Unlock()
			if failures != 0 || d.AuthenticationReady() {
				t.Fatal("token error consumed budget or retained admission")
			}
			shutdown(t, d)
			if !notified.Load() || len(p.actions) != 0 {
				t.Fatal("rejection callback missing or source modified")
			}
		})
	}
}

func TestBusinessRateLimitUsesScheduledRetryAndRecovers(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d.cfg.RetryBackoffSeconds = 1
	d.cfg.MaxRetries = 1
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3)}
	base := http.DefaultTransport
	var metadata atomic.Int32
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") == "filemetas" && metadata.Add(1) == 1 {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":31034}`)), Request: r}, nil
		}
		return base.RoundTrip(r)
	})
	if err := d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 1, "retry_wait")
	job := d.GetTaskByFsID(1)
	if job.NextAttempt.IsZero() || job.Attempts != 1 {
		t.Fatalf("missing bounded retry: %+v", job)
	}
	if job.NextAttempt.Location() != time.UTC {
		t.Fatalf("retry deadline must be a persisted UTC wall-clock time, got %s", job.NextAttempt.Location())
	}
	time.Sleep(100 * time.Millisecond)
	if metadata.Load() != 1 {
		t.Fatal("rate limit retried without backoff")
	}
	waitForStatus(t, d, 1, "done")
	shutdown(t, d)
	if metadata.Load() != 2 || len(p.actions) != 1 {
		t.Fatal("retry did not recover once")
	}
}
