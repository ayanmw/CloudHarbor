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
	ctx           context.Context
	cancel        context.CancelFunc
	queue         *scheduler
	cfg           *config.Config
	client        *baiduapi.Client
	clientMutex   sync.RWMutex
	authManaged   bool
	authReady     bool
	authExpiry    time.Time
	authEpoch     uint64
	onRejected    func(string)
	tasks         map[int64]*Task
	taskMutex     sync.RWMutex
	scanMutex     sync.Mutex
	cloudMutex    sync.RWMutex
	intakeMutex   sync.RWMutex
	intakeBoot    string
	intakeJournal intakeJournal
	intakes       map[string]intakeRecord
	inFlight      map[int64]bool
	modeMutex     sync.RWMutex
	downloadMode  DownloadMode
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
	d.taskMutex.Lock()
	defer d.taskMutex.Unlock()
	task := &Task{FileInfo: info, LocalPath: local, Status: "downloading", StartTime: time.Now(), LastProgressTime: time.Now(), ResumeSupported: true, MD5: info.MD5}
	if old := d.tasks[info.FsID]; old != nil {
		task.Attempts = old.Attempts
		task.NextAttempt = old.NextAttempt
		task.Root = old.Root
		task.DownloadMode = old.DownloadMode
	}
	d.tasks[info.FsID] = task
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
	result := make([]*Task, 0, len(d.tasks))
	for id, task := range d.tasks {
		result = append(result, d.copyTaskLocked(id, task))
	}
	return result
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

	page := TaskPage{Offset: offset, Limit: limit, Counts: make(map[string]int)}
	page.Total = len(d.tasks)
	for _, task := range d.tasks {
		page.Counts[task.Status]++
	}
	page.HasMore = offset < page.Total && limit < page.Total-offset
	if limit == 0 || offset >= page.Total || page.Total == 0 {
		return page
	}

	wanted := page.Total
	if offset < page.Total && limit <= page.Total-offset {
		wanted = offset + limit
	}
	selected := make([]*Task, 0, wanted)
	for _, task := range d.tasks {
		if len(selected) < wanted {
			selected = append(selected, task)
			siftTaskPageUp(selected, len(selected)-1, sortBy)
			continue
		}
		if wanted > 0 && taskComesBefore(task, selected[0], sortBy) {
			selected[0] = task
			siftTaskPageDown(selected, 0, sortBy)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return taskComesBefore(selected[i], selected[j], sortBy) })

	start := offset
	if start > len(selected) {
		start = len(selected)
	}
	end := len(selected)
	if limit < len(selected)-start {
		end = start + limit
	}
	page.Tasks = make([]*Task, 0, end-start)
	for _, task := range selected[start:end] {
		page.Tasks = append(page.Tasks, d.copyTaskLocked(task.FileInfo.FsID, task))
	}
	return page
}

func taskComesBefore(a, b *Task, sortBy string) bool {
	if sortBy == "size" && a.FileInfo.Size != b.FileInfo.Size {
		return a.FileInfo.Size > b.FileInfo.Size
	}
	if sortBy == "progress" {
		aProgress, bProgress := float64(0), float64(0)
		if a.FileInfo.Size > 0 {
			aProgress = float64(a.Progress) / float64(a.FileInfo.Size)
		}
		if b.FileInfo.Size > 0 {
			bProgress = float64(b.Progress) / float64(b.FileInfo.Size)
		}
		if aProgress != bProgress {
			return aProgress > bProgress
		}
	}
	if sortBy == "name" {
		aName, bName := a.FileInfo.Path, b.FileInfo.Path
		if aName == "" {
			aName = a.FileInfo.ServerFilename
		}
		if bName == "" {
			bName = b.FileInfo.ServerFilename
		}
		aName, bName = strings.ToLower(aName), strings.ToLower(bName)
		if aName != bName {
			return aName < bName
		}
	}
	return a.FileInfo.FsID < b.FileInfo.FsID
}

// The bounded heap keeps the worst selected task at index 0.
func siftTaskPageUp(tasks []*Task, child int, sortBy string) {
	for child > 0 {
		parent := (child - 1) / 2
		if !taskComesBefore(tasks[parent], tasks[child], sortBy) {
			break
		}
		tasks[parent], tasks[child] = tasks[child], tasks[parent]
		child = parent
	}
}

func siftTaskPageDown(tasks []*Task, parent int, sortBy string) {
	for {
		left := parent*2 + 1
		if left >= len(tasks) {
			return
		}
		worst := left
		right := left + 1
		if right < len(tasks) && taskComesBefore(tasks[worst], tasks[right], sortBy) {
			worst = right
		}
		if !taskComesBefore(tasks[parent], tasks[worst], sortBy) {
			return
		}
		tasks[parent], tasks[worst] = tasks[worst], tasks[parent]
		parent = worst
	}
}

func (d *Downloader) copyTaskLocked(id int64, task *Task) *Task {
	copy := *task
	// Action IDs remain exact in browsers, including values above 2^53.
	copy.TaskID = strconv.FormatInt(copy.FileInfo.FsID, 10)
	job := d.queue.jobs[id]
	if copy.Root && job != nil && !d.queue.stopping && d.queue.err == nil && d.ctx.Err() == nil {
		copy.CanCancel = job.Status == "pending" || job.Status == "downloading" || job.Status == "retry_wait" || job.Status == "auth_wait"
		copy.CanRetry = job.SourceState == "" && !d.queue.blockedSources[job.Info.Path] && !d.queue.blockedIntakes[job.Info.Path] && (job.Status == "failed" || job.Status == "cancelled") && d.queue.active[id] == nil && d.queue.eligible[id] == job.Info && d.AuthenticationReady()
	}
	return &copy
}

func (d *Downloader) GetTaskStats() (int, map[string]int) {
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()
	counts := make(map[string]int)
	for _, task := range d.tasks {
		counts[task.Status]++
	}
	return len(d.tasks), counts
}
func (d *Downloader) GetTaskByFsID(id int64) *Task {
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()
	if task := d.tasks[id]; task != nil {
		copy := *task
		return &copy
	}
	return nil
}
