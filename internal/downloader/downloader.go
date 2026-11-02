package downloader

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type Task struct {
	TransferStats     *TransferStatistics
	FileInfo          baiduapi.FileInfo
	Status            string // pending, downloading, done, failed
	LocalPath         string
	Error             string
	StartTime         time.Time
	FinishTime        time.Time
	Progress          int64
	LastProgress      int64
	LastProgressTime  time.Time
	Speed             int64 // bytes/sec
	ResumeSupported   bool
	MD5               string // provider metadata only; not assumed to be a plain content MD5
	Attempts          int
	NextAttempt       time.Time
	Root              bool
	DirectoryPrepared bool // current attempt finished enumerating and creating its local tree
	DownloadMode      DownloadMode
	TaskID            string
	CanCancel         bool
	CanPause          bool
	CanResume         bool
	CanRetry          bool
	LocalStorage      *LocalStorageEvidence `json:"local_storage,omitempty"`
}

type TaskPage struct {
	Tasks      []*Task
	Total      int
	QueueTotal int
	Counts     map[string]int
	Offset     int
	Limit      int
	HasMore    bool
}

// keep retains the cloud item and marks its name; delete removes it only after
// all local files have been published successfully.
type DownloadMode string

const (
	DownloadModeKeep   DownloadMode = "keep"
	DownloadModeDelete DownloadMode = "delete"
)

type Downloader struct {
	ctx                    context.Context
	cancel                 context.CancelFunc
	queue                  *scheduler
	cfg                    *config.Config
	client                 *baiduapi.Client
	clientMutex            sync.RWMutex
	authManaged            bool
	authReady              bool
	authExpiry             time.Time
	authEpoch              uint64
	onRejected             func(string)
	tasks                  map[int64]*Task
	taskMutex              sync.RWMutex
	scanMutex              sync.Mutex
	cloudMutex             sync.RWMutex
	pathOperations         sync.RWMutex
	intakeMutex            sync.RWMutex
	intakeBoot             string
	intakeJournal          intakeJournal
	intakes                map[string]intakeRecord
	inFlight               map[int64]bool
	transferSlots          chan struct{}
	localSpaceProbe        func(string) (int64, error)
	localSpaceMu           sync.Mutex
	localSpaceReservations map[int64]int64
	modeMutex              sync.RWMutex
	downloadMode           DownloadMode
	pathMu                 sync.RWMutex
	queueDir               string
	localDownDir           string
	settingsMu             sync.Mutex
	localMigrationMu       sync.Mutex
	localMigrationRunning  bool
	localMigrationPending  bool
	localMigrationShutdown bool
	localMigrationWG       sync.WaitGroup
	localMigrationComplete func()
	inventoryScanMutex     sync.Mutex
	inventoryScanCancel    context.CancelFunc
	inventoryScanStarting  bool
	inventoryScanKind      string
	// inventoryPageCommitHook synchronizes the read/commit race regression test.
	inventoryPageCommitHook func(string)
	// inventoryScanAdmissionHook synchronizes path-maintenance admission regression tests.
	inventoryScanAdmissionHook func()
	// managedPathsAdmissionHook and cloudMutationAdmissionHook synchronize path snapshot race regression tests.
	managedPathsAdmissionHook  func()
	cloudMutationAdmissionHook func()
	// localMigrationCopyStartHook synchronizes migration progress drift regression tests.
	localMigrationCopyStartHook func()
	localMigrationRemoveAllHook func(string) error
	duplicateScanMutex          sync.Mutex
	duplicateScanCancel         context.CancelFunc
	duplicateScanStarting       bool
	cloudAPIPacer               *cloudAPIPacer
	duplicateScanRetryBase      time.Duration
	taskCatalogGeneration       atomic.Uint64
	taskStatsGeneration         atomic.Uint64
}

func NewDownloader(cfg *config.Config, client *baiduapi.Client) *Downloader {
	ctx, cancel := context.WithCancel(context.Background())
	parallel := cfg.MaxParallel
	if parallel < 1 {
		parallel = 2
	}
	pacer := newCloudAPIPacer(cloudAPIRequestInterval)
	d := &Downloader{cfg: cfg, client: client.WithRequestPacer(pacer), cloudAPIPacer: pacer, duplicateScanRetryBase: 5 * time.Second, tasks: make(map[int64]*Task), inFlight: make(map[int64]bool), transferSlots: make(chan struct{}, parallel), localSpaceProbe: localAvailableBytes, localSpaceReservations: make(map[int64]int64), ctx: ctx, cancel: cancel, intakes: make(map[string]intakeRecord), queueDir: cfg.QueueDir, localDownDir: cfg.LocalDownDir}
	d.queue = newScheduler(d)
	return d
}
func (d *Downloader) UpdateClient(client *baiduapi.Client) {
	d.clientMutex.Lock()
	defer d.clientMutex.Unlock()
	d.client = client.WithRequestPacer(d.cloudAPIPacer)
}
func (d *Downloader) getClient() *baiduapi.Client {
	d.clientMutex.RLock()
	defer d.clientMutex.RUnlock()
	return d.client
}
func (d *Downloader) SetDownloadMode(mode DownloadMode) {
	if mode != DownloadModeKeep && mode != DownloadModeDelete {
		return
	}
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || q.err != nil {
		return
	}
	if q.store != nil {
		if err := d.saveAppSettings(map[string]any{"download_mode": mode}); err != nil {
			q.err = fmt.Errorf("设置保存失败: %w", err)
			d.cancel()
			return
		}
	}
	d.modeMutex.Lock()
	defer d.modeMutex.Unlock()
	d.downloadMode = mode
}
func (d *Downloader) GetDownloadMode() DownloadMode {
	d.modeMutex.RLock()
	defer d.modeMutex.RUnlock()
	if d.downloadMode == "" {
		return DownloadModeKeep
	}
	return d.downloadMode
}

