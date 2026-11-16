package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/config"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestManagedPathImpactStringIdentityPreservesLargeIDsAndLegacyFormat(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	root := t.TempDir()
	cfg = &config.Config{LocalDownDir: filepath.Join(root, "downloads"), QueueDir: "/old", StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	store, err := taskstore.Open(filepath.Join(cfg.StateDir, "queue"), cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	for index, id := range []int64{9007199254740993, 9223372036854775807} {
		body := fmt.Sprintf(`{"version":1,"file":{"fs_id":%d,"path":"/old/file-%d.txt","server_filename":"file-%d.txt","size":7},"mode":"keep","status":"pending"}`, id, index, index)
		if err = store.Save(id, []byte(body)); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("synthetic-token", "synthetic-app"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dl.Shutdown(context.Background()) })
	var digest string
	for _, mode := range []string{"", "&identity_format=string"} {
		response := httptest.NewRecorder()
		handleManagedPaths(response, httptest.NewRequest(http.MethodGet, "/api/settings/paths?queue_dir=/new&limit=50"+mode, nil))
		if response.Code != 200 {
			t.Fatalf("status=%d", response.Code)
		}
		var body struct {
			Impact struct {
				Digest string `json:"digest"`
			} `json:"queue_change_impact"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Impact.Digest == "" {
			t.Fatalf("invalid impact digest: %s", response.Body.String())
		}
		if digest != "" && body.Impact.Digest != digest {
			t.Fatal("string identity format changed the confirmation digest")
		}
		digest = body.Impact.Digest
		for _, id := range []string{"9007199254740993", "9223372036854775807"} {
			expected := `"fs_id":` + id
			if mode != "" {
				expected = `"fs_id":"` + id + `"`
			}
			if !strings.Contains(response.Body.String(), expected) {
				t.Fatalf("identity format=%q did not preserve %s: %s", mode, id, response.Body.String())
			}
		}
	}
	for _, query := range []string{"identity_format=rounded", "identity_format=string&identity_format=string"} {
		response := httptest.NewRecorder()
		handleManagedPaths(response, httptest.NewRequest(http.MethodGet, "/api/settings/paths?"+query, nil))
		if response.Code != 400 {
			t.Fatalf("invalid identity format accepted: %s status=%d", query, response.Code)
		}
	}
}
