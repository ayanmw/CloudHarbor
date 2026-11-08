package uploads

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorageSnapshotCountsOnlyOwnedStagingFiles(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{strings.Repeat("a", 32) + ".data": "1234567", strings.Repeat("b", 32) + ".part": "12345678901", "uploads.sqlite": "ignored", "other.data": "ignored"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, strings.Repeat("c", 32)+".data"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := readStorageSnapshot(context.Background(), dir, func(string) (int64, error) { return 123, nil })
	if err != nil {
		t.Fatal(err)
	}
	if got.StagingBytes != "18" || got.StagingFiles != "2" || got.AvailableBytes == nil || *got.AvailableBytes != "123" || got.Partial {
		t.Fatalf("bad snapshot: %+v", got)
	}
}
func TestStorageSnapshotProbeFailureIsNotZeroAndCancellationIsReturned(t *testing.T) {
	dir := t.TempDir()
	got, err := readStorageSnapshot(context.Background(), dir, func(string) (int64, error) { return 0, errors.New("private provider error") })
	if err != nil {
		t.Fatal(err)
	}
	if got.AvailableBytes != nil || got.AvailableError == "" || strings.Contains(got.AvailableError, "private") {
		t.Fatalf("bad failure: %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = readStorageSnapshot(ctx, dir, func(string) (int64, error) { t.Fatal("probe after cancel"); return 0, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = readStorageSnapshot(context.Background(), filepath.Join(dir, "missing"), func(string) (int64, error) { return 0, nil }); err == nil {
		t.Fatal("missing directory accepted")
	}
}
func TestStorageStatusHTTPIsReadOnlyAndNotCached(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	w := httptest.NewRecorder()
	m.HandleStorage(w, httptest.NewRequest("GET", "/api/uploads/storage", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	m.HandleStorage(w, httptest.NewRequest("POST", "/api/uploads/storage", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal(w.Code)
	}
}

func TestStorageAvailableProbeAndSymlinkExclusion(t *testing.T) {
	dir := t.TempDir()
	available, err := uploadAvailableBytes(dir)
	if err != nil || available < 0 {
		t.Fatalf("available=%d error=%v", available, err)
	}
	target := filepath.Join(t.TempDir(), "outside")
	if err = os.WriteFile(target, []byte("outside bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, filepath.Join(dir, strings.Repeat("d", 32)+".data")); err != nil {
		t.Skip("symlink creation unavailable: " + err.Error())
	}
	got, err := readStorageSnapshot(context.Background(), dir, uploadAvailableBytes)
	if err != nil || got.StagingBytes != "0" || got.StagingFiles != "0" {
		t.Fatalf("followed symlink: %+v %v", got, err)
	}
}