func (d *Downloader) ScanAndDownload() error {
	if !d.scanMutex.TryLock() {
		return ErrQueueMonitorScanSkipped
	}
	defer d.scanMutex.Unlock()
	d.queue.mu.Lock()
	paused := d.queue.maintenance
	d.queue.mu.Unlock()
	if paused {
		return ErrQueueMonitorScanSkipped
	}
	d.cloudMutex.RLock()
	defer d.cloudMutex.RUnlock()
	if err := d.ctx.Err(); err != nil {
		return err
	}
	client, epoch, err := d.authorizedClient(d.ctx)
	if err != nil {
		return err
	}
	runID, err := newQueueMonitorScanID()
	if err != nil {
		return err
	}
	startedAt := time.Now().UTC()
	account := client.Fingerprint()
	store := d.queue.store
	if store != nil {
		boundAccount, bindErr := store.BoundAccountFingerprint()
		if bindErr == nil {
			account = boundAccount
		} else if !errors.Is(bindErr, taskstore.ErrAccountNotBound) {
			return fmt.Errorf("读取百度账号绑定失败: %w", bindErr)
		}
		retryState, retryErr := store.QueueMonitorRetryState(account)
		if retryErr != nil {
			return fmt.Errorf("读取自动扫描重试状态失败: %w", retryErr)
		}
		if retryState != nil && time.Now().UTC().Before(retryState.RetryAtUTC) {
			return ErrQueueMonitorScanSkipped
		}
		if err = store.StartQueueMonitorScan(account, runID, d.QueueDirectory(), startedAt); err != nil {
			return fmt.Errorf("记录自动扫描开始失败: %w", err)
		}
	}
	files, err := client.ListDir(d.QueueDirectory())
	if err != nil {
		d.reportAuthenticationFailure(err)
		code := "provider_error"
		if errors.Is(err, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) {
			code = "authentication_wait"
		}
		var finishErr error
		if store != nil {
			if code == "provider_error" && baiduapi.IsRetryable(err) {
				prior, stateErr := store.QueueMonitorRetryState(account)
				if stateErr != nil {
					finishErr = stateErr
				} else {
					failures := 1
					if prior != nil {
						failures += prior.FailureCount
					}
					delay := queueMonitorRetryDelay(failures, providerRetryAfter(err))
					_, finishErr = store.ScheduleQueueMonitorRetry(account, code, failures, delay, time.Now().UTC())
				}
			} else {
				finishErr = store.ClearQueueMonitorRetryState(account)
			}
			finishErr = errors.Join(finishErr, store.FinishQueueMonitorScan(account, runID, "failed", code, time.Now().UTC()))
		}
		return errors.Join(fmt.Errorf("列目录失败: %w", err), finishErr)
	}
	observedAt := time.Now().UTC()
	if store != nil {
		if err = store.ClearQueueMonitorRetryState(account); err != nil {
			return fmt.Errorf("清除自动扫描退避状态失败: %w", err)
		}
		if err = store.RecordQueueMonitorCloudListed(account, runID, observedAt); err != nil {
			return fmt.Errorf("记录百度目录观察时间失败: %w", err)
		}
	}
	err = d.queue.reconcileObserved(files, epoch, observedAt)
	if err != nil {
		var finishErr error
		if store != nil {
			finishErr = store.FinishQueueMonitorScan(account, runID, "failed", "queue_error", time.Now().UTC())
		}
		return errors.Join(err, finishErr)
	}
	if store != nil {
		if err = store.FinishQueueMonitorScan(account, runID, "success", "", time.Now().UTC()); err != nil {
			return fmt.Errorf("记录自动扫描完成失败: %w", err)
		}
	}
	return nil
}

