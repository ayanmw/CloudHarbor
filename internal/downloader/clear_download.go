package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func clearableDownloadRecord(q *scheduler, job *queueJob) bool {
	return job != nil && q.active[job.Info.FsID] == nil && job.SourceState != "sending" && (job.Status == "done" || job.Status == "failed" || job.Status == "cancelled" || job.Status == "source_unknown")
}
func downloadClearToken(job *queueJob) string {
	body, _ := json.Marshal(job)
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

// ClearDownloadRecord discards this root's history and source quarantine only.
// It never calls the provider or removes local content. A later fresh scan may
// register the same identity under the then-current download policy.
func (d *Downloader) ClearDownloadRecord(ctx context.Context, id int64, token string) error {
	if !d.pathOperations.TryRLock() {
		return ErrTaskConflict
	}
	defer d.pathOperations.RUnlock()
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || q.err != nil || q.maintenance || q.uploadMaintenance || d.ctx.Err() != nil || q.store == nil {
		return ErrTaskConflict
	}
	job := q.jobs[id]
	if job == nil {
		return ErrTaskNotFound
	}
	if !clearableDownloadRecord(q, job) || len(token) != 64 || token != downloadClearToken(job) {
		return ErrTaskConflict
	}
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if err = q.store.ClearDownloadRecord(ctx, id, body); err != nil {
		if errors.Is(err, taskstore.ErrDownloadRecordChanged) || errors.Is(err, os.ErrNotExist) {
			return ErrTaskConflict
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		q.err = err
		return ErrQueueUnavailable
	}
	q.removeCandidate(job)
	delete(q.jobs, id)
	delete(q.eligible, id)
	delete(q.transferMeters, id)
	q.rebuildSourceBlocks()
	d.taskMutex.Lock()
	for taskID, task := range d.tasks {
		if taskID == id || job.Info.IsDir == 1 && !task.Root && strings.HasPrefix(task.FileInfo.Path, strings.TrimSuffix(job.Info.Path, "/")+"/") {
			delete(d.tasks, taskID)
		}
	}
	d.taskCatalogGeneration.Add(1)
	d.taskStatsGeneration.Add(1)
	d.taskMutex.Unlock()
	return nil
}
