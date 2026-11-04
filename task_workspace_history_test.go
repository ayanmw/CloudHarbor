package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestTaskWorkspaceUsesSavedCountsAndSeparatesPreviousAttempt(t *testing.T) {
	summary := &taskstore.DownloadManifestSummary{Files: 2, Folders: 3, Size: 30, Completed: 1, CompletedBytes: 10}
	history := &downloader.RootDownloadHistory{Source: "current_attempt", Summary: summary, LiveProgress: 5, LiveActive: 1, LiveSpeed: 2}
	root := &downloader.Task{Root: true, FileInfo: baiduapi.FileInfo{FsID: 42, Path: "/queue/root", IsDir: 1}, Status: "paused", DownloadHistory: history}
	page, found := buildTaskWorkspace([]*downloader.Task{root}, "42", "", "", 0, 50)
	if !found {
		t.Fatal("root missing")
	}
	row := page["root"].(*workspaceTask)
	if row.Files != 2 || row.Folders != 3 || row.Size != 30 || row.Completed != 1 || row.Progress != 15 || row.Active != 1 || row.Speed != 2 || !row.Prepared {
		t.Fatalf("saved aggregate=%+v", row)
	}
	history.Source = "previous_attempt"
	stale := &downloader.Task{FileInfo: baiduapi.FileInfo{FsID: 43, Path: "/queue/root/old", Size: 999}, Status: "done"}
	page, _ = buildTaskWorkspace([]*downloader.Task{root, stale}, "42", "", "", 0, 50)
	row = page["root"].(*workspaceTask)
	if row.Files != 0 || row.Size != 0 || row.Completed != 0 || row.Prepared || row.DownloadHistory.Summary.Files != 2 {
		t.Fatalf("old observation presented as current=%+v", row)
	}
	history.Source = "unrecorded"
	history.Summary = nil
	page, _ = buildTaskWorkspace([]*downloader.Task{root}, "42", "", "", 0, 50)
	row = page["root"].(*workspaceTask)
	if row.Files != 0 || row.Completed != 0 || row.Prepared {
		t.Fatalf("invented missing history=%+v", row)
	}
}

func TestTaskWorkspaceReadsPersistedSummaryAndFailsExplicitlyWhenStoreCloses(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	local := t.TempDir()
	id := "99999999999999999999999999999999"
	store, err := taskstore.Open(filepath.Join(local, ".nas-downloader", "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	info := baiduapi.FileInfo{FsID: 9007199254740993, Path: "/queue/root", ServerFilename: "root", IsDir: 1}
	body, _ := json.Marshal(map[string]any{"version": 1, "file": info, "mode": "keep", "status": "paused", "paused_from_status": "pending", "attempt_id": id, "created": now, "updated": now})
	if err = store.Save(info.FsID, body); err != nil {
		t.Fatal(err)
	}
	if err = store.BeginDownloadManifest(ctx, taskstore.DownloadManifest{ID: id, RootID: info.FsID, RootPath: info.Path, AttemptID: id, StartedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	root := taskstore.DownloadManifestEntry{FsID: info.FsID, Path: info.Path, Name: "root", IsDir: 1, RelativePath: "root", ObservedAtUTC: now}
	child := taskstore.DownloadManifestEntry{FsID: 43, Path: info.Path + "/a", Name: "a", RelativePath: "root/a", Size: 3, ObservedAtUTC: now}
	if err = store.AppendDownloadManifestEntries(ctx, id, []taskstore.DownloadManifestEntry{root, child}); err != nil {
		t.Fatal(err)
	}
	if err = store.PublishDownloadManifest(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordDownloadManifestResult(ctx, id, child, taskstore.DownloadManifestResult{Kind: "published", SHA256: strings.Repeat("a", 64), RecordedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := downloader.OpenDownloader(&config.Config{LocalDownDir: local, QueueDir: "/queue", MaxParallel: 1}, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	oldDL := dl
	dl = restored
	t.Cleanup(func() { dl = oldDL })
	defer restored.Shutdown(ctx)
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handleTaskWorkspace(w, httptest.NewRequest(http.MethodGet, "/api/task-workspace?root=9007199254740993", nil))
		return w
	}
	response := request()
	if response.Code != 200 {
		t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
	}
	var page struct {
		Root  workspaceTask   `json:"root"`
		Items []workspaceTask `json:"items"`
		Total int             `json:"total"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Root.ID != "9007199254740993" || page.Root.Files != 1 || page.Root.Folders != 1 || page.Root.Size != 3 || page.Root.Completed != 1 || page.Root.Progress != 3 || !page.Root.Prepared || page.Root.DownloadHistory.Source != "current_attempt" {
		t.Fatalf("restored root=%+v", page.Root)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "43" || page.Items[0].Status != "done" || page.Items[0].FileEvidence == nil || page.Items[0].FileEvidence.Result == nil || page.Items[0].LocalPath != "" {
		t.Fatalf("persisted file page=%+v", page.Items)
	}
	if err = restored.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	response = request()
	if response.Code != 503 || !strings.Contains(response.Body.String(), "历史暂不可读") {
		t.Fatalf("unreadable history disguised as empty: HTTP %d %s", response.Code, response.Body.String())
	}
}
