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

func seedWorkspaceManifest(t *testing.T, local, id, name string) {
	t.Helper()
	s, err := taskstore.Open(filepath.Join(local, ".nas-downloader", "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	info := baiduapi.FileInfo{FsID: 42, Path: "/queue/root", ServerFilename: "root", IsDir: 1}
	body, _ := json.Marshal(map[string]any{"version": 1, "file": info, "mode": "keep", "status": "paused", "paused_from_status": "pending", "attempt_id": id, "created": now, "updated": now})
	if err = s.Save(42, body); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginDownloadManifest(context.Background(), taskstore.DownloadManifest{ID: id, RootID: 42, RootPath: info.Path, AttemptID: id, StartedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	entries := []taskstore.DownloadManifestEntry{{FsID: 42, Path: info.Path, Name: "root", IsDir: 1, RelativePath: "root", ObservedAtUTC: now}, {FsID: 43, Path: info.Path + "/" + name, Name: name, RelativePath: "root/" + name, Size: 3, ObservedAtUTC: now}}
	if err = s.AppendDownloadManifestEntries(context.Background(), id, entries); err != nil {
		t.Fatal(err)
	}
	if err = s.PublishDownloadManifest(context.Background(), id, now); err != nil {
		t.Fatal(err)
	}
}

func TestTaskWorkspaceManifestChangeRejectsOldPageInsteadOfMixingAttempts(t *testing.T) {
	local := t.TempDir()
	oldID, newID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	seedWorkspaceManifest(t, local, oldID, "old.bin")
	oldDL := dl
	t.Cleanup(func() {
		if dl != oldDL {
			dl.Shutdown(context.Background())
		}
		dl = oldDL
	})
	open := func() {
		t.Helper()
		var err error
		dl, err = downloader.OpenDownloader(&config.Config{LocalDownDir: local, QueueDir: "/queue", MaxParallel: 1}, baiduapi.NewClient("", ""))
		if err != nil {
			t.Fatal(err)
		}
	}
	request := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handleTaskWorkspace(w, httptest.NewRequest(http.MethodGet, "/api/task-workspace"+query, nil))
		return w
	}
	open()
	response := request("?root=42")
	var first struct {
		Manifest string `json:"manifest_id"`
	}
	json.Unmarshal(response.Body.Bytes(), &first)
	if response.Code != 200 || first.Manifest != oldID {
		t.Fatalf("initial manifest: HTTP %d %s", response.Code, response.Body.String())
	}
	for _, query := range []string{"?root=42&manifest=invalid", "?manifest=" + oldID, "?root=42&manifest=" + strings.ToUpper(oldID)} {
		if r := request(query); r.Code != 400 {
			t.Fatalf("invalid manifest accepted: %s HTTP %d", query, r.Code)
		}
	}
	if err := dl.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	seedWorkspaceManifest(t, local, newID, "new.bin")
	open()
	response = request("?root=42&manifest=" + oldID + "&offset=50")
	var conflict struct {
		Code string `json:"code"`
	}
	json.Unmarshal(response.Body.Bytes(), &conflict)
	if response.Code != 409 || conflict.Code != "manifest_changed" {
		t.Fatalf("old page silently changed: HTTP %d %s", response.Code, response.Body.String())
	}
	response = request("?root=42&manifest=" + newID)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "new.bin") || strings.Contains(response.Body.String(), "old.bin") {
		t.Fatalf("new page=%d %s", response.Code, response.Body.String())
	}
}

func TestTaskWorkspaceLegacyRootHasNoInventedManifest(t *testing.T) {
	ctx := context.Background()
	local := t.TempDir()
	s, err := taskstore.Open(filepath.Join(local, ".nas-downloader", "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info := baiduapi.FileInfo{FsID: 42, Path: "/queue/legacy", ServerFilename: "legacy", IsDir: 1}
	body, _ := json.Marshal(map[string]any{"version": 1, "file": info, "mode": "keep", "status": "paused", "paused_from_status": "pending", "created": time.Now()})
	if err = s.Save(42, body); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := downloader.OpenDownloader(&config.Config{LocalDownDir: local, QueueDir: "/queue", MaxParallel: 1}, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Shutdown(ctx)
	oldDL := dl
	dl = restored
	t.Cleanup(func() { dl = oldDL })
	w := httptest.NewRecorder()
	handleTaskWorkspace(w, httptest.NewRequest(http.MethodGet, "/api/task-workspace?root=42", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"manifest_id":""`) {
		t.Fatalf("invented manifest: HTTP %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handleTaskWorkspace(w, httptest.NewRequest(http.MethodGet, "/api/task-workspace?root=42&manifest="+strings.Repeat("a", 32), nil))
	if w.Code != 409 {
		t.Fatalf("absent requested manifest silently accepted: HTTP %d %s", w.Code, w.Body.String())
	}
}
