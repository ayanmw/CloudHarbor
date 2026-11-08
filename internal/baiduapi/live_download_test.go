package baiduapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Explicit opt-in only. Reads the one original account file; never copies tokens
// or mutates remote content. Fixtures must come from this task's upload batch.
func TestLiveAccountUploadedFixtureDownload(t *testing.T) {
	stateDir, evidenceDir := os.Getenv("BAIDU_LIVE_STATE_DIR"), os.Getenv("BAIDU_LIVE_EVIDENCE_DIR")
	if stateDir == "" || evidenceDir == "" {
		t.Skip("explicit real-account fixture acceptance is not enabled")
	}
	var credentials struct {
		AccessToken string `json:"access_token"`
		AppKey      string `json:"app_key"`
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, "queue", "credentials.json"))
	if err != nil {
		t.Fatal("original credentials unavailable")
	}
	if json.Unmarshal(raw, &credentials) != nil || credentials.AccessToken == "" {
		t.Fatal("original credentials invalid")
	}
	var fixture struct {
		RemoteTestDirectory string `json:"remote_test_directory"`
	}
	raw, err = os.ReadFile(filepath.Join(evidenceDir, "test-context.json"))
	if err != nil || json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("fixture context unavailable")
	}
	if len(fixture.RemoteTestDirectory) < len("/_NAS_Downloader_RealTest_") || fixture.RemoteTestDirectory[:len("/_NAS_Downloader_RealTest_")] != "/_NAS_Downloader_RealTest_" {
		t.Fatal("remote fixture scope invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	client := NewClient(credentials.AccessToken, credentials.AppKey).WithContext(ctx)
	files, err := client.ListDir(fixture.RemoteTestDirectory + "/round2")
	if err != nil {
		t.Fatalf("real listing failed: %T", err)
	}
	results := []map[string]any{}
	for _, name := range []string{"中文验证.txt", "multi-block.bin"} {
		var item *FileInfo
		for i := range files {
			if files[i].Path == fixture.RemoteTestDirectory+"/round2/"+name {
				item = &files[i]
			}
		}
		if item == nil {
			t.Fatalf("uploaded fixture absent: %s", name)
		}
		link, err := client.GetDownloadLink(item.FsID)
		if err != nil {
			t.Fatalf("real download link failed: %T", err)
		}
		body, _, err := client.DownloadFileWithRange(link, 0)
		if err != nil {
			t.Fatalf("real download response failed: %T", err)
		}
		actual, readErr := io.ReadAll(io.LimitReader(body, 6*1024*1024))
		body.Close()
		if readErr != nil {
			t.Fatalf("real download read failed: %T", readErr)
		}
		expected, err := os.ReadFile(filepath.Join(evidenceDir, name))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("real downloaded content differs: %s", name)
		}
		if err := os.WriteFile(filepath.Join(evidenceDir, "downloaded-"+name), actual, 0600); err != nil {
			t.Fatal(err)
		}
		results = append(results, map[string]any{"path": item.Path, "fs_id": fmt.Sprint(item.FsID), "size": len(actual), "sha256": fmt.Sprintf("%x", sha256.Sum256(actual)), "observed_at_utc": time.Now().UTC().Format(time.RFC3339Nano)})
		t.Logf("real remote download verified: %s, %d bytes", name, len(actual))
	}
	raw, err = json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidenceDir, "download-verification.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
