package downloader

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestManagedPathsPersistAcrossRestartAndKeepStoreScope(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	initialLocal := filepath.Join(root, "downloads")
	cfg := &config.Config{QueueDir: "/old-monitor", LocalDownDir: initialLocal, StateDir: state, MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	newLocal := filepath.Join(root, "new-downloads")
	want := ManagedPaths{QueueDir: "/new-monitor", LocalDownDir: newLocal}
	if _, err = d.SetManagedPaths(context.Background(), want, false); err != nil {
		t.Fatal(err)
	}
	if got := d.GetManagedPaths(); got.QueueDir != want.QueueDir || !sameLocalPath(got.LocalDownDir, want.LocalDownDir) {
		t.Fatalf("runtime path not updated: %#v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = d.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenDownloader(&config.Config{QueueDir: "/old-monitor", LocalDownDir: initialLocal, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Shutdown(context.Background())
	got := reopened.GetManagedPaths()
	if got.QueueDir != want.QueueDir || !sameLocalPath(got.LocalDownDir, want.LocalDownDir) {
		t.Fatalf("managed paths not restored: %#v", got)
	}
	if _, err = os.Stat(newLocal); err != nil {
		t.Fatalf("new local directory was not created: %v", err)
	}
	if err = reopened.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	explicitLocal := filepath.Join(root, "environment-downloads")
	explicit, err := OpenDownloader(&config.Config{QueueDir: "/environment-monitor", QueueDirExplicit: true, LocalDownDir: explicitLocal, LocalDownDirExplicit: true, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer explicit.Shutdown(context.Background())
	got = explicit.GetManagedPaths()
	if got.QueueDir != "/environment-monitor" || !sameLocalPath(got.LocalDownDir, explicitLocal) {
		t.Fatalf("explicit startup paths must override web settings: %#v", got)
	}
}

func TestPreviewManagedPathChangeFiltersAndSortsAffectedJobs(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())

	d.queue.mu.Lock()
	for _, tc := range []struct {
		id     int64
		name   string
		path   string
		status string
	}{
		{id: 3, name: "three.txt", path: "/queue/three.txt", status: "pending"},
		{id: 1, name: "one.txt", path: "/queue/one.txt", status: "auth_wait"},
		{id: 2, name: "two.txt", path: "/queue/two.txt", status: "retry_wait"},
		{id: 4, name: "done.txt", path: "/queue/done.txt", status: "done"},
		{id: 5, name: "active.txt", path: "/queue/active.txt", status: "downloading"},
		{id: 6, name: "nested.txt", path: "/queue/new/nested.txt", status: "pending"},
		{id: 7, name: "elsewhere.txt", path: "/elsewhere/elsewhere.txt", status: "pending"},
	} {
		info := file(tc.id, tc.name, 1)
		info.Path = tc.path
		d.queue.jobs[tc.id] = &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: tc.status}
	}
	d.queue.mu.Unlock()

	impact, err := d.PreviewManagedPathChange("/queue/new")
	if err != nil {
		t.Fatal(err)
	}
	if impact.CurrentQueueDir != "/queue" || impact.TargetQueueDir != "/queue/new" || impact.Count != 3 {
		t.Fatalf("unexpected impact summary: %+v", impact)
	}
	wantIDs := []int64{1, 2, 3}
	if len(impact.Tasks) != len(wantIDs) {
		t.Fatalf("impact tasks = %+v", impact.Tasks)
	}
	for i, want := range wantIDs {
		if impact.Tasks[i].FsID != want {
			t.Fatalf("impact tasks are not stably sorted: %+v", impact.Tasks)
		}
	}
}

func TestPreviewManagedPathChangeDigestChangesWithAffectedJobSnapshot(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())

	info := file(8, "first.txt", 1)
	d.queue.mu.Lock()
	d.queue.jobs[info.FsID] = &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending"}
	d.queue.mu.Unlock()
	first, err := d.PreviewManagedPathChange("/new-queue")
	if err != nil {
		t.Fatal(err)
	}

	d.queue.mu.Lock()
	changed := *d.queue.jobs[info.FsID]
	changed.Status = "retry_wait"
	changed.Info.Size++
	d.queue.jobs[info.FsID] = &changed
	d.queue.mu.Unlock()
	second, err := d.PreviewManagedPathChange("/new-queue")
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == "" || second.Digest == "" || first.Digest == second.Digest {
		t.Fatalf("preview digest did not bind the affected job snapshot: first=%+v second=%+v", first, second)
	}
}

func TestPreviewManagedPathChangePagesOneStableSnapshot(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	for id := int64(31); id <= 33; id++ {
		info := file(id, "item.txt", 3)
		d.queue.jobs[id] = &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending"}
	}
	first, err := d.PreviewManagedPathChangePage("/new-queue", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.PreviewManagedPathChangePage("/new-queue", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.Count != 3 || first.Offset != 0 || first.Limit != 2 || !first.HasMore || len(first.Tasks) != 2 {
		t.Fatalf("unexpected first impact page: %+v", first)
	}
	if second.Count != first.Count || second.Offset != 2 || second.HasMore || len(second.Tasks) != 1 {
		t.Fatalf("unexpected second impact page: %+v", second)
	}
	if first.Digest != second.Digest || first.Tasks[0].FsID != 31 || first.Tasks[1].FsID != 32 || second.Tasks[0].FsID != 33 {
		t.Fatalf("pages did not share one ordered snapshot: first=%+v second=%+v", first, second)
	}
	beyond, err := d.PreviewManagedPathChangePage("/new-queue", int(^uint(0)>>1), 2)
	if err != nil {
		t.Fatal(err)
	}
	if beyond.Offset != beyond.Count || len(beyond.Tasks) != 0 || beyond.HasMore || beyond.Digest != first.Digest {
		t.Fatalf("out-of-range page was not safely clamped: %+v", beyond)
	}
}

func TestSetManagedPathsRequiresConfirmationForAffectedQueueJobs(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	info := file(20, "pending.txt", 3)
	d.queue.mu.Lock()
	d.queue.jobs[info.FsID] = &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending"}
	d.queue.mu.Unlock()
	before, err := d.queue.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	want := ManagedPaths{QueueDir: "/new-queue", LocalDownDir: d.LocalDownloadDirectory()}
	if _, err = d.SetManagedPaths(context.Background(), want, false); err == nil {
		t.Fatal("path change with affected queue jobs was accepted without confirmation")
	}
	if got := d.GetManagedPaths(); got.QueueDir != "/queue" {
		t.Fatalf("rejected change modified runtime path: %+v", got)
	}
	after, err := d.queue.store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("rejected change modified persisted settings: before=%s after=%s", before, after)
	}
}

func TestSetManagedPathsAcceptsMatchingImpactConfirmation(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	info := file(21, "pending.txt", 3)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "retry_wait", Attempts: 1}
	d.queue.mu.Lock()
	d.queue.jobs[info.FsID] = job
	d.queue.mu.Unlock()
	if err = d.queue.persist(job); err != nil {
		t.Fatal(err)
	}
	impact, err := d.PreviewManagedPathChange("/new-queue")
	if err != nil {
		t.Fatal(err)
	}
	want := ManagedPaths{QueueDir: "/new-queue", LocalDownDir: d.LocalDownloadDirectory()}
	if _, err = d.SetManagedPathsConfirmed(context.Background(), want, false, impact.Digest); err != nil {
		t.Fatalf("matching preview confirmation was rejected: %v", err)
	}
	if got := d.GetManagedPaths(); got.QueueDir != want.QueueDir {
		t.Fatalf("confirmed path was not saved: %+v", got)
	}
	if err = d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Shutdown(context.Background())
	if got := reopened.GetManagedPaths(); got.QueueDir != want.QueueDir {
		t.Fatalf("confirmed queue path did not survive restart: %+v", got)
	}
	if task := reopened.GetTaskByFsID(info.FsID); task == nil || task.FileInfo != info || task.Status != "retry_wait" || task.Attempts != 1 {
		t.Fatalf("confirmed path change altered the persisted task before a successful scan: %+v", task)
	}
}

func TestSetManagedPathsInvalidatesOldQueueCandidatesUntilNewScan(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	info := file(23, "old-queue.txt", 3)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending", Created: time.Now()}
	d.queue.mu.Lock()
	d.queue.jobs[info.FsID] = job
	d.queue.eligible[info.FsID] = retainFileInfo(info)
	d.queue.enqueueCandidate(job)
	d.queue.mu.Unlock()
	if job.candidate == nil {
		t.Fatal("test setup did not enqueue the previously scanned candidate")
	}
	impact, err := d.PreviewManagedPathChange("/new-queue")
	if err != nil {
		t.Fatal(err)
	}
	want := ManagedPaths{QueueDir: "/new-queue", LocalDownDir: d.LocalDownloadDirectory()}
	if _, err = d.SetManagedPathsConfirmed(context.Background(), want, false, impact.Digest); err != nil {
		t.Fatal(err)
	}
	d.queue.mu.Lock()
	if job.candidate != nil || d.queue.eligible[info.FsID] != nil {
		d.queue.mu.Unlock()
		t.Fatal("old queue eligibility remained after the queue path changed")
	}
	d.queue.mu.Unlock()
	if claimed, _, _ := d.queue.claimAt(time.Now()); claimed != nil {
		t.Fatalf("stale candidate was claimed before a successful scan of the new queue: %+v", claimed)
	}
	if job.Status != "pending" {
		t.Fatalf("path change should preserve the historical task status, got %q", job.Status)
	}
}

func TestSetManagedPathsRejectsStaleImpactConfirmationWithoutMutation(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	info := file(22, "pending.txt", 3)
	d.queue.mu.Lock()
	d.queue.jobs[info.FsID] = &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "pending"}
	d.queue.mu.Unlock()
	preview, err := d.PreviewManagedPathChange("/new-queue")
	if err != nil {
		t.Fatal(err)
	}
	d.queue.mu.Lock()
	changed := *d.queue.jobs[info.FsID]
	changed.Status = "retry_wait"
	changed.Attempts = 1
	d.queue.jobs[info.FsID] = &changed
	d.queue.mu.Unlock()
	want := ManagedPaths{QueueDir: "/new-queue", LocalDownDir: d.LocalDownloadDirectory()}
	_, err = d.SetManagedPathsConfirmed(context.Background(), want, false, preview.Digest)
	var confirmationErr *ManagedPathImpactConfirmationError
	if !errors.As(err, &confirmationErr) || !confirmationErr.Stale || confirmationErr.Impact.Tasks[0].Status != "retry_wait" {
		t.Fatalf("stale confirmation did not return the latest impact: err=%v impact=%+v", err, confirmationErr)
	}
	if got := d.GetManagedPaths(); got.QueueDir != "/queue" {
		t.Fatalf("stale confirmation modified runtime path: %+v", got)
	}
}

func TestOpenDownloaderRejectsLocalDownloadPathAliasedIntoStateDirectory(t *testing.T) {
	for _, mode := range []string{"explicit config", "persisted settings"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			state := filepath.Join(root, "state")
			alias := filepath.Join(root, "state-alias")
			target := filepath.Join(state, "nested")
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Fatal(err)
			}
			createMigrationTestAlias(t, alias, target)
			aliasedLocal := filepath.Join(alias, "downloads")
			baseLocal := filepath.Join(root, "downloads")
			cfg := &config.Config{QueueDir: "/queue", LocalDownDir: baseLocal, StateDir: state, MaxParallel: 1}
			if mode == "explicit config" {
				cfg.LocalDownDir, cfg.LocalDownDirExplicit = aliasedLocal, true
				d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
				if err == nil {
					_ = d.Shutdown(context.Background())
					t.Fatal("startup accepted a configured download directory aliased into the SQLite state directory")
				}
				return
			}
			d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
			if err != nil {
				t.Fatal(err)
			}
			if err = d.saveAppSettings(map[string]any{"managed_local_download_dir": aliasedLocal}); err != nil {
				_ = d.Shutdown(context.Background())
				t.Fatal(err)
			}
			if err = d.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if reopened, openErr := OpenDownloader(cfg, baiduapi.NewClient("token", "app")); openErr == nil {
				_ = reopened.Shutdown(context.Background())
				t.Fatal("startup accepted a persisted download directory aliased into the SQLite state directory")
			}
		})
	}
}