func (d *Downloader) beginTask(info baiduapi.FileInfo, local string) {
	info = shareFileInfoStrings(info)
	d.taskMutex.Lock()
	defer d.taskMutex.Unlock()
	task := &Task{FileInfo: info, LocalPath: local, Status: "downloading", StartTime: time.Now(), LastProgressTime: time.Now(), ResumeSupported: true, MD5: info.MD5}
	catalogChanged, statsChanged := true, true
	if old := d.tasks[info.FsID]; old != nil {
		task.DirectoryPrepared = old.DirectoryPrepared
		catalogChanged = !old.Root || old.FileInfo != info
		statsChanged = !old.Root && old.Status != task.Status
		task.Attempts = old.Attempts
		task.NextAttempt = old.NextAttempt
		task.Root = old.Root
		task.DownloadMode = old.DownloadMode
	}
	d.tasks[info.FsID] = task
	if catalogChanged {
		d.taskCatalogGeneration.Add(1)
	}
	if statsChanged {
		d.taskStatsGeneration.Add(1)
	}
}
func (d *Downloader) updateTaskStatus(id int64, status, message string) {
	d.taskMutex.Lock()
	defer d.taskMutex.Unlock()
	if task := d.tasks[id]; task != nil {
		// Root completion belongs to the durable scheduler, including source
		// processing. Local publication alone must not briefly display done.
		if d.inFlight[id] {
			return
		}
		if task.Status != status {
			d.taskStatsGeneration.Add(1)
		}
		task.Status = status
		task.Error = errorSummary(message)
		if status == "done" || status == "failed" {
			task.FinishTime = time.Now()
			task.Speed = 0
		}
	}
}
func (d *Downloader) updateProgress(id, progress int64) {
	d.taskMutex.Lock()
	defer d.taskMutex.Unlock()
	if task := d.tasks[id]; task != nil {
		task.Progress = progress
		if elapsed := time.Since(task.LastProgressTime).Seconds(); elapsed >= 1 {
			task.Speed = int64(float64(progress-task.LastProgress) / elapsed)
			task.LastProgress = progress
			task.LastProgressTime = time.Now()
		}
	}
}
func (d *Downloader) finish(id int64, err error) {
	if err != nil {
		d.updateTaskStatus(id, "failed", err.Error())
	} else {
		d.updateTaskStatus(id, "done", "")
	}
}

func (d *Downloader) downloadFile(info baiduapi.FileInfo) {
	d.finish(info.FsID, d.runRoot(d.ctx, info, d.GetDownloadMode()))
}

func (d *Downloader) downloadFolder(info baiduapi.FileInfo) {
	d.finish(info.FsID, d.runRoot(d.ctx, info, d.GetDownloadMode()))
}

func (d *Downloader) runRoot(ctx context.Context, info baiduapi.FileInfo, mode DownloadMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validName(info.ServerFilename) {
		d.beginTask(info, "")
		return fmt.Errorf("invalid cloud filename %q", info.ServerFilename)
	}
	if info.Path != path.Join(d.QueueDirectory(), info.ServerFilename) {
		d.beginTask(info, "")
		return fmt.Errorf("cloud root path does not match queue: %s", info.Path)
	}
	var err error
	if info.IsDir == 1 {
		err = d.downloadLocalFolder(ctx, info, info.ServerFilename)
	} else {
		err = d.downloadLocalFile(ctx, info, info.ServerFilename)
	}
	// Children are never renamed individually: keeping their cloud paths stable
	// allows an incomplete folder to be retried without losing failed descendants.
	if err == nil {
		err = d.postprocess(ctx, info, mode)
	}
	return err
}

