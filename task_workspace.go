package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
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
	FileEvidence    *downloader.DownloadFileEvidence `json:"file_evidence,omitempty"`
	DownloadHistory *downloader.RootDownloadHistory  `json:"download_history,omitempty"`
	Destination     *downloader.RootDestination      `json:"destination,omitempty"`
	ID              string                           `json:"id"`
	Name            string                           `json:"name"`
	Path            string                           `json:"path"`
	Status          string                           `json:"status"`
	LocalPath       string                           `json:"local_path"`
	Error           string                           `json:"error,omitempty"`
	Size            int64                            `json:"size"`
	Progress        int64                            `json:"progress"`
	Speed           int64                            `json:"speed"`
	Files           int                              `json:"files"`
	Folders         int                              `json:"folders"`
	Completed       int                              `json:"completed"`
	Active          int                              `json:"active"`
	Mode            string                           `json:"mode"`
	CanPause        bool                             `json:"can_pause"`
	CanResume       bool                             `json:"can_resume"`
	CanCancel       bool                             `json:"can_cancel"`
	CanRetry        bool                             `json:"can_retry"`
	Prepared        bool                             `json:"prepared"`

	TransferStats *downloader.TransferStatistics `json:"transfer_statistics"`
}

func workspaceRow(t *downloader.Task) workspaceTask {
	return workspaceTask{FileEvidence: t.FileEvidence, DownloadHistory: t.DownloadHistory, Destination: t.Destination, TransferStats: t.TransferStats, ID: strconv.FormatInt(t.FileInfo.FsID, 10), Name: t.FileInfo.ServerFilename, Path: t.FileInfo.Path, Status: t.Status, LocalPath: t.LocalPath, Error: t.Error, Size: t.FileInfo.Size, Progress: t.Progress, Speed: t.Speed, Mode: string(t.DownloadMode), CanPause: t.CanPause, CanResume: t.CanResume, CanCancel: t.CanCancel, CanRetry: t.CanRetry, Prepared: t.DirectoryPrepared || t.FileInfo.IsDir == 0}
}

func workspacePriority(status string) int {
	return downloader.WorkspaceStatusPriority(status)
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
		applyRootDownloadHistory(&row)
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
	expectedManifest := q.Get("manifest")
	if expectedManifest != "" {
		decoded, decodeErr := hex.DecodeString(expectedManifest)
		if root == "" || decodeErr != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != expectedManifest {
			http.Error(w, "invalid manifest", http.StatusBadRequest)
			return
		}
	}
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
	var result map[string]any
	if root == "" {
		page, pageErr := dl.GetRootWorkspacePage(offset, limit, status, query)
		if pageErr != nil {
			http.Error(w, "下载任务暂不可读，请稍后刷新", http.StatusServiceUnavailable)
			return
		}
		if err = dl.EnrichRootWorkspacePage(r.Context(), page.Tasks); err != nil {
			http.Error(w, "下载历史暂不可读，请稍后刷新", http.StatusServiceUnavailable)
			return
		}
		result, _ = buildTaskWorkspace(page.Tasks, "", "", "", 0, len(page.Tasks))
		result["total"], result["offset"], result["limit"], result["counts"], result["root_count"] = page.Total, page.Offset, page.Limit, page.Counts, page.QueueTotal
	} else {
		id, _ := strconv.ParseInt(root, 10, 64) // Format and range were validated above.
		task, rootErr := dl.GetTaskWorkspaceRoot(r.Context(), id)
		if errors.Is(rootErr, downloader.ErrTaskWorkspaceRootNotFound) {
			http.NotFound(w, r)
			return
		}
		if rootErr != nil {
			http.Error(w, "下载历史暂不可读，请稍后刷新", http.StatusServiceUnavailable)
			return
		}
		manifestID := ""
		if task.DownloadHistory != nil && task.DownloadHistory.Summary != nil {
			manifestID = task.DownloadHistory.Summary.Manifest.ID
		}
		if expectedManifest != "" && expectedManifest != manifestID {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"code": "manifest_changed", "message": "下载清单已更新，请重新载入第一页。"})
			return
		}
		var found bool
		result, found = buildTaskWorkspace([]*downloader.Task{task}, root, status, query, offset, limit)
		if !found {
			http.NotFound(w, r)
			return
		}
		result["root_count"] = dl.RootWorkspaceCount()
		result["manifest_id"] = manifestID
		page, available, pageErr := dl.GetDownloadHistoryFiles(r.Context(), task, status, query, offset, limit)
		if pageErr != nil {
			http.Error(w, "下载明细暂不可读，请稍后刷新", http.StatusServiceUnavailable)
			return
		}
		if available {
			items := make([]workspaceTask, 0, len(page.Tasks))
			for _, file := range page.Tasks {
				items = append(items, workspaceRow(file))
			}
			result["items"], result["total"], result["counts"], result["offset"] = items, page.Total, page.Counts, page.Offset
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(result)
}
