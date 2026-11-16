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

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestAuthenticationGatesScanAndResumesAfterFreshScan(t *testing.T) {
	original, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { shutdown(t, d) }()
	if err = d.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3)}
	transport := http.DefaultTransport
	var calls atomic.Int32
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Query().Get("method") == "filemetas" && r.URL.Query().Get("access_token") == "old" {
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		}
		return transport.RoundTrip(r)
	})
	old := baiduapi.NewClient("old", "app")
	d.SetAuthentication(old, false, time.Time{})
	if err := d.ScanAndDownload(); !errors.Is(err, ErrAuthenticationWaiting) || calls.Load() != 0 {
		t.Fatalf("unverified scan: %v calls=%d", err, calls.Load())
	}
	rejected := make(chan string, 1)
	d.SetAuthenticationFailureHandler(func(fp string) { rejected <- fp })
	d.SetAuthentication(old, true, time.Now().Add(time.Hour))
	if err := d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	runs, _, more, err := d.QueueMonitorScanHistoryRuns(context.Background(), 0, taskstore.QueueMonitorScanFilter{}, 10)
	if err != nil || more || len(runs) != 1 || runs[0].Status != "success" || runs[0].CloudObservedAtUTC == nil || runs[0].FinishedAtUTC == nil {
		t.Fatalf("durable monitor scan history=%+v more=%v err=%v", runs, more, err)
	}
	waitForStatus(t, d, 1, "auth_wait")
	select {
	case fp := <-rejected:
		if fp != old.Fingerprint() {
			t.Fatal("wrong rejected credential")
		}
	case <-time.After(time.Second):
		t.Fatal("rejection not forwarded")
	}
	d.queue.mu.Lock()
	failures := d.queue.jobs[1].Failures
	d.queue.mu.Unlock()
	if failures != 0 || d.AuthenticationReady() {
		t.Fatal("authentication consumed retry budget or retained admission")
	}
	shutdown(t, d)
	d, err = OpenDownloader(original.cfg, baiduapi.NewClient("new", "app"))
	if err != nil {
		t.Fatal(err)
	}
	runs, _, more, err = d.QueueMonitorScanHistoryRuns(context.Background(), 0, taskstore.QueueMonitorScanFilter{}, 10)
	if err != nil || more || len(runs) != 1 || runs[0].Status != "success" {
		t.Fatalf("monitor scan history after restart=%+v more=%v err=%v", runs, more, err)
	}
	d.SetAuthentication(baiduapi.NewClient("new", "app"), false, time.Time{})
	assertStatus(t, d, 1, "auth_wait")
	d.SetAuthentication(baiduapi.NewClient("new", "app"), true, time.Now().Add(time.Hour))
	time.Sleep(150 * time.Millisecond)
	assertStatus(t, d, 1, "auth_wait")
	if err := d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 1, "done")
}