func (d *Downloader) downloadPreparedFolder(ctx context.Context, info baiduapi.FileInfo, relative string, lists map[string][]baiduapi.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	local, err := d.localPath(relative)
	d.beginTask(info, local)
	if err == nil {
		err = ensureDirectory(local)
	}
	if err != nil {
		d.finish(info.FsID, err)
		return err
	}
	children, exists := lists[info.Path]
	if !exists {
		return fmt.Errorf("本地目录准备记录缺失")
	}
	var failures []error
	files := make([]baiduapi.FileInfo, 0, len(children))
	directories := make([]baiduapi.FileInfo, 0)
	for _, child := range children {
		if err := ctx.Err(); err != nil {
			d.finish(info.FsID, err)
			return err
		}
		// Old markers do not prove that this NAS still has the child.
		if strings.HasSuffix(child.ServerFilename, ".可删除") {
			failures = append(failures, fmt.Errorf("%s: 发现旧版标记，无法验证本地文件；请移出队列目录或恢复原名后重试", child.Path))
			continue
		}
		if !validName(child.ServerFilename) {
			failures = append(failures, fmt.Errorf("%s: invalid cloud filename %q", child.Path, child.ServerFilename))
			continue
		}
		if child.Path != path.Join(info.Path, child.ServerFilename) {
			failures = append(failures, fmt.Errorf("%s: cloud child path does not match directory", child.Path))
			continue
		}
		if child.IsDir == 1 {
			directories = append(directories, child)
		} else {
			files = append(files, child)
		}
	}
	// Direct files use bounded workers. The shared transfer semaphore also
	// covers files from other roots, so the configured limit is downloader-wide.
	workers := cap(d.transferSlots)
	if workers > len(files) {
		workers = len(files)
	}
	// Reserve slots before starting goroutines. Otherwise every active root
	// could create MaxParallel workers that block inside downloadLocalFile.
	reserved := 0
	for i := 0; i < workers; i++ {
		select {
		case d.transferSlots <- struct{}{}:
			reserved++
		default:
			i = workers
		}
	}
	if reserved == 0 {
		for _, child := range files {
			childErr := d.downloadLocalFile(ctx, child, filepath.Join(relative, child.ServerFilename))
			if errors.Is(childErr, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(childErr) {
				d.finish(info.FsID, childErr)
				return childErr
			}
			if err := ctx.Err(); err != nil {
				d.finish(info.FsID, err)
				return err
			}
			if childErr != nil {
				failures = append(failures, fmt.Errorf("%s: %w", child.Path, childErr))
			}
		}
	} else {
		childCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		type fileJob struct {
			index int
			info  baiduapi.FileInfo
		}
		jobs := make(chan fileJob)
		results := make([]error, len(files))
		var authErr error
		var authOnce sync.Once
		var wg sync.WaitGroup
		for i := 0; i < reserved; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-d.transferSlots }()
				for job := range jobs {
					if childCtx.Err() != nil {
						continue
					}
					childRelative := filepath.Join(relative, job.info.ServerFilename)
					err := d.downloadLocalFileWithSlot(childCtx, job.info, childRelative)
					if errors.Is(err, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) {
						authOnce.Do(func() { authErr = err; cancel() })
						continue
					}
					results[job.index] = err
				}
			}()
		}
	feed:
		for i, child := range files {
			select {
			case jobs <- fileJob{index: i, info: child}:
			case <-childCtx.Done():
				break feed
			}
		}
		close(jobs)
		wg.Wait()
		if authErr != nil {
			d.finish(info.FsID, authErr)
			return authErr
		}
		if err := ctx.Err(); err != nil {
			d.finish(info.FsID, err)
			return err
		}
		for i, err := range results {
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", files[i].Path, err))
			}
		}
	}
	for _, child := range directories {
		if err = d.downloadPreparedFolder(ctx, child, filepath.Join(relative, child.ServerFilename), lists); err != nil {
			if errors.Is(err, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) {
				d.finish(info.FsID, err)
				return err
			}
			failures = append(failures, fmt.Errorf("%s: %w", child.Path, err))
		}
	}
	if len(failures) > 0 {
		err = fmt.Errorf("目录有 %d 个失败项目: %w", len(failures), errors.Join(failures...))
	} else {
		err = nil
	}
	d.finish(info.FsID, err)
	return err
}

func (d *Downloader) downloadLocalFile(ctx context.Context, info baiduapi.FileInfo, relative string) error {
	return d.downloadLocalFileWithSlotHeld(ctx, info, relative, false)
}

// downloadLocalFileWithSlot uses a permit already reserved by a directory
// worker; ordinary roots acquire a slot only for the duration of one transfer.
func (d *Downloader) downloadLocalFileWithSlot(ctx context.Context, info baiduapi.FileInfo, relative string) error {
	return d.downloadLocalFileWithSlotHeld(ctx, info, relative, true)
}

func (d *Downloader) downloadLocalFileWithSlotHeld(ctx context.Context, info baiduapi.FileInfo, relative string, slotHeld bool) error {
	local, err := d.localPath(relative)
	d.beginTask(info, local)
	if err == nil {
		if !slotHeld {
			select {
			case d.transferSlots <- struct{}{}:
				defer func() { <-d.transferSlots }()
			case <-ctx.Done():
				err = ctx.Err()
			}
		}
		if err == nil {
			err = d.transfer(ctx, info, relative, local)
		}
	}
	d.finish(info.FsID, err)
	return err
}

func (d *Downloader) postprocess(ctx context.Context, info baiduapi.FileInfo, mode DownloadMode) error {
	d.cloudMutex.RLock()
	defer d.cloudMutex.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := d.sourceOperationClient(ctx, info)
	if err != nil {
		return err
	}
	tracked, err := d.beginSourceOperation(ctx, info)
	if err != nil {
		return err
	}
	// Saving the journal can overlap cancellation or credential changes.
	// Recheck before entering the HTTP client; no request has been sent yet.
	client, err = d.sourceOperationClient(ctx, info)
	if err != nil {
		if tracked {
			return d.finishSourceOperation(info, err)
		}
		return err
	}
	if tracked {
		current, verifyErr := client.GetFileInfo(info.FsID)
		verifyObservedAt := time.Now().UTC()
		if errors.Is(verifyErr, baiduapi.ErrFileMetadataMissing) {
			verifyErr = &sourceIdentityChangedError{observedAt: verifyObservedAt, missing: true}
		} else if verifyErr == nil && current != info {
			observed := current
			verifyErr = &sourceIdentityChangedError{observed: &observed, observedAt: verifyObservedAt}
		}
		if verifyErr != nil {
			return d.finishSourceOperation(info, verifyErr)
		}
	}
	if mode == DownloadModeDelete {
		err = client.DeleteFile(info.Path)
	} else {
		err = client.RenameFile(info.Path, path.Base(info.Path)+".可删除")
	}
	if tracked {
		err = d.finishSourceOperation(info, err)
	}
	if errors.Is(err, ErrSourceOperationUnknown) || errors.Is(err, baiduapi.ErrSourceOperationUnconfirmed) {
		return fmt.Errorf("%w: %w", ErrSourceOperationUnknown, err)
	}
	if err != nil {
		return fmt.Errorf("本地已完成，网盘后处理待重试: %w", err)
	}
	return nil
}

