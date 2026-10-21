package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
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
}

// keep retains the cloud item and marks its name; delete removes it only after
// all local files have been published successfully.
type DownloadMode string

const (
	DownloadModeKeep   DownloadMode = "keep"
	DownloadModeDelete DownloadMode = "delete"
)

type Downloader struct {
	ctx          context.Context
	cancel       context.CancelFunc
	queue        *scheduler
	cfg          *config.Config
	client       *baiduapi.Client
	clientMutex  sync.RWMutex
	authManaged  bool
	authReady    bool
	authExpiry   time.Time
	authEpoch    uint64
	onRejected   func(string)
	tasks        map[int64]*Task
	taskMutex    sync.RWMutex
	scanMutex    sync.Mutex
	inFlight     map[int64]bool
	modeMutex    sync.RWMutex
	downloadMode DownloadMode
}

func NewDownloader(cfg *config.Config, client *baiduapi.Client) *Downloader {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Downloader{cfg: cfg, client: client, tasks: make(map[int64]*Task), inFlight: make(map[int64]bool), ctx: ctx, cancel: cancel}
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
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := d.sourceOperationClient(ctx, info)
	if err != nil {
		return err
	}
	if mode == DownloadModeDelete {
		err = client.DeleteFile(info.Path)
	} else {
		err = client.RenameFile(info.Path, path.Base(info.Path)+".可删除")
	}
	if err != nil {
		return fmt.Errorf("本地已完成，网盘后处理待重试: %w", err)
	}
	return nil
}

// Copy under the lock so serialization never reads live download progress.
func (d *Downloader) GetTasks() []*Task {
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()
	result := make([]*Task, 0, len(d.tasks))
	for _, task := range d.tasks {
		copy := *task
		result = append(result, &copy)
	}
	return result
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
