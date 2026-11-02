package downloader

import (
	"path"
	"strings"
	"time"
)

// Only measured increments are durable; a restart never resumes an old clock.
type TransferStatistics struct {
	ActiveNanos   int64     `json:"active_ns"`
	NetworkBytes  int64     `json:"network_bytes"`
	RecordedAtUTC time.Time `json:"recorded_at_utc"`
	Partial       bool      `json:"partial,omitempty"`
}

type transferMeter struct {
	active      int
	last, saved time.Time
}

func copyTransferStatistics(s *TransferStatistics) *TransferStatistics {
	if s == nil {
		return nil
	}
	copy := *s
	return &copy
}

// Caller holds q.mu. Overlapping file transfers count once for the root.
func (q *scheduler) recordTransferStats(id, bytes int64, activeDelta int, now time.Time, force bool) error {
	job := q.jobs[id]
	if job == nil {
		return nil
	}
	if q.transferMeters == nil {
		q.transferMeters = make(map[int64]*transferMeter)
	}
	m := q.transferMeters[id]
	if m == nil {
		m = &transferMeter{}
		q.transferMeters[id] = m
	}
	if job.TransferStats == nil {
		job.TransferStats = &TransferStatistics{Partial: job.Attempts > 1}
	}
	if m.active > 0 && now.After(m.last) {
		job.TransferStats.ActiveNanos += now.Sub(m.last).Nanoseconds()
	}
	m.last = now
	m.active += activeDelta
	if bytes > 0 {
		job.TransferStats.NetworkBytes += bytes
	}
	job.TransferStats.RecordedAtUTC = now.UTC()
	if force || m.saved.IsZero() || now.Sub(m.saved) >= 5*time.Second {
		if err := q.persist(job); err != nil {
			return err
		}
		m.saved = now
	}
	return nil
}

// Body transfer only: exclude lookup, local verification and cloud mutations.
// Bytes exclude resumed offsets and local reuse; retransmissions are included.
func (d *Downloader) beginTransferStatistics(cloudPath string) func(int64, bool) error {
	q := d.queue
	q.mu.Lock()
	var rootID int64
	for id, job := range q.jobs {
		if cloudPath == job.Info.Path || strings.HasPrefix(cloudPath, strings.TrimSuffix(path.Clean(job.Info.Path), "/")+"/") {
			rootID = id
			break
		}
	}
	var startErr error
	if rootID != 0 {
		startErr = q.recordTransferStats(rootID, 0, 1, time.Now(), true)
	}
	q.mu.Unlock()
	closed := false
	return func(bytes int64, done bool) error {
		if rootID == 0 || closed {
			return startErr
		}
		q.mu.Lock()
		defer q.mu.Unlock()
		delta := 0
		if done {
			delta = -1
			closed = true
		}
		err := q.recordTransferStats(rootID, bytes, delta, time.Now(), done)
		if startErr != nil {
			return startErr
		}
		return err
	}
}
