package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestSourceOperationFailureEventRecordsStructuredProviderEvidence(t *testing.T) {
	base, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d, err := OpenDownloader(base.cfg, base.client)
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	info := file(701, "source.txt", 3)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeDelete, Status: "downloading", Created: time.Now()}
	d.queue.jobs[info.FsID] = job
	if err = d.queue.persist(job); err != nil {
		t.Fatal(err)
	}
	if _, err = d.beginSourceOperation(context.Background(), info); err != nil {
		t.Fatal(err)
	}
	providerErr := &baiduapi.APIError{Operation: "delete", Status: http.StatusServiceUnavailable, Code: 31034}
	if err = d.finishSourceOperation(info, providerErr); err == nil {
		t.Fatal("expected retryable source operation error")
	}
	events, err := d.queue.store.Events(info.FsID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("source operation events missing")
	}
	event := events[len(events)-1]
	if event.Kind != "source_operation_retryable_failure" || event.ResponseObservedAtUTC == nil || event.OccurredAtUTC != nil {
		t.Fatalf("failure event timestamps or kind are incorrect: %+v", event)
	}
	var evidence struct {
		ErrorClass         string `json:"error_class"`
		ProviderOperation  string `json:"provider_operation"`
		ProviderHTTPStatus int    `json:"provider_http_status"`
		ProviderErrorCode  int    `json:"provider_error_code"`
	}
	if err = json.Unmarshal(event.Evidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.ErrorClass != "provider_api_error" || evidence.ProviderOperation != "delete" || evidence.ProviderHTTPStatus != http.StatusServiceUnavailable || evidence.ProviderErrorCode != 31034 {
		t.Fatalf("structured provider evidence missing: %+v", evidence)
	}
	if strings.Contains(string(event.Evidence), providerErr.Error()) {
		t.Fatal("raw provider error text was persisted in event evidence")
	}
}

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
			var events []taskstore.Event
			var sendingID string
			foundSending, foundUnknown := false, false
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) && !foundUnknown {
				events, err = d.queue.store.Events(1)
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range events {
					switch event.Kind {
					case "source_operation_sending":
						foundSending = true
						sendingID = event.OperationID
					case "source_operation_unknown":
						foundUnknown = true
						if sendingID == "" || event.OperationID != sendingID || event.AttemptID == "" || event.ResponseObservedAtUTC != nil || event.OccurredAtUTC != nil {
							t.Fatalf("unknown receipt is not linked to the durable send: sending=%q event=%+v", sendingID, event)
						}
					}
				}
				if !foundUnknown {
					time.Sleep(10 * time.Millisecond)
				}
			}
			if !foundSending || !foundUnknown {
				t.Fatalf("source operation evidence missing: %+v", events)
			}
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

