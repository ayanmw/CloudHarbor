package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
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

	t.Run("cancel retry wait records removed deadline", func(t *testing.T) {
		d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
		deadline := time.Date(2026, 11, 2, 4, 5, 6, 789, time.UTC)
		job := &queueJob{Version: 1, Info: file(3, "retry-cancel.bin", 1), Mode: DownloadModeKeep, Status: "retry_wait", Attempts: 4, Failures: 2, NextAttempt: deadline}
		store := seedEventJob(t, d, job)
		if err := d.Cancel(job.Info.FsID); err != nil {
			t.Fatal(err)
		}
		event, err := latestTaskEvent(t, store, job.Info.FsID, "task_cancelled")
		if err != nil {
			t.Fatal(err)
		}
		evidence := decodeEventEvidence(t, event)
		if got := evidence["previous_next_attempt_at_utc"]; got != deadline.Format(time.RFC3339Nano) {
			t.Fatalf("cancel event lost retry deadline: %#v", evidence)
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

func TestRetryPersistenceFailurePreservesFailedTask(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	info := file(4, "retry-persist-failure.bin", 42)
	deadline := time.Now().UTC().Add(time.Hour)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "failed", Attempts: 3, Failures: 2, Error: "previous download error", NextAttempt: deadline}
	d.queue.jobs[info.FsID] = job
	d.SetAuthentication(baiduapi.NewClient("retry-token", "app"), true, time.Now().Add(time.Hour))
	d.queue.eligible[info.FsID] = &info
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Retry(info.FsID); err == nil {
		t.Fatal("retry with closed SQLite store unexpectedly succeeded")
	} else if !strings.Contains(strings.ToLower(err.Error()), "closed") {
		t.Fatalf("retry did not reach SQLite persistence failure: %v", err)
	}
	if job.Status != "failed" || job.Attempts != 3 || job.Failures != 2 || job.Error != "previous download error" || !job.NextAttempt.Equal(deadline) {
		t.Fatalf("failed retry changed durable task projection in memory: %+v", job)
	}
}

func TestAuthenticationWaitEventRecordsGateReasonAndProviderResponse(t *testing.T) {
	t.Run("credentials not ready", func(t *testing.T) {
		d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
		job := &queueJob{Version: 1, Info: file(81, "auth-wait.bin", 1), Mode: DownloadModeKeep, Status: "pending"}
		store := seedEventJob(t, d, job)
		d.SetAuthentication(baiduapi.NewClient("", "app"), false, time.Time{})
		event, err := latestTaskEvent(t, store, job.Info.FsID, "authentication_wait_entered")
		if err != nil {
			t.Fatal(err)
		}
		evidence := decodeEventEvidence(t, event)
		if evidence["reason"] != "credentials_not_ready" || evidence["previous_status"] != "pending" || evidence["status"] != "auth_wait" {
			t.Fatalf("authentication gate evidence = %#v", evidence)
		}
		if event.ResponseObservedAtUTC != nil || event.OccurredAtUTC != nil {
			t.Fatalf("local credential state fabricated an online response/occurrence time: %+v", event)
		}
	})

	t.Run("provider rejected credential", func(t *testing.T) {
		d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
		job := &queueJob{Version: 1, Info: file(82, "auth-rejected.bin", 1), Mode: DownloadModeKeep, Status: "pending"}
		store := seedEventJob(t, d, job)
		client := baiduapi.NewClient("auth-event-secret", "app")
		d.SetAuthentication(client, true, time.Now().Add(time.Hour))
		oldTransport := http.DefaultTransport
		defer func() { http.DefaultTransport = oldTransport }()
		http.DefaultTransport = transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		})
		_, providerErr := client.ListDir("/queue")
		if providerErr == nil {
			t.Fatal("expected provider rejection")
		}
		d.reportAuthenticationFailure(providerErr)
		event, err := latestTaskEvent(t, store, job.Info.FsID, "authentication_wait_entered")
		if err != nil {
			t.Fatal(err)
		}
		evidence := decodeEventEvidence(t, event)
		if evidence["reason"] != "provider_rejected" || evidence["error_class"] != "provider_api_error" || evidence["provider_operation"] != "list" || evidence["provider_http_status"] != float64(http.StatusUnauthorized) {
			t.Fatalf("provider rejection evidence = %#v", evidence)
		}
		if event.ResponseObservedAtUTC == nil || event.OccurredAtUTC != nil || strings.Contains(string(event.Evidence), client.AccessToken) {
			t.Fatalf("provider event timestamps or credential redaction are incorrect: %+v", event)
		}
	})
}

