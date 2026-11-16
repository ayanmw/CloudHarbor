package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestUploadStagedFilesOverlapPayloadButSerializeNamespace(t *testing.T) {
	testParallelUploadTransfers(t, "")
}

func TestUploadStagedFilesShareMissingParentWithoutDuplicateCreate(t *testing.T) {
	testParallelUploadTransfers(t, "/same")
}

func TestUploadStagedFileDoesNotWriteIntoQuarantinedParent(t *testing.T) {
	d, p, _, _, _ := intakeFixture(t)
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 700, Path: "/blocked", ServerFilename: "blocked", IsDir: 1}}
	if err := d.QuarantineUploadDirectory("/blocked"); err != nil {
		t.Fatal(err)
	}
	base := http.DefaultTransport
	writes := 0
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Query().Get("method") {
		case "precreate", "upload", "create":
			writes++
			return nil, io.ErrUnexpectedEOF
		}
		return base.RoundTrip(r)
	})
	local := filepath.Join(t.TempDir(), "payload")
	if err := os.WriteFile(local, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := d.UploadStagedFile(context.Background(), local, "/blocked/a.txt", func(string, int64) error { return nil }, nil)
	if writes != 0 || !errors.Is(err, baiduapi.ErrUploadUnconfirmed) {
		t.Fatalf("quarantined upload wrote: requests=%d err=%v", writes, err)
	}
}

func testParallelUploadTransfers(t *testing.T, prefix string) {
	t.Helper()
	d, p, _, _, _ := intakeFixture(t)
	p.metadataOverride = func(id int64, _ int) *baiduapi.FileInfo {
		if id == 901 {
			return &baiduapi.FileInfo{FsID: id, Path: prefix + "/a.txt", ServerFilename: "a.txt", Size: 3}
		}
		if id == 902 {
			return &baiduapi.FileInfo{FsID: id, Path: prefix + "/b.txt", ServerFilename: "b.txt", Size: 3}
		}
		return nil
	}
	base := http.DefaultTransport
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var mu sync.Mutex
	namespace, maximum, directoryCreates := 0, 0, 0
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		method := r.URL.Query().Get("method")
		body := ""
		switch method {
		case "precreate", "create":
			mu.Lock()
			namespace++
			if namespace > maximum {
				maximum = namespace
			}
			mu.Unlock()
			defer func() { mu.Lock(); namespace--; mu.Unlock() }()
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			remote := r.Form.Get("path")
			if method == "create" && r.Form.Get("isdir") == "1" {
				mu.Lock()
				directoryCreates++
				mu.Unlock()
				p.mu.Lock()
				p.lists["/"] = []baiduapi.FileInfo{{FsID: 700, Path: prefix, ServerFilename: "same", IsDir: 1}}
				p.mu.Unlock()
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0}`)), Request: r}, nil
			}

			if method == "precreate" {
				body = `{"errno":0,"return_type":1,"uploadid":"session-` + strings.TrimPrefix(remote, "/") + `","block_list":[0]}`
			} else {
				fsid := 901
				if remote == prefix+"/b.txt" {
					fsid = 902
				}
				body = fmt.Sprintf(`{"errno":0,"fs_id":%d,"path":%q,"size":3}`, fsid, remote)
			}
		case "upload":
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				return nil, err
			}
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			body = `{"errno":0,"md5":"900150983cd24fb0d6963f7d28e17f72"}`
		default:
			return base.RoundTrip(r)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	local := filepath.Join(t.TempDir(), "payload")
	if err := os.WriteFile(local, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, remote := range []string{prefix + "/a.txt", prefix + "/b.txt"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := d.UploadStagedFile(context.Background(), local, remote, func(string, int64) error { return nil }, nil)
			results <- err
		}()
	}
	defer func() { unblock(); wg.Wait() }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case err := <-results:
			unblock()
			t.Fatalf("transfer returned before both payloads overlapped: %v", err)
		case <-time.After(5 * time.Second):
			unblock()
			t.Fatal("payload streams did not overlap")
		}
	}
	unblock()
	wg.Wait()
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if prefix != "" && directoryCreates != 1 {
		t.Fatalf("duplicate parent creation: %d", directoryCreates)
	}
	if maximum != 1 {
		t.Fatalf("overlapping namespace requests=%d", maximum)
	}
	d.inventoryScanMutex.Lock()
	defer d.inventoryScanMutex.Unlock()
	if d.inventoryScanStarting || d.uploadLeaseMembers != 0 {
		t.Fatal("upload admission leaked")
	}
}
