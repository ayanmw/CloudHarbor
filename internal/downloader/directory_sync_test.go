package downloader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/durablefs"
)

func TestDirectorySyncFailureCannotAuthorizeSourceOperationOnRetry(t *testing.T) {
	original, provider := setup(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "abc") })
	original.cfg.MaxRetries = 0
	d, err := OpenDownloader(original.cfg, original.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	var syncCalls atomic.Int32
	originalSync := durablefs.SyncDirectory
	syncPublishedDirectory = func(string) error {
		syncCalls.Add(1)
		return errors.New("simulated directory sync failure")
	}
	t.Cleanup(func() { syncPublishedDirectory = originalSync })
	provider.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3)}
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 1, "failed")
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	if err = d.Retry(1); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, 1, "failed")
	if syncCalls.Load() < 2 {
		t.Fatalf("published-file retry skipped directory sync: calls=%d", syncCalls.Load())
	}
	provider.mu.Lock()
	actions := append([]string(nil), provider.actions...)
	provider.mu.Unlock()
	if len(actions) != 0 {
		t.Fatalf("remote source operation ran before directory durability was confirmed: %v", actions)
	}
	if _, err = os.Stat(filepath.Join(d.cfg.LocalDownDir, "a.txt")); err != nil {
		t.Fatalf("expected locally published file for retry scenario: %v", err)
	}
}

func TestRetryRemovesOrphanedPublishedPartialHardlink(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "abc") })
	info := file(1, "a.txt", 3)
	local := filepath.Join(d.cfg.LocalDownDir, "a.txt")
	var removeCalls atomic.Int32
	originalRemove := removePublishedPartial
	removePublishedPartial = func(name string) error {
		if filepath.Base(name) == "content.part" && removeCalls.Add(1) == 1 {
			return errors.New("simulated transient unlink failure")
		}
		return os.Remove(name)
	}
	t.Cleanup(func() { removePublishedPartial = originalRemove })
	if err := d.transfer(context.Background(), info, "a.txt", local); err == nil {
		t.Fatal("first transfer ignored the failed partial unlink")
	}
	partials, err := filepath.Glob(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "*", "content.part"))
	if err != nil || len(partials) != 1 {
		t.Fatalf("expected one orphaned published hardlink: %v %v", partials, err)
	}
	if err = d.transfer(context.Background(), info, "a.txt", local); err != nil {
		t.Fatalf("retry did not accept verified final file: %v", err)
	}
	if _, err = os.Stat(partials[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retry left orphaned partial hardlink: %v", err)
	}
}

func TestCleanupDoesNotRemoveUnrelatedPartialFile(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "final.bin")
	partialPath := filepath.Join(dir, "partial.bin")
	if err := os.WriteFile(finalPath, []byte("verified final"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partialPath, []byte("different partial"), 0600); err != nil {
		t.Fatal(err)
	}
	finalInfo, err := os.Stat(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	removePublishedPartialIfLinked(finalInfo, partialPath)
	body, err := os.ReadFile(partialPath)
	if err != nil || string(body) != "different partial" {
		t.Fatalf("unrelated partial changed during cleanup: %q %v", body, err)
	}
}
