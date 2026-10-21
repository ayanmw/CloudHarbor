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
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type queueJob struct {
	Version     int               `json:"version"`
	Info        baiduapi.FileInfo `json:"file"`
	Mode        DownloadMode      `json:"mode"`
	Status      string            `json:"status"`
	Attempts    int               `json:"attempts"`
	Failures    int               `json:"failures"`
	NextAttempt time.Time         `json:"next_attempt,omitempty"`
	Error       string            `json:"error,omitempty"`
	Created     time.Time         `json:"created"`
	Updated     time.Time         `json:"updated"`
}
type scheduler struct {
	mu             sync.Mutex
	owner          *Downloader
	store          *taskstore.Store
	jobs           map[int64]*queueJob
	eligible       map[int64]baiduapi.FileInfo
	blockedIntakes map[string]bool
	active         map[int64]context.CancelFunc
	wake           chan struct{}
	done           chan struct{}
	wg             sync.WaitGroup
	started        bool
	stopping       bool
	err            error
	closeErr       error
	shutdownOnce   sync.Once
}

func newScheduler(d *Downloader) *scheduler {
	return &scheduler{owner: d, jobs: make(map[int64]*queueJob), eligible: make(map[int64]baiduapi.FileInfo), blockedIntakes: make(map[string]bool), active: make(map[int64]context.CancelFunc), wake: make(chan struct{}, 1), done: make(chan struct{})}
}
func OpenDownloader(cfg *config.Config, client *baiduapi.Client) (*Downloader, error) {
	if cfg.MaxParallel < 0 || cfg.MaxParallel > 32 || cfg.MaxRetries < 0 || cfg.MaxRetries > 20 || cfg.RetryBackoffSeconds < 0 {
		return nil, fmt.Errorf("invalid queue configuration")
	}
	if !strings.HasPrefix(cfg.QueueDir, "/") {
		return nil, fmt.Errorf("QUEUE_DIR must be an absolute cloud path")
	}
	d := NewDownloader(cfg, client)
	root, err := filepath.Abs(cfg.LocalDownDir)
	if err == nil {
		err = rejectSymlinks(root, filepath.Join(stateDirectory, "queue"))
	}
	if err != nil {
		d.cancel()
		return nil, err
	}
	store, err := taskstore.Open(filepath.Join(root, stateDirectory, "queue"), path.Clean(cfg.QueueDir))
	if err != nil {
		d.cancel()
		return nil, err
	}
	d.queue.store = store
	d.intakeJournal = store
	settings, err := store.LoadSettings()
	if err != nil {
		store.Close()
		d.cancel()
		return nil, err
	}
	if len(settings) > 0 {
		var saved struct {
			Mode DownloadMode `json:"download_mode"`
		}
		if err = json.Unmarshal(settings, &saved); err != nil || (saved.Mode != DownloadModeKeep && saved.Mode != DownloadModeDelete) {
			store.Close()
			d.cancel()
			return nil, fmt.Errorf("invalid persisted download mode")
		}
		d.downloadMode = saved.Mode
	}
	records, err := store.Load()
	if err != nil {
		store.Close()
		d.cancel()
		return nil, err
	}
	for id, body := range records {
		var job queueJob
		if err = json.Unmarshal(body, &job); err != nil || job.Version != 1 || job.Info.FsID != id || !validJobStatus(job.Status) || job.Attempts < 0 || job.Failures < 0 || (job.Mode != DownloadModeKeep && job.Mode != DownloadModeDelete) {
			store.Close()
			d.cancel()
			return nil, fmt.Errorf("invalid persisted job %d", id)
		}
		if job.Status == "downloading" {
			job.Status = "pending"
			job.Error = "服务重启，等待扫描确认后恢复"
		}
		d.queue.jobs[id] = &job
		d.queue.reflect(&job)
	}
	if err = d.loadIntakes(); err != nil {
		store.Close()
		d.cancel()
		return nil, err
	}
	return d, nil
}
func validJobStatus(status string) bool {
	switch status {
	case "pending", "downloading", "retry_wait", "auth_wait", "failed", "done", "cancelled":
		return true
	}
	return false
}
func (q *scheduler) persist(job *queueJob) error {
	job.Error = errorSummary(job.Error)
	job.Updated = time.Now()
	if q.store == nil {
		return nil
	}
	body, err := json.Marshal(job)
	if err == nil {
		err = q.store.Save(job.Info.FsID, body)
	}
	if err != nil {
		q.err = fmt.Errorf("队列保存失败，停止调度: %w", err)
		q.owner.cancel()
	}
	return err
}

