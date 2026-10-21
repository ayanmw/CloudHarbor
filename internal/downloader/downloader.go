package downloader

import (
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
}

// keep retains the cloud item and marks its name; delete removes it only after
// all local files have been published successfully.
type DownloadMode string

const (
	DownloadModeKeep   DownloadMode = "keep"
	DownloadModeDelete DownloadMode = "delete"
)

type Downloader struct {
	cfg          *config.Config
	client       *baiduapi.Client
	clientMutex  sync.RWMutex
	tasks        map[int64]*Task
	taskMutex    sync.RWMutex
	scanMutex    sync.Mutex
	inFlight     map[int64]bool
	modeMutex    sync.RWMutex
	downloadMode DownloadMode
}

func NewDownloader(cfg *config.Config, client *baiduapi.Client) *Downloader {
	return &Downloader{cfg: cfg, client: client, tasks: make(map[int64]*Task), inFlight: make(map[int64]bool)}
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
	d.scanMutex.Lock()
	defer d.scanMutex.Unlock()
	files, err := d.getClient().ListDir(d.cfg.QueueDir)
	if err != nil {
		return fmt.Errorf("列目录失败: %w", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f.ServerFilename, ".可删除") {
			continue
		}
		// Claim before launching: overlapping manual/timer scans must not write the
		// same temporary file. A failed task remains discoverable on the next scan.
		d.taskMutex.Lock()
		previous := d.tasks[f.FsID]
		if d.inFlight[f.FsID] || (previous != nil && previous.Status == "done" && previous.FileInfo == f) {
			d.taskMutex.Unlock()
			continue
		}
		d.inFlight[f.FsID] = true
		d.taskMutex.Unlock()
		go func(info baiduapi.FileInfo) {
			defer func() { d.taskMutex.Lock(); delete(d.inFlight, info.FsID); d.taskMutex.Unlock() }()
			if info.IsDir == 1 {
				d.downloadFolder(info)
			} else {
				d.downloadFile(info)
			}
		}(f)
	}
	return nil
}

func (d *Downloader) beginTask(info baiduapi.FileInfo, local string) {
	d.taskMutex.Lock()
	defer d.taskMutex.Unlock()
	d.tasks[info.FsID] = &Task{FileInfo: info, LocalPath: local, Status: "downloading", StartTime: time.Now(), LastProgressTime: time.Now(), ResumeSupported: true, MD5: info.MD5}
}
func (d *Downloader) updateTaskStatus(id int64, status, message string) {
	d.taskMutex.Lock()
	defer d.taskMutex.Unlock()
	if task := d.tasks[id]; task != nil {
		task.Status = status
		task.Error = message
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
	if !validName(info.ServerFilename) {
		d.beginTask(info, "")
		d.finish(info.FsID, fmt.Errorf("invalid cloud filename %q", info.ServerFilename))
		return
	}
	mode := d.GetDownloadMode()
	err := d.downloadLocalFile(info, info.ServerFilename)
	if err == nil {
		err = d.postprocess(info, mode)
	}
	d.finish(info.FsID, err)
}

func (d *Downloader) downloadFolder(info baiduapi.FileInfo) {
	if !validName(info.ServerFilename) {
		d.beginTask(info, "")
		d.finish(info.FsID, fmt.Errorf("invalid cloud filename %q", info.ServerFilename))
		return
	}
	mode := d.GetDownloadMode()
	err := d.downloadLocalFolder(info, info.ServerFilename)
	// Children are never renamed individually: keeping their cloud paths stable
	// allows an incomplete folder to be retried without losing failed descendants.
	if err == nil {
		err = d.postprocess(info, mode)
	}
	d.finish(info.FsID, err)
}

func (d *Downloader) downloadLocalFolder(info baiduapi.FileInfo, relative string) error {
	local, err := d.localPath(relative)
	d.beginTask(info, local)
	if err == nil {
		err = ensureDirectory(local)
	}
	if err != nil {
		d.finish(info.FsID, err)
		return err
	}
	children, err := d.getClient().ListDir(info.Path)
	if err != nil {
		d.finish(info.FsID, err)
		return err
	}
	var failures []string
	for _, child := range children {
		// Old markers do not prove that this NAS still has the child.
		if strings.HasSuffix(child.ServerFilename, ".可删除") {
			failures = append(failures, child.Path+": 发现旧版标记，无法验证本地文件；请移出队列目录或恢复原名后重试")
			continue
		}
		if !validName(child.ServerFilename) {
			err = fmt.Errorf("invalid cloud filename %q", child.ServerFilename)
		} else {
			childRelative := filepath.Join(relative, child.ServerFilename)
			if child.IsDir == 1 {
				err = d.downloadLocalFolder(child, childRelative)
			} else {
				err = d.downloadLocalFile(child, childRelative)
			}
		}
		if err != nil {
			failures = append(failures, child.Path+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		err = fmt.Errorf("目录有 %d 个失败项目: %s", len(failures), strings.Join(failures, "; "))
	} else {
		err = nil
	}
	d.finish(info.FsID, err)
	return err
}

func (d *Downloader) downloadLocalFile(info baiduapi.FileInfo, relative string) error {
	local, err := d.localPath(relative)
	d.beginTask(info, local)
	if err == nil {
		err = d.transfer(info, relative, local)
	}
	d.finish(info.FsID, err)
	return err
}

func (d *Downloader) postprocess(info baiduapi.FileInfo, mode DownloadMode) error {
	var err error
	if mode == DownloadModeDelete {
		err = d.getClient().DeleteFile(info.Path)
	} else {
		err = d.getClient().RenameFile(info.Path, path.Base(info.Path)+".可删除")
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
