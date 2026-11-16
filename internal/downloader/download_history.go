package downloader

import (
	"context"
	"fmt"
	"strings"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

type RootDownloadHistory struct {
	Source       string                             `json:"source"`
	Reason       string                             `json:"reason,omitempty"`
	Summary      *taskstore.DownloadManifestSummary `json:"summary,omitempty"`
	LiveProgress int64                              `json:"-"`
	LiveSpeed    int64                              `json:"-"`
	LiveActive   int                                `json:"-"`
}

// The workspace overlays only currently transferring files. Historical entries
// remain in SQLite; a result already committed is never counted a second time.
func (d *Downloader) GetTaskWorkspaceSnapshot(ctx context.Context) ([]*Task, error) {
	q := d.queue
	q.mu.Lock()
	d.taskMutex.RLock()
	tasks := make([]*Task, 0, len(q.jobs)+cap(d.transferSlots))
	for id, job := range q.jobs {
		task := d.taskWithActionsLocked(id, d.taskViewLocked(id, job))
		task.Root = true // Durable queue membership defines roots, even during preparation.
		tasks = append(tasks, task)
	}
	for id, task := range d.tasks {
		if q.jobs[id] == nil && task.FileInfo.IsDir == 0 && task.Status == "downloading" {
			tasks = append(tasks, d.taskWithActionsLocked(id, d.taskViewLocked(id, nil)))
		}
	}
	d.taskMutex.RUnlock()
	q.mu.Unlock()
	return tasks, d.enrichTaskWorkspaceHistory(ctx, tasks)
}

func (d *Downloader) enrichTaskWorkspaceHistory(ctx context.Context, tasks []*Task) error {
	q := d.queue
	q.mu.Lock()
	store := q.store
	q.mu.Unlock()
	for _, root := range tasks {
		if !root.Root {
			continue
		}
		h, err := d.GetDownloadHistoryForTask(ctx, root)
		if err != nil {
			return err
		}
		root.DownloadHistory = h
		if h.Source != "current_attempt" || root.Status != "downloading" {
			continue
		}
		for _, task := range tasks {
			if task.FileInfo.IsDir != 0 || task.Status != "downloading" || (task.FileInfo.FsID != root.FileInfo.FsID && !strings.HasPrefix(task.FileInfo.Path, strings.TrimSuffix(root.FileInfo.Path, "/")+"/")) {
				continue
			}
			result, err := store.DownloadManifestFileResult(ctx, h.Summary.Manifest.ID, task.FileInfo.FsID)
			if err != nil {
				return err
			}
			if result != nil {
				continue
			}
			progress := task.Progress
			if progress < 0 {
				progress = 0
			}
			if progress > task.FileInfo.Size {
				progress = task.FileInfo.Size
			}
			h.LiveProgress += progress
			h.LiveSpeed += task.Speed
			h.LiveActive++
		}
	}
	return nil
}

func (d *Downloader) GetRootDownloadHistory(ctx context.Context, id int64) (*RootDownloadHistory, error) {
	task := d.GetTaskByFsID(id)
	if task == nil || !task.Root {
		return nil, fmt.Errorf("download root not found")
	}
	return d.GetDownloadHistoryForTask(ctx, task)
}

// Classify against the same Task snapshot the caller is rendering. Reading a
// newer scheduler attempt here could mix old UI rows with a different attempt.
func (d *Downloader) GetDownloadHistoryForTask(ctx context.Context, task *Task) (*RootDownloadHistory, error) {
	if task == nil || !task.Root {
		return nil, fmt.Errorf("download root required")
	}
	q := d.queue
	q.mu.Lock()
	store := q.store
	q.mu.Unlock()
	history := &RootDownloadHistory{Source: "unrecorded"}
	if store == nil {
		return history, nil
	}
	summary, err := store.CurrentDownloadManifestSummary(ctx, task.FileInfo.FsID)
	if err != nil {
		return nil, err
	}
	if summary == nil {
		return history, nil
	}
	history.Source = "previous_attempt"
	history.Reason = "root_identity_changed"
	if summary.Manifest.AttemptID != task.AttemptID {
		history.Reason = "attempt_changed"
	}
	history.Summary = summary
	root := summary.Root
	info := task.FileInfo
	if summary.Manifest.AttemptID == task.AttemptID && root.FsID == info.FsID && root.Path == info.Path && root.Name == info.ServerFilename && root.IsDir == info.IsDir && root.Size == info.Size && root.Mtime == info.Mtime && root.CloudMD5 == info.MD5 {
		history.Source = "current_attempt"
		history.Reason = ""
	}
	return history, nil
}
