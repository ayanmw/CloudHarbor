package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/config"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
	"github.com/ayanmw/CloudHarbor/internal/management"
	"github.com/ayanmw/CloudHarbor/internal/webui"
)

func TestWorkspaceAndDownloadSettingsRequireAdminAndValidateInput(t *testing.T) {
	oldCfg, oldDL, oldUI := cfg, dl, ui
	t.Cleanup(func() { cfg, dl, ui = oldCfg, oldDL, oldUI })
	cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/queue", MaxParallel: 1}
	var err error
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		dl.Shutdown(ctx)
	}()
	ui = webui.NewServer(dl)
	gate, err := management.New("workspace-test-password")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(applicationHandler(gate, nil))
	defer server.Close()
	for _, tc := range []struct {
		method, path, body string
		auth               bool
		want               int
	}{
		{"GET", "/api/task-workspace", "", false, 401}, {"GET", "/api/download-settings", "", false, 401},
		{"GET", "/api/task-workspace?root=-1", "", true, 400}, {"GET", "/api/task-workspace?limit=999", "", true, 400},
		{"GET", "/api/task-workspace?status=invalid", "", true, 400}, {"GET", "/api/task-workspace?root=99", "", true, 404},
		{"GET", "/api/task-workspace", "", true, 200}, {"GET", "/api/download-settings", "", true, 200},
		{"POST", "/api/download-settings", `{"download_mode":"invalid"}`, true, 400},
		{"POST", "/api/download-settings", `{"download_mode":"keep"} {}`, true, 400},
		{"POST", "/api/download-settings", `{"download_mode":"keep"}`, true, 200},
	} {
		req, _ := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(tc.body))
		if tc.auth {
			req.Header.Set("Authorization", "Bearer workspace-test-password")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Errorf("%s %s: %d want %d", tc.method, tc.path, res.StatusCode, tc.want)
		}
	}
}

func TestTaskWorkspaceSeparatesDirectoriesAndPrioritizesActiveFiles(t *testing.T) {
	makeTask := func(id int64, p, status string, dir, root bool, size, progress int64) *downloader.Task {
		isdir := 0
		if dir {
			isdir = 1
		}
		return &downloader.Task{FileInfo: baiduapi.FileInfo{FsID: id, Path: p, ServerFilename: p, IsDir: isdir, Size: size}, Status: status, Root: root, Progress: progress}
	}
	tasks := []*downloader.Task{makeTask(9007199254740993, "/queue/a", "downloading", true, true, 0, 0), makeTask(2, "/queue/a/folder", "downloading", true, false, 0, 0), makeTask(3, "/queue/a/done", "done", false, false, 100, 0), makeTask(4, "/queue/a/wait", "pending", false, false, 200, 0), makeTask(5, "/queue/a/live", "downloading", false, false, 300, 50), makeTask(6, "/queue/ab/other", "done", false, false, 1000, 0)}
	page, ok := buildTaskWorkspace(tasks, "9007199254740993", "", "", 0, 50)
	if !ok {
		t.Fatal("root not found")
	}
	root := page["root"].(*workspaceTask)
	if root.Files != 3 || root.Folders != 2 || root.Active != 1 || root.Completed != 1 || root.Size != 600 || root.Progress != 150 {
		t.Fatalf("aggregate=%+v", root)
	}
	rows := page["items"].([]workspaceTask)
	if len(rows) != 3 || rows[0].ID != "5" || rows[1].ID != "4" || rows[2].ID != "3" {
		t.Fatalf("file ordering=%+v", rows)
	}
	page, _ = buildTaskWorkspace(tasks, "9007199254740993", "done", "", 0, 1)
	if page["total"].(int) != 1 || page["counts"].(map[string]int)["pending"] != 1 {
		t.Fatalf("filtered page=%+v", page)
	}
	if _, ok = buildTaskWorkspace(tasks, "99", "", "", 0, 50); ok {
		t.Fatal("unknown root accepted")
	}
	page, _ = buildTaskWorkspace(tasks, "", "", "", 0, 50)
	rows = page["items"].([]workspaceTask)
	if len(rows) != 1 || rows[0].ID != "9007199254740993" {
		t.Fatalf("root-only overview=%+v", rows)
	}
}
