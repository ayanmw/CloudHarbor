package downloader

import (
	"errors"
	"fmt"
	"time"
)

var ErrQueueUnavailable = errors.New("下载队列不可用")

type QueuePauseStatus struct {
	Paused       bool       `json:"paused"`
	Maintenance  bool       `json:"maintenance,omitempty"`
	UpdatedAtUTC *time.Time `json:"updated_at_utc,omitempty"`
	ActiveTasks  int        `json:"active_tasks"`
}

func (d *Downloader) QueuePauseStatus() QueuePauseStatus {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	return queuePauseStatusLocked(q)
}

func (d *Downloader) SetQueuePaused(paused bool) (QueuePauseStatus, error) {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || q.err != nil || d.ctx.Err() != nil {
		return QueuePauseStatus{}, ErrQueueUnavailable
	}
	if q.userPaused == paused {
		return queuePauseStatusLocked(q), nil
	}
	if q.store == nil {
		return QueuePauseStatus{}, fmt.Errorf("应用设置存储不可用")
	}
	updatedAt := time.Now().UTC()
	if err := d.saveAppSettings(map[string]any{
		"queue_paused":               paused,
		"queue_pause_updated_at_utc": updatedAt,
	}); err != nil {
		return QueuePauseStatus{}, err
	}
	q.userPaused = paused
	q.pauseUpdatedAtUTC = updatedAt
	q.signal()
	return queuePauseStatusLocked(q), nil
}

func queuePauseStatusLocked(q *scheduler) QueuePauseStatus {
	state := QueuePauseStatus{Paused: q.userPaused, Maintenance: q.maintenance, ActiveTasks: len(q.active)}
	if !q.pauseUpdatedAtUTC.IsZero() {
		updatedAt := q.pauseUpdatedAtUTC
		state.UpdatedAtUTC = &updatedAt
	}
	return state
}
