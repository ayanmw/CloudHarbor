package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func openEventStore(t *testing.T, d *Downloader) *taskstore.Store {
	t.Helper()
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	t.Cleanup(func() {
		if d.queue.store == store {
			_ = store.Close()
			d.queue.store = nil
		}
	})
	return store
}

func seedEventJob(t *testing.T, d *Downloader, job *queueJob) *taskstore.Store {
	t.Helper()
	store := openEventStore(t, d)
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(job.Info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.jobs[job.Info.FsID] = job
	d.queue.started = true
	return store
}

func decodeEventEvidence(t *testing.T, event taskstore.Event) map[string]any {
	t.Helper()
	var evidence map[string]any
	if err := json.Unmarshal(event.Evidence, &evidence); err != nil {
		t.Fatal(err)
	}
	return evidence
}

func latestTaskEvent(t *testing.T, store *taskstore.Store, id int64, kind string) (taskstore.Event, error) {
	t.Helper()
	events, err := store.Events(id)
	if err != nil {
		return taskstore.Event{}, err
	}
	if len(events) == 0 || events[len(events)-1].Kind != kind {
		return taskstore.Event{}, errors.New("expected latest task event " + kind)
	}
	return events[len(events)-1], nil
}

func TestCancelAndRetryPersistIntentEvents(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
		job := &queueJob{Version: 1, Info: file(1, "one.bin", 1), Mode: DownloadModeKeep, Status: "pending", Attempts: 2, Failures: 1}
		store := seedEventJob(t, d, job)
		if err := d.Cancel(1); err != nil {
			t.Fatal(err)
		}
		event, err := latestTaskEvent(t, store, 1, "task_cancelled")
		if err != nil {
			t.Fatal(err)
		}
		evidence := decodeEventEvidence(t, event)
		if evidence["previous_status"] != "pending" || evidence["status"] != "cancelled" || evidence["reason"] != "user_request" || evidence["attempts"] != float64(2) || evidence["failures"] != float64(1) {
			t.Fatalf("cancel evidence = %#v", evidence)
		}
	})

	t.Run("manual retry", func(t *testing.T) {
		d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
		job := &queueJob{Version: 1, Info: file(2, "two.bin", 1), Mode: DownloadModeKeep, Status: "failed", Attempts: 3, Failures: 3}
		store := seedEventJob(t, d, job)
		d.SetAuthentication(baiduapi.NewClient("token", "app"), true, time.Now().Add(time.Hour))
		d.queue.eligible[2] = &job.Info
		if err := d.Retry(2); err != nil {
			t.Fatal(err)
		}
		event, err := latestTaskEvent(t, store, 2, "task_retry_requested")
		if err != nil {
			t.Fatal(err)
		}
		evidence := decodeEventEvidence(t, event)
		if evidence["previous_status"] != "failed" || evidence["status"] != "pending" || evidence["reason"] != "manual_retry" || evidence["previous_attempts"] != float64(3) || evidence["attempts"] != float64(0) || evidence["previous_failures"] != float64(3) || evidence["failures"] != float64(0) {
			t.Fatalf("retry evidence = %#v", evidence)
		}
	})
}

