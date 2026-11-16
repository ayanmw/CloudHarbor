package downloader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/config"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

type QueueMonitorObservation struct {
	RunID         string `json:"run_id"`
	Path          string `json:"path"`
	ObservedAtUTC string `json:"observed_at_utc"`
}

type queueJob struct {
	MonitorObservation *QueueMonitorObservation `json:"monitor_observation,omitempty"`
	IntakeID           string                   `json:"intake_id,omitempty"`
	Destination        *RootDestination         `json:"destination,omitempty"`
	TransferStats      *TransferStatistics      `json:"transfer_statistics,omitempty"`
	Version            int                      `json:"version"`
	Info               baiduapi.FileInfo        `json:"file"`
	Mode               DownloadMode             `json:"mode"`
	Status             string                   `json:"status"`
	SourceState        string                   `json:"source_state,omitempty"`
	SourceOperationID  string                   `json:"source_operation_id,omitempty"`
	SourceAttemptID    string                   `json:"source_attempt_id,omitempty"`
	Attempts           int                      `json:"attempts"`
	AttemptID          string                   `json:"attempt_id,omitempty"`
	Failures           int                      `json:"failures"`
	NextAttempt        time.Time                `json:"next_attempt,omitempty"`
	Error              string                   `json:"error,omitempty"`
	LocalStorage       *LocalStorageEvidence    `json:"local_storage,omitempty"`
	PausedFromStatus   string                   `json:"paused_from_status,omitempty"`
	Created            time.Time                `json:"created"`
	Updated            time.Time                `json:"updated"`
	candidate          *candidateEntry
}

const (
	maxNewJobPersistenceBatch = 1024
	retryClockRecheckInterval = 30 * time.Second
	taskSourceMissingError    = "源项目已离开队列或已被标记；请核对网盘与本地结果，恢复源位置后手动重试"
)

type scheduler struct {
	transferMeters map[int64]*transferMeter

	mu      sync.Mutex
	owner   *Downloader
	store   *taskstore.Store
	jobs    map[int64]*queueJob
	ready   candidateHeap
	delayed candidateHeap
	// eligible points into jobs when scan metadata matches, and owns a separate
	// snapshot only while a changed identity conflicts with an active/unknown job.
	eligible             map[int64]*baiduapi.FileInfo
	blockedIntakeSources map[string]bool
	blockedIntakeIDs     map[int64]bool
	blockedIntakes       map[string]bool
	blockedSources       map[string]bool
	blockedRemoteIDs     map[int64]bool
	blockedRemotePaths   map[string]bool
	active               map[int64]context.CancelFunc
	wake                 chan struct{}
	done                 chan struct{}
	wg                   sync.WaitGroup
	started              bool
	maintenance          bool
	uploadMaintenance    bool
	userPaused           bool
	pauseUpdatedAtUTC    time.Time
	activeChanged        chan struct{}
	stopping             bool
	err                  error
	closeErr             error
	shutdownOnce         sync.Once
	statsCache           taskStatsCache
	nameIndex            taskOrderIndex
	sizeIndex            taskOrderIndex
	progressIndex        progressTaskIndex
}

type taskStatsCache struct {
	generation uint64
	total      int
	counts     map[string]int
	valid      bool
}

type taskOrderIndex struct {
	ids        []int64
	generation uint64
	ready      bool
}

type progressTaskIndex struct {
	ids        []int64
	activeIDs  []int64
	generation uint64
	ready      bool
}

func newScheduler(d *Downloader) *scheduler {
	return &scheduler{owner: d, jobs: make(map[int64]*queueJob), eligible: make(map[int64]*baiduapi.FileInfo), blockedIntakes: make(map[string]bool), blockedRemoteIDs: make(map[int64]bool), blockedRemotePaths: make(map[string]bool), active: make(map[int64]context.CancelFunc), activeChanged: make(chan struct{}), wake: make(chan struct{}, 32), done: make(chan struct{})}
}