func errorSummary(message string) string {
	if len(message) <= 8192 {
		return message
	}
	return strings.ToValidUTF8(message[:8192], "?") + "…（错误过长，已截断）"
}
func (q *scheduler) reflect(job *queueJob) {
	d := q.owner
	d.taskMutex.Lock()
	defer d.taskMutex.Unlock()
	task := d.tasks[job.Info.FsID]
	if task == nil || task.FileInfo != job.Info {
		task = &Task{FileInfo: job.Info}
		d.tasks[job.Info.FsID] = task
	}
	task.Status = job.Status
	task.Root = true
	task.DownloadMode = job.Mode
	task.Error = job.Error
	task.Attempts = job.Attempts
	task.NextAttempt = job.NextAttempt
	if task.StartTime.IsZero() {
		task.StartTime = job.Created
	}
	if job.Status == "done" || job.Status == "failed" || job.Status == "cancelled" {
		task.FinishTime = job.Updated
	} else {
		task.FinishTime = time.Time{}
	}
	if job.Status == "done" && job.Info.IsDir == 0 {
		task.Progress = job.Info.Size
	}
	if task.Status != "downloading" {
		task.Speed = 0
	}
}
func (q *scheduler) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
func (q *scheduler) reconcile(files []baiduapi.FileInfo, epoch uint64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || q.owner.ctx.Err() != nil {
		return context.Canceled
	}
	if q.err != nil {
		return q.err
	}
	q.owner.clientMutex.RLock()
	validScan := q.owner.authenticationReadyLocked() && q.owner.authEpoch == epoch
	q.owner.clientMutex.RUnlock()
	if !validScan {
		return ErrAuthenticationWaiting
	}
	eligible := make(map[int64]baiduapi.FileInfo)
	for _, f := range files {
		if q.blockedIntakes[f.Path] {
			continue
		}
		if strings.HasSuffix(f.ServerFilename, ".可删除") {
			continue
		}
		if f.FsID <= 0 {
			return fmt.Errorf("invalid root fs_id")
		}
		eligible[f.FsID] = f
		old := q.jobs[f.FsID]
		if old != nil && (old.Info == f || q.active[f.FsID] != nil) {
			continue
		}
		job := &queueJob{Version: 1, Info: f, Mode: q.owner.GetDownloadMode(), Status: "pending", Created: time.Now()}
		// Durable registration happens before a worker can observe the job.
		if err := q.persist(job); err != nil {
			return err
		}
		q.jobs[f.FsID] = job
		q.reflect(job)
	}
	q.eligible = eligible
	for id, job := range q.jobs {
		// A quarantined target was deliberately excluded above, not missing.
		if q.blockedIntakes[job.Info.Path] {
			continue
		}
		if job.Status == "auth_wait" && eligible[id] == job.Info && q.active[id] == nil {
			job.Status = "pending"
			job.Error = ""
			if err := q.persist(job); err != nil {
				return err
			}
			q.reflect(job)
		}
		if _, present := eligible[id]; present || q.active[id] != nil {
			continue
		}
		if job.Status == "pending" || job.Status == "retry_wait" || job.Status == "auth_wait" {
			job.Status = "failed"
			job.Error = "源项目已离开队列或已被标记；请核对网盘与本地结果，恢复源位置后手动重试"
			job.NextAttempt = time.Time{}
			if err := q.persist(job); err != nil {
				return err
			}
			q.reflect(job)
		}
	}
	if !q.started {
		q.started = true
		workers := q.owner.cfg.MaxParallel
		if workers == 0 {
			workers = 2
		}
		for i := 0; i < workers; i++ {
			q.wg.Add(1)
			go q.worker()
		}
	}
	q.signal()
	return nil
}
func (q *scheduler) claim() (*queueJob, context.Context) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || q.err != nil || q.owner.ctx.Err() != nil {
		return nil, nil
	}
	if !q.owner.AuthenticationReady() {
		q.parkAuthenticationWaiting()
		return nil, nil
	}
	var chosen *queueJob
	for _, job := range q.jobs {
		if q.blockedIntakes[job.Info.Path] || (job.Status != "pending" && job.Status != "retry_wait") || q.active[job.Info.FsID] != nil || q.eligible[job.Info.FsID] != job.Info || time.Now().Before(job.NextAttempt) {
			continue
		}
		if chosen == nil || job.Created.Before(chosen.Created) || (job.Created.Equal(chosen.Created) && job.Info.FsID < chosen.Info.FsID) {
			chosen = job
		}
	}
	if chosen == nil {
		return nil, nil
	}
	chosen.Status = "downloading"
	chosen.Attempts++
	chosen.NextAttempt = time.Time{}
	if err := q.persist(chosen); err != nil {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(q.owner.ctx)
	q.active[chosen.Info.FsID] = cancel
	q.owner.taskMutex.Lock()
	q.owner.inFlight[chosen.Info.FsID] = true
	q.owner.taskMutex.Unlock()
	q.reflect(chosen)
	copy := *chosen
	return &copy, ctx
}
func (q *scheduler) worker() {
	defer q.wg.Done()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if job, ctx := q.claim(); job != nil {
			err := q.owner.runRoot(ctx, job.Info, job.Mode)
			q.complete(job, err)
			continue
		}
		select {
		case <-q.owner.ctx.Done():
			return
		case <-q.wake:
		case <-ticker.C:
		}
	}
}
func (q *scheduler) complete(executed *queueJob, err error) {
	q.owner.reportAuthenticationFailure(err)
	q.mu.Lock()
	defer q.mu.Unlock()
	id := executed.Info.FsID
	if cancel := q.active[id]; cancel != nil {
		cancel()
	}
	delete(q.active, id)
	q.owner.taskMutex.Lock()
	delete(q.owner.inFlight, id)
	q.owner.taskMutex.Unlock()
	job := q.jobs[id]
	if job.Status != "cancelled" {
		job.Status = "done"
		job.Error = ""
		job.NextAttempt = time.Time{}
		if err != nil {
			job.Error = err.Error()
			job.Status = "failed"
			if errors.Is(err, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) {
				job.Status = "auth_wait"
			} else if q.stopping || errors.Is(err, context.Canceled) {
				job.Status = "pending"
			} else if baiduapi.IsRetryable(err) {
				job.Failures++
				if job.Failures <= q.owner.cfg.MaxRetries {
					job.Status = "retry_wait"
					seconds := q.owner.cfg.RetryBackoffSeconds
					if seconds <= 0 {
						seconds = 5
					}
					delay := time.Duration(seconds) * time.Second
					for attempt := 1; attempt < job.Failures && delay < 5*time.Minute; attempt++ {
						delay *= 2
					}
					if delay > 5*time.Minute {
						delay = 5 * time.Minute
					}
					job.NextAttempt = time.Now().Add(delay)
				}
			}
		}
	}
	if saveErr := q.persist(job); saveErr != nil {
		job.Status = "failed"
		job.Error = q.err.Error()
	}
	q.reflect(job)
	q.signal()
}