func TestExpiredAuthenticationPreventsSourcePostprocessing(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d.SetAuthentication(d.getClient(), true, time.Now().Add(-time.Second))
	if err := d.postprocess(context.Background(), file(1, "a.txt", 3), DownloadModeDelete); !errors.Is(err, ErrAuthenticationWaiting) {
		t.Fatalf("expired postprocess: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatal("source changed with expired credential")
	}
}

func TestLateRejectionOnlyResumesWithFreshMatchingNewCredential(t *testing.T) {
	for _, scenario := range []string{"fresh", "no_scan", "changed_version", "expired", "same_token", "unmanaged", "missing_fingerprint"} {
		t.Run(scenario, func(t *testing.T) {
			d, _ := setup(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
			old := baiduapi.NewClient("old", "app")
			link, err := old.GetDownloadLink(1)
			if err != nil {
				t.Fatal(err)
			}
			_, _, rejection := old.DownloadFileWithRange(link, 0)
			if !baiduapi.IsAuthenticationError(rejection) {
				t.Fatalf("fixture did not reject old request: %v", rejection)
			}
			if scenario != "unmanaged" {
				client := baiduapi.NewClient("new", "app")
				if scenario == "same_token" {
					client = old
				}
				expiry := time.Now().Add(time.Hour)
				if scenario == "expired" {
					expiry = time.Now().Add(-time.Second)
				}
				d.SetAuthentication(client, true, expiry)
			}
			info := file(1, "a.txt", 6)
			job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "downloading"}
			d.queue.jobs[1] = job
			if scenario != "no_scan" {
				scanned := info
				if scenario == "changed_version" {
					scanned.Mtime++
				}
				d.queue.eligible[1] = &scanned
			}
			if scenario == "missing_fingerprint" {
				rejection = &baiduapi.APIError{Operation: "download", Status: http.StatusUnauthorized}
			}
			d.queue.complete(job, rejection)
			want := "auth_wait"
			if scenario == "fresh" {
				want = "pending"
			}
			if job.Status != want || job.Failures != 0 {
				t.Fatalf("incorrect admission after late rejection: status=%s failures=%d want=%s", job.Status, job.Failures, want)
			}
		})
	}
}

func TestShutdownWaitsForBackgroundBeforeClosingStore(t *testing.T) {
	original, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	if err = d.StartBackground(func(ctx context.Context) { <-ctx.Done(); stopped <- d.BindAccount("123") }); err != nil {
		t.Fatal(err)
	}
	shutdown(t, d)
	if err = <-stopped; err != nil {
		t.Fatalf("store closed before background drained: %v", err)
	}
	if err = d.StartBackground(func(context.Context) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("background admitted after stop: %v", err)
	}
}

func TestCredentialSwitchRejectsInFlightOldScan(t *testing.T) {
	d, p := setup(t, func(http.ResponseWriter, *http.Request) {})
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3)}
	base := http.DefaultTransport
	started, release := make(chan struct{}), make(chan struct{})
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") == "list" {
			close(started)
			<-release
		}
		return base.RoundTrip(r)
	})
	d.SetAuthentication(baiduapi.NewClient("old", "app"), true, time.Now().Add(time.Hour))
	done := make(chan error, 1)
	go func() { done <- d.ScanAndDownload() }()
	<-started
	d.SetAuthentication(baiduapi.NewClient("new", "app"), true, time.Now().Add(time.Hour))
	close(release)
	if err := <-done; !errors.Is(err, ErrAuthenticationWaiting) {
		t.Fatalf("old scan accepted: %v", err)
	}
	if len(d.GetTasks()) != 0 {
		t.Fatal("old scan registered runnable jobs")
	}
}

func TestPostprocessWaitsForScanAfterCredentialChange(t *testing.T) {
	d, p := setup(t, func(http.ResponseWriter, *http.Request) {})
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	if err := d.postprocess(context.Background(), file(1, "a.txt", 3), DownloadModeDelete); !errors.Is(err, ErrAuthenticationWaiting) {
		t.Fatalf("unconfirmed source operation: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatal("source changed before new scan")
	}
}

func TestSameTokenExpiryUpdateRejectsOldScan(t *testing.T) {
	d, p := setup(t, func(http.ResponseWriter, *http.Request) {})
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3)}
	base := http.DefaultTransport
	started, release := make(chan struct{}), make(chan struct{})
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") == "list" {
			close(started)
			<-release
		}
		return base.RoundTrip(r)
	})
	client := baiduapi.NewClient("same-token", "app")
	d.SetAuthentication(client, true, time.Now().Add(30*time.Millisecond))
	done := make(chan error, 1)
	go func() { done <- d.ScanAndDownload() }()
	<-started
	time.Sleep(60 * time.Millisecond)
	d.SetAuthentication(client, true, time.Now().Add(time.Hour))
	close(release)
	if err := <-done; !errors.Is(err, ErrAuthenticationWaiting) {
		t.Fatalf("old scan survived expiry update: %v", err)
	}
}