func TestManagedPathsRejectRunningTasksAndExplicitEnvironment(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	d.queue.mu.Lock()
	d.queue.active[42] = func() {}
	d.queue.mu.Unlock()
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/new", LocalDownDir: filepath.Join(root, "new")}, false); err == nil {
		t.Fatal("running task should prevent path changes")
	}
	d.queue.mu.Lock()
	delete(d.queue.active, 42)
	d.queue.mu.Unlock()
	cfg.QueueDirExplicit = true
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/new", LocalDownDir: filepath.Join(root, "next")}, false); err == nil {
		t.Fatal("explicit environment queue path should be immutable in the web settings path")
	}
	if got := d.GetManagedPaths().QueueDir; got != "/queue" {
		t.Fatalf("rejected change modified active path: %s", got)
	}
}

func TestManagedPathExpectedSnapshotIsCheckedAfterAdmissionUnderPathLock(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "initial"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("synthetic-token", "synthetic-app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	expected := d.GetManagedPaths()
	changed := ManagedPaths{QueueDir: "/other", LocalDownDir: filepath.Join(root, "changed")}
	d.managedPathsAdmissionHook = func() {
		d.pathMu.Lock()
		d.queueDir, d.localDownDir = changed.QueueDir, changed.LocalDownDir
		d.pathMu.Unlock()
	}
	target := ManagedPaths{QueueDir: "/target", LocalDownDir: filepath.Join(root, "target")}
	_, err = d.SetManagedPathsConfirmed(context.Background(), target, false, "", expected)
	if !errors.Is(err, ErrManagedPathsChanged) {
		t.Fatalf("admission changed paths but result=%v", err)
	}
	if d.GetManagedPaths() != changed {
		t.Fatal("old confirmation overwrote admitted current paths")
	}
	if _, statErr := os.Stat(target.LocalDownDir); !os.IsNotExist(statErr) {
		t.Fatalf("stale confirmation created local target: %v", statErr)
	}
}

func TestManagedPathQueueObservationReportsActiveDownloadsWithoutAcquiringMaintenance(t *testing.T) {
	root := t.TempDir()
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}, baiduapi.NewClient("synthetic-token", "synthetic-app"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.queue.mu.Lock()
		delete(d.queue.active, 42)
		d.queue.mu.Unlock()
		_ = d.Shutdown(context.Background())
	})
	if reason := d.ManagedPathQueueBlockReason(); reason != "" {
		t.Fatalf("idle queue is blocked: %s", reason)
	}
	d.queue.mu.Lock()
	d.queue.active[42] = func() {}
	d.queue.mu.Unlock()
	if reason := d.ManagedPathQueueBlockReason(); !strings.Contains(reason, "下载任务正在运行") {
		t.Fatalf("active download not observed: %s", reason)
	}
	d.queue.mu.Lock()
	maintenance := d.queue.maintenance
	delete(d.queue.active, 42)
	d.queue.mu.Unlock()
	if maintenance {
		t.Fatal("read-only queue observation acquired maintenance")
	}
	if reason := d.ManagedPathQueueBlockReason(); reason != "" {
		t.Fatalf("released active queue remains blocked: %s", reason)
	}
}

