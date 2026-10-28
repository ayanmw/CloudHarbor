package downloader

import (
	"context"
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

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type queueJob struct {
	Version           int               `json:"version"`
	Info              baiduapi.FileInfo `json:"file"`
	Mode              DownloadMode      `json:"mode"`
	Status            string            `json:"status"`
	SourceState       string            `json:"source_state,omitempty"`
	SourceOperationID string            `json:"source_operation_id,omitempty"`
	SourceAttemptID   string            `json:"source_attempt_id,omitempty"`
	Attempts          int               `json:"attempts"`
	AttemptID         string            `json:"attempt_id,omitempty"`
	Failures          int               `json:"failures"`
	NextAttempt       time.Time         `json:"next_attempt,omitempty"`
	Error             string            `json:"error,omitempty"`
	Created           time.Time         `json:"created"`
	Updated           time.Time         `json:"updated"`
	candidate         *candidateEntry
}

const (
	maxNewJobPersistenceBatch = 1024
	retryClockRecheckInterval = 30 * time.Second
)

type scheduler struct {
	mu      sync.Mutex
	owner   *Downloader
	store   *taskstore.Store
	jobs    map[int64]*queueJob
	ready   candidateHeap
	delayed candidateHeap
	// eligible points into jobs when scan metadata matches, and owns a separate
	// snapshot only while a changed identity conflicts with an active/unknown job.
	eligible       map[int64]*baiduapi.FileInfo
	blockedIntakes map[string]bool
	blockedSources map[string]bool
	active         map[int64]context.CancelFunc
	wake           chan struct{}
	done           chan struct{}
	wg             sync.WaitGroup
	started        bool
	stopping       bool
	err            error
	closeErr       error
	shutdownOnce   sync.Once
	statsCache     taskStatsCache
	nameIndex      taskOrderIndex
	sizeIndex      taskOrderIndex
	progressIndex  progressTaskIndex
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
	return &scheduler{owner: d, jobs: make(map[int64]*queueJob), eligible: make(map[int64]*baiduapi.FileInfo), blockedIntakes: make(map[string]bool), active: make(map[int64]context.CancelFunc), wake: make(chan struct{}, 32), done: make(chan struct{})}
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
		job.Status = status
		job.Mode = mode
		job.Info = shareFileInfoStrings(job.Info)
		oldStatus, oldSourceState := job.Status, job.SourceState
		if job.SourceState == "sending" {
			job.SourceState = "unknown"
			job.Error = ErrSourceOperationUnknown.Error()
		}
		if job.Status == "downloading" {
			job.Status = "pending"
			job.Error = "服务重启，等待扫描确认后恢复"
		}
		d.queue.jobs[id] = &job
		if oldStatus != job.Status || oldSourceState != job.SourceState {
			evidence, _ := json.Marshal(struct {
				PreviousStatus      string `json:"previous_status"`
				RecoveredStatus     string `json:"recovered_status"`
				PreviousSourceState string `json:"previous_source_state,omitempty"`
				RecoveredSource     string `json:"recovered_source_state,omitempty"`
			}{oldStatus, job.Status, oldSourceState, job.SourceState})
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
	}
	q.owner.taskStatsGeneration.Add(uint64(len(jobs)))
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
	eligible := make(map[int64]*baiduapi.FileInfo)
	newJobs := make([]*queueJob, 0)
	seen := make(map[int64]struct{}, len(files))
	for _, f := range files {
		f = shareFileInfoStrings(f)
		if q.blockedIntakes[f.Path] || q.blockedSources[f.Path] {
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
		if old != nil && old.Info == f {
			eligible[f.FsID] = &old.Info
			continue
		}
		// A provider may show the same ID under another name while its old
		// mutation is unresolved. Preserve the journal and its original paths.
		if old != nil && (old.SourceState == "sending" || old.SourceState == "unknown") {
			eligible[f.FsID] = retainFileInfo(f)
			continue
		}
		if old != nil && q.active[f.FsID] != nil {
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
	for _, job := range newJobs {
		q.jobs[job.Info.FsID] = job
		q.owner.taskCatalogGeneration.Add(1)
		q.reflect(job)
	}
	q.eligible = eligible
	q.clearCandidates()
	for id, job := range q.jobs {
		// A quarantined target was deliberately excluded above, not missing.
		if q.blockedIntakes[job.Info.Path] || q.blockedSources[job.Info.Path] {
			continue
		}
		if job.Status == "auth_wait" && eligible[id] != nil && *eligible[id] == job.Info && q.active[id] == nil {
			previousStatus := job.Status
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
				return err
			}
			q.reflect(job)
		}
		if _, present := eligible[id]; present || q.active[id] != nil {
			q.enqueueCandidate(job)
			continue
		}
		if job.Status == "pending" || job.Status == "retry_wait" || job.Status == "auth_wait" {
			previousStatus := job.Status
			job.Status = "failed"
			job.Error = "源项目已离开队列或已被标记；请核对网盘与本地结果，恢复源位置后手动重试"
			job.NextAttempt = time.Time{}
			evidence, _ := json.Marshal(struct {
				PreviousStatus string `json:"previous_status"`
				Status         string `json:"status"`
				Reason         string `json:"reason"`
				ScanEpoch      uint64 `json:"scan_epoch"`
				Attempts       int    `json:"attempts"`
				Failures       int    `json:"failures"`
			}{previousStatus, job.Status, "source_not_eligible_after_successful_scan", epoch, job.Attempts, job.Failures})
			observedAt := cloudObservedAt.UTC()
			if err := q.persistWithEvent(job, taskstore.Event{Kind: "task_source_missing", CloudObservedAtUTC: &observedAt, EvidenceType: "successful_cloud_scan", Evidence: evidence}); err != nil {
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
	if q.stopping || q.err != nil || q.owner.ctx.Err() != nil {
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
	chosen.Status = "downloading"
	chosen.Attempts++
	chosen.AttemptID = attemptID
	chosen.NextAttempt = time.Time{}
	attemptEvidence, _ := json.Marshal(struct {
		Attempt int    `json:"attempt"`
		Status  string `json:"status"`
	}{chosen.Attempts, chosen.Status})
	if err := q.persistWithEvent(chosen, taskstore.Event{Kind: "download_attempt_started", AttemptID: attemptID, EvidenceType: "local_scheduler_claim", Evidence: attemptEvidence}); err != nil {
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
	q.owner.taskMutex.Lock()
	delete(q.owner.inFlight, id)
	q.owner.taskMutex.Unlock()
	job := q.jobs[id]
	if job.SourceState == "sending" || job.SourceState == "unknown" {
		job.SourceState = "unknown"
		job.Status = "source_unknown"
		job.Error = ErrSourceOperationUnknown.Error()
		job.NextAttempt = time.Time{}
	} else if job.SourceState == "confirmed" {
		job.Status = "done"
		job.Error = ""
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
		Status             string     `json:"status"`
		OutcomeClass       string     `json:"outcome_class"`
		Attempts           int        `json:"attempts"`
		Failures           int        `json:"failures"`
		SourceState        string     `json:"source_state,omitempty"`
		NextAttemptAtUTC   *time.Time `json:"next_attempt_at_utc"`
		ProviderOperation  string     `json:"provider_operation,omitempty"`
		ProviderHTTPStatus int        `json:"provider_http_status,omitempty"`
		ProviderErrorCode  int        `json:"provider_error_code,omitempty"`
		ErrorClass         string     `json:"error_class,omitempty"`
	}{job.Status, downloadOutcomeClass(job, err, q.stopping), job.Attempts, job.Failures, job.SourceState, nextAttempt, providerOperation, providerHTTPStatus, providerErrorCode, errorClass(err)})
	if saveErr := q.persistWithEvent(job, taskstore.Event{Kind: "download_attempt_finished", AttemptID: executed.AttemptID, ResponseObservedAtUTC: responseObservedAt, EvidenceType: "local_attempt_result", Evidence: resultEvidence}); saveErr != nil {
		job.Status = "failed"
		job.Error = q.err.Error()
		if job.SourceState == "sending" || job.SourceState == "unknown" {
			job.Status = "source_unknown"
			job.Error = ErrSourceOperationUnknown.Error() + "; " + q.err.Error()
		}
	}
	q.reflect(job)
	q.enqueueCandidate(job)
	q.signal()
}

func downloadOutcomeClass(job *queueJob, err error, stopping bool) string {
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
	attempts, failures := job.Attempts, job.Failures
	job.Status = "cancelled"
	job.Error = "用户取消"
	job.NextAttempt = time.Time{}
	evidence, _ := json.Marshal(struct {
		PreviousStatus string `json:"previous_status"`
		Status         string `json:"status"`
		Attempts       int    `json:"attempts"`
		Failures       int    `json:"failures"`
		Reason         string `json:"reason"`
	}{previousStatus, job.Status, attempts, failures, "user_request"})
	if err := q.persistWithEvent(job, taskstore.Event{Kind: "task_cancelled", EvidenceType: "local_user_action", Evidence: evidence}); err != nil {
		return err
	}
	q.removeCandidate(job)
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
	eligibleInfo := q.eligible[id]
	if q.stopping || q.err != nil || job.SourceState != "" || q.blockedSources[job.Info.Path] || q.blockedIntakes[job.Info.Path] || !d.AuthenticationReady() || q.active[id] != nil || (job.Status != "failed" && job.Status != "cancelled") || eligibleInfo == nil || *eligibleInfo != job.Info {
		return ErrTaskConflict
	}
	previousStatus := job.Status
	previousAttempts, previousFailures := job.Attempts, job.Failures
	job.Status = "pending"
	job.Attempts = 0
	job.Failures = 0
	job.Error = ""
	job.NextAttempt = time.Time{}
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