// Copy under the lock so serialization never reads live download progress.
func (d *Downloader) GetTasks() []*Task {
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()
	result := make([]*Task, 0, len(d.queue.jobs)+len(d.tasks))
	for id, job := range d.queue.jobs {
		result = append(result, d.taskWithActionsLocked(id, d.taskViewLocked(id, job)))
	}
	for id := range d.tasks {
		if d.queue.jobs[id] == nil {
			result = append(result, d.taskWithActionsLocked(id, d.taskViewLocked(id, nil)))
		}
	}
	return result
}

// taskViewLocked derives inactive display data from the durable job and copies
// Task only when it contains live progress that has not yet been persisted.
// The caller holds queue.mu and taskMutex (read or write).
func (d *Downloader) taskViewLocked(id int64, job *queueJob) *Task {
	if task := d.tasks[id]; task != nil {
		copy := *task
		if job != nil {
			copy.TransferStats = copyTransferStatistics(job.TransferStats)
		}
		return &copy
	}
	if job == nil {
		return nil
	}
	task := &Task{
		TransferStats: copyTransferStatistics(job.TransferStats),
		FileInfo:      job.Info, Status: job.Status, Error: job.Error,
		StartTime: job.Created, Attempts: job.Attempts, NextAttempt: job.NextAttempt,
		Root: true, DownloadMode: job.Mode, LocalStorage: job.LocalStorage,
	}
	if job.Status == "done" || job.Status == "failed" || job.Status == "cancelled" {
		task.FinishTime = job.Updated
	}
	if job.Status == "done" && job.Info.IsDir == 0 {
		task.Progress = job.Info.Size
	}
	return task
}

// GetTaskPage selects a bounded window from the globally sorted task list while
// counting all statuses. Only tasks returned to the caller are copied.
func (d *Downloader) GetTaskPage(offset, limit int, sortBy string) TaskPage {
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if sortBy != "progress" && sortBy != "size" {
		sortBy = "name"
	}

	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()

	total, counts := d.taskStatsLocked()
	page := TaskPage{Offset: offset, Limit: limit, Total: total, QueueTotal: total, Counts: counts}
	page.HasMore = offset < page.Total && limit < page.Total-offset
	if limit == 0 || offset >= page.Total || page.Total == 0 {
		return page
	}
	if sortBy == "name" || sortBy == "size" {
		ids := d.taskOrderIDsLocked(sortBy, page.Total)
		end := len(ids)
		if limit < end-offset {
			end = offset + limit
		}
		page.Tasks = make([]*Task, 0, end-offset)
		for _, id := range ids[offset:end] {
			candidate := taskCandidate{job: q.jobs[id], live: d.tasks[id]}
			page.Tasks = append(page.Tasks, d.taskWithActionsLocked(id, candidate.taskView(d)))
		}
		return page
	}
	if sortBy == "progress" {
		index := d.progressOrderIDsLocked(page.Total)
		active := make([]taskCandidate, 0, len(index.activeIDs))
		for _, id := range index.activeIDs {
			active = append(active, d.taskCandidateLocked(id))
		}
		sort.Slice(active, func(i, j int) bool {
			return taskCandidateComesBefore(active[i], active[j], "progress")
		})
		wanted := page.Total
		if limit <= page.Total-offset {
			wanted = offset + limit
		}
		selected := make([]taskCandidate, 0, wanted)
		stableIndex, activeIndex := 0, 0
		for len(selected) < wanted && (stableIndex < len(index.ids) || activeIndex < len(active)) {
			if activeIndex < len(active) && (stableIndex == len(index.ids) || taskCandidateComesBefore(active[activeIndex], d.taskCandidateLocked(index.ids[stableIndex]), "progress")) {
				selected = append(selected, active[activeIndex])
				activeIndex++
			} else {
				selected = append(selected, d.taskCandidateLocked(index.ids[stableIndex]))
				stableIndex++
			}
		}
		start := offset
		if start > len(selected) {
			start = len(selected)
		}
		end := len(selected)
		if limit < len(selected)-start {
			end = start + limit
		}
		page.Tasks = make([]*Task, 0, end-start)
		for _, candidate := range selected[start:end] {
			page.Tasks = append(page.Tasks, d.taskWithActionsLocked(candidate.info().FsID, candidate.taskView(d)))
		}
		return page
	}

	wanted := page.Total
	if offset < page.Total && limit <= page.Total-offset {
		wanted = offset + limit
	}
	selected := make([]taskCandidate, 0, wanted)
	for id, job := range q.jobs {
		candidate := taskCandidate{job: job, live: d.tasks[id]}
		if len(selected) < wanted {
			selected = append(selected, candidate)
			siftTaskCandidatesUp(selected, len(selected)-1, sortBy)
			continue
		}
		if wanted > 0 && taskCandidateComesBefore(candidate, selected[0], sortBy) {
			selected[0] = candidate
			siftTaskCandidatesDown(selected, 0, sortBy)
		}
	}
	for id, task := range d.tasks {
		if q.jobs[id] != nil {
			continue
		}
		candidate := taskCandidate{live: task}
		if len(selected) < wanted {
			selected = append(selected, candidate)
			siftTaskCandidatesUp(selected, len(selected)-1, sortBy)
		} else if wanted > 0 && taskCandidateComesBefore(candidate, selected[0], sortBy) {
			selected[0] = candidate
			siftTaskCandidatesDown(selected, 0, sortBy)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return taskCandidateComesBefore(selected[i], selected[j], sortBy) })

	start := offset
	if start > len(selected) {
		start = len(selected)
	}
	end := len(selected)
	if limit < len(selected)-start {
		end = start + limit
	}
	page.Tasks = make([]*Task, 0, end-start)
	for _, candidate := range selected[start:end] {
		page.Tasks = append(page.Tasks, d.taskWithActionsLocked(candidate.info().FsID, candidate.taskView(d)))
	}
	return page
}

