package main

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"web_browser_baidu_onekey_upload/internal/downloader"
)

func handleDownloadSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		json.NewEncoder(w).Encode(map[string]string{"download_mode": string(dl.GetDownloadMode())})
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Mode string `json:"download_mode"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || (body.Mode != "keep" && body.Mode != "delete") {
		http.Error(w, "invalid download mode", 400)
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid body", 400)
		return
	}
	dl.SetDownloadMode(downloader.DownloadMode(body.Mode))
	if !dl.QueueOperational() || string(dl.GetDownloadMode()) != body.Mode {
		http.Error(w, "settings could not be saved", 503)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"download_mode": body.Mode})
}

type workspaceTask struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Status    string `json:"status"`
	LocalPath string `json:"local_path"`
	Error     string `json:"error,omitempty"`
	Size      int64  `json:"size"`
	Progress  int64  `json:"progress"`
	Speed     int64  `json:"speed"`
	Files     int    `json:"files"`
	Folders   int    `json:"folders"`
	Completed int    `json:"completed"`
	Active    int    `json:"active"`
	Mode      string `json:"mode"`
	CanPause  bool   `json:"can_pause"`
	CanResume bool   `json:"can_resume"`
	CanCancel bool   `json:"can_cancel"`
	CanRetry  bool   `json:"can_retry"`
	Prepared  bool   `json:"prepared"`

	TransferStats *downloader.TransferStatistics `json:"transfer_statistics"`
}

func workspaceRow(t *downloader.Task) workspaceTask {
	return workspaceTask{TransferStats: t.TransferStats, ID: strconv.FormatInt(t.FileInfo.FsID, 10), Name: t.FileInfo.ServerFilename, Path: t.FileInfo.Path, Status: t.Status, LocalPath: t.LocalPath, Error: t.Error, Size: t.FileInfo.Size, Progress: t.Progress, Speed: t.Speed, Mode: string(t.DownloadMode), CanPause: t.CanPause, CanResume: t.CanResume, CanCancel: t.CanCancel, CanRetry: t.CanRetry, Prepared: t.DirectoryPrepared || t.FileInfo.IsDir == 0}
}

func workspacePriority(status string) int {
	switch status {
	case "downloading":
		return 0
	case "failed", "source_unknown", "auth_wait":
		return 1
	case "pending", "retry_wait":
		return 2
	case "paused":
		return 3
	case "cancelled":
		return 4
	case "done":
		return 5
	}
	return 6
}

// Aggregate only observed descendants. A running root's discovered size is not
// a complete provider inventory and must not be presented as a final total.
func buildTaskWorkspace(tasks []*downloader.Task, rootID, status, query string, offset, limit int) (map[string]any, bool) {
	roots := make([]*downloader.Task, 0)
	for _, t := range tasks {
		if t.Root {
			roots = append(roots, t)
		}
	}
	rows := make([]workspaceTask, 0)
	var selected *workspaceTask
	files := make([]workspaceTask, 0)
	counts := map[string]int{}
	for _, root := range roots {
		row := workspaceRow(root)
		row.Size = 0
		row.Progress = 0
		row.Speed = 0
		for _, t := range tasks {
			if t.FileInfo.FsID != root.FileInfo.FsID && !strings.HasPrefix(t.FileInfo.Path, strings.TrimSuffix(root.FileInfo.Path, "/")+"/") {
				continue
			}
			if t.FileInfo.IsDir == 1 {
				row.Folders++
				continue
			}
			row.Files++
			row.Size += t.FileInfo.Size
			progress := t.Progress
			if t.Status == "done" {
				progress = t.FileInfo.Size
				row.Completed++
			}
			if progress > t.FileInfo.Size {
				progress = t.FileInfo.Size
			}
			if progress < 0 {
				progress = 0
			}
			row.Progress += progress
			if t.Status == "downloading" {
				row.Active++
				row.Speed += t.Speed
			}
			if row.ID == rootID {
				counts[t.Status]++
				file := workspaceRow(t)
				if (status == "" || t.Status == status) && (query == "" || strings.Contains(strings.ToLower(t.FileInfo.Path), query)) {
					files = append(files, file)
				}
			}
		}
		if row.ID == rootID {
			copy := row
			selected = &copy
		}
		if (status == "" || row.Status == status) && (query == "" || strings.Contains(strings.ToLower(row.Path), query)) {
			rows = append(rows, row)
		}
	}
	if rootID != "" && selected == nil {
		return nil, false
	}
	items := rows
	if rootID != "" {
		items = files
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := workspacePriority(items[i].Status), workspacePriority(items[j].Status)
		if a != b {
			return a < b
		}
		if items[i].Path != items[j].Path {
			return items[i].Path < items[j].Path
		}
		return items[i].ID < items[j].ID
	})
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return map[string]any{"items": items[offset:end], "root": selected, "total": total, "offset": offset, "limit": limit, "counts": counts, "root_count": len(roots)}, true
}

func handleTaskWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	status, query, err := downloader.NormalizeTaskFilters(q.Get("status"), q.Get("q"))
	if err != nil {
		http.Error(w, "invalid filters", 400)
		return
	}
	root := q.Get("root")
	if root != "" {
		if id, e := strconv.ParseInt(root, 10, 64); e != nil || id <= 0 {
			http.Error(w, "invalid root", 400)
			return
		}
	}
	offset, limit := 0, 50
	if q.Has("offset") {
		offset, err = strconv.Atoi(q.Get("offset"))
		if err != nil || offset < 0 {
			http.Error(w, "invalid offset", 400)
			return
		}
	}
	if q.Has("limit") {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > 200 {
			http.Error(w, "invalid limit", 400)
			return
		}
	}
	result, found := buildTaskWorkspace(dl.GetTasks(), root, status, query, offset, limit)
	if !found {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(result)
}