func (q *scheduler) isRemoteBlocked(id int64, cloudPath string) bool {
	if q.blockedRemoteIDs[id] || q.blockedRemotePaths[cloudPath] {
		return true
	}
	for blocked := range q.blockedRemotePaths {
		if insideCloud(cloudPath, blocked) {
			return true
		}
	}
	return false
}
func OpenDownloader(cfg *config.Config, client *baiduapi.Client) (*Downloader, error) {
	if cfg.MaxParallel < 0 || cfg.MaxParallel > 32 || cfg.MaxRetries < 0 || cfg.MaxRetries > 20 || cfg.RetryBackoffSeconds < 0 {
		return nil, fmt.Errorf("invalid queue configuration")
	}
	if !strings.HasPrefix(cfg.QueueDir, "/") {
		return nil, fmt.Errorf("QUEUE_DIR must be an absolute cloud path")
	}
	d := NewDownloader(cfg, client)
	stateRoot := cfg.StateDir
	if stateRoot == "" {
		stateRoot = filepath.Join(cfg.LocalDownDir, stateDirectory)
	}
	root, err := filepath.Abs(stateRoot)
	if err == nil {
		err = rejectSymlinks(filepath.Dir(root), filepath.Base(root))
	}
	if err != nil {
		d.cancel()
		return nil, err
	}
	queueStateDir := filepath.Join(root, "queue")
	var store *taskstore.Store
	if cfg.StateDir != "" {
		store, err = taskstore.OpenUsingExistingScope(queueStateDir, path.Clean(cfg.QueueDir))
	} else {
		store, err = taskstore.Open(queueStateDir, path.Clean(cfg.QueueDir))
	}
	if err != nil {
		d.cancel()
		return nil, err
	}
	d.queue.store = store
	d.intakeJournal = store
	remoteOperations, err := store.LoadRemoteOperations()
	if err != nil {
		store.Close()
		d.cancel()
		return nil, err
	}
	for _, operation := range remoteOperations {
		// Restore uncertainty before authentication is loaded. This state directory
		// belongs to one bound account; credential renewal must not release an
		// unresolved operation or rewrite its historical credential evidence.
		if operation.Status == "unknown" {
			if operation.ObjectID > 0 {
				d.queue.blockedRemoteIDs[operation.ObjectID] = true
			}
			if operation.SourcePath != "" {
				d.queue.blockedRemotePaths[operation.SourcePath] = true
			}
			if operation.DestinationPath != "" {
				d.queue.blockedRemotePaths[operation.DestinationPath] = true
			}
		}
	}
	settings, err := store.LoadSettings()
	if err != nil {
		store.Close()
		d.cancel()
		return nil, err
	}
	if len(settings) > 0 {
		var saved struct {
			Mode                DownloadMode `json:"download_mode"`
			QueueDir            string       `json:"managed_queue_dir,omitempty"`
			LocalDownDir        string       `json:"managed_local_download_dir,omitempty"`
			QueuePaused         bool         `json:"queue_paused,omitempty"`
			QueuePauseUpdatedAt time.Time    `json:"queue_pause_updated_at_utc,omitempty"`
		}
		if err = json.Unmarshal(settings, &saved); err != nil || (saved.Mode != "" && saved.Mode != DownloadModeKeep && saved.Mode != DownloadModeDelete) {
			store.Close()
			d.cancel()
			return nil, fmt.Errorf("invalid persisted settings")
		}
		if saved.Mode != "" {
			d.downloadMode = saved.Mode
		}
		d.queue.userPaused = saved.QueuePaused
		d.queue.pauseUpdatedAtUTC = saved.QueuePauseUpdatedAt.UTC()
		if saved.QueueDir != "" && !cfg.QueueDirExplicit {
			if !validCloudPath(saved.QueueDir) || saved.QueueDir == "/" {
				store.Close()
				d.cancel()
				return nil, fmt.Errorf("invalid persisted cloud monitor path")
			}
			d.queueDir = saved.QueueDir
			d.queuePathSaved = true
		}
		if saved.LocalDownDir != "" && !cfg.LocalDownDirExplicit {
			local, pathErr := filepath.Abs(saved.LocalDownDir)
			if pathErr != nil || (cfg.StateDir != "" && pathsOverlap(local, cfg.StateDir)) {
				store.Close()
				d.cancel()
				return nil, fmt.Errorf("invalid persisted local download path")
			}
			d.localDownDir = local
			d.localPathSaved = true
		}
	}
	if cfg.StateDir != "" {
		if err = validateLocalPathOutsideState(d.localDownDir, cfg.StateDir); err != nil {
			store.Close()
			d.cancel()
			return nil, fmt.Errorf("invalid local download path: %w", err)
		}
	}
	var recoveredJobs []*queueJob
	var recoveryEvents []taskstore.Event
	err = store.LoadEachRaw(func(id int64, body json.RawMessage) error {
		var job queueJob
		decodeErr := json.Unmarshal(body, &job)
		status, statusValid := canonicalJobStatus(job.Status)
		mode, modeValid := canonicalJobMode(job.Mode)
		if decodeErr != nil || job.Version != 1 || job.Info.FsID != id || !statusValid || !validSourceRecord(&job) || job.Attempts < 0 || job.Failures < 0 || !modeValid {
			return fmt.Errorf("invalid persisted job %d", id)
		}
		if status == "paused" {
			if job.SourceState != "" || (job.PausedFromStatus != "pending" && job.PausedFromStatus != "retry_wait" && job.PausedFromStatus != "downloading") {
				return fmt.Errorf("invalid persisted paused job %d", id)
			}
		} else if job.PausedFromStatus != "" {
			return fmt.Errorf("invalid persisted pause origin %d", id)
		}
		job.Status = status
		job.Mode = mode
		job.Info = shareFileInfoStrings(job.Info)
		oldStatus, oldSourceState := job.Status, job.SourceState
		if job.SourceState == "sending" {
			job.SourceState = "unknown"
			job.Error = ErrSourceOperationUnknown.Error()
		}
		if job.Status == "downloading" {
			// A real interrupted Range download retained more content than the last
			// measured checkpoint. Preserve those counts, but do not present them
			// as complete: the lost process may have uncommitted meter increments.
			if job.TransferStats != nil {
				job.TransferStats.Partial = true
			}
			job.Status = "pending"
			job.Error = "服务重启，等待扫描确认后恢复"
		}
		d.queue.jobs[id] = &job
		if oldStatus != job.Status || oldSourceState != job.SourceState {
			evidence, _ := json.Marshal(struct {
				PreviousStatus       string `json:"previous_status"`
				RecoveredStatus      string `json:"recovered_status"`
				PreviousSourceState  string `json:"previous_source_state,omitempty"`
				RecoveredSource      string `json:"recovered_source_state,omitempty"`
				IncompleteStatistics bool   `json:"transfer_statistics_may_be_incomplete,omitempty"`
			}{oldStatus, job.Status, oldSourceState, job.SourceState, oldStatus == "downloading" && job.TransferStats != nil})
			recoveredJobs = append(recoveredJobs, &job)
			recoveryEvents = append(recoveryEvents, taskstore.Event{Kind: "startup_recovery", AttemptID: job.AttemptID, OperationID: job.SourceOperationID, EvidenceType: "local_restart_recovery", Evidence: evidence})
		}
		// Restored records are all inactive, and the derived task map starts empty.
		// Keep it lazy instead of locking it once per persisted record.
		return nil
	})
	if err != nil {
		store.Close()
		d.cancel()
		return nil, err
	}
	for i, job := range recoveredJobs {
		if err = d.queue.persistWithEvent(job, recoveryEvents[i]); err != nil {
			store.Close()
			d.cancel()
			return nil, err
		}
	}
	d.queue.rebuildSourceBlocks()
	if err = d.loadIntakes(); err != nil {
		store.Close()
		d.cancel()
		return nil, err
	}
	if _, err = store.LoadActiveLocalDirectoryMigration(); err == nil {
		d.queue.maintenance = true
		d.localMigrationPending = true
	} else if err != sql.ErrNoRows {
		store.Close()
		d.cancel()
		return nil, err
	}
	return d, nil
}

// canonicalJobStatus lets the runtime share a persisted status value instead
// of retaining a separate decoded string for every job.
func canonicalJobStatus(status string) (string, bool) {
	switch status {
	case "pending":
		return "pending", true
	case "downloading":
		return "downloading", true
	case "paused":
		return "paused", true
	case "retry_wait":
		return "retry_wait", true
	case "auth_wait":
		return "auth_wait", true
	case "failed":
		return "failed", true
	case "done":
		return "done", true
	case "cancelled":
		return "cancelled", true
	case "source_unknown":
		return "source_unknown", true
	default:
		return status, false
	}
}

func canonicalJobMode(mode DownloadMode) (DownloadMode, bool) {
	switch mode {
	case DownloadModeKeep:
		return DownloadModeKeep, true
	case DownloadModeDelete:
		return DownloadModeDelete, true
	default:
		return mode, false
	}
}