func TestManagedPathsRejectPublicationBetweenPathReadAndMaintenanceAdmission(t *testing.T) {
	root := t.TempDir()
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "initial"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}, baiduapi.NewClient("synthetic-token", "synthetic-app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	expected := d.GetManagedPaths()
	changed := ManagedPaths{QueueDir: "/published", LocalDownDir: filepath.Join(root, "published")}
	d.queue.mu.Lock()
	d.queue.maintenance = true
	d.queue.mu.Unlock()
	d.managedPathsMaintenanceHook = func() {
		// A previous migration publishes paths before it releases queue maintenance.
		d.pathMu.Lock()
		d.queueDir, d.localDownDir = changed.QueueDir, changed.LocalDownDir
		d.pathMu.Unlock()
		d.EndMaintenance()
	}
	target := ManagedPaths{QueueDir: "/target", LocalDownDir: filepath.Join(root, "target")}
	_, err = d.SetManagedPathsConfirmed(context.Background(), target, false, "", expected)
	if !errors.Is(err, ErrManagedPathsChanged) {
		t.Fatalf("migration publication was not rejected: %v", err)
	}
	if d.GetManagedPaths() != changed {
		t.Fatal("stale source snapshot replaced the published paths")
	}
	if _, statErr := os.Stat(target.LocalDownDir); !os.IsNotExist(statErr) {
		t.Fatalf("stale source created a target: %v", statErr)
	}
	if reason := d.ManagedPathQueueBlockReason(); reason != "" {
		t.Fatalf("rejected write retained maintenance: %s", reason)
	}
}