type taskOrderKey struct {
	id   int64
	name string
	size int64
}

// taskOrderIDsLocked lazily sorts stable task fields and caches only FsIDs.
// The caller holds queue.mu and taskMutex, so the IDs and their keys are a
// coherent snapshot and page views can be read safely after the sort.
func (d *Downloader) taskOrderIDsLocked(sortBy string, capacity int) []int64 {
	q := d.queue
	index := &q.nameIndex
	if sortBy == "size" {
		index = &q.sizeIndex
	}
	generation := d.taskCatalogGeneration.Load()
	if index.ready && index.generation == generation {
		return index.ids
	}
	keys := make([]taskOrderKey, 0, capacity)
	appendKey := func(id int64, info *baiduapi.FileInfo) {
		name := info.Path
		if name == "" {
			name = info.ServerFilename
		}
		keys = append(keys, taskOrderKey{id: id, name: strings.ToLower(name), size: info.Size})
	}
	for id, job := range q.jobs {
		appendKey(id, &job.Info)
	}
	for id, task := range d.tasks {
		if q.jobs[id] == nil {
			appendKey(id, &task.FileInfo)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if sortBy == "size" && a.size != b.size {
			return a.size > b.size
		}
		if sortBy == "name" && a.name != b.name {
			return a.name < b.name
		}
		return a.id < b.id
	})
	ids := make([]int64, len(keys))
	for i, key := range keys {
		ids[i] = key.id
	}
	index.ids = ids
	index.generation = generation
	index.ready = true
	return index.ids
}

// progressOrderIDsLocked caches only tasks whose progress cannot change until
// taskStatsGeneration advances. Live downloads are merged separately per page.
func (d *Downloader) progressOrderIDsLocked(capacity int) *progressTaskIndex {
	index := &d.queue.progressIndex
	generation := d.taskStatsGeneration.Load()
	if index.ready && index.generation == generation {
		return index
	}
	type progressEntry struct {
		id    int64
		fsid  int64
		ratio float64
	}
	entries := make([]progressEntry, 0, capacity)
	activeIDs := make([]int64, 0, len(d.tasks))
	for id := range d.queue.jobs {
		if task := d.tasks[id]; task != nil && task.Status == "downloading" {
			activeIDs = append(activeIDs, id)
			continue
		}
		candidate := d.taskCandidateLocked(id)
		entries = append(entries, progressEntry{id: id, fsid: candidate.info().FsID, ratio: candidate.progressRatio(candidate.info())})
	}
	for id, task := range d.tasks {
		if d.queue.jobs[id] != nil {
			continue
		}
		if task.Status == "downloading" {
			activeIDs = append(activeIDs, id)
		} else {
			candidate := taskCandidate{live: task}
			entries = append(entries, progressEntry{id: id, fsid: candidate.info().FsID, ratio: candidate.progressRatio(candidate.info())})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ratio != entries[j].ratio {
			return entries[i].ratio > entries[j].ratio
		}
		return entries[i].fsid < entries[j].fsid
	})
	ids := make([]int64, len(entries))
	for i := range entries {
		ids[i] = entries[i].id
	}
	index.ids = ids
	index.activeIDs = activeIDs
	index.generation = generation
	index.ready = true
	return index
}

