package downloader

import (
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestScanAndDownloadReportsSkippedInsteadOfSuccess(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "abc") })
	d.scanMutex.Lock()
	if err := d.ScanAndDownload(); !errors.Is(err, ErrQueueMonitorScanSkipped) {
		d.scanMutex.Unlock()
		t.Fatalf("overlapping scan err=%v", err)
	}
	d.scanMutex.Unlock()
	d.queue.mu.Lock()
	d.queue.maintenance = true
	d.queue.mu.Unlock()
	if err := d.ScanAndDownload(); !errors.Is(err, ErrQueueMonitorScanSkipped) {
		t.Fatalf("maintenance scan err=%v", err)
	}
	if len(p.listRequests) != 0 {
		t.Fatalf("skipped scans called Baidu list API: %v", p.listRequests)
	}
}