func TestSetManagedPathsIgnoresExpiredPreviewButBlocksValidPreview(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())

	id := strings.Repeat("a", 32)
	d.intakeMutex.Lock()
	d.intakes[id] = intakeRecord{Version: 1, Boot: d.intakeBoot, Epoch: d.authEpoch, Intake: Intake{ID: id, State: "preview", Expires: time.Now().Add(time.Minute)}}
	d.intakeMutex.Unlock()
	want := ManagedPaths{QueueDir: "/new-queue", LocalDownDir: filepath.Join(root, "new-downloads")}
	if _, err = d.SetManagedPaths(context.Background(), want, false); err == nil {
		t.Fatal("a currently valid preview must block managed path changes")
	}

	d.intakeMutex.Lock()
	record := d.intakes[id]
	record.Expires = time.Now().Add(-time.Second)
	d.intakes[id] = record
	d.intakeMutex.Unlock()
	if _, err = d.SetManagedPaths(context.Background(), want, false); err != nil {
		t.Fatalf("an expired preview should not block managed path changes: %v", err)
	}
	if got := d.GetManagedPaths(); got.QueueDir != want.QueueDir || !sameLocalPath(got.LocalDownDir, want.LocalDownDir) {
		t.Fatalf("managed paths were not updated after expiring the preview: %+v", got)
	}
}