func (d *Downloader) taskCandidateLocked(id int64) taskCandidate {
	if job := d.queue.jobs[id]; job != nil {
		return taskCandidate{job: job, live: d.tasks[id]}
	}
	return taskCandidate{live: d.tasks[id]}
}

type taskCandidate struct {
	job  *queueJob
	live *Task
}

func (c taskCandidate) info() *baiduapi.FileInfo {
	if c.job != nil {
		return &c.job.Info
	}
	return &c.live.FileInfo
}

func (c taskCandidate) progressRatio(info *baiduapi.FileInfo) float64 {
	if info.Size <= 0 {
		return 0
	}
	if c.live != nil {
		return float64(c.live.Progress) / float64(info.Size)
	}
	// Inactive history has a fixed ratio: done files are full and other jobs are zero.
	if c.job != nil && c.job.Status == "done" && info.IsDir == 0 {
		return 1
	}
	return 0
}

func (c taskCandidate) taskView(d *Downloader) *Task {
	if c.job != nil {
		return d.taskViewLocked(c.job.Info.FsID, c.job)
	}
	copy := *c.live
	return &copy
}

func taskCandidateComesBefore(a, b taskCandidate, sortBy string) bool {
	aInfo, bInfo := a.info(), b.info()
	if sortBy == "size" && aInfo.Size != bInfo.Size {
		return aInfo.Size > bInfo.Size
	}
	if sortBy == "progress" {
		aProgress, bProgress := a.progressRatio(aInfo), b.progressRatio(bInfo)
		if aProgress != bProgress {
			return aProgress > bProgress
		}
	}
	if sortBy == "name" {
		aName, bName := aInfo.Path, bInfo.Path
		if aName == "" {
			aName = aInfo.ServerFilename
		}
		if bName == "" {
			bName = bInfo.ServerFilename
		}
		aName, bName = strings.ToLower(aName), strings.ToLower(bName)
		if aName != bName {
			return aName < bName
		}
	}
	return aInfo.FsID < bInfo.FsID
}

func siftTaskCandidatesUp(tasks []taskCandidate, child int, sortBy string) {
	for child > 0 {
		parent := (child - 1) / 2
		if !taskCandidateComesBefore(tasks[parent], tasks[child], sortBy) {
			break
		}
		tasks[parent], tasks[child] = tasks[child], tasks[parent]
		child = parent
	}
}

func siftTaskCandidatesDown(tasks []taskCandidate, root int, sortBy string) {
	for {
		left := root*2 + 1
		if left >= len(tasks) {
			return
		}
		worst := left
		right := left + 1
		if right < len(tasks) && taskCandidateComesBefore(tasks[worst], tasks[right], sortBy) {
			worst = right
		}
		if !taskCandidateComesBefore(tasks[root], tasks[worst], sortBy) {
			return
		}
		tasks[root], tasks[worst] = tasks[worst], tasks[root]
		root = worst
	}
}

func taskComesBefore(a, b *Task, sortBy string) bool {
	return taskCandidateComesBefore(taskCandidate{live: a}, taskCandidate{live: b}, sortBy)
}

// The bounded heap keeps the worst selected task candidate at index 0.

func (d *Downloader) taskWithActionsLocked(id int64, task *Task) *Task {
	// Action IDs remain exact in browsers, including values above 2^53.
	task.TaskID = strconv.FormatInt(task.FileInfo.FsID, 10)
	job := d.queue.jobs[id]
	if task.Root && job != nil && !d.queue.stopping && d.queue.err == nil && d.ctx.Err() == nil {
		task.CanCancel = job.Status == "pending" || job.Status == "downloading" || job.Status == "retry_wait" || job.Status == "auth_wait" || job.Status == "paused"
		task.CanPause = job.SourceState == "" && (job.Status == "pending" || job.Status == "retry_wait" || job.Status == "downloading")
		task.CanResume = job.SourceState == "" && job.Status == "paused" && d.queue.active[id] == nil
		eligibleInfo := d.queue.eligible[id]
		task.CanRetry = job.SourceState == "" && !d.queue.blockedSources[job.Info.Path] && !d.queue.blockedIntakes[job.Info.Path] && (job.Status == "failed" || job.Status == "cancelled") && d.queue.active[id] == nil && eligibleInfo != nil && *eligibleInfo == job.Info && d.AuthenticationReady()
	}
	return task
}

func (d *Downloader) GetTaskStats() (int, map[string]int) {
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()
	return d.taskStatsLocked()
}

