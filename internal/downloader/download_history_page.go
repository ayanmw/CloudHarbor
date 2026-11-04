package downloader

import (
	"context"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type DownloadFileEvidence struct {
	ObservedAtUTC     time.Time                         `json:"observed_at_utc"`
	ObservationSource string                            `json:"observation_source"`
	StateAtUTC        *time.Time                        `json:"state_at_utc,omitempty"`
	Interrupted       bool                              `json:"interrupted"`
	Result            *taskstore.DownloadManifestResult `json:"result,omitempty"`
	Historical        bool                              `json:"historical"`
}

type DownloadHistoryPage struct {
	Tasks  []*Task
	Total  int
	Counts map[string]int
	Offset int
	Limit  int
}

func (d *Downloader) GetDownloadHistoryFiles(ctx context.Context, root *Task, status, query string, offset, limit int) (DownloadHistoryPage, bool, error) {
	var page DownloadHistoryPage
	h := root.DownloadHistory
	if h == nil || h.Summary == nil {
		return page, false, nil
	}
	q := d.queue
	q.mu.Lock()
	store := q.store
	q.mu.Unlock()
	if store == nil {
		return page, false, nil
	}
	current := h.Source == "current_attempt"
	dbPage, err := store.DownloadManifestFilePage(ctx, h.Summary.Manifest.ID, status, query, offset, limit, current && root.Status == "downloading")
	if err != nil {
		return page, true, err
	}
	page = DownloadHistoryPage{Tasks: make([]*Task, 0, len(dbPage.Items)), Total: dbPage.Total, Counts: dbPage.Counts, Offset: dbPage.Offset, Limit: dbPage.Limit}
	q.mu.Lock()
	defer q.mu.Unlock()
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()
	job := q.jobs[root.FileInfo.FsID]
	overlay := current && job != nil && job.AttemptID == root.AttemptID && job.Info == root.FileInfo
	for _, f := range dbPage.Items {
		e := f.Entry
		info := baiduapi.FileInfo{FsID: e.FsID, Path: e.Path, ServerFilename: e.Name, IsDir: e.IsDir, Size: e.Size, Mtime: e.Mtime, MD5: e.CloudMD5}
		task := &Task{FileInfo: info, TaskID: strconv.FormatInt(e.FsID, 10), Status: f.Status, Error: f.Error, DownloadMode: root.DownloadMode, DirectoryPrepared: true, FileEvidence: &DownloadFileEvidence{ObservedAtUTC: e.ObservedAtUTC, ObservationSource: e.ObservationSource, StateAtUTC: f.StateAtUTC, Interrupted: f.Interrupted, Result: f.Result, Historical: !current}}
		if task.Status == "done" {
			task.Progress = e.Size
		}
		// This is display-only and is derived solely from a matching historical
		// destination snapshot, never the current download-directory setting.
		if current && root.Destination != nil && root.Destination.AttemptID == h.Summary.Manifest.AttemptID {
			suffix := strings.TrimPrefix(e.RelativePath, path.Base(h.Summary.Manifest.RootPath)+"/")
			task.LocalPath = root.Destination.Path
			if e.FsID != root.FileInfo.FsID {
				task.LocalPath = filepath.Join(root.Destination.Path, filepath.FromSlash(suffix))
			}
		}
		if overlay && task.Status == "downloading" {
			if live := d.tasks[e.FsID]; live != nil && live.FileInfo == info && live.Status == "downloading" {
				task.Progress = live.Progress
				task.Speed = live.Speed
				if task.Progress < 0 {
					task.Progress = 0
				}
				if task.Progress > e.Size {
					task.Progress = e.Size
				}
			}
		}
		page.Tasks = append(page.Tasks, task)
	}
	return page, true, nil
}