// shareFileInfoStrings reuses the path bytes for ServerFilename when it is
// exactly the path basename. FileInfo values are immutable after registration.
func shareFileInfoStrings(info baiduapi.FileInfo) baiduapi.FileInfo {
	if info.Path != "" && info.ServerFilename != "" && path.Base(info.Path) == info.ServerFilename {
		info.ServerFilename = info.Path[len(info.Path)-len(info.ServerFilename):]
	}
	return info
}

func retainFileInfo(info baiduapi.FileInfo) *baiduapi.FileInfo {
	return &info
}

func canRebaseUnstartedJob(job *queueJob, active bool) bool {
	return !active && job.SourceState == "" && job.SourceOperationID == "" && job.SourceAttemptID == "" &&
		job.Attempts == 0 && job.Failures == 0 && (job.Status == "pending" || job.Status == "auth_wait" || job.Status == "paused")
}

func canResolveChangedSourceIdentity(job *queueJob, active bool) bool {
	return !active && job.SourceState == "changed" && job.SourceOperationID == "" && job.SourceAttemptID == "" && job.Status == "failed"
}

func canRebaseReturnedSourceJob(job *queueJob, active bool) bool {
	return !active && job.SourceState == "" && job.SourceOperationID == "" && job.SourceAttemptID == "" &&
		job.Status == "failed" && job.Attempts == 0 && job.Failures == 0 && job.Error == taskSourceMissingError
}

func sameQueuedCloudContent(a, b baiduapi.FileInfo) bool {
	if a.FsID != b.FsID || a.IsDir != b.IsDir {
		return false
	}
	if a.IsDir == 1 {
		return true
	}
	return a.Size == b.Size && a.Mtime == b.Mtime && (a.MD5 == "" || strings.EqualFold(a.MD5, b.MD5))
}

func queueIdentityEvidence(reason string, previousStatus string, previous, current baiduapi.FileInfo, attempts, failures int) json.RawMessage {
	body, _ := json.Marshal(struct {
		Reason         string            `json:"reason"`
		PreviousStatus string            `json:"previous_status"`
		PreviousInfo   baiduapi.FileInfo `json:"previous_info"`
		CurrentInfo    baiduapi.FileInfo `json:"current_info"`
		Attempts       int               `json:"attempts"`
		Failures       int               `json:"failures"`
	}{reason, previousStatus, previous, current, attempts, failures})
	return body
}

// applyConfirmedCloudIdentityMutation keeps an untouched queue job attached to
// the same provider object after its path changes. The caller already verified
// the provider response and holds queue maintenance, so no worker can claim it.
func (q *scheduler) applyConfirmedCloudIdentityMutation(before, after baiduapi.FileInfo, operationID string, responseObservedAt, cloudObservedAt time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobs[before.FsID]
	active := q.active[before.FsID] != nil
	if job == nil || (!canRebaseUnstartedJob(job, active) && !canRebaseReturnedSourceJob(job, active)) ||
		!sameQueuedCloudContent(job.Info, before) || before.FsID != after.FsID || before.IsDir != after.IsDir {
		return nil
	}
	previous := job.Info
	previousStatus, previousError, previousUpdated := job.Status, job.Error, job.Updated
	if canRebaseReturnedSourceJob(job, active) {
		job.Error = "网盘源项已重新进入监控目录，可手动重试"
	}
	q.removeCandidate(job)
	job.Info = shareFileInfoStrings(after)
	reason, evidenceType := "confirmed_cloud_mutation", "confirmed_cloud_mutation"
	if previousError == taskSourceMissingError {
		reason, evidenceType = "source_returned_after_confirmed_mutation", "source_returned_after_confirmed_mutation"
	}
	evidence := queueIdentityEvidence(reason, previousStatus, previous, job.Info, job.Attempts, job.Failures)
	responseAt, observedAt := responseObservedAt.UTC(), cloudObservedAt.UTC()
	event := taskstore.Event{Kind: "task_cloud_identity_updated", OperationID: operationID, ResponseObservedAtUTC: &responseAt, CloudObservedAtUTC: &observedAt, EvidenceType: evidenceType, Evidence: evidence}
	if err := q.persistWithEvent(job, event); err != nil {
		job.Info, job.Status, job.Error, job.Updated = previous, previousStatus, previousError, previousUpdated
		return err
	}
	q.owner.taskCatalogGeneration.Add(1)
	q.reflect(job)
	return nil
}

func (q *scheduler) cancelConfirmedCloudDelete(info baiduapi.FileInfo, operationID string, responseObservedAt time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobs[info.FsID]
	if job == nil || !canRebaseUnstartedJob(job, q.active[info.FsID] != nil) || !sameQueuedCloudContent(job.Info, info) ||
		job.Info.Path != info.Path || job.Info.ServerFilename != info.ServerFilename {
		return nil
	}
	previousStatus, previousError, previousNext, previousUpdated := job.Status, job.Error, job.NextAttempt, job.Updated
	previous := job.Info
	job.Status = "cancelled"
	job.Error = "网盘项目已由远端管理删除"
	job.NextAttempt = time.Time{}
	responseAt := responseObservedAt.UTC()
	evidence, _ := json.Marshal(struct {
		Reason         string            `json:"reason"`
		PreviousStatus string            `json:"previous_status"`
		Status         string            `json:"status"`
		PreviousInfo   baiduapi.FileInfo `json:"previous_info"`
		Attempts       int               `json:"attempts"`
		Failures       int               `json:"failures"`
	}{"confirmed_cloud_delete", previousStatus, job.Status, previous, job.Attempts, job.Failures})
	if err := q.persistWithEvent(job, taskstore.Event{Kind: "task_source_deleted_remotely", OperationID: operationID, ResponseObservedAtUTC: &responseAt, EvidenceType: "confirmed_cloud_delete", Evidence: evidence}); err != nil {
		job.Status, job.Error, job.NextAttempt, job.Updated = previousStatus, previousError, previousNext, previousUpdated
		return err
	}
	q.removeCandidate(job)
	q.owner.taskCatalogGeneration.Add(1)
	q.reflect(job)
	return nil
}

func (q *scheduler) persist(job *queueJob) error {
	return q.persistWithEvent(job, taskstore.Event{})
}

