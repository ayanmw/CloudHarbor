package main

import (
	"context"
	"database/sql"
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

func TestTaskWorkspaceOnlyReadsHistoryForRequestedRoots(t *testing.T) {
	local := t.TempDir()
	state := filepath.Join(local, ".nas-downloader", "queue")
	ctx := context.Background()
	now := time.Now().UTC()
	store, err := taskstore.Open(state, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i, name := range []string{"a", "z"} {
		info := baiduapi.FileInfo{FsID: int64(42 + i), Path: "/queue/" + name, ServerFilename: name, IsDir: 1}
		manifest := strings.Repeat([]string{"a", "b"}[i], 32)
		body, _ := json.Marshal(map[string]any{"version": 1, "file": info, "mode": "keep", "status": "paused", "paused_from_status": "pending", "attempt_id": manifest, "created": now, "updated": now})
		if err = store.Save(info.FsID, body); err != nil {
			t.Fatal(err)
		}
		if err = store.BeginDownloadManifest(ctx, taskstore.DownloadManifest{ID: manifest, RootID: info.FsID, RootPath: info.Path, AttemptID: manifest, StartedAtUTC: now}); err != nil {
			t.Fatal(err)
		}
		if err = store.AppendDownloadManifestEntries(ctx, manifest, []taskstore.DownloadManifestEntry{{FsID: info.FsID, Path: info.Path, Name: name, IsDir: 1, RelativePath: name, ObservedAtUTC: now}}); err != nil {
			t.Fatal(err)
		}
		if err = store.PublishDownloadManifest(ctx, manifest, now); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.Close(); err != nil {
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
	db, err := sql.Open("sqlite", filepath.Join(state, "queue.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Make only the off-page projection unreadable. Reading it must still fail,
	// but it must not prevent the healthy first page or another root's details.
	if _, err = db.Exec(`DELETE FROM download_manifest_totals WHERE manifest_id=?`, strings.Repeat("b", 32)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		url  string
		want int
	}{{"/api/task-workspace?limit=1", 200}, {"/api/task-workspace?root=42", 200}, {"/api/task-workspace?root=999", 404}, {"/api/task-workspace?offset=1&limit=1", 503}, {"/api/task-workspace?root=43", 503}} {
		w := httptest.NewRecorder()
		handleTaskWorkspace(w, httptest.NewRequest(http.MethodGet, tc.url, nil))
		if w.Code != tc.want {
			t.Fatalf("%s: HTTP %d want %d: %s", tc.url, w.Code, tc.want, w.Body.String())
		}
		if tc.url == "/api/task-workspace?limit=1" {
			var page struct {
				Total     int             `json:"total"`
				RootCount int             `json:"root_count"`
				Items     []workspaceTask `json:"items"`
			}
			if err = json.Unmarshal(w.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if page.Total != 2 || page.RootCount != 2 || len(page.Items) != 1 || page.Items[0].ID != "42" {
				t.Fatalf("root page lost totals: %+v", page)
			}
		}
	}
}
