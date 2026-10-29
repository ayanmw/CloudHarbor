package downloader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	_ "modernc.org/sqlite"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestReconcileSharesIdenticalPersistedFileInfoStorage(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	info := baiduapi.FileInfo{
		FsID: 71, Path: strings.Clone("/queue/a-large-file.bin"),
		ServerFilename: strings.Clone("a-large-file.bin"), Size: 1024, Mtime: 123,
		MD5: strings.Clone("0123456789abcdef0123456789abcdef"),
	}
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "done"}
	d.queue.jobs[info.FsID] = job
	d.queue.started = true // Keep this reconciliation fixture from launching workers.

	scanned := info
	scanned.Path = strings.Clone(info.Path)
	scanned.ServerFilename = strings.Clone(info.ServerFilename)
	scanned.MD5 = strings.Clone(info.MD5)
	if unsafe.StringData(scanned.Path) == unsafe.StringData(info.Path) {
		t.Fatal("fixture path unexpectedly shares storage")
	}
	if err := d.queue.reconcile([]baiduapi.FileInfo{scanned}, 0); err != nil {
		t.Fatal(err)
	}
	eligible := d.queue.eligible[info.FsID]
	if eligible == nil || eligible != &job.Info {
		t.Fatalf("eligible metadata pointer was not reused: got %p want %p", eligible, &job.Info)
	}
	if unsafe.StringData(eligible.Path) != unsafe.StringData(job.Info.Path) ||
		unsafe.StringData(eligible.ServerFilename) != unsafe.StringData(job.Info.ServerFilename) ||
		unsafe.StringData(eligible.MD5) != unsafe.StringData(job.Info.MD5) {
		t.Fatal("identical persisted metadata retained duplicate string storage")
	}
}

