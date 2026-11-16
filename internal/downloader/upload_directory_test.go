package downloader

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestEnsureUploadDirectoryDistinguishesExistingAndFile(t *testing.T) {
	for _, directory := range []int{0, 1} {
		d, p, _, _, _ := intakeFixture(t)
		p.lists["/"] = []baiduapi.FileInfo{{FsID: 123, Path: "/folder", ServerFilename: "folder", IsDir: directory}}
		result, at, err := d.EnsureUploadDirectory(context.Background(), "/folder")
		if directory == 1 {
			if err != nil || result.Status != "existing" || result.ObjectID != 123 || at.IsZero() {
				t.Fatalf("result=%+v at=%v err=%v", result, at, err)
			}
		} else if err == nil || result.Status != "failed" {
			t.Fatalf("same-name file accepted: %+v %v", result, err)
		}
	}
}

func TestEnsureUploadDirectoryKeepsMutationReceiptAndUnknown(t *testing.T) {
	for _, lost := range []bool{false, true} {
		d, p, _, _, _ := intakeFixture(t)
		p.lists["/"] = []baiduapi.FileInfo{}
		base := http.DefaultTransport
		calls := 0
		http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Query().Get("method") != "create" {
				return base.RoundTrip(r)
			}
			calls++
			p.mu.Lock()
			p.lists["/"] = []baiduapi.FileInfo{{FsID: 456, Path: "/empty", ServerFilename: "empty", IsDir: 1}}
			p.mu.Unlock()
			if lost {
				return nil, io.ErrUnexpectedEOF
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0}`)), Request: r}, nil
		})
		result, at, err := d.EnsureUploadDirectory(context.Background(), "/empty")
		if lost {
			if err == nil || result.Status != "unknown" {
				t.Fatalf("lost result=%+v err=%v", result, err)
			}
		} else if err != nil || result.Status != "confirmed" || result.ObjectID != 456 || at.IsZero() {
			t.Fatalf("result=%+v at=%v err=%v", result, at, err)
		}
		if calls != 1 || result.OperationID == "" {
			t.Fatalf("calls=%d result=%+v", calls, result)
		}
		if lost {
			again, _, retryErr := d.EnsureUploadDirectory(context.Background(), "/empty")
			if retryErr == nil || again.Status != "unknown" || calls != 1 {
				t.Fatalf("unknown write became existing or replayed: %+v err=%v calls=%d", again, retryErr, calls)
			}
		}
	}
}

func TestEnsureUploadDirectoryQuarantinesLostDurableCreateReceipt(t *testing.T) {
	d, p, _, _, _ := intakeFixture(t)
	p.lists["/"] = []baiduapi.FileInfo{}
	base := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") != "create" {
			return base.RoundTrip(r)
		}
		p.mu.Lock()
		p.lists["/"] = []baiduapi.FileInfo{{FsID: 456, Path: "/empty", ServerFilename: "empty", IsDir: 1}}
		p.mu.Unlock()
		if err := d.queue.store.Close(); err != nil {
			t.Error(err)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0}`)), Request: r}, nil
	})
	result, _, err := d.EnsureUploadDirectory(context.Background(), "/empty")
	if err == nil || result.Status != "unknown" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	d.queue.mu.Lock()
	blocked := d.queue.isRemoteBlocked(0, "/empty")
	d.queue.mu.Unlock()
	if !blocked {
		t.Fatal("successful mkdir with lost durable receipt was not quarantined")
	}
}

func TestEnsureUploadDirectoryRechecksQuarantineAfterListing(t *testing.T) {
	d, p, _, _, _ := intakeFixture(t)
	p.lists["/"] = []baiduapi.FileInfo{{FsID: 456, Path: "/empty", ServerFilename: "empty", IsDir: 1}}
	base := http.DefaultTransport
	changed := false
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if !changed && r.URL.Query().Get("method") == "list" && r.URL.Query().Get("dir") == "/" {
			changed = true
			d.quarantineCloudMutationSource(0, "", "/empty")
		}
		return base.RoundTrip(r)
	})
	result, _, err := d.EnsureUploadDirectory(context.Background(), "/empty")
	if !changed || err == nil || result.Status != "unknown" {
		t.Fatalf("quarantine bypassed: %+v err=%v changed=%v", result, err, changed)
	}
}
