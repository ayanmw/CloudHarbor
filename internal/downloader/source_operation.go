package downloader

import (
	"context"
	"errors"
	"path"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

var ErrSourceOperationUnknown = errors.New("网盘后处理结果未知，保持隔离，禁止重发")

func validSourceRecord(job *queueJob) bool {
	switch job.SourceState {
	case "":
		return job.Status != "source_unknown"
	case "sending", "unknown":
		return job.Status == "source_unknown"
	case "confirmed":
		return job.Status == "done"
	}
	return false
}
func (q *scheduler) rebuildSourceBlocks() {
	q.blockedSources = make(map[string]bool)
	for _, job := range q.jobs {
		if job.SourceState != "sending" && job.SourceState != "unknown" {
			continue
		}
		q.blockedSources[job.Info.Path] = true
		if job.Mode == DownloadModeKeep {
			q.blockedSources[path.Join(path.Dir(job.Info.Path), path.Base(job.Info.Path)+".可删除")] = true
		}
	}
}
func (d *Downloader) beginSourceOperation(ctx context.Context, info baiduapi.FileInfo) (bool, error) {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.blockedSources[info.Path] {
		return false, ErrSourceOperationUnknown
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if q.err != nil {
		return false, q.err
	}
	// Legacy nonpersistent callers have no recovery journal. Main always uses
	// OpenDownloader; its source requests require a registered durable root.
	if q.store == nil {
		return false, nil
	}
	job := q.jobs[info.FsID]
	if job == nil || job.Info != info || job.Status != "downloading" || q.stopping {
		return false, ErrTaskConflict
	}
	job.SourceState = "sending"
	job.Status = "source_unknown"
	job.Error = "网盘后处理请求即将发送；完成前禁止重发"
	job.NextAttempt = time.Time{}
	q.rebuildSourceBlocks()
	if err := q.persist(job); err != nil {
		return false, err
	}
	q.reflect(job)
	return true, nil
}
func (d *Downloader) finishSourceOperation(info baiduapi.FileInfo, operationErr error) error {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobs[info.FsID]
	if job == nil || job.SourceState != "sending" {
		return ErrTaskConflict
	}
	switch {
	case operationErr == nil:
		job.SourceState = "confirmed"
		job.Status = "done"
		job.Error = ""
	case errors.Is(operationErr, baiduapi.ErrSourceOperationUnconfirmed):
		job.SourceState = "unknown"
		job.Status = "source_unknown"
		job.Error = ErrSourceOperationUnknown.Error()
	default:
		job.SourceState = ""
		job.Status = "downloading"
		job.Error = ""
	}
	if err := q.persist(job); err != nil {
		// The old durable sending record must remain authoritative if saving a
		// receipt fails. Never let the in-memory state release the quarantine.
		job.SourceState = "unknown"
		job.Status = "source_unknown"
		job.Error = ErrSourceOperationUnknown.Error()
		q.rebuildSourceBlocks()
		q.reflect(job)
		return err
	}
	q.rebuildSourceBlocks()
	q.reflect(job)
	return operationErr
}
