package downloader

import (
	"context"
	"encoding/json"
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
)

type Task struct {
	FileInfo         baiduapi.FileInfo
	Status           string // pending, downloading, done, failed
	LocalPath        string
	Error            string
	StartTime        time.Time
	FinishTime       time.Time
	Progress         int64
	LastProgress     int64
	LastProgressTime time.Time
	Speed            int64 // bytes/sec
	ResumeSupported  bool
	MD5              string // provider metadata only; not assumed to be a plain content MD5
	Attempts         int
	NextAttempt      time.Time
	Root             bool
	DownloadMode     DownloadMode
	TaskID           string
	CanCancel        bool
	CanRetry         bool
}

type TaskPage struct {
	Tasks   []*Task
	Total   int
	Counts  map[string]int
	Offset  int
	Limit   int
	HasMore bool
}

// keep retains the cloud item and marks its name; delete removes it only after
// all local files have been published successfully.
type DownloadMode string

const (
	DownloadModeKeep   DownloadMode = "keep"
	DownloadModeDelete DownloadMode = "delete"
)

type Downloader struct {
	ctx                   context.Context
	cancel                context.CancelFunc
	queue                 *scheduler
	cfg                   *config.Config
	client                *baiduapi.Client
	clientMutex           sync.RWMutex
	authManaged           bool
	authReady             bool
	authExpiry            time.Time
	authEpoch             uint64
	onRejected            func(string)
	tasks                 map[int64]*Task
	taskMutex             sync.RWMutex
	scanMutex             sync.Mutex
	cloudMutex            sync.RWMutex
	intakeMutex           sync.RWMutex
	intakeBoot            string
	intakeJournal         intakeJournal
	intakes               map[string]intakeRecord
	inFlight              map[int64]bool
	modeMutex             sync.RWMutex
	downloadMode          DownloadMode
	taskCatalogGeneration atomic.Uint64
	taskStatsGeneration   atomic.Uint64
}

func NewDownloader(cfg *config.Config, client *baiduapi.Client) *Downloader {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Downloader{cfg: cfg, client: client, tasks: make(map[int64]*Task), inFlight: make(map[int64]bool), ctx: ctx, cancel: cancel, intakes: make(map[string]intakeRecord)}
	d.queue = newScheduler(d)
	return d
}
func (d *Downloader) UpdateClient(client *baiduapi.Client) {
	d.clientMutex.Lock()
	defer d.clientMutex.Unlock()
	d.client = client
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
		body, _ := json.Marshal(map[string]DownloadMode{"download_mode": mode})
		if err := q.store.SaveSettings(body); err != nil {
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
		return nil
	}
	defer d.scanMutex.Unlock()
	d.cloudMutex.RLock()
	defer d.cloudMutex.RUnlock()
	if err := d.ctx.Err(); err != nil {
		return err
	}
	client, epoch, err := d.authorizedClient(d.ctx)
	if err != nil {
		return err
	}
	files, err := client.ListDir(d.cfg.QueueDir)
	if err != nil {
		d.reportAuthenticationFailure(err)
		return fmt.Errorf("列目录失败: %w", err)
	}
	return d.queue.reconcile(files, epoch)
}

func (d *Downloader) beginTask(info baiduapi.FileInfo, local string) {
	info = shareFileInfoStrings(info)
	d.taskMutex.Lock()
	defer d.taskMutex.Unlock()
	task := &Task{FileInfo: info, LocalPath: local, Status: "downloading", StartTime: time.Now(), LastProgressTime: time.Now(), ResumeSupported: true, MD5: info.MD5}
	catalogChanged, statsChanged := true, true
	if old := d.tasks[info.FsID]; old != nil {
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
	if info.Path != path.Join(d.cfg.QueueDir, info.ServerFilename) {
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

func (d *Downloader) downloadLocalFolder(ctx context.Context, info baiduapi.FileInfo, relative string) error {
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
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return err
	}
	children, err := client.ListDir(info.Path)
	if err != nil {
		d.finish(info.FsID, err)
		return err
	}
	var failures []error
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
			err = fmt.Errorf("invalid cloud filename %q", child.ServerFilename)
		} else if child.Path != path.Join(info.Path, child.ServerFilename) {
			err = fmt.Errorf("cloud child path does not match directory: %s", child.Path)
		} else {
			childRelative := filepath.Join(relative, child.ServerFilename)
			if child.IsDir == 1 {
				err = d.downloadLocalFolder(ctx, child, childRelative)
			} else {
				err = d.downloadLocalFile(ctx, child, childRelative)
			}
		}
		if err != nil {
			if errors.Is(err, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) {
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
	local, err := d.localPath(relative)
	d.beginTask(info, local)
	if err == nil {
		err = d.transfer(ctx, info, relative, local)
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
		if errors.Is(verifyErr, baiduapi.ErrFileMetadataMissing) || (verifyErr == nil && current != info) {
			verifyErr = ErrSourceIdentityChanged
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
		return &copy
	}
	if job == nil {
		return nil
	}
	task := &Task{
		FileInfo: job.Info, Status: job.Status, Error: job.Error,
		StartTime: job.Created, Attempts: job.Attempts, NextAttempt: job.NextAttempt,
		Root: true, DownloadMode: job.Mode,
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
	page := TaskPage{Offset: offset, Limit: limit, Total: total, Counts: counts}
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
		task.CanCancel = job.Status == "pending" || job.Status == "downloading" || job.Status == "retry_wait" || job.Status == "auth_wait"
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
