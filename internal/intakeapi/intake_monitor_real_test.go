package intakeapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/management"
)

// Explicit real acceptance only: fresh owned remote scope and isolated local
// state. Read original credentials into memory, never initialize auth sidecars.
func TestRealOwnedHTTPIntakeMonitor(t *testing.T) {
	descriptor := os.Getenv("NAS_REAL_INTAKE_FIXTURE")
	if descriptor == "" {
		t.Skip("requires an explicitly owned HTTP intake fixture")
	}
	var f struct{ Release, LocalRoot, RemoteRoot, Evidence string }
	raw, err := os.ReadFile(descriptor)
	if err != nil || json.Unmarshal(raw, &f) != nil {
		t.Fatal("invalid descriptor")
	}
	if _, err = os.Stat(f.Evidence); !os.IsNotExist(err) {
		t.Fatal("existing evidence means a prior intent may exist; never replay")
	}
	base := filepath.Dir(descriptor)
	inside := func(value string) bool {
		r, e := filepath.Rel(base, value)
		return e == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(os.PathSeparator)) && !filepath.IsAbs(r)
	}
	if !inside(f.LocalRoot) || !inside(f.Evidence) || !strings.HasPrefix(filepath.Base(f.LocalRoot), "local-intake-real-") || !strings.HasPrefix(f.RemoteRoot, "/_NAS_Downloader_IntakeHTTP_20261107_") || strings.Contains(strings.TrimPrefix(f.RemoteRoot, "/"), "/") {
		t.Fatal("fixture boundary invalid")
	}
	if _, err = os.Stat(f.LocalRoot); !os.IsNotExist(err) {
		t.Fatal("requires a new local root; never replay a fixture")
	}
	credentialPath := filepath.Join(f.Release, ".nas-downloader", "queue", "credentials.json")
	credentialRaw, err := os.ReadFile(credentialPath)
	if err != nil {
		t.Fatal("original credentials unavailable")
	}
	credentialSHA := sha256.Sum256(credentialRaw)
	var credentials struct {
		AccessToken string `json:"access_token"`
		AppKey      string `json:"app_key"`
	}
	if json.Unmarshal(credentialRaw, &credentials) != nil || credentials.AccessToken == "" || credentials.AppKey == "" {
		t.Fatal("original credentials incomplete")
	}
	client := baiduapi.NewClient(credentials.AccessToken, credentials.AppKey)
	proof := map[string]any{"remote_root": f.RemoteRoot, "started_at_utc": time.Now().UTC()}
	save := func() {
		b, e := json.MarshalIndent(proof, "", "  ")
		if e != nil || os.WriteFile(f.Evidence, b, 0600) != nil {
			t.Fatal("cannot persist acceptance evidence")
		}
	}
	// Persist intent before every fixture write. No mutation is retried.
	for _, dir := range []string{f.RemoteRoot, f.RemoteRoot + "/source", f.RemoteRoot + "/queue", f.RemoteRoot + "/source/intake-tree", f.RemoteRoot + "/source/intake-tree/empty"} {
		proof["fixture_write_intent"] = map[string]any{"operation": "mkdir", "path": dir, "utc": time.Now().UTC()}
		save()
		if err = client.CreateDirectory(dir); err != nil {
			t.Fatal("fixture mkdir failed; inspect intent, never replay")
		}
	}
	const sourceParent = "/_NAS_Downloader_Dedupe_Test_20261106_90318344/a"
	entries, err := client.ListDir(sourceParent)
	if err != nil {
		t.Fatal("owned source unavailable")
	}
	var source baiduapi.FileInfo
	for _, entry := range entries {
		if entry.Path == sourceParent+"/one.txt" && entry.IsDir == 0 && entry.Size == 46 {
			source = entry
		}
	}
	if source.FsID == 0 {
		t.Fatal("owned 46-byte source absent")
	}
	link, err := client.GetDownloadLink(source.FsID)
	if err != nil {
		t.Fatal("owned source link failed")
	}
	reader, _, err := client.DownloadFile(link)
	if err != nil {
		t.Fatal("owned source read failed")
	}
	content, err := io.ReadAll(io.LimitReader(reader, 47))
	reader.Close()
	if err != nil || len(content) != 46 {
		t.Fatal("owned content length mismatch")
	}
	expectedSHA := sha256.Sum256(content)
	proof["fixture_write_intent"] = map[string]any{"operation": "copy", "source": source.Path, "destination": f.RemoteRoot + "/source/intake-tree/payload.txt", "utc": time.Now().UTC()}
	save()
	if err = client.CopyFile(source.Path, f.RemoteRoot+"/source/intake-tree", "payload.txt"); err != nil {
		t.Fatal("fixture copy failed; never replay")
	}
	cfg := &config.Config{LocalDownDir: f.LocalRoot, QueueDir: f.RemoteRoot + "/queue", QueueDirExplicit: true, LocalDownDirExplicit: true, MaxParallel: 2}
	var d *downloader.Downloader
	open := func() {
		d, err = downloader.OpenDownloader(cfg, client)
		if err != nil {
			t.Fatal(err)
		}
		user, e := client.UserID()
		if e != nil || d.BindAccount(user) != nil {
			t.Fatal("isolated account binding failed")
		}
		d.SetAuthentication(client, true, time.Now().Add(time.Hour))
		d.SetDownloadMode(downloader.DownloadModeKeep)
	}
	closeDownloader := func() {
		if d == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := d.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		d = nil
	}
	open()
	defer closeDownloader()
	scanResult := make(chan error, 1)
	api := New(d, func() { scanResult <- d.ScanAndDownload() })
	mux := http.NewServeMux()
	mux.HandleFunc("/api/intakes/preview", api.HandlePreview)
	mux.HandleFunc("/api/intakes/", api.HandleAction)
	gate, err := management.New("owned-intake-probe-key")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gate.Wrap(mux))
	defer server.Close()
	request := func(path string, body any) downloader.Intake {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer owned-intake-probe-key")
		req.Header.Set("Content-Type", "application/json")
		res, e := server.Client().Do(req)
		if e != nil {
			t.Fatal("loopback HTTP failed")
		}
		defer res.Body.Close()
		var intake downloader.Intake
		if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&intake) != nil {
			t.Fatalf("intake HTTP rejected: %d", res.StatusCode)
		}
		return intake
	}
	preview := request("/api/intakes/preview", map[string]any{"paths": []string{f.RemoteRoot + "/source/intake-tree"}})
	if preview.ID == "" || len(preview.Items) != 1 || preview.Mode != downloader.DownloadModeKeep {
		t.Fatal("unexpected preview")
	}
	proof["preview"] = preview
	proof["commit_intent_at_utc"] = time.Now().UTC()
	save()
	committed := request("/api/intakes/"+preview.ID+"/commit", struct{}{})
	if len(committed.Items) != 1 || committed.Items[0].State != "moved" || !committed.Items[0].SyncConfirmed {
		t.Fatal("move lacks synchronous completion evidence; never replay")
	}
	proof["committed"] = committed
	save()
	if err = <-scanResult; err != nil {
		t.Fatal(err)
	}
	rootID := committed.Items[0].Source.FsID
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		task := d.GetTaskByFsID(rootID)
		if task != nil && task.Status == "done" {
			proof["completed_task"] = task
			break
		}
		// source_unknown is also published while postprocessing is in flight.
		// The first real probe stopped here too early and canceled metadata
		// readback; wait for a confirmed terminal result without reissuing.
		if task != nil && (task.Status == "failed" || task.Status == "auth_wait") {
			t.Fatalf("monitor stopped: %s", task.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if proof["completed_task"] == nil {
		t.Fatal("monitor completion deadline")
	}
	server.Close()
	closeDownloader()
	actual, err := os.ReadFile(filepath.Join(f.LocalRoot, "intake-tree", "payload.txt"))
	if err != nil || sha256.Sum256(actual) != expectedSHA {
		t.Fatal("downloaded content mismatch")
	}
	if stat, e := os.Stat(filepath.Join(f.LocalRoot, "intake-tree", "empty")); e != nil || !stat.IsDir() {
		t.Fatal("empty directory missing")
	}
	remote, err := client.GetFileInfo(rootID)
	if err != nil || remote.Path != cfg.QueueDir+"/intake-tree.可删除" {
		t.Fatal("keep postprocess remote identity/path not confirmed")
	}
	open()
	if task := d.GetTaskByFsID(rootID); task == nil || task.Status != "done" {
		t.Fatal("completed task missing after reopen")
	}
	records := d.GetIntakes()
	if len(records) != 1 || records[0].ID != preview.ID || records[0].Items[0].State != "moved" {
		t.Fatal("intake receipt missing after reopen")
	}
	closeDownloader()
	current, err := os.ReadFile(credentialPath)
	if err != nil || sha256.Sum256(current) != credentialSHA {
		t.Fatal("original credentials changed")
	}
	err = filepath.WalkDir(filepath.Join(f.LocalRoot, ".nas-downloader"), func(path string, e os.DirEntry, walkErr error) error {
		if walkErr != nil || e.IsDir() {
			return walkErr
		}
		if e.Name() == "credentials.json" || e.Name() == "admin-key.json" || e.Name() == "baidu-app.json" {
			t.Fatal("credential sidecar copied")
		}
		b, e2 := os.ReadFile(path)
		if e2 != nil {
			return e2
		}
		if bytes.Contains(b, []byte(credentials.AccessToken)) || bytes.Contains(b, []byte(credentials.AppKey)) {
			t.Fatal("credential persisted in isolated state")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	proof["sha256"] = hex.EncodeToString(expectedSHA[:])
	proof["size"] = len(actual)
	proof["remote_keep_readback"] = remote
	proof["reopen_task_and_intake_verified"] = true
	proof["original_credentials_unchanged_no_copies"] = true
	proof["finished_at_utc"] = time.Now().UTC()
	save()
}