var ErrTaskNotFound = errors.New("task not found")
var ErrTaskConflict = errors.New("task cannot be changed in its current state")

func (d *Downloader) Cancel(id int64) error {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || q.err != nil {
		return ErrTaskConflict
	}
	job := q.jobs[id]
	if job == nil {
		return ErrTaskNotFound
	}
	if job.Status == "done" {
		return ErrTaskConflict
	}
	job.Status = "cancelled"
	job.Error = "用户取消"
	job.NextAttempt = time.Time{}
	if err := q.persist(job); err != nil {
		return err
	}
	if cancel := q.active[id]; cancel != nil {
		cancel()
	}
	q.reflect(job)
	return nil
}
func (d *Downloader) Retry(id int64) error {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobs[id]
	if job == nil {
		return ErrTaskNotFound
	}
	if q.stopping || q.err != nil || q.blockedIntakes[job.Info.Path] || !d.AuthenticationReady() || q.active[id] != nil || (job.Status != "failed" && job.Status != "cancelled") || q.eligible[id] != job.Info {
		return ErrTaskConflict
	}
	job.Status = "pending"
	job.Attempts = 0
	job.Failures = 0
	job.Error = ""
	job.NextAttempt = time.Time{}
	if err := q.persist(job); err != nil {
		return err
	}
	q.reflect(job)
	q.signal()
	return nil
}
func (d *Downloader) Shutdown(ctx context.Context) error {
	q := d.queue
	q.shutdownOnce.Do(func() {
		d.cancel()
		go func() {
			q.mu.Lock()
			q.stopping = true
			q.mu.Unlock()
			q.wg.Wait()
			if q.store != nil {
				q.closeErr = q.store.Close()
			}
			close(q.done)
		}()
	})
	select {
	case <-q.done:
		return q.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (d *Downloader) QueueError() error {
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	return d.queue.err
}