func TestValidSourceRecordRequiresPairedOperationIDs(t *testing.T) {
	job := &queueJob{Status: "source_unknown", SourceState: "unknown", SourceOperationID: "0123456789abcdef0123456789abcdef"}
	if validSourceRecord(job) {
		t.Fatal("accepted source operation without its paired attempt ID")
	}
	job.SourceAttemptID = "fedcba9876543210fedcba9876543210"
	if !validSourceRecord(job) {
		t.Fatal("rejected well-formed linked source operation IDs")
	}
	job.SourceState = "confirmed"
	job.Status = "done"
	if validSourceRecord(job) {
		t.Fatal("accepted operation IDs after the operation was finalized")
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

func TestMissingSourceIdentityRecordsMetadataResponseEvidence(t *testing.T) {
	base, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d, err := OpenDownloader(base.cfg, base.client)
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	info := file(75, "vanished.txt", 3)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeDelete, Status: "downloading", Created: time.Now()}
	d.queue.mu.Lock()
	d.queue.jobs[info.FsID] = job
	if err = d.queue.persist(job); err != nil {
		d.queue.mu.Unlock()
		t.Fatal(err)
	}
	d.queue.mu.Unlock()
	err = d.postprocess(context.Background(), info, DownloadModeDelete)
	if !errors.Is(err, ErrSourceIdentityChanged) {
		t.Fatalf("postprocess error=%v, want missing source identity conflict", err)
	}
	event, err := latestTaskEvent(t, d.queue.store, info.FsID, "source_identity_changed")
	if err != nil {
		t.Fatal(err)
	}
	if event.ResponseObservedAtUTC == nil || event.CloudObservedAtUTC != nil || event.OccurredAtUTC != nil {
		t.Fatalf("missing metadata response time was not classified separately: %+v", event)
	}
	var evidence struct {
		Expected baiduapi.FileInfo  `json:"expected"`
		Observed *baiduapi.FileInfo `json:"observed"`
		Missing  bool               `json:"missing"`
	}
	if err = json.Unmarshal(event.Evidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Expected != info || evidence.Observed != nil || !evidence.Missing {
		t.Fatalf("missing source evidence is incomplete: %+v", evidence)
	}
	if len(p.actions) != 0 {
		t.Fatalf("missing source unexpectedly caused cloud postprocessing: %v", p.actions)
	}
}

func TestChangedSourceIdentityWaitsForFreshListingBeforeRetry(t *testing.T) {
	base, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d, err := OpenDownloader(base.cfg, base.client)
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	d.SetDownloadMode(DownloadModeDelete)
	original := file(74, "same.txt", 3)
	changed := original
	changed.Mtime++
	p.lists["/queue"] = []baiduapi.FileInfo{changed} // The metadata check has observed a new version; listing still reports the old one.
	job := &queueJob{Version: 1, Info: original, Mode: DownloadModeDelete, Status: "downloading", Created: time.Now()}
	d.queue.mu.Lock()
	d.queue.jobs[original.FsID] = job
	d.queue.eligible[original.FsID] = &original
	if err = d.queue.persist(job); err != nil {
		d.queue.mu.Unlock()
		t.Fatal(err)
	}
	d.queue.mu.Unlock()
	err = d.postprocess(context.Background(), original, DownloadModeDelete)
	if !errors.Is(err, ErrSourceIdentityChanged) {
		t.Fatalf("postprocess error=%v", err)
	}
	if err.Error() != ErrSourceIdentityChanged.Error() {
		t.Fatalf("identity conflict was described as retryable: %v", err)
	}
	event, eventErr := latestTaskEvent(t, d.queue.store, original.FsID, "source_identity_changed")
	if eventErr != nil {
		t.Fatal(eventErr)
	}
	if event.ResponseObservedAtUTC == nil || event.CloudObservedAtUTC != nil || event.OccurredAtUTC != nil {
		t.Fatalf("metadata response time was not kept distinct from scan/occurrence time: %+v", event)
	}
	var identityEvidence struct {
		Expected baiduapi.FileInfo  `json:"expected"`
		Observed *baiduapi.FileInfo `json:"observed"`
		Missing  bool               `json:"missing"`
	}
	if eventErr = json.Unmarshal(event.Evidence, &identityEvidence); eventErr != nil {
		t.Fatal(eventErr)
	}
	if identityEvidence.Expected != original || identityEvidence.Observed == nil || *identityEvidence.Observed != changed || identityEvidence.Missing {
		t.Fatalf("identity mismatch evidence did not preserve expected and observed cloud metadata: %+v", identityEvidence)
	}
	d.queue.complete(job, err)
	if err = d.Retry(original.FsID); !errors.Is(err, ErrTaskConflict) {
		t.Fatalf("retry after identity mismatch = %v, want conflict until a fresh listing updates the identity", err)
	}
	if d.queue.jobs[original.FsID].SourceState != "changed" {
		t.Fatalf("identity mismatch state was not persisted: %+v", d.queue.jobs[original.FsID])
	}
	if len(p.actions) != 0 {
		t.Fatalf("identity mismatch sent a source operation: %v", p.actions)
	}
	shutdown(t, d)
	d, err = OpenDownloader(base.cfg, base.client)
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	if err = d.Retry(original.FsID); !errors.Is(err, ErrTaskConflict) {
		t.Fatalf("restart allowed retry against stale identity: %v", err)
	}

	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, original.FsID, "done")
	events, err := d.queue.store.Events(original.FsID)
	if err != nil {
		t.Fatal(err)
	}
	confirmed := false
	for _, event := range events {
		if event.Kind == "source_operation_confirmed" {
			confirmed = event.OperationID != "" && event.AttemptID != "" && event.ResponseObservedAtUTC != nil && event.OccurredAtUTC == nil
		}
	}
	if !confirmed {
		t.Fatalf("synchronous result evidence missing or occurrence time was guessed: %+v", events)
	}
	if task := d.GetTaskByFsID(original.FsID); task == nil || task.FileInfo != changed {
		t.Fatalf("fresh listing did not replace stale identity: %+v", task)
	}
	if len(p.actions) != 1 || !strings.Contains(p.actions[0], "/queue/same.txt") {
		t.Fatalf("fresh identity did not receive exactly one postprocess: %v", p.actions)
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

func TestSourceIntentPersistenceFailureKeepsDefinitelyUnsentJobState(t *testing.T) {
	original, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected source operation request") })
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	info := file(91, "a.txt", 3)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeDelete, Status: "downloading", Error: "prior error", Created: time.Now(), NextAttempt: time.Now().Add(time.Minute)}
	d.queue.mu.Lock()
	d.queue.jobs[info.FsID] = job
	if err = d.queue.persist(job); err != nil {
		d.queue.mu.Unlock()
		t.Fatal(err)
	}
	d.queue.mu.Unlock()
	if err = d.queue.store.Close(); err != nil {
		t.Fatal(err)
	}
	if tracked, beginErr := d.beginSourceOperation(context.Background(), info); beginErr == nil || tracked {
		t.Fatalf("failed send-intent persistence = tracked %v, error %v", tracked, beginErr)
	}
	if job.Status != "downloading" || job.SourceState != "" || job.SourceOperationID != "" || job.SourceAttemptID != "" || job.Error != "prior error" || job.NextAttempt.IsZero() {
		t.Fatalf("definitely unsent job was projected as uncertain or mutated: %+v", job)
	}
	if d.queue.blockedSources[info.Path] {
		t.Fatal("definitely unsent source remained quarantined")
	}
	shutdown(t, d)
	d, err = OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	if restored := d.GetTaskByFsID(info.FsID); restored == nil || restored.Status != "pending" || restored.CanRetry {
		t.Fatalf("reopened definitely-unsent task did not return to scan-gated pending state: %+v", restored)
	} else if persisted := d.queue.jobs[info.FsID]; persisted.SourceState != "" || persisted.SourceOperationID != "" || persisted.SourceAttemptID != "" {
		t.Fatalf("reopened definitely-unsent task retained a source journal: %+v", persisted)
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