func (q *scheduler) persistWithEvent(job *queueJob, event taskstore.Event) error {
	q.owner.taskStatsGeneration.Add(1)
	job.Error = errorSummary(job.Error)
	job.Updated = time.Now()
	if q.store == nil {
		return nil
	}
	body, err := json.Marshal(job)
	if err == nil {
		err = q.store.SaveWithEvent(job.Info.FsID, body, event)
	}
	if err != nil {
		q.err = fmt.Errorf("队列保存失败，停止调度: %w", err)
		q.owner.cancel()
	}
	return err
}

func (q *scheduler) persistNewJobs(jobs []*queueJob, cloudObservedAt time.Time) error {
	if len(jobs) == 0 {
		return nil
	}
	for start := 0; start < len(jobs); start += maxNewJobPersistenceBatch {
		end := min(start+maxNewJobPersistenceBatch, len(jobs))
		records := make([]taskstore.Record, 0, end-start)
		events := make([]taskstore.Event, 0, end-start)
		for _, job := range jobs[start:end] {
			job.Error = errorSummary(job.Error)
			job.Updated = time.Now()
			if q.store == nil {
				continue
			}
			body, err := json.Marshal(job)
			if err != nil {
				q.err = fmt.Errorf("队列序列化失败，停止调度: %w", err)
				q.owner.cancel()
				return q.err
			}
			records = append(records, taskstore.Record{ID: job.Info.FsID, Data: body})
			observedAt := time.Now().UTC()
			evidence, evidenceErr := json.Marshal(job.Info)
			if evidenceErr != nil {
				q.err = fmt.Errorf("队列证据序列化失败，停止调度: %w", evidenceErr)
				q.owner.cancel()
				return q.err
			}
			observed := cloudObservedAt
			if observed.IsZero() {
				observed = observedAt
			}
			events = append(events, taskstore.Event{Kind: "cloud_file_observed", EvidenceType: "baidu_file_list", CloudObservedAtUTC: &observed, Evidence: evidence})
		}
		if q.store != nil {
			if err := q.store.SaveBatchWithEvents(records, events); err != nil {
				q.err = fmt.Errorf("队列批量保存失败，停止调度: %w", err)
				q.owner.cancel()
				return q.err
			}
		}
		// Publish each committed batch immediately so a later batch failure
		// cannot hide jobs that are already durable in SQLite from the task view.
		for _, job := range jobs[start:end] {
			q.jobs[job.Info.FsID] = job
			q.owner.taskCatalogGeneration.Add(1)
			q.reflect(job)
		}
		q.owner.taskStatsGeneration.Add(uint64(end - start))
	}
	return nil
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
	if job.Status != "downloading" {
		// queueJob is the durable source for inactive task views; retain Task only
		// while it carries live transfer progress that queueJob cannot represent.
		delete(d.tasks, job.Info.FsID)
		return
	}
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
	// Callers hold q.mu; one queued notification per worker coalesces state changes.
	if !q.started {
		return
	}
	workers := q.owner.cfg.MaxParallel
	if workers <= 0 {
		workers = 2
	}
	if workers > cap(q.wake) {
		workers = cap(q.wake)
	}
	for i := 0; i < workers; i++ {
		select {
		case q.wake <- struct{}{}:
		default:
			return
		}
	}
}
func (q *scheduler) reconcile(files []baiduapi.FileInfo, epoch uint64) error {
	return q.reconcileObserved(files, epoch, time.Now().UTC())
}

func (q *scheduler) reconcileObserved(files []baiduapi.FileInfo, epoch uint64, cloudObservedAt time.Time) error {
	return q.reconcileMonitorObserved(files, epoch, cloudObservedAt, "")
}