func TestAuthenticationRecoveryDoesNotRetainPreviousRetryDeadline(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	job := &queueJob{
		Version: 1, Info: file(83, "retry-after-auth.bin", 1), Mode: DownloadModeKeep,
		Status: "retry_wait", Attempts: 2, Failures: 1, NextAttempt: time.Now().Add(time.Hour),
	}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(job.Info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.jobs[job.Info.FsID] = job
	d.queue.started = true // Keep the scheduler deterministic; claimAt below drives the candidate.
	oldRetryDeadline := job.NextAttempt.UTC()

	d.SetAuthentication(baiduapi.NewClient("", "app"), false, time.Time{})
	if job.Status != "auth_wait" {
		t.Fatalf("job status after credentials were rejected = %q, want auth_wait", job.Status)
	}
	if !job.NextAttempt.IsZero() {
		t.Fatalf("authentication wait retained obsolete retry deadline: %v", job.NextAttempt)
	}
	parkedEvent, err := latestTaskEvent(t, store, job.Info.FsID, "authentication_wait_entered")
	if err != nil {
		t.Fatal(err)
	}
	parkedEvidence := decodeEventEvidence(t, parkedEvent)
	if got, ok := parkedEvidence["cancelled_retry_deadline_at_utc"].(string); !ok || got != oldRetryDeadline.Format(time.RFC3339Nano) {
		t.Fatalf("cancelled retry deadline evidence missing or changed: %#v", parkedEvidence)
	}
	d.SetAuthentication(baiduapi.NewClient("fresh-token", "app"), true, time.Now().Add(time.Hour))
	if err := d.queue.reconcileObserved([]baiduapi.FileInfo{job.Info}, d.authEpoch, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if job.Status != "pending" {
		t.Fatalf("job status after successful scan = %q, want pending", job.Status)
	}
	claimed, _, wait := d.queue.claimAt(time.Now())
	if claimed == nil || wait != 0 {
		t.Fatalf("recovered task remained behind stale retry deadline: claimed=%v wait=%v next_attempt=%v", claimed != nil, wait, job.NextAttempt)
	}
}

func TestReconcilePersistsScanRecoveryAndMissingSourceEvidence(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	recovered := &queueJob{Version: 1, Info: file(1, "one.bin", 1), Mode: DownloadModeKeep, Status: "auth_wait", NextAttempt: time.Now().Add(time.Hour)}
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
		if id == 1 {
			if !recovered.NextAttempt.IsZero() {
				t.Fatalf("stale auth_wait deadline survived successful scan: %v", recovered.NextAttempt)
			}
			var evidence map[string]any
			if err := json.Unmarshal(event.Evidence, &evidence); err != nil {
				t.Fatal(err)
			}
			if _, ok := evidence["cleared_retry_deadline_at_utc"].(string); !ok {
				t.Fatalf("legacy deadline removal was not evidenced: %#v", evidence)
			}
		} else {
			var evidence map[string]any
			if err := json.Unmarshal(event.Evidence, &evidence); err != nil {
				t.Fatal(err)
			}
			if evidence["monitored_queue_dir"] != "/queue" {
				t.Fatalf("source-missing event must record the queue directory observed successfully: %#v", evidence)
			}
		}
	}
}

func TestReconcileUpdatesUnstartedJobByFsIDWithoutResettingTaskPolicy(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	created := time.Date(2026, 10, 30, 1, 2, 3, 0, time.UTC)
	oldInfo := file(88, "old-name.bin", 13)
	job := &queueJob{Version: 1, Info: oldInfo, Mode: DownloadModeDelete, Status: "pending", Created: created}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(job.Info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.jobs[job.Info.FsID] = job
	d.queue.started = true
	d.SetAuthentication(baiduapi.NewClient("fresh-token", "app"), true, time.Now().Add(time.Hour))
	observed := time.Date(2026, 10, 30, 2, 3, 4, 0, time.UTC)
	newInfo := oldInfo
	newInfo.Path = "/queue/new-name.bin"
	newInfo.ServerFilename = "new-name.bin"
	if err = d.queue.reconcileObserved([]baiduapi.FileInfo{newInfo}, d.authEpoch, observed); err != nil {
		t.Fatal(err)
	}
	if got := d.queue.jobs[oldInfo.FsID]; got != job || got.Info != newInfo || got.Mode != DownloadModeDelete || got.Created != created || got.Attempts != 0 || got.Failures != 0 {
		t.Fatalf("same fs_id was recreated or task policy/history changed: %+v", got)
	}
	event, err := latestTaskEvent(t, store, oldInfo.FsID, "task_cloud_identity_updated")
	if err != nil {
		t.Fatal(err)
	}
	if event.CloudObservedAtUTC == nil || !event.CloudObservedAtUTC.Equal(observed) || event.OperationID != "" || event.EvidenceType != "successful_cloud_scan" {
		t.Fatalf("scan identity evidence did not preserve its evidence source/time: %+v", event)
	}
	persisted, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var restored queueJob
	if err = json.Unmarshal(persisted[oldInfo.FsID], &restored); err != nil || restored.Info != newInfo || restored.Mode != DownloadModeDelete || restored.Created != created {
		t.Fatalf("updated task projection was not durable: %+v err=%v", restored, err)
	}
}

func TestReconcileDoesNotReplacePreviouslyAttemptedJobAfterIdentityChange(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	oldInfo := file(89, "attempted.bin", 13)
	job := &queueJob{Version: 1, Info: oldInfo, Mode: DownloadModeKeep, Status: "retry_wait", Attempts: 2, Failures: 1}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(job.Info.FsID, body); err != nil {
		t.Fatal(err)
	}
	d.queue.jobs[job.Info.FsID] = job
	d.queue.started = true
	d.SetAuthentication(baiduapi.NewClient("fresh-token", "app"), true, time.Now().Add(time.Hour))
	changed := oldInfo
	changed.Path = "/queue/renamed-attempted.bin"
	changed.ServerFilename = "renamed-attempted.bin"
	if err = d.queue.reconcileObserved([]baiduapi.FileInfo{changed}, d.authEpoch, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if got := d.queue.jobs[oldInfo.FsID]; got != job || got.Info != oldInfo || got.Mode != DownloadModeKeep || got.Status != "retry_wait" || got.Attempts != 2 || got.Failures != 1 {
		t.Fatalf("changed identity replaced/restarted an attempted task: %+v", got)
	}
	if job.candidate != nil {
		t.Fatal("attempted task with mismatched source identity became runnable")
	}
	persisted, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var restored queueJob
	if err = json.Unmarshal(persisted[oldInfo.FsID], &restored); err != nil || restored.Info != oldInfo || restored.Attempts != 2 || restored.Failures != 1 {
		t.Fatalf("original attempted job was not durable: %+v err=%v", restored, err)
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

func TestDownloadAttemptPersistsLocalStorageEvidence(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	job := &queueJob{Version: 1, Info: file(1, "one.bin", 1), Mode: DownloadModeKeep, Status: "downloading", Attempts: 1}
	store := seedEventJob(t, d, job)
	checkedAt := time.Date(2026, 11, 1, 1, 2, 3, 0, time.UTC)
	failure := &localStorageSpaceError{Kind: localStorageBlocked, Path: `D:\downloads`, RequiredBytes: 4096, AvailableBytes: 1024, ReservedBytes: 512, CheckedAtUTC: checkedAt}
	d.queue.complete(job, failure)
	event, err := latestTaskEvent(t, store, 1, "download_attempt_finished")
	if err != nil {
		t.Fatal(err)
	}
	evidence := decodeEventEvidence(t, event)
	if job.Status != "failed" || !job.NextAttempt.IsZero() {
		t.Fatalf("storage failure status=%s next_attempt=%v; want terminal/manual retry", job.Status, job.NextAttempt)
	}
	if evidence["outcome_class"] != localStorageBlocked || evidence["error_class"] != "local_storage_insufficient" {
		t.Fatalf("storage attempt classifications = %#v", evidence)
	}
	local, ok := evidence["local_storage"].(map[string]any)
	if !ok || local["path"] != `D:\downloads` || local["required_bytes"] != float64(4096) || local["available_bytes"] != float64(1024) || local["reserved_bytes"] != float64(512) || local["checked_at_utc"] != checkedAt.Format(time.RFC3339Nano) {
		t.Fatalf("local storage evidence = %#v", evidence["local_storage"])
	}
	if event.ResponseObservedAtUTC != nil || event.CloudObservedAtUTC != nil || event.OccurredAtUTC != nil {
		t.Fatalf("local capacity time was attributed to a provider or cloud observation: %+v", event)
	}
}

func TestLocalStorageSummaryIsPersistedOnTaskAndClearedForRetry(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	job := &queueJob{Version: 1, Info: file(1, "one.bin", 1), Mode: DownloadModeKeep, Status: "downloading", Attempts: 1}
	store := seedEventJob(t, d, job)
	checkedAt := time.Date(2026, 11, 2, 1, 2, 3, 0, time.UTC)
	failure := &localStorageSpaceError{Kind: localStorageBlocked, Path: `D:\downloads`, RequiredBytes: 4096, AvailableBytes: -1, ReservedBytes: 512, CheckedAtUTC: checkedAt}
	d.queue.complete(job, failure)

	page := d.GetTaskPage(0, 10, "name")
	if len(page.Tasks) != 1 || page.Tasks[0].LocalStorage == nil {
		t.Fatalf("failed task local storage summary = %+v", page.Tasks)
	}
	if page.Tasks[0].LocalStorage.Path != failure.Path || page.Tasks[0].LocalStorage.AvailableBytes != -1 || !page.Tasks[0].LocalStorage.CheckedAtUTC.Equal(checkedAt) {
		t.Fatalf("task local storage summary = %+v", page.Tasks[0].LocalStorage)
	}

	// The same optional field must survive SQLite queue JSON and accept records
	// written before the diagnostic existed.
	var persisted queueJob
	records, err := store.Load()
	body := records[1]
	if err != nil || json.Unmarshal(body, &persisted) != nil || persisted.LocalStorage == nil || persisted.LocalStorage.Path != failure.Path {
		t.Fatalf("persisted local storage summary = %+v, load err=%v", persisted, err)
	}
	var legacy queueJob
	if err := json.Unmarshal([]byte(`{"version":1,"file":{"fs_id":2},"mode":"keep","status":"failed"}`), &legacy); err != nil || legacy.LocalStorage != nil {
		t.Fatalf("legacy queue job decode = %+v, err=%v", legacy, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	d.queue.store = nil
	restored, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatalf("reopen downloader queue: %v", err)
	}
	defer shutdown(t, restored)
	restartedPage := restored.GetTaskPage(0, 10, "name")
	if len(restartedPage.Tasks) != 1 || restartedPage.Tasks[0].LocalStorage == nil || restartedPage.Tasks[0].LocalStorage.Path != failure.Path {
		t.Fatalf("task local storage summary after restart = %+v", restartedPage.Tasks)
	}

	restored.SetAuthentication(baiduapi.NewClient("token", "app"), true, time.Now().Add(time.Hour))
	restoredJob := restored.queue.jobs[1]
	restored.queue.eligible[1] = &restoredJob.Info
	if err := restored.Retry(1); err != nil {
		t.Fatal(err)
	}
	if restoredJob.LocalStorage != nil {
		t.Fatalf("manual retry retained stale local storage summary: %+v", restoredJob.LocalStorage)
	}
	restoredJob.Status = "downloading"
	restoredJob.LocalStorage = localStorageDetails(failure)
	restored.queue.complete(restoredJob, errors.New("unrelated transfer failure"))
	if restoredJob.LocalStorage != nil {
		t.Fatalf("non-storage failure retained stale local storage summary: %+v", restoredJob.LocalStorage)
	}
}

func TestLocalStorageErrorsDoNotScheduleProviderRetry(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	d.cfg.MaxRetries = 8
	job := &queueJob{Version: 1, Info: file(1, "one.bin", 1), Mode: DownloadModeKeep, Status: "downloading", Attempts: 1}
	store := seedEventJob(t, d, job)
	d.queue.complete(job, &localStorageSpaceError{Kind: localStorageFull, Path: "downloads", RequiredBytes: 1, AvailableBytes: 0, CheckedAtUTC: time.Now().UTC()})
	event, err := latestTaskEvent(t, store, 1, "download_attempt_finished")
	if err != nil {
		t.Fatal(err)
	}
	evidence := decodeEventEvidence(t, event)
	if job.Status != "failed" || job.Failures != 0 || evidence["outcome_class"] != localStorageFull || evidence["error_class"] != localStorageFull {
		t.Fatalf("local storage error entered provider retry: job=%+v evidence=%#v", job, evidence)
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

func TestDownloadAttemptEventClassifiesIncompleteStream(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	job := &queueJob{Version: 1, Info: file(1, "one.bin", 1), Mode: DownloadModeKeep, Status: "downloading", Attempts: 1}
	store := seedEventJob(t, d, job)
	d.queue.complete(job, io.ErrUnexpectedEOF)
	event, err := latestTaskEvent(t, store, 1, "download_attempt_finished")
	if err != nil {
		t.Fatal(err)
	}
	evidence := decodeEventEvidence(t, event)
	if evidence["error_class"] != "incomplete_stream" {
		t.Fatalf("incomplete stream evidence class = %#v", evidence["error_class"])
	}
	if string(event.Evidence) == "" || strings.Contains(string(event.Evidence), "unexpected EOF") {
		t.Fatalf("raw incomplete-stream error leaked into evidence: %s", event.Evidence)
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