// taskStatsLocked caches the whole-queue status summary while the queue and
// task locks are held in their established order. Return a copy so callers
// cannot mutate the cached counts.
func (d *Downloader) taskStatsLocked() (int, map[string]int) {
	q := d.queue
	generation := d.taskStatsGeneration.Load()
	if !q.statsCache.valid || q.statsCache.generation != generation {
		cache := taskStatsCache{generation: generation, total: len(q.jobs), counts: make(map[string]int), valid: true}
		for _, job := range q.jobs {
			cache.counts[job.Status]++
		}
		for id, task := range d.tasks {
			if q.jobs[id] == nil {
				cache.total++
				cache.counts[task.Status]++
			}
		}
		q.statsCache = cache
	}
	counts := make(map[string]int, len(q.statsCache.counts))
	for status, count := range q.statsCache.counts {
		counts[status] = count
	}
	return q.statsCache.total, counts
}
func (d *Downloader) GetTaskByFsID(id int64) *Task {
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()
	if job := d.queue.jobs[id]; job != nil {
		return d.taskViewLocked(id, job)
	}
	if task := d.tasks[id]; task != nil {
		copy := *task
		return &copy
	}
	return nil
}

func (d *Downloader) TaskEventPage(id, beforeSequence int64, limit int) ([]taskstore.Event, int64, bool, error) {
	d.queue.mu.Lock()
	store := d.queue.store
	d.queue.mu.Unlock()
	if store == nil {
		return nil, 0, false, errors.New("task event store unavailable")
	}
	return store.EventPage(id, beforeSequence, limit)
}

func (d *Downloader) TaskEventPageFiltered(ctx context.Context, id, beforeSequence int64, limit int, filter taskstore.TaskEventFilter) ([]taskstore.Event, int64, int64, bool, error) {
	d.queue.mu.Lock()
	store := d.queue.store
	d.queue.mu.Unlock()
	if store == nil {
		return nil, 0, 0, false, errors.New("task event store unavailable")
	}
	return store.EventPageFiltered(ctx, id, beforeSequence, limit, filter)
}

func (d *Downloader) TaskEventPageFilteredWithCounts(ctx context.Context, id, beforeSequence int64, limit int, filter taskstore.TaskEventFilter) ([]taskstore.Event, int64, int64, bool, []taskstore.TaskEventKindCount, error) {
	d.queue.mu.Lock()
	store := d.queue.store
	d.queue.mu.Unlock()
	if store == nil {
		return nil, 0, 0, false, nil, errors.New("task event store unavailable")
	}
	return store.EventPageFilteredWithCounts(ctx, id, beforeSequence, limit, filter)
}

func (d *Downloader) TaskEventHistoryPage(ctx context.Context, filter taskstore.GlobalTaskEventFilter, beforeSequence int64, asOfSequence *int64, limit int) (taskstore.TaskEventHistoryPage, error) {
	d.queue.mu.Lock()
	store := d.queue.store
	d.queue.mu.Unlock()
	if store == nil {
		return taskstore.TaskEventHistoryPage{}, errors.New("task event store unavailable")
	}
	return store.TaskEventHistoryPage(ctx, filter, beforeSequence, asOfSequence, limit)
}

func (d *Downloader) TaskEventHistoryExport(ctx context.Context, filter taskstore.GlobalTaskEventFilter, asOfSequence *int64) ([]taskstore.Event, int64, int64, error) {
	d.queue.mu.Lock()
	store := d.queue.store
	d.queue.mu.Unlock()
	if store == nil {
		return nil, 0, 0, errors.New("task event store unavailable")
	}
	return store.TaskEventHistoryExport(ctx, filter, asOfSequence)
}

func (d *Downloader) TaskEventContext(ctx context.Context, id, sequence int64) (taskstore.TaskEventContext, error) {
	d.queue.mu.Lock()
	store := d.queue.store
	d.queue.mu.Unlock()
	if store == nil {
		return taskstore.TaskEventContext{}, errors.New("task event store unavailable")
	}
	return store.TaskEventContext(ctx, id, sequence)
}

func (d *Downloader) TaskEventEvidenceExport(ctx context.Context, id int64) ([]taskstore.Event, error) {
	d.queue.mu.Lock()
	store := d.queue.store
	d.queue.mu.Unlock()
	if store == nil {
		return nil, errors.New("task event store unavailable")
	}
	return store.TaskEventEvidenceExport(ctx, id)
}

func (d *Downloader) TaskEventEvidenceExportFiltered(ctx context.Context, id int64, filter taskstore.TaskEventFilter) ([]taskstore.Event, int64, error) {
	d.queue.mu.Lock()
	store := d.queue.store
	d.queue.mu.Unlock()
	if store == nil {
		return nil, 0, errors.New("task event store unavailable")
	}
	return store.TaskEventEvidenceExportFiltered(ctx, id, filter)
}

func (d *Downloader) TaskStoreStartupDiagnostics() (taskstore.StartupDiagnostics, error) {
	d.queue.mu.Lock()
	store := d.queue.store
	d.queue.mu.Unlock()
	if store == nil {
		return taskstore.StartupDiagnostics{}, errors.New("task store unavailable")
	}
	return store.StartupDiagnostics()
}