func TestReconcilePersistsScanRecoveryAndMissingSourceEvidence(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	recovered := &queueJob{Version: 1, Info: file(1, "one.bin", 1), Mode: DownloadModeKeep, Status: "auth_wait"}
	missing := &queueJob{Version: 1, Info: file(2, "two.bin", 1), Mode: DownloadModeKeep, Status: "retry_wait", Attempts: 2, Failures: 1, NextAttempt: time.Now().Add(time.Minute)}
	for _, job := range []*queueJob{recovered, missing} {
		body, err := json.Marshal(job)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Save(job.Info.FsID, body); err != nil {
			t.Fatal(err)
		}
		d.queue.jobs[job.Info.FsID] = job
	}
	d.queue.started = true
	d.SetAuthentication(baiduapi.NewClient("fresh-token", "app"), true, time.Now().Add(time.Hour))
	observed := time.Date(2026, 10, 28, 1, 2, 3, 0, time.FixedZone("UTC+8", 8*60*60))
	if err := d.queue.reconcileObserved([]baiduapi.FileInfo{recovered.Info}, d.authEpoch, observed); err != nil {
		t.Fatal(err)
	}
	for id, kind := range map[int64]string{1: "task_recovered_after_scan", 2: "task_source_missing"} {
		event, err := latestTaskEvent(t, store, id, kind)
		if err != nil {
			t.Fatal(err)
		}
		if event.CloudObservedAtUTC == nil || !event.CloudObservedAtUTC.Equal(observed) {
			t.Fatalf("task %d event = %+v", id, event)
		}
		if event.OccurredAtUTC != nil || event.ResponseObservedAtUTC != nil {
			t.Fatalf("scan fabricated online occurrence/response time: %+v", event)
		}
		if event.RunID == "" {
			t.Fatalf("task %d event missing run id", id)
		}
	}
}

