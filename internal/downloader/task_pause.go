package downloader

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func (d *Downloader) PauseTask(id int64) error {
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
	if job.Status == "paused" {
		return nil
	}
	if job.SourceState != "" || (job.Status != "pending" && job.Status != "retry_wait" && job.Status != "downloading") {
		return ErrTaskConflict
	}
	previousStatus, previousFrom, previousError := job.Status, job.PausedFromStatus, job.Error
	previousUpdated, previousNext, previousStorage := job.Updated, job.NextAttempt, job.LocalStorage
	var previousNextUTC *time.Time
	if !previousNext.IsZero() {
		value := previousNext.UTC()
		previousNextUTC = &value
	}
	q.removeCandidate(job)
	job.PausedFromStatus = previousStatus
	job.Status = "paused"
	job.Error = ""
	observedAt := time.Now().UTC()
	evidence, _ := json.Marshal(struct {
		PreviousStatus   string     `json:"previous_status"`
		Status           string     `json:"status"`
		RecordedAtUTC    time.Time  `json:"recorded_at_utc"`
		NextAttemptAtUTC *time.Time `json:"next_attempt_at_utc,omitempty"`
	}{previousStatus, job.Status, observedAt, previousNextUTC})
	if err := q.persistWithEvent(job, taskstore.Event{Kind: "task_paused", EvidenceType: "local_user_action", Evidence: evidence}); err != nil {
		job.Status, job.PausedFromStatus, job.Error = previousStatus, previousFrom, previousError
		job.Updated, job.NextAttempt, job.LocalStorage = previousUpdated, previousNext, previousStorage
		q.enqueueCandidate(job)
		return fmt.Errorf("%w: %v", ErrQueueUnavailable, err)
	}
	if cancel := q.active[id]; cancel != nil {
		cancel()
	}
	q.reflect(job)
	q.signal()
	return nil
}

func (d *Downloader) ResumeTask(id int64) error {
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
	if job.Status != "paused" || job.SourceState != "" || q.active[id] != nil {
		return ErrTaskConflict
	}
	resumeStatus := job.PausedFromStatus
	if resumeStatus != "pending" && resumeStatus != "retry_wait" && resumeStatus != "downloading" {
		return ErrTaskConflict
	}
	previousError, previousUpdated := job.Error, job.Updated
	previousStatus, previousFrom, previousNext := job.Status, job.PausedFromStatus, job.NextAttempt
	var previousNextUTC *time.Time
	if !previousNext.IsZero() {
		value := previousNext.UTC()
		previousNextUTC = &value
	}
	if resumeStatus == "downloading" {
		resumeStatus = "pending"
	}
	job.Status = resumeStatus
	job.PausedFromStatus = ""
	job.Error = ""
	observedAt := time.Now().UTC()
	evidence, _ := json.Marshal(struct {
		PreviousStatus   string     `json:"previous_status"`
		Status           string     `json:"status"`
		ResumeFromStatus string     `json:"resume_from_status"`
		RecordedAtUTC    time.Time  `json:"recorded_at_utc"`
		NextAttemptAtUTC *time.Time `json:"next_attempt_at_utc,omitempty"`
	}{previousStatus, job.Status, previousFrom, observedAt, previousNextUTC})
	if err := q.persistWithEvent(job, taskstore.Event{Kind: "task_resumed", EvidenceType: "local_user_action", Evidence: evidence}); err != nil {
		job.Status, job.PausedFromStatus, job.Error = previousStatus, previousFrom, previousError
		job.Updated, job.NextAttempt = previousUpdated, previousNext
		return fmt.Errorf("%w: %v", ErrQueueUnavailable, err)
	}
	q.enqueueCandidate(job)
	q.reflect(job)
	q.signal()
	return nil
}
