package downloader

import (
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