func TestSetManagedPathsWaitsForIntakeOperation(t *testing.T) {
	d, _, _, _, _ := intakeFixture(t)
	base := http.DefaultTransport
	entered, release := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") == "list" {
			enteredOnce.Do(func() { close(entered) })
			<-release
		}
		return base.RoundTrip(r)
	})
	defer func() {
		releaseOnce.Do(func() { close(release) })
		http.DefaultTransport = base
	}()

	previewDone := make(chan error, 1)
	go func() {
		_, err := d.PreviewIntake(context.Background(), []string{"/source/a.txt"})
		previewDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("intake operation did not reach the controlled provider response")
	}

	pathDone := make(chan error, 1)
	want := ManagedPaths{QueueDir: "/new-queue", LocalDownDir: filepath.Join(t.TempDir(), "new-downloads")}
	go func() {
		_, err := d.SetManagedPaths(context.Background(), want, false)
		pathDone <- err
	}()
	select {
	case err := <-pathDone:
		t.Fatalf("path update crossed an in-flight intake operation: %v", err)
	case <-time.After(80 * time.Millisecond):
	}

	releaseOnce.Do(func() { close(release) })
	if err := <-previewDone; err != nil {
		t.Fatalf("controlled preview failed: %v", err)
	}
	if err := <-pathDone; err == nil {
		t.Fatal("path update should recheck the now-valid preview and remain blocked")
	}
	if got := d.GetManagedPaths().QueueDir; got != "/queue" {
		t.Fatalf("path update changed the queue while preserving a valid preview: %s", got)
	}
}

