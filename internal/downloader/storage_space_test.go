package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
)

func TestLocalAvailableBytesReadsExistingDirectory(t *testing.T) {
	directory := t.TempDir()
	available, err := localAvailableBytes(directory)
	if err != nil {
		t.Fatalf("read available bytes for %s: %v", directory, err)
	}
	if available < 0 {
		t.Fatalf("available bytes = %d, want non-negative", available)
	}
}

func TestLocalAvailableBytesRejectsMissingDirectory(t *testing.T) {
	_, err := localAvailableBytes(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("missing directory unexpectedly had an available-space value")
	}
}

func TestDownloaderStorageSpaceProbeCanBeInjected(t *testing.T) {
	d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
	defer d.Shutdown(context.Background())
	if d.localSpaceProbe == nil {
		t.Fatal("Downloader local space probe is not initialized")
	}
	d.localSpaceProbe = func(string) (int64, error) { return 1234, nil }
	available, err := d.localSpaceProbe("ignored")
	if err != nil || available != 1234 {
		t.Fatalf("injected local space probe = %d, %v", available, err)
	}
}

func TestLocalSpaceReservationAccountsForActiveAndWrittenBytes(t *testing.T) {
	d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
	defer d.Shutdown(context.Background())
	d.localSpaceProbe = func(string) (int64, error) { return 100, nil }

	first, err := d.reserveLocalSpace(1, t.TempDir(), 80)
	if err != nil {
		t.Fatalf("reserve first download: %v", err)
	}
	if _, err := d.reserveLocalSpace(2, t.TempDir(), 21); err == nil {
		t.Fatal("second download exceeded available capacity")
	}
	first.UpdateRemaining(40)
	second, err := d.reserveLocalSpace(2, t.TempDir(), 60)
	if err != nil {
		t.Fatalf("reserve after first download wrote 40 bytes: %v", err)
	}
	first.Release()
	first.Release()
	second.Release()
	if _, err := d.reserveLocalSpace(3, t.TempDir(), 100); err != nil {
		t.Fatalf("released capacity was not available: %v", err)
	}
}

func TestDownloadRejectsInsufficientLocalSpaceBeforeProviderRequest(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "payload") })
	d.localSpaceProbe = func(string) (int64, error) { return 3, nil }
	info := baiduapi.FileInfo{FsID: 501, ServerFilename: "payload.bin", Size: 7}
	local := filepath.Join(d.LocalDownloadDirectory(), info.ServerFilename)
	err := d.transfer(context.Background(), info, info.ServerFilename, local)
	if err == nil {
		t.Fatal("download unexpectedly proceeded with insufficient local space")
	}
	p.mu.Lock()
	calls := p.downloadLinkCalls
	p.mu.Unlock()
	if calls != 0 {
		t.Fatalf("provider download-link requests = %d, want 0", calls)
	}
}

func TestDownloadRejectsUnknownLocalSpaceBeforeProviderRequest(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "payload") })
	d.localSpaceProbe = func(string) (int64, error) { return 0, errors.New("probe unavailable") }
	info := baiduapi.FileInfo{FsID: 502, ServerFilename: "payload.bin", Size: 7}
	err := d.transfer(context.Background(), info, info.ServerFilename, filepath.Join(d.LocalDownloadDirectory(), info.ServerFilename))
	if err == nil {
		t.Fatal("download unexpectedly proceeded when local capacity was unknown")
	}
	p.mu.Lock()
	calls := p.downloadLinkCalls
	p.mu.Unlock()
	if calls != 0 {
		t.Fatalf("provider download-link requests = %d, want 0", calls)
	}
}

func TestFullSizeUnverifiedPartialRequiresFullCapacityWithoutTruncation(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "payload") })
	info := baiduapi.FileInfo{FsID: 504, ServerFilename: "payload.bin", Size: 7}
	relative := info.ServerFilename
	key := sha256.Sum256([]byte(relative))
	state := filepath.Join(d.LocalDownloadDirectory(), stateDirectory, hex.EncodeToString(key[:]))
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(state, "content.part")
	if err := os.WriteFile(partial, []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeRecord(filepath.Join(state, "partial.json"), recordFor(info, relative)); err != nil {
		t.Fatal(err)
	}
	d.localSpaceProbe = func(string) (int64, error) { return 6, nil }
	err := d.transfer(context.Background(), info, relative, filepath.Join(d.LocalDownloadDirectory(), info.ServerFilename))
	var spaceErr *localStorageSpaceError
	if !errors.As(err, &spaceErr) || spaceErr.RequiredBytes != 7 {
		t.Fatalf("full-size unverified partial preflight = %v; want 7-byte requirement", err)
	}
	contents, readErr := os.ReadFile(partial)
	if readErr != nil || string(contents) != "payload" {
		t.Fatalf("partial was truncated before capacity check: %q err=%v", contents, readErr)
	}
	p.mu.Lock()
	calls := p.downloadLinkCalls
	p.mu.Unlock()
	if calls != 0 {
		t.Fatalf("provider download-link requests = %d, want 0", calls)
	}
}

