package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/config"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
)

func managedPathHTTPFixture(t *testing.T) string {
	t.Helper()
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	root := t.TempDir()
	cfg = &config.Config{LocalDownDir: filepath.Join(root, "initial"), QueueDir: "/queue", StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	var err error
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("synthetic-token", "synthetic-app"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dl.Shutdown(context.Background()) })
	return root
}

func TestManagedPathHTTPRejectsAnObsoleteLocalSourceBeforeCreatingTarget(t *testing.T) {
	root := managedPathHTTPFixture(t)
	old := dl.GetManagedPaths()
	second := downloader.ManagedPaths{QueueDir: old.QueueDir, LocalDownDir: filepath.Join(root, "second")}
	if _, err := dl.SetManagedPaths(context.Background(), second, false); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "third")
	post := func(expected downloader.ManagedPaths) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"queue_dir": old.QueueDir, "local_download_dir": target, "migrate_files": false, "expected_paths": expected})
		response := httptest.NewRecorder()
		handleManagedPaths(response, httptest.NewRequest(http.MethodPost, "/api/settings/paths", strings.NewReader(string(body))))
		return response
	}
	stale := post(old)
	if stale.Code != http.StatusConflict {
		t.Fatalf("obsolete settings confirmation status=%d body=%s", stale.Code, stale.Body.String())
	}
	if dl.GetManagedPaths() != second {
		t.Fatal("obsolete request changed current paths")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("obsolete request created target: %v", err)
	}
	current := post(second)
	if current.Code != http.StatusOK {
		t.Fatalf("fresh precondition status=%d body=%s", current.Code, current.Body.String())
	}
	if dl.GetManagedPaths().LocalDownDir != target {
		t.Fatal("fresh settings were not applied")
	}
}

func TestManagedPathHTTPReportsTheLiveQueueMaintenanceLock(t *testing.T) {
	managedPathHTTPFixture(t)
	defer dl.EndMaintenance()
	for _, busy := range []bool{false, true, false} {
		if busy {
			if err := dl.TryBeginMaintenance(); err != nil {
				t.Fatal(err)
			}
		} else {
			dl.EndMaintenance()
		}
		response := httptest.NewRecorder()
		handleManagedPaths(response, httptest.NewRequest(http.MethodGet, "/api/settings/paths", nil))
		var result struct {
			Busy   *bool  `json:"queue_busy"`
			Reason string `json:"queue_busy_reason"`
		}
		if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Busy == nil || *result.Busy != busy {
			t.Fatalf("queue lock metadata busy=%v body=%s", busy, response.Body.String())
		}
		if busy != (result.Reason != "") {
			t.Fatalf("queue lock reason disagrees with state: %+v", result)
		}
	}
}