func TestReconcileDoesNotPublishJobsWhenBatchPersistenceFails(t *testing.T) {
	root := t.TempDir()
	d := NewDownloader(&config.Config{LocalDownDir: root, QueueDir: "/queue"}, baiduapi.NewClient("token", "app"))
	defer d.cancel()
	store, err := taskstore.Open(filepath.Join(root, stateDirectory, "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	d.queue.started = true
	d.SetAuthentication(baiduapi.NewClient("token", "app"), true, time.Now().Add(time.Hour))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	err = d.queue.reconcile([]baiduapi.FileInfo{file(1, "one.bin", 1), file(2, "two.bin", 1)}, d.authEpoch)
	if err == nil {
		t.Fatal("reconcile accepted a partial durable batch")
	}
	if len(d.queue.jobs) != 0 || len(d.queue.eligible) != 0 || !d.queue.started || d.ctx.Err() == nil {
		t.Fatalf("failed batch was published: jobs=%d eligible=%d started=%v cancelled=%v", len(d.queue.jobs), len(d.queue.eligible), d.queue.started, d.ctx.Err())
	}
	check, err := taskstore.Open(filepath.Join(root, stateDirectory, "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	loaded, err := check.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 || len(d.queue.jobs) != 0 {
		t.Fatalf("failed SQLite batch was not atomic: disk records=%d in-memory jobs=%d", len(loaded), len(d.queue.jobs))
	}
}

func TestReconcileQuarantinesIdentityWithUnknownRemoteMutation(t *testing.T) {
	root := t.TempDir()
	d := NewDownloader(&config.Config{LocalDownDir: root, QueueDir: "/queue", MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	defer d.cancel()
	store, err := taskstore.Open(filepath.Join(root, stateDirectory, "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	d.queue.started = true
	d.SetAuthentication(baiduapi.NewClient("token", "app"), true, time.Now().Add(time.Hour))
	info := file(881, "uncertain.bin", 10)
	d.queue.blockedRemoteIDs[info.FsID] = true
	if err = d.queue.reconcileObserved([]baiduapi.FileInfo{info}, d.authEpoch, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if d.queue.jobs[info.FsID] != nil || d.queue.eligible[info.FsID] != nil {
		t.Fatalf("unknown remote identity entered the download queue: job=%+v eligible=%+v", d.queue.jobs[info.FsID], d.queue.eligible[info.FsID])
	}
}

func TestCloudScanEvidenceSeparatesObservationFromProviderMtime(t *testing.T) {
	root := t.TempDir()
	d := NewDownloader(&config.Config{LocalDownDir: root, QueueDir: "/queue"}, baiduapi.NewClient("token", "app"))
	defer d.cancel()
	store, err := taskstore.Open(filepath.Join(root, stateDirectory, "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	d.queue.started = true
	d.SetAuthentication(baiduapi.NewClient("token", "app"), true, time.Now().Add(time.Hour))
	observed := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	info := file(81, "observed.bin", 12)
	info.Mtime = 1_800_000_000
	if err = d.queue.reconcileObserved([]baiduapi.FileInfo{info}, d.authEpoch, observed); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(info.FsID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "cloud_file_observed" || events[0].EvidenceType != "baidu_file_list" || events[0].CloudObservedAtUTC == nil || !events[0].CloudObservedAtUTC.Equal(observed) || events[0].OccurredAtUTC != nil || !events[0].RecordedAtUTC.After(observed) {
		t.Fatalf("cloud/local times were conflated or missing: %+v", events)
	}
	var evidence baiduapi.FileInfo
	if err = json.Unmarshal(events[0].Evidence, &evidence); err != nil || evidence.Mtime != info.Mtime {
		t.Fatalf("provider mtime missing from cloud evidence: %+v err=%v", evidence, err)
	}
}

func TestDownloadAttemptHasLinkedStartAndFinishEvidence(t *testing.T) {
	base, provider := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	provider.lists["/queue"] = []baiduapi.FileInfo{file(91, "attempt.bin", 3)}
	d, err := OpenDownloader(base.cfg, base.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 91, "done")
	events, err := d.queue.store.Events(91)
	if err != nil {
		t.Fatal(err)
	}
	var startedID string
	foundFinish := false
	for _, event := range events {
		switch event.Kind {
		case "download_attempt_started":
			startedID = event.AttemptID
		case "download_attempt_finished":
			foundFinish = event.AttemptID != "" && event.AttemptID == startedID
		}
	}
	if startedID == "" || !foundFinish {
		t.Fatalf("download attempt was not linked across its durable lifecycle: %+v", events)
	}
}

func TestReconcileRetainsChangedScanIdentityBesideUnknownJournal(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	oldInfo := baiduapi.FileInfo{FsID: 72, Path: "/queue/original.bin", ServerFilename: "original.bin", Size: 1024, Mtime: 123}
	job := &queueJob{Version: 1, Info: oldInfo, Mode: DownloadModeKeep, Status: "source_unknown", SourceState: "unknown"}
	d.queue.jobs[oldInfo.FsID] = job
	d.queue.started = true // Keep this reconciliation fixture from launching workers.

	scanned := oldInfo
	scanned.Path = "/queue/replaced.bin"
	scanned.ServerFilename = "replaced.bin"
	scanned.Mtime++
	if err := d.queue.reconcile([]baiduapi.FileInfo{scanned}, 0); err != nil {
		t.Fatal(err)
	}
	eligible := d.queue.eligible[oldInfo.FsID]
	if eligible == nil || *eligible != scanned {
		t.Fatalf("scan snapshot was not retained: got %+v want %+v", eligible, scanned)
	}
	if eligible == &job.Info || job.Info != oldInfo {
		t.Fatal("changed scan identity replaced or aliased the unknown journal")
	}
}

func TestFileInfoFilenameSharesPathStorageWhenItIsTheBasename(t *testing.T) {
	info := baiduapi.FileInfo{Path: strings.Clone("/queue/a-large-file.bin"), ServerFilename: strings.Clone("a-large-file.bin")}
	if unsafe.StringData(info.Path) == unsafe.StringData(info.ServerFilename) {
		t.Fatal("fixture path and filename unexpectedly share storage")
	}
	compacted := shareFileInfoStrings(info)
	if compacted != info {
		t.Fatalf("file metadata changed: got %+v want %+v", compacted, info)
	}
	pathBasename := compacted.Path[len(compacted.Path)-len(compacted.ServerFilename):]
	if unsafe.StringData(pathBasename) != unsafe.StringData(compacted.ServerFilename) {
		t.Fatal("basename filename did not reuse path storage")
	}
}

func TestFileInfoFilenameStorageIsNotSharedWhenItIsNotTheBasename(t *testing.T) {
	info := baiduapi.FileInfo{Path: strings.Clone("/queue/a-large-file.bin"), ServerFilename: strings.Clone("renamed.bin")}
	compacted := shareFileInfoStrings(info)
	if compacted != info {
		t.Fatalf("mismatched file metadata changed: got %+v want %+v", compacted, info)
	}
	if unsafe.StringData(compacted.Path) != unsafe.StringData(info.Path) ||
		unsafe.StringData(compacted.ServerFilename) != unsafe.StringData(info.ServerFilename) {
		t.Fatal("non-basename filename was replaced with a path substring")
	}
}

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
func shutdown(t testing.TB, d *Downloader) {
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

func TestRestoreKeepsTaskViewsWithoutMaterializingEveryTask(t *testing.T) {
	base, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	stored, err := OpenDownloader(base.cfg, base.getClient())
	if err != nil {
		t.Fatal(err)
	}
	shutdown(t, base)
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	job := queueJob{
		Version: 1,
		Info:    baiduapi.FileInfo{FsID: 31, Path: "/queue/restored.bin", ServerFilename: "restored.bin", Size: 4096},
		Mode:    DownloadModeKeep, Status: "done", Attempts: 2, Created: created, Updated: created.Add(time.Minute),
	}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := stored.queue.store.Save(job.Info.FsID, body); err != nil {
		t.Fatal(err)
	}
	shutdown(t, stored)

	restored, err := OpenDownloader(base.cfg, base.getClient())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdown(t, restored) })
	if len(restored.tasks) != 0 {
		t.Fatalf("restored %d derived task objects at startup; want lazy task views", len(restored.tasks))
	}

	byID := restored.GetTaskByFsID(31)
	if byID == nil || byID.Status != "done" || byID.Progress != 4096 || byID.Attempts != 2 || byID.StartTime != created || byID.FinishTime != created.Add(time.Minute) {
		t.Fatalf("restored task lookup = %+v", byID)
	}
	tasks := restored.GetTasks()
	if len(tasks) != 1 || tasks[0].FileInfo.FsID != 31 || tasks[0].TaskID != "31" {
		t.Fatalf("restored task list = %+v", tasks)
	}
	page := restored.GetTaskPage(0, 10, "name")
	if page.Total != 1 || page.Counts["done"] != 1 || len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 31 {
		t.Fatalf("restored task page = %+v", page)
	}
	total, counts := restored.GetTaskStats()
	if total != 1 || counts["done"] != 1 {
		t.Fatalf("restored task stats = total %d counts %+v", total, counts)
	}
}

func TestSchedulerSignalWakesEveryConfiguredWorker(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	d.cfg.MaxParallel = 3
	d.queue.mu.Lock()
	d.queue.started = true
	d.queue.signal()
	d.queue.mu.Unlock()
	if got := len(d.queue.wake); got != 3 {
		t.Fatalf("queued worker wakeups = %d, want 3", got)
	}
}

func TestClaimWaitsUntilEarliestRetryDeadline(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	laterInfo := file(81, "later.txt", 3)
	earlierInfo := file(82, "earlier.txt", 3)
	laterJob := &queueJob{Version: 1, Info: laterInfo, Mode: DownloadModeKeep, Status: "retry_wait", NextAttempt: time.Now().Add(100 * time.Millisecond)}
	earlierJob := &queueJob{Version: 1, Info: earlierInfo, Mode: DownloadModeKeep, Status: "retry_wait", NextAttempt: time.Now().Add(40 * time.Millisecond)}
	d.queue.jobs[laterInfo.FsID] = laterJob
	d.queue.eligible[laterInfo.FsID] = &laterInfo
	d.queue.jobs[earlierInfo.FsID] = earlierJob
	d.queue.eligible[earlierInfo.FsID] = &earlierInfo
	d.queue.rebuildCandidates()

	claimed, ctx, wait := d.queue.claim()
	if claimed != nil || ctx != nil || wait <= 0 || wait > time.Second {
		t.Fatalf("claim before retry deadline = job %v, ctx %v, wait %v", claimed, ctx, wait)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	<-timer.C

	claimed, ctx, wait = d.queue.claim()
	if claimed == nil || claimed.Info.FsID != earlierJob.Info.FsID || claimed.Status != "downloading" || ctx == nil || wait != 0 {
		t.Fatalf("claim at retry deadline = job %v, ctx %v, wait %v", claimed, ctx, wait)
	}
}

func TestClaimRechecksLongRetryDeadlinesForWallClockChanges(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	info := file(83, "clock-adjusted.txt", 3)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "retry_wait", NextAttempt: time.Now().Add(time.Minute)}
	d.queue.jobs[info.FsID] = job
	d.queue.eligible[info.FsID] = &info
	d.queue.rebuildCandidates()

	claimed, ctx, wait := d.queue.claim()
	if claimed != nil || ctx != nil || wait <= 0 || wait > 30*time.Second {
		t.Fatalf("long retry wait = job %v, ctx %v, wait %v; want wall-clock recheck within 30s", claimed, ctx, wait)
	}
}

func TestClaimRunsDueRetryAfterWallClockMovesForward(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	info := file(84, "clock-jump.txt", 3)
	wallNow := time.Now().UTC().Truncate(time.Second)
	deadline := wallNow.Add(time.Hour)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "retry_wait", NextAttempt: deadline}
	d.queue.jobs[info.FsID] = job
	d.queue.eligible[info.FsID] = &info
	d.queue.rebuildCandidates()

	claimed, ctx, wait := d.queue.claimAt(wallNow)
	if claimed != nil || ctx != nil || wait <= 0 || wait > 30*time.Second {
		t.Fatalf("claim before wall-clock jump = job %v, ctx %v, wait %v", claimed, ctx, wait)
	}

	claimed, ctx, wait = d.queue.claimAt(deadline.Add(time.Second))
	if claimed == nil || claimed.Info.FsID != info.FsID || ctx == nil || wait != 0 {
		t.Fatalf("claim after wall-clock jump = job %v, ctx %v, wait %v; want retry job immediately eligible", claimed, ctx, wait)
	}
	d.queue.active[info.FsID]()
}

func TestClaimUsesEligibleReadyQueueAndKeepsFutureRetriesDelayed(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	readyFirst := file(11, "ready-first.bin", 3)
	readyTie := file(10, "ready-tie.bin", 3)
	delayed := file(1, "delayed.bin", 3)
	d.queue.jobs[readyFirst.FsID] = &queueJob{Version: 1, Info: readyFirst, Mode: DownloadModeKeep, Status: "pending", Created: created}
	d.queue.eligible[readyFirst.FsID] = &readyFirst
	d.queue.jobs[readyTie.FsID] = &queueJob{Version: 1, Info: readyTie, Mode: DownloadModeKeep, Status: "pending", Created: created}
	d.queue.eligible[readyTie.FsID] = &readyTie
	d.queue.jobs[delayed.FsID] = &queueJob{Version: 1, Info: delayed, Mode: DownloadModeKeep, Status: "retry_wait", Created: created.Add(-time.Hour), NextAttempt: time.Now().Add(time.Hour)}
	d.queue.eligible[delayed.FsID] = &delayed
	for id := int64(100); id < 100100; id++ {
		d.queue.jobs[id] = &queueJob{Info: baiduapi.FileInfo{FsID: id}, Status: "done"}
	}
	d.queue.rebuildCandidates()

	claimed, ctx, wait := d.queue.claim()
	if claimed == nil || claimed.Info.FsID != readyTie.FsID || ctx == nil || wait != 0 {
		t.Fatalf("claim = job %v, ctx %v, wait %v; want equal-created ready job with lowest fs_id", claimed, ctx, wait)
	}
	d.queue.active[claimed.Info.FsID]()
}

func TestAuthenticationChangeClearsReadyCandidatesUntilFreshScan(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	info := file(92, "auth-gated.bin", 3)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending", Created: time.Now()}
	d.queue.jobs[info.FsID] = job
	d.queue.eligible[info.FsID] = &info
	d.queue.rebuildCandidates()
	if job.candidate == nil {
		t.Fatal("fixture did not create a runnable candidate")
	}

	d.SetAuthentication(baiduapi.NewClient("rotated", "app"), true, time.Now().Add(time.Hour))
	if job.candidate != nil {
		t.Fatal("credential rotation left a candidate authorized by the previous scan")
	}
	claimed, ctx, wait := d.queue.claim()
	if claimed != nil || ctx != nil || wait != 0 || job.Status != "pending" {
		t.Fatalf("claim after credential rotation = job %v, ctx %v, wait %v, status %s", claimed, ctx, wait, job.Status)
	}
}

func BenchmarkSchedulerClaimIdleHistory(b *testing.B) {
	for _, count := range []int{10000, 100000} {
		b.Run(fmt.Sprintf("jobs_%d", count), func(b *testing.B) {
			d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
			defer d.cancel()
			d.queue.jobs = make(map[int64]*queueJob, count)
			for id := 1; id <= count; id++ {
				d.queue.jobs[int64(id)] = &queueJob{Info: baiduapi.FileInfo{FsID: int64(id)}, Status: "done"}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if job, ctx, wait := d.queue.claim(); job != nil || ctx != nil || wait != 0 {
					b.Fatal("idle history returned a runnable task")
				}
			}
		})
	}
}

func BenchmarkReconcileNewJobs(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("jobs_%d", count), func(b *testing.B) {
			files := make([]baiduapi.FileInfo, count)
			for id := range files {
				name := fmt.Sprintf("new-%06d.bin", id+1)
				files[id] = baiduapi.FileInfo{FsID: int64(id + 1), Path: "/queue/" + name, ServerFilename: name, Size: int64(id + 1)}
			}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				root := b.TempDir()
				d := NewDownloader(&config.Config{LocalDownDir: root, QueueDir: "/queue"}, baiduapi.NewClient("token", "app"))
				store, err := taskstore.Open(filepath.Join(root, stateDirectory, "queue"), "/queue")
				if err != nil {
					b.Fatal(err)
				}
				d.queue.store = store
				d.queue.started = true
				d.SetAuthentication(baiduapi.NewClient("token", "app"), true, time.Now().Add(time.Hour))
				b.StartTimer()
				err = d.queue.reconcile(files, d.authEpoch)
				b.StopTimer()
				d.cancel()
				if closeErr := store.Close(); err == nil {
					err = closeErr
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
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

func TestOpenDownloaderRejectsMalformedJSONFromRawRestore(t *testing.T) {
	base, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	queuePath := filepath.Join(base.cfg.LocalDownDir, stateDirectory, "queue")
	store, err := taskstore.Open(queuePath, base.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	validBody, err := json.Marshal(queueJob{Version: 1, Info: file(1, "a.txt", 1), Mode: DownloadModeKeep, Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(1, validBody); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(queuePath, "queue.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE jobs SET payload_json=? WHERE id=1`, []byte(`{broken`)); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	if restored, err := OpenDownloader(base.cfg, base.getClient()); err == nil {
		shutdown(t, restored)
		t.Fatal("accepted malformed persisted JSON")
	}
	store, err = taskstore.Open(queuePath, base.cfg.QueueDir)
	if err != nil {
		t.Fatalf("malformed record leaked queue store lock: %v", err)
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
	store, err := taskstore.Open(filepath.Join(original.cfg.LocalDownDir, stateDirectory, "queue"), original.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(1, []byte(record)); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
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