func TestSetManagedPathsReadsCurrentPathAfterAdmission(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())

	middle := filepath.Join(root, "intermediate-downloads")
	if err = os.MkdirAll(middle, 0755); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	d.managedPathsAdmissionHook = func() {
		close(entered)
		<-release
	}
	type migrationOutcome struct {
		migration taskstore.LocalDirectoryMigration
		err       error
	}
	result := make(chan migrationOutcome, 1)
	want := ManagedPaths{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "final-downloads")}
	go func() {
		migration, setErr := d.SetManagedPaths(context.Background(), want, true)
		result <- migrationOutcome{migration: migration, err: setErr}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("path update did not reach the controlled post-admission point")
	}
	d.pathMu.Lock()
	d.localDownDir = middle
	d.pathMu.Unlock()
	releaseOnce.Do(func() { close(release) })
	outcome := <-result
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	if !sameLocalPath(outcome.migration.SourcePath, middle) {
		t.Fatalf("migration used a pre-admission source path: got %q want %q", outcome.migration.SourcePath, middle)
	}
}

func TestManagedPathSourcesFollowSavedAndEnvironmentPrecedence(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/default", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	first := d.GetManagedPathSnapshot()
	if first.QueueSource != "startup" || first.LocalDownloadSource != "startup" {
		t.Fatalf("startup sources: %#v", first)
	}
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/saved", LocalDownDir: filepath.Join(root, "saved")}, false); err != nil {
		t.Fatal(err)
	}
	saved := d.GetManagedPathSnapshot()
	if saved.QueueSource != "saved" || saved.LocalDownloadSource != "saved" {
		t.Fatalf("saved sources: %#v", saved)
	}
	if err = d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, explicit := range []bool{false, true} {
		next := *cfg
		next.QueueDirExplicit = explicit
		reopened, e := OpenDownloader(&next, baiduapi.NewClient("token", "app"))
		if e != nil {
			t.Fatal(e)
		}
		got := reopened.GetManagedPathSnapshot()
		want := "saved"
		if explicit {
			want = "environment"
		}
		if got.QueueSource != want || got.LocalDownloadSource != "saved" {
			t.Fatalf("explicit=%v snapshot=%#v", explicit, got)
		}
		if explicit && got.QueueDir != "/default" || !explicit && got.QueueDir != "/saved" {
			t.Fatalf("wrong effective path: %#v", got)
		}
		if e = reopened.Shutdown(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
}

func TestManagedPathSourcesDoNotInferMissingSavedFields(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/default", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	if err = d.queue.store.SaveSettings([]byte(`{"managed_queue_dir":"/saved-only"}`)); err != nil {
		t.Fatal(err)
	}
	if err = d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, explicit := range []bool{false, true} {
		next := *cfg
		next.LocalDownDirExplicit = explicit
		reopened, e := OpenDownloader(&next, baiduapi.NewClient("token", "app"))
		if e != nil {
			t.Fatal(e)
		}
		got := reopened.GetManagedPathSnapshot()
		want := "startup"
		if explicit {
			want = "environment"
		}
		if got.QueueSource != "saved" || got.QueueDir != "/saved-only" || got.LocalDownloadSource != want {
			t.Fatalf("missing-field source inferred: %#v", got)
		}
		if e = reopened.Shutdown(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
}