func TestManualRetryAfterSpaceReleaseResumesAndVerifiesFile(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=3-" {
			w.Header().Set("Content-Range", "bytes 3-6/7")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "load")
			return
		}
		_, _ = io.WriteString(w, "payload")
	})
	info := baiduapi.FileInfo{FsID: 505, ServerFilename: "payload.bin", Size: 7}
	relative := info.ServerFilename
	key := sha256.Sum256([]byte(relative))
	state := filepath.Join(d.LocalDownloadDirectory(), stateDirectory, hex.EncodeToString(key[:]))
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "content.part"), []byte("pay"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeRecord(filepath.Join(state, "partial.json"), recordFor(info, relative)); err != nil {
		t.Fatal(err)
	}
	available := int64(2)
	d.localSpaceProbe = func(string) (int64, error) { return available, nil }
	local := filepath.Join(d.LocalDownloadDirectory(), info.ServerFilename)
	if err := d.transfer(context.Background(), info, relative, local); err == nil {
		t.Fatal("first attempt unexpectedly bypassed insufficient-space block")
	}
	partialBefore, err := os.ReadFile(filepath.Join(state, "content.part"))
	if err != nil || string(partialBefore) != "pay" {
		t.Fatalf("partial after blocked attempt = %q err=%v", partialBefore, err)
	}
	available = 4
	if err = d.transfer(context.Background(), info, relative, local); err != nil {
		t.Fatalf("manual retry after freeing space: %v", err)
	}
	contents, err := os.ReadFile(local)
	if err != nil || string(contents) != "payload" {
		t.Fatalf("published retry content = %q err=%v", contents, err)
	}
	receipt, err := readRecord(filepath.Join(state, "complete.json"))
	wantDigest := sha256.Sum256([]byte("payload"))
	if err != nil || receipt.SHA256 != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("verified retry receipt = %+v err=%v", receipt, err)
	}
}

func TestDownloadClassifiesDiskFullAndPreservesPartial(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "payload") })
	d.localSpaceProbe = func(string) (int64, error) { return 100, nil }
	oldWrite := writePartialChunk
	oldFullCheck := localStorageFullCheck
	localStorageFullCheck = func(err error) bool { return errors.Is(err, syscall.ENOSPC) }
	writePartialChunk = func(file *os.File, data []byte) (int, error) {
		count, err := file.Write(data[:3])
		if err != nil {
			return count, err
		}
		return count, syscall.ENOSPC
	}
	t.Cleanup(func() { writePartialChunk = oldWrite; localStorageFullCheck = oldFullCheck })
	info := baiduapi.FileInfo{FsID: 503, ServerFilename: "payload.bin", Size: 7}
	local := filepath.Join(d.LocalDownloadDirectory(), info.ServerFilename)
	err := d.transfer(context.Background(), info, info.ServerFilename, local)
	var spaceErr *localStorageSpaceError
	if !errors.As(err, &spaceErr) || spaceErr.Kind != localStorageFull {
		t.Fatalf("disk full error = %v, want typed local storage full error", err)
	}
	if _, statErr := os.Stat(local); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("final file stat error = %v, want file not published", statErr)
	}
	stateDir := filepath.Join(d.LocalDownloadDirectory(), stateDirectory)
	entries, readErr := os.ReadDir(stateDir)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("partial state entries = %v, err=%v", entries, readErr)
	}
	partial := filepath.Join(stateDir, entries[0].Name(), "content.part")
	if stat, statErr := os.Stat(partial); statErr != nil || stat.Size() != 3 {
		t.Fatalf("partial after disk full = %+v, err=%v; want 3 bytes retained", stat, statErr)
	}
	if _, exists := d.localSpaceReservations[info.FsID]; exists {
		t.Fatal("disk-full path retained local capacity reservation")
	}
}