func TestStaleScanDoesNotWriteTransitionEvidence(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	job := &queueJob{Version: 1, Info: file(1, "one.bin", 1), Mode: DownloadModeKeep, Status: "auth_wait"}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(job.Info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.jobs[job.Info.FsID] = job
	d.queue.started = true
	d.SetAuthentication(baiduapi.NewClient("fresh-token", "app"), true, time.Now().Add(time.Hour))
	observed := time.Date(2026, 10, 28, 1, 2, 3, 0, time.UTC)
	if err := d.queue.reconcileObserved([]baiduapi.FileInfo{job.Info}, d.authEpoch-1, observed); !errors.Is(err, ErrAuthenticationWaiting) {
		t.Fatalf("stale scan error = %v", err)
	}
	events, err := store.Events(job.Info.FsID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || job.Status != "auth_wait" {
		t.Fatalf("stale scan changed state or added evidence: job=%+v events=%+v", job, events)
	}
}

func TestDownloadAttemptEventStoresClassAndFutureRetryTime(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	d.cfg.MaxRetries = 1
	d.cfg.RetryBackoffSeconds = 1
	job := &queueJob{Version: 1, Info: file(1, "one.bin", 1), Mode: DownloadModeKeep, Status: "downloading", Attempts: 1}
	store := seedEventJob(t, d, job)
	apiErr := &baiduapi.APIError{Operation: "download", Status: http.StatusServiceUnavailable, Code: 31034}
	d.queue.complete(job, apiErr)
	event, err := latestTaskEvent(t, store, 1, "download_attempt_finished")
	if err != nil {
		t.Fatal(err)
	}
	evidence := decodeEventEvidence(t, event)
	if evidence["outcome_class"] != "retry_scheduled" || evidence["status"] != "retry_wait" || evidence["provider_operation"] != "download" || evidence["provider_http_status"] != float64(503) || evidence["provider_error_code"] != float64(31034) {
		t.Fatalf("attempt evidence = %#v", evidence)
	}
	if _, exists := evidence["error_type"]; exists {
		t.Fatalf("legacy non-structured error field remains: %#v", evidence)
	}
	if event.ResponseObservedAtUTC == nil || event.OccurredAtUTC != nil || event.CloudObservedAtUTC != nil {
		t.Fatalf("HTTP response time was not kept separate from occurrence/scan time: %+v", event)
	}
	next, ok := evidence["next_attempt_at_utc"].(string)
	if !ok || next == "" {
		t.Fatalf("missing future retry time: %#v", evidence)
	}
	parsed, err := time.Parse(time.RFC3339Nano, next)
	if err != nil || !parsed.After(time.Now().Add(-time.Second)) || parsed.Location() != time.UTC || !parsed.Equal(job.NextAttempt.UTC()) {
		t.Fatalf("retry time is not a future UTC timestamp: %q (%v)", next, err)
	}
}

func TestDownloadAttemptEventStoresSafeFailureClassWithoutRawError(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	job := &queueJob{Version: 1, Info: file(1, "one.bin", 1), Mode: DownloadModeKeep, Status: "downloading", Attempts: 1}
	store := seedEventJob(t, d, job)
	failure := &os.PathError{Op: "write", Path: `C:\private\secret.bin`, Err: errors.New("disk write failed")}
	d.queue.complete(job, failure)
	event, lookupErr := latestTaskEvent(t, store, 1, "download_attempt_finished")
	if lookupErr != nil {
		t.Fatal(lookupErr)
	}
	evidence := decodeEventEvidence(t, event)
	if evidence["error_class"] != "filesystem_error" {
		t.Fatalf("attempt evidence missing safe failure class: %#v", evidence)
	}
	if strings.Contains(string(event.Evidence), "secret.bin") || strings.Contains(string(event.Evidence), "disk write failed") {
		t.Fatalf("raw error leaked into event evidence: %s", event.Evidence)
	}
}

type timeoutTestError struct{}

func (timeoutTestError) Error() string   { return "request timed out" }
func (timeoutTestError) Timeout() bool   { return true }
func (timeoutTestError) Temporary() bool { return true }

func TestDownloadAttemptEventDistinguishesNetworkTimeoutWithoutLeakingURL(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	job := &queueJob{Version: 1, Info: file(1, "one.bin", 1), Mode: DownloadModeKeep, Status: "downloading", Attempts: 1}
	store := seedEventJob(t, d, job)
	failure := &url.Error{Op: "Get", URL: "https://private.example/download?token=secret", Err: net.Error(timeoutTestError{})}
	d.queue.complete(job, failure)
	event, lookupErr := latestTaskEvent(t, store, 1, "download_attempt_finished")
	if lookupErr != nil {
		t.Fatal(lookupErr)
	}
	evidence := decodeEventEvidence(t, event)
	if evidence["error_class"] != "network_timeout" {
		t.Fatalf("timeout evidence class = %#v", evidence["error_class"])
	}
	if strings.Contains(string(event.Evidence), "private.example") || strings.Contains(string(event.Evidence), "token=secret") {
		t.Fatalf("sensitive URL leaked into event evidence: %s", event.Evidence)
	}
}

func TestDownloadOutcomeClassMatchesSchedulerFinalState(t *testing.T) {
	retryable := &baiduapi.APIError{Operation: "download", Status: http.StatusServiceUnavailable}
	authentication := &baiduapi.APIError{Operation: "download", Status: http.StatusUnauthorized}
	cases := []struct {
		name     string
		status   string
		err      error
		stopping bool
		jobError string
		want     string
	}{
		{name: "success", status: "done", want: "succeeded"},
		{name: "authentication wait", status: "auth_wait", err: ErrAuthenticationWaiting, want: "authentication_wait"},
		{name: "recovered credentials", status: "pending", err: authentication, want: "authentication_recovered"},
		{name: "interrupted", status: "pending", err: context.Canceled, want: "interrupted"},
		{name: "retry scheduled", status: "retry_wait", err: retryable, want: "retry_scheduled"},
		{name: "retry exhausted", status: "failed", err: retryable, want: "retry_exhausted"},
		{name: "non retryable", status: "failed", err: errors.New("local failure"), want: "non_retryable"},
		{name: "cancelled", status: "cancelled", err: context.Canceled, want: "cancelled"},
		{name: "source operation unknown", status: "source_unknown", err: ErrSourceOperationUnknown, want: "source_operation_unknown"},
		{name: "unclassified", status: "unexpected", want: "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job := &queueJob{Status: tc.status, Error: tc.jobError}
			if got := downloadOutcomeClass(job, tc.err, tc.stopping); got != tc.want {
				t.Fatalf("downloadOutcomeClass() = %q, want %q", got, tc.want)
			}
		})
	}
}
