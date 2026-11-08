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
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

// Explicit opt-in only: the descriptor identifies five tiny files created by
// the real acceptance harness. No global scanner or provider mutation runs here.
func TestRealDuplicateFixtureContentVerification(t *testing.T) {
	descriptor := os.Getenv("NAS_REAL_DUPLICATE_FIXTURE")
	if descriptor == "" {
		t.Skip("requires explicit real-account fixture descriptor")
	}
	var fixture struct {
		Root    string `json:"root"`
		Release string `json:"release"`
		SHA256  string `json:"sha256"`
	}
	body, err := os.ReadFile(descriptor)
	if err != nil || json.Unmarshal(body, &fixture) != nil {
		t.Fatal("fixture descriptor unreadable")
	}
	if !strings.HasPrefix(fixture.Root, "/_NAS_Downloader_Dedupe_Test_") || strings.ContainsAny(strings.TrimPrefix(fixture.Root, "/"), "/\\") || len(fixture.SHA256) != 64 || !filepath.IsAbs(fixture.Release) {
		t.Fatal("invalid fixture boundary")
	}
	queue := filepath.Join(fixture.Release, ".nas-downloader", "queue")
	var credentials struct {
		AccessToken string `json:"access_token"`
		AppKey      string `json:"app_key"`
	}
	body, err = os.ReadFile(filepath.Join(queue, "credentials.json"))
	if err != nil || json.Unmarshal(body, &credentials) != nil || credentials.AccessToken == "" || credentials.AppKey == "" {
		t.Fatal("original credentials unavailable")
	}
	store, err := taskstore.OpenUsingExistingScope(queue, "/queue")
	if err != nil {
		t.Fatal("original store unavailable; service must be stopped")
	}
	defer store.Close()
	account, err := store.BoundAccountFingerprint()
	if err != nil {
		t.Fatal("account binding unavailable")
	}
	client := baiduapi.NewClient(credentials.AccessToken, credentials.AppKey)
	namespace, err := store.ResolveCloudCacheScope(account, client.ApplicationFingerprint(), client.Fingerprint(), time.Now().UTC())
	if err != nil {
		t.Fatal("cache scope unavailable")
	}
	client = client.WithCloudCacheNamespace(namespace)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	type verified struct {
		Path       string    `json:"path"`
		SHA256     string    `json:"sha256"`
		VerifiedAt time.Time `json:"verified_at_utc"`
	}
	result := []verified{}
	for _, dir := range []string{"a", "b", "ab"} {
		items, err := client.ListDir(fixture.Root + "/" + dir)
		if err != nil {
			t.Fatal("real fixture listing failed")
		}
		expected := 2
		if dir == "ab" {
			expected = 1
		}
		if len(items) != expected {
			t.Fatal("unexpected fixture count")
		}
		for _, info := range items {
			if info.IsDir != 0 || info.Size <= 0 || info.Size > 1024 || !strings.HasPrefix(info.Path, fixture.Root+"/"+dir+"/") {
				t.Fatal("fixture escaped small-file boundary")
			}
			item := taskstore.CloudInventoryEntry{FsID: info.FsID, Path: info.Path, Name: info.ServerFilename, Size: info.Size, ProviderMtime: info.Mtime, ContentMD5: info.MD5}
			digest, err := hashCloudFile(ctx, client, item)
			if err != nil {
				t.Fatal("real content verification failed")
			}
			if digest != fixture.SHA256 {
				t.Fatal("real downloaded content mismatch")
			}
			at := time.Now().UTC()
			if err = store.SaveVerifiedCloudContentHash(namespace, item, digest, at); err != nil {
				t.Fatal("verified cache publication failed")
			}
			result = append(result, verified{info.Path, digest, at})
		}
	}
	body, err = json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(descriptor), "dedupe-real-hashes.json"), body, 0600); err != nil {
		t.Fatal("verification evidence write failed")
	}
}
