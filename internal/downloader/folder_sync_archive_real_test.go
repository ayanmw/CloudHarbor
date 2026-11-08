package downloader

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/foldersync"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

// Opt-in, read-only provider verification of an already moved owned fixture.
// Never calls Apply, starts a scheduler, copies credentials, or clears unknowns.
func TestRealOwnedArchiveReadback(t *testing.T) {
	descriptor := os.Getenv("NAS_REAL_ARCHIVE_FIXTURE")
	if descriptor == "" {
		t.Skip("requires explicit owned archive fixture descriptor")
	}
	var f struct {
		Release  string             `json:"release"`
		Config   foldersync.Config  `json:"config"`
		RunID    string             `json:"run_id"`
		Step     foldersync.Step    `json:"step"`
		Expected []foldersync.Entry `json:"expected"`
	}
	raw, err := os.ReadFile(descriptor)
	if err != nil || json.Unmarshal(raw, &f) != nil {
		t.Fatal("invalid fixture descriptor")
	}
	if !filepath.IsAbs(f.Release) || !strings.HasPrefix(f.Config.RemoteRoot, "/_NAS_Downloader_SyncTest_") || !strings.HasPrefix(f.Step.Path, "subtree-review-") || len(f.Expected) != 4 {
		t.Fatal("fixture boundary invalid")
	}
	for _, e := range f.Expected {
		if e.Path != f.Step.Path && !strings.HasPrefix(e.Path, f.Step.Path+"/") || e.Size < 0 || e.Size > 1024 {
			t.Fatal("fixture escaped owned small tree")
		}
	}
	queue := filepath.Join(f.Release, ".nas-downloader", "queue")
	var credentials struct {
		AccessToken string `json:"access_token"`
		AppKey      string `json:"app_key"`
	}
	raw, err = os.ReadFile(filepath.Join(queue, "credentials.json"))
	if err != nil || json.Unmarshal(raw, &credentials) != nil || credentials.AccessToken == "" || credentials.AppKey == "" {
		t.Fatal("original credentials unavailable")
	}
	store, err := taskstore.OpenUsingExistingScope(queue, "/queue")
	if err != nil {
		t.Fatal("original store unavailable; stop service first")
	}
	defer store.Close()
	account, err := store.BoundAccountFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	client := baiduapi.NewClient(credentials.AccessToken, credentials.AppKey)
	namespace, err := store.ResolveCloudCacheScope(account, client.ApplicationFingerprint(), client.Fingerprint(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	client = client.WithCloudCacheNamespace(namespace)
	d := NewDownloader(&config.Config{LocalDownDir: t.TempDir(), QueueDir: "/queue"}, client)
	defer d.cancel()
	d.queue.store = store
	d.SetAuthentication(client, true, time.Now().Add(time.Hour))
	ctx, cancel := context.WithTimeout(foldersync.WithRunID(context.Background(), f.RunID), 3*time.Minute)
	defer cancel()
	proof, err := (FolderSyncRemote{d}).VerifyArchive(ctx, f.Config, f.Step, f.RunID, f.Expected)
	if err != nil {
		t.Fatal("real archive readback failed:", err)
	}
	if proof.Version != "remote-archive-verification-v2" || proof.ExpectedContentIdentitySHA256 != proof.ObservedContentIdentitySHA256 || proof.ExpectedItems != "4" || proof.ObservedItems != "4" || len(proof.MtimeChanges) != 4 || proof.ExpectedSHA256 == proof.ObservedSHA256 {
		t.Fatal("real archive evidence incomplete")
	}
	raw, err = json.MarshalIndent(proof, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(descriptor), "subtree-archive-real-readback-v2.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