func (q *scheduler) reconcileMonitorObserved(files []baiduapi.FileInfo, epoch uint64, cloudObservedAt time.Time, runID string) error {
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
	account := ""
	if validScan {
		account = q.owner.client.Fingerprint()
	}
	q.owner.clientMutex.RUnlock()
	if !validScan {
		return ErrAuthenticationWaiting
	}
	monitoredQueueDir := q.owner.QueueDirectory()
	eligible := make(map[int64]*baiduapi.FileInfo)
	newJobs := make([]*queueJob, 0)
	seen := make(map[int64]struct{}, len(files))
	for _, f := range files {
		f = shareFileInfoStrings(f)
		if q.isRemoteBlocked(f.FsID, f.Path) || q.blockedIntakes[f.Path] || q.blockedSources[f.Path] {
			continue
		}
		if strings.HasSuffix(f.ServerFilename, ".可删除") {
			continue
		}
		if f.FsID <= 0 {
			return fmt.Errorf("invalid root fs_id")
		}
		if _, ok := seen[f.FsID]; ok {
			return fmt.Errorf("duplicate root fs_id: %d", f.FsID)
		}
		seen[f.FsID] = struct{}{}
		old := q.jobs[f.FsID]
		if old != nil {
			if old.Info == f {
				eligible[f.FsID] = &old.Info
				continue
			}
			resolveChangedSource := canResolveChangedSourceIdentity(old, q.active[f.FsID] != nil)
			reactivateReturnedSource := canRebaseReturnedSourceJob(old, q.active[f.FsID] != nil) && sameQueuedCloudContent(old.Info, f)
			if (canRebaseUnstartedJob(old, q.active[f.FsID] != nil) && sameQueuedCloudContent(old.Info, f)) || resolveChangedSource || reactivateReturnedSource {
				previous, previousStatus := old.Info, old.Status
				previousSourceState, previousError, previousNextAttempt, previousUpdated := old.SourceState, old.Error, old.NextAttempt, old.Updated
				q.removeCandidate(old)
				old.Info = f
				reason := "successful_cloud_scan"
				if resolveChangedSource {
					reason = "resolved_changed_source_after_successful_scan"
					old.SourceState = ""
					old.Status = "pending"
					old.Error = ""
					old.NextAttempt = time.Time{}
				} else if reactivateReturnedSource {
					reason = "source_returned_after_successful_scan"
					old.Error = "网盘源项已重新进入监控目录，可手动重试"
				}
				evidence := queueIdentityEvidence(reason, previousStatus, previous, f, old.Attempts, old.Failures)
				observedAt := cloudObservedAt.UTC()
				if err := q.persistWithEvent(old, taskstore.Event{Kind: "task_cloud_identity_updated", CloudObservedAtUTC: &observedAt, EvidenceType: "successful_cloud_scan", Evidence: evidence}); err != nil {
					old.Info, old.Status, old.SourceState, old.Error, old.NextAttempt, old.Updated = previous, previousStatus, previousSourceState, previousError, previousNextAttempt, previousUpdated
					return err
				}
				q.owner.taskCatalogGeneration.Add(1)
				q.reflect(old)
				eligible[f.FsID] = &old.Info
				continue
			}
			// Preserve the original task policy/history when the same ID has
			// changed content or the prior task is not safe to rebase.
			eligible[f.FsID] = retainFileInfo(f)
			continue
		}
		job := &queueJob{Version: 1, Info: f, Mode: q.owner.GetDownloadMode(), Status: "pending", Created: time.Now()}
		newJobs = append(newJobs, job)
		eligible[f.FsID] = &job.Info
	}
	// Persist the full scan's new roots before publishing them to the worker
	// queue. The sources remain in the cloud queue until later post-processing.
	if err := q.persistNewJobs(newJobs, cloudObservedAt); err != nil {
		return err
	}
	// An individual observation remains meaningful even if a later step fails.
	// It is not a claim that the entire scan succeeded or downloading started.
	if runID != "" {
		for id, info := range eligible {
			job := q.jobs[id]
			if job == nil || job.IntakeID == "" || job.MonitorObservation != nil || job.Info != *info {
				continue
			}
			previousUpdated := job.Updated
			job.MonitorObservation = &QueueMonitorObservation{RunID: runID, Path: info.Path, ObservedAtUTC: cloudObservedAt.UTC().Format(time.RFC3339Nano)}
			evidence, _ := json.Marshal(job.MonitorObservation)
			observedAt := cloudObservedAt.UTC()
			if err := q.persistWithEvent(job, taskstore.Event{Kind: "intake_root_observed", EvidenceType: "queue_monitor_file_list", CloudObservedAtUTC: &observedAt, Evidence: evidence}); err != nil {
				job.MonitorObservation = nil
				job.Updated = previousUpdated
				return err
			}
		}
	}
	q.eligible = eligible
	q.clearCandidates()
	for id, job := range q.jobs {
		// A quarantined target was deliberately excluded above, not missing.
		if q.isRemoteBlocked(job.Info.FsID, job.Info.Path) || q.blockedIntakes[job.Info.Path] || q.blockedSources[job.Info.Path] {
			continue
		}
		if job.Status == "auth_wait" && eligible[id] != nil && *eligible[id] == job.Info && q.active[id] == nil {
			previousStatus, previousError, previousNextAttempt, previousUpdated := job.Status, job.Error, job.NextAttempt, job.Updated
			var clearedRetryDeadline *time.Time
			if !job.NextAttempt.IsZero() {
				value := job.NextAttempt.UTC()
				clearedRetryDeadline = &value
			}
			job.Status = "pending"
			job.Error = ""
			job.NextAttempt = time.Time{}
			evidence, _ := json.Marshal(struct {
				PreviousStatus            string     `json:"previous_status"`
				Status                    string     `json:"status"`
				Reason                    string     `json:"reason"`
				ScanEpoch                 uint64     `json:"scan_epoch"`
				ClearedRetryDeadlineAtUTC *time.Time `json:"cleared_retry_deadline_at_utc,omitempty"`
			}{previousStatus, job.Status, "authentication_ready_after_successful_scan", epoch, clearedRetryDeadline})
			observedAt := cloudObservedAt.UTC()
			if err := q.persistWithEvent(job, taskstore.Event{Kind: "task_recovered_after_scan", CloudObservedAtUTC: &observedAt, EvidenceType: "successful_cloud_scan", Evidence: evidence}); err != nil {
				job.Status, job.Error, job.NextAttempt, job.Updated = previousStatus, previousError, previousNextAttempt, previousUpdated
				return err
			}
			q.reflect(job)
		}
		if _, present := eligible[id]; present || q.active[id] != nil {
			q.enqueueCandidate(job)
			continue
		}
		if job.Status == "pending" || job.Status == "retry_wait" || job.Status == "auth_wait" {
			queueRoot := path.Clean(q.owner.QueueDirectory())
			jobPath := path.Clean(job.Info.Path)
			insideQueue := jobPath == queueRoot || strings.HasPrefix(jobPath, strings.TrimSuffix(queueRoot, "/")+"/")
			if q.store != nil && insideQueue && canRebaseUnstartedJob(job, q.active[id] != nil) {
				receipt, err := q.store.ConfirmedRemoteDeleteForSource(account, job.Info.FsID, job.Info.Path, job.Created)
				if err != nil {
					return fmt.Errorf("读取已确认的远端删除凭据失败: %w", err)
				}
				if receipt != nil {
					var prepared struct {
						Expected baiduapi.FileInfo `json:"expected"`
					}
					if json.Unmarshal(receipt.PreparedEvidence, &prepared) == nil &&
						receipt.AccountFingerprint == account && receipt.ObjectID == job.Info.FsID &&
						receipt.SourcePath == job.Info.Path && prepared.Expected.Path == job.Info.Path &&
						prepared.Expected.ServerFilename == job.Info.ServerFilename &&
						sameQueuedCloudContent(job.Info, prepared.Expected) {
						previousStatus, previousError, previousNext, previousUpdated := job.Status, job.Error, job.NextAttempt, job.Updated
						previous := job.Info
						job.Status = "cancelled"
						job.Error = "网盘项目已由远端管理删除"
						job.NextAttempt = time.Time{}
						responseAt := receipt.ResponseObservedAtUTC.UTC()
						evidence, _ := json.Marshal(struct {
							Reason         string            `json:"reason"`
							PreviousStatus string            `json:"previous_status"`
							Status         string            `json:"status"`
							PreviousInfo   baiduapi.FileInfo `json:"previous_info"`
							Attempts       int               `json:"attempts"`
							Failures       int               `json:"failures"`
						}{"recovered_confirmed_cloud_delete", previousStatus, job.Status, previous, job.Attempts, job.Failures})
						if err := q.persistWithEvent(job, taskstore.Event{Kind: "task_source_deleted_remotely", OperationID: receipt.OperationID, ResponseObservedAtUTC: &responseAt, EvidenceType: "confirmed_cloud_delete_recovered", Evidence: evidence}); err != nil {
							job.Status, job.Error, job.NextAttempt, job.Updated = previousStatus, previousError, previousNext, previousUpdated
							return err
						}
						q.removeCandidate(job)
						q.owner.taskCatalogGeneration.Add(1)
						q.reflect(job)
						continue
					}
				}
			}
			previousStatus, previousError, previousNext, previousUpdated := job.Status, job.Error, job.NextAttempt, job.Updated
			job.Status = "failed"
			job.Error = taskSourceMissingError
			job.NextAttempt = time.Time{}
			evidence, _ := json.Marshal(struct {
				PreviousStatus    string `json:"previous_status"`
				Status            string `json:"status"`
				Reason            string `json:"reason"`
				ScanEpoch         uint64 `json:"scan_epoch"`
				MonitoredQueueDir string `json:"monitored_queue_dir"`
				Attempts          int    `json:"attempts"`
				Failures          int    `json:"failures"`
			}{previousStatus, job.Status, "source_not_eligible_after_successful_scan", epoch, monitoredQueueDir, job.Attempts, job.Failures})
			observedAt := cloudObservedAt.UTC()
			if err := q.persistWithEvent(job, taskstore.Event{Kind: "task_source_missing", CloudObservedAtUTC: &observedAt, EvidenceType: "successful_cloud_scan", Evidence: evidence}); err != nil {
				job.Status, job.Error, job.NextAttempt, job.Updated = previousStatus, previousError, previousNext, previousUpdated
				q.reflect(job)
				return err
			}
			q.reflect(job)
		}
		q.enqueueCandidate(job)
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
func (q *scheduler) claim() (*queueJob, context.Context, time.Duration) {
	return q.claimAt(time.Now())
}

func (q *scheduler) claimAt(now time.Time) (*queueJob, context.Context, time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || q.maintenance || q.userPaused || q.err != nil || q.owner.ctx.Err() != nil {
		return nil, nil, 0
	}
	if !q.owner.AuthenticationReady() {
		q.parkAuthenticationWaiting("credentials_unavailable", nil)
		return nil, nil, 0
	}
	chosen := q.popReadyCandidate(now)
	if chosen == nil {
		if len(q.delayed) > 0 {
			wait := q.delayed[0].job.NextAttempt.Sub(now)
			if wait <= 0 {
				wait = time.Nanosecond
			} else if wait > retryClockRecheckInterval {
				// Persisted retry deadlines use wall time. Recompute long waits
				// periodically so clock adjustments are reflected while workers sleep.
				wait = retryClockRecheckInterval
			}
			return nil, nil, wait
		}
		return nil, nil, 0
	}
	attemptID, err := newSourceOperationID()
	if err != nil {
		q.err = fmt.Errorf("下载尝试 ID 生成失败，停止调度: %w", err)
		q.owner.cancel()
		return nil, nil, 0
	}
	previousStatus, previousAttempts, previousAttemptID := chosen.Status, chosen.Attempts, chosen.AttemptID
	previousStats := chosen.TransferStats
	if chosen.TransferStats == nil && chosen.Attempts == 0 {
		// A new attempt can reuse local files without ever opening a response body.
		chosen.TransferStats = &TransferStatistics{RecordedAtUTC: now.UTC()}
	}
	previousNextAttempt, previousError, previousStorage, previousUpdated := chosen.NextAttempt, chosen.Error, chosen.LocalStorage, chosen.Updated
	chosen.Status = "downloading"
	chosen.Attempts++
	chosen.AttemptID = attemptID
	chosen.NextAttempt = time.Time{}
	chosen.LocalStorage = nil
	attemptEvidence, _ := json.Marshal(struct {
		Attempt int    `json:"attempt"`
		Status  string `json:"status"`
	}{chosen.Attempts, chosen.Status})
	if err := q.persistWithEvent(chosen, taskstore.Event{Kind: "download_attempt_started", AttemptID: attemptID, EvidenceType: "local_scheduler_claim", Evidence: attemptEvidence}); err != nil {
		chosen.Status, chosen.Attempts, chosen.AttemptID = previousStatus, previousAttempts, previousAttemptID
		chosen.TransferStats = previousStats
		chosen.NextAttempt, chosen.Error, chosen.LocalStorage, chosen.Updated = previousNextAttempt, previousError, previousStorage, previousUpdated
		return nil, nil, 0
	}
	ctx, cancel := context.WithCancel(q.owner.ctx)
	q.active[chosen.Info.FsID] = cancel
	q.owner.taskMutex.Lock()
	q.owner.inFlight[chosen.Info.FsID] = true
	q.owner.taskMutex.Unlock()
	q.reflect(chosen)
	copy := *chosen
	return &copy, ctx, 0
}
func (q *scheduler) worker() {
	defer q.wg.Done()
	for {
		job, ctx, wait := q.claim()
		if job != nil {
			err := q.owner.runRoot(ctx, job.Info, job.Mode)
			q.complete(job, err)
			continue
		}
		var retryTimer *time.Timer
		var retryC <-chan time.Time
		if wait > 0 {
			retryTimer = time.NewTimer(wait)
			retryC = retryTimer.C
		}
		select {
		case <-q.owner.ctx.Done():
			if retryTimer != nil {
				retryTimer.Stop()
			}
			return
		case <-q.wake:
		case <-retryC:
		}
		if retryTimer != nil && !retryTimer.Stop() {
			select {
			case <-retryTimer.C:
			default:
			}
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
	close(q.activeChanged)
	q.activeChanged = make(chan struct{})
	q.owner.taskMutex.Lock()
	delete(q.owner.inFlight, id)
	q.owner.taskMutex.Unlock()
	job := q.jobs[id]
	previouslyPersisted := *job
	if job.SourceState == "sending" || job.SourceState == "unknown" {
		job.SourceState = "unknown"
		job.Status = "source_unknown"
		job.Error = ErrSourceOperationUnknown.Error()
		job.NextAttempt = time.Time{}
	} else if job.SourceState == "confirmed" {
		job.Status = "done"
		job.Error = ""
	} else if job.Status == "paused" {
		// A durable user pause outranks the context.Canceled result from the
		// active transfer. Resume will retry interrupted work through the normal
		// partial verification and scheduler path.
		if job.PausedFromStatus == "downloading" {
			job.PausedFromStatus = "pending"
		}
		job.Error = ""
		job.NextAttempt = time.Time{}
	} else if job.Status != "cancelled" {
		job.Status = "done"
		job.Error = ""
		job.NextAttempt = time.Time{}
		if err != nil {
			job.Error = err.Error()
			job.Status = "failed"
			if errors.Is(err, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) {
				job.Status = "auth_wait"
				// Rotation may already have triggered a fresh scan before the old
				// request returns 401. Do not lose that scan's admission and wait
				// another full scan interval for an already verified new credential.
				fingerprint, rejected := baiduapi.AuthenticationFingerprint(err)
				q.owner.clientMutex.RLock()
				eligibleInfo := q.eligible[id]
				if rejected && fingerprint != "" && q.owner.authManaged && q.owner.authenticationReadyLocked() &&
					fingerprint != q.owner.client.Fingerprint() && eligibleInfo != nil && *eligibleInfo == job.Info {
					job.Status = "pending"
					job.Error = ""
				}
				q.owner.clientMutex.RUnlock()
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
					// UTC strips Go's process-local monotonic reading so the durable
					// wall-clock deadline responds to later system clock adjustments.
					job.NextAttempt = time.Now().UTC().Add(delay)
				}
			}
		}
	}
	job.LocalStorage = nil
	if job.Status == "failed" {
		job.LocalStorage = localStorageDetails(err)
	}
	var nextAttempt *time.Time
	if job.Status == "retry_wait" && !job.NextAttempt.IsZero() {
		utc := job.NextAttempt.UTC()
		nextAttempt = &utc
	}
	providerOperation, providerHTTPStatus, providerErrorCode := "", 0, 0
	var responseObservedAt *time.Time
	if providerErr := apiError(err); providerErr != nil {
		providerOperation, providerHTTPStatus, providerErrorCode = providerErr.Operation, providerErr.Status, providerErr.Code
		if providerErr.Status != 0 {
			observedAt := time.Now().UTC()
			responseObservedAt = &observedAt
		}
	}
	resultEvidence, _ := json.Marshal(struct {
		Status             string                `json:"status"`
		OutcomeClass       string                `json:"outcome_class"`
		Attempts           int                   `json:"attempts"`
		Failures           int                   `json:"failures"`
		SourceState        string                `json:"source_state,omitempty"`
		NextAttemptAtUTC   *time.Time            `json:"next_attempt_at_utc"`
		ProviderOperation  string                `json:"provider_operation,omitempty"`
		ProviderHTTPStatus int                   `json:"provider_http_status,omitempty"`
		ProviderErrorCode  int                   `json:"provider_error_code,omitempty"`
		ErrorClass         string                `json:"error_class,omitempty"`
		LocalStorage       *LocalStorageEvidence `json:"local_storage,omitempty"`
	}{job.Status, downloadOutcomeClass(job, err, q.stopping), job.Attempts, job.Failures, job.SourceState, nextAttempt, providerOperation, providerHTTPStatus, providerErrorCode, errorClass(err), localStorageDetails(err)})
	if saveErr := q.persistWithEvent(job, taskstore.Event{Kind: "download_attempt_finished", AttemptID: executed.AttemptID, ResponseObservedAtUTC: responseObservedAt, EvidenceType: "local_attempt_result", Evidence: resultEvidence}); saveErr != nil {
		if job.SourceState == "sending" || job.SourceState == "unknown" {
			job.Status = "source_unknown"
			job.Error = ErrSourceOperationUnknown.Error() + "; " + q.err.Error()
		} else if previouslyPersisted.SourceState == "confirmed" || previouslyPersisted.Status == "paused" || previouslyPersisted.Status == "cancelled" || previouslyPersisted.Status == "auth_wait" || previouslyPersisted.Status == "pending" {
			*job = previouslyPersisted
		} else {
			job.Status = "failed"
			job.Error = q.err.Error()
		}
	}
	if executed.Info.IsDir == 1 && (job.Status == "paused" || (job.Status == "cancelled" && job.Error == "用户取消")) {
		q.owner.reconcileInterruptedDirectoryTasks(job.Info, job.Status)
	}
	q.reflect(job)
	q.enqueueCandidate(job)
	q.signal()
}

// BeginMaintenance stops new claims and waits for active tasks to finish.
// The caller must call EndMaintenance after its external operation.
func (d *Downloader) BeginMaintenance(ctx context.Context) error {
	return d.beginMaintenance(ctx, false)
}

func (d *Downloader) beginUploadMaintenance(ctx context.Context) error {
	return d.beginMaintenance(ctx, true)
}

func (d *Downloader) beginMaintenance(ctx context.Context, upload bool) error {
	q := d.queue
	q.mu.Lock()
	if upload && !d.validUploadLease(ctx) {
		q.mu.Unlock()
		return fmt.Errorf("上传租约已失效")
	}
	if q.stopping || q.err != nil {
		q.mu.Unlock()
		return fmt.Errorf("download queue is unavailable")
	}
	if q.maintenance {
		q.mu.Unlock()
		return fmt.Errorf("download queue is already under maintenance")
	}
	q.maintenance = true
	q.uploadMaintenance = upload
	q.signal()
	q.mu.Unlock()
	for {
		q.mu.Lock()
		if len(q.active) == 0 {
			q.mu.Unlock()
			return nil
		}
		changed := q.activeChanged
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			d.EndMaintenance()
			return ctx.Err()
		case <-changed:
		}
	}
}

func (d *Downloader) EndMaintenance() {
	q := d.queue
	q.mu.Lock()
	q.maintenance = false
	q.uploadMaintenance = false
	q.signal()
	q.mu.Unlock()
}

func downloadOutcomeClass(job *queueJob, err error, stopping bool) string {
	if job.Status == "paused" {
		return "user_paused"
	}
	var storageErr *localStorageSpaceError
	if errors.As(err, &storageErr) {
		return storageErr.Kind
	}
	switch job.Status {
	case "done":
		return "succeeded"
	case "auth_wait":
		return "authentication_wait"
	case "retry_wait":
		return "retry_scheduled"
	case "cancelled":
		return "cancelled"
	case "source_unknown":
		return "source_operation_unknown"
	case "failed":
		if baiduapi.IsRetryable(err) {
			return "retry_exhausted"
		}
		return "non_retryable"
	case "pending":
		if baiduapi.IsAuthenticationError(err) && job.Error == "" {
			return "authentication_recovered"
		}
		if stopping || errors.Is(err, context.Canceled) {
			return "interrupted"
		}
	}
	return "unknown"
}

func apiError(err error) *baiduapi.APIError {
	var apiErr *baiduapi.APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return nil
}

func errorClass(err error) string {
	if err == nil {
		return ""
	}
	var storageErr *localStorageSpaceError
	if errors.As(err, &storageErr) {
		switch storageErr.Kind {
		case localStorageFull:
			return localStorageFull
		default:
			if storageErr.AvailableBytes < 0 {
				return "local_storage_space_unavailable"
			}
			return "local_storage_insufficient"
		}
	}
	if apiError(err) != nil {
		return "provider_api_error"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "incomplete_stream"
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return "filesystem_error"
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		if networkErr.Timeout() {
			return "network_timeout"
		}
		return "network_error"
	}
	return "other_error"
}

var ErrTaskNotFound = errors.New("task not found")
var ErrTaskConflict = errors.New("task cannot be changed in its current state")

func (d *Downloader) Cancel(id int64) error {
	if !d.pathOperations.TryRLock() {
		return ErrTaskConflict
	}
	defer d.pathOperations.RUnlock()
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
	if job.Status == "done" || job.SourceState != "" {
		return ErrTaskConflict
	}
	previousStatus := job.Status
	previousError, previousNext := job.Error, job.NextAttempt
	previousPausedFrom, previousUpdated := job.PausedFromStatus, job.Updated
	var previousNextUTC *time.Time
	if !previousNext.IsZero() {
		value := previousNext.UTC()
		previousNextUTC = &value
	}
	attempts, failures := job.Attempts, job.Failures
	job.Status = "cancelled"
	job.Error = "用户取消"
	job.NextAttempt = time.Time{}
	job.PausedFromStatus = ""
	evidence, _ := json.Marshal(struct {
		PreviousStatus           string     `json:"previous_status"`
		Status                   string     `json:"status"`
		Attempts                 int        `json:"attempts"`
		Failures                 int        `json:"failures"`
		Reason                   string     `json:"reason"`
		PreviousNextAttemptAtUTC *time.Time `json:"previous_next_attempt_at_utc,omitempty"`
	}{previousStatus, job.Status, attempts, failures, "user_request", previousNextUTC})
	if err := q.persistWithEvent(job, taskstore.Event{Kind: "task_cancelled", EvidenceType: "local_user_action", Evidence: evidence}); err != nil {
		job.Status, job.Error, job.NextAttempt = previousStatus, previousError, previousNext
		job.PausedFromStatus, job.Updated = previousPausedFrom, previousUpdated
		return err
	}
	q.removeCandidate(job)
	if cancel := q.active[id]; cancel != nil {
		cancel()
	}
	if job.Info.IsDir == 1 {
		d.reconcileInterruptedDirectoryTasks(job.Info, "cancelled")
	}
	q.reflect(job)
	return nil
}

// Reconcile only descendants interrupted by the user's root pause/cancel.
// Child tasks are an in-memory view; the durable queue state remains on root.
func (d *Downloader) reconcileInterruptedDirectoryTasks(root baiduapi.FileInfo, status string) {
	prefix := strings.TrimSuffix(root.Path, "/") + "/"
	changed := false
	d.taskMutex.Lock()
	for _, task := range d.tasks {
		if !strings.HasPrefix(task.FileInfo.Path, prefix) {
			continue
		}
		interrupted := task.Status == "failed" && task.Error == context.Canceled.Error()
		if status == "paused" && interrupted {
			task.Status = "paused"
		} else if status == "cancelled" && (task.Status == "paused" || interrupted) {
			task.Status = "cancelled"
		} else {
			continue
		}
		task.Error = ""
		task.Speed = 0
		if status == "cancelled" {
			task.FinishTime = time.Now()
		} else {
			task.FinishTime = time.Time{}
		}
		changed = true
	}
	if changed {
		d.taskStatsGeneration.Add(1)
	}
	d.taskMutex.Unlock()
}

func (d *Downloader) Retry(id int64) error {
	if !d.pathOperations.TryRLock() {
		return ErrTaskConflict
	}
	defer d.pathOperations.RUnlock()
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobs[id]
	if job == nil {
		return ErrTaskNotFound
	}
	eligibleInfo := q.eligible[id]
	if q.stopping || q.err != nil || job.SourceState != "" || q.blockedSources[job.Info.Path] || q.blockedIntakes[job.Info.Path] || !d.AuthenticationReady() || q.active[id] != nil || (job.Status != "failed" && job.Status != "cancelled") || eligibleInfo == nil || *eligibleInfo != job.Info {
		return ErrTaskConflict
	}
	previousStatus := job.Status
	previousAttempts, previousFailures := job.Attempts, job.Failures
	previousError, previousNext, previousStorage, previousUpdated := job.Error, job.NextAttempt, job.LocalStorage, job.Updated
	job.Status = "pending"
	job.Attempts = 0
	job.Failures = 0
	job.Error = ""
	job.NextAttempt = time.Time{}
	job.LocalStorage = nil
	evidence, _ := json.Marshal(struct {
		PreviousStatus   string `json:"previous_status"`
		Status           string `json:"status"`
		PreviousAttempts int    `json:"previous_attempts"`
		Attempts         int    `json:"attempts"`
		PreviousFailures int    `json:"previous_failures"`
		Failures         int    `json:"failures"`
		Reason           string `json:"reason"`
	}{previousStatus, job.Status, previousAttempts, job.Attempts, previousFailures, job.Failures, "manual_retry"})
	if err := q.persistWithEvent(job, taskstore.Event{Kind: "task_retry_requested", EvidenceType: "local_user_action", Evidence: evidence}); err != nil {
		job.Status, job.Attempts, job.Failures = previousStatus, previousAttempts, previousFailures
		job.Error, job.NextAttempt, job.LocalStorage, job.Updated = previousError, previousNext, previousStorage, previousUpdated
		return err
	}
	q.reflect(job)
	q.enqueueCandidate(job)
	q.signal()
	return nil
}
func (d *Downloader) Shutdown(ctx context.Context) error {
	q := d.queue
	q.shutdownOnce.Do(func() {
		d.cancel()
		d.localMigrationMu.Lock()
		d.localMigrationShutdown = true
		d.localMigrationMu.Unlock()
		go func() {
			q.mu.Lock()
			q.stopping = true
			q.mu.Unlock()
			q.wg.Wait()
			d.localMigrationWG.Wait()
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

// QueueOperational reports whether the scheduler can accept and process queue work.
// Keep the underlying persistence error private to health diagnostics.
func (d *Downloader) QueueOperational() bool {
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	return d.queue.err == nil && !d.queue.stopping && d.ctx.Err() == nil
}
