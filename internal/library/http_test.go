package library

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type blockedDownload struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	release chan struct{}
	started bool
}

func (w *blockedDownload) Write(b []byte) (int, error) {
	if !w.started {
		w.started = true
		close(w.entered)
		<-w.release
	}
	return w.ResponseRecorder.Write(b)
}

func TestConfigurationCannotChangeDuringDownload(t *testing.T) {
	m, _, u := downloadable(t)
	c, err := m.Config()
	if err != nil {
		t.Fatal(err)
	}
	w := &blockedDownload{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	defer close(w.release)
	go func() { defer close(done); m.Handle(w, httptest.NewRequest("GET", u, nil)) }()
	select {
	case <-w.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("download never started")
	}
	if _, err = m.Configure(c.Root, c.Revision); !errors.Is(err, ErrBusy) {
		t.Fatalf("in-flight config was not rejected: %v", err)
	}
	if _, err = m.Write(context.Background(), WriteRequest{ID: opID('a'), Kind: "mkdir", RootRevision: c.Revision, Target: "during-download", ParentID: c.RootID}, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("write during download was not rejected: %v", err)
	}

}

func TestPrivateCaseVariantContentCannotBeDownloaded(t *testing.T) {
	m, root, _ := fixture(t)
	p := filepath.Join(root, ".ENV")
	if err := os.WriteFile(p, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := m.Configure(root, "")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	id, err := identity(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	q := url.Values{"path": {".ENV"}, "revision": {observationRevision(c, ".ENV", id, info)}}
	w := httptest.NewRecorder()
	m.Handle(w, httptest.NewRequest(http.MethodGet, "/api/library/content?"+q.Encode(), nil))
	if w.Code != 400 || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("private case content exposed: %d %q", w.Code, w.Body.String())
	}
}

func TestCanceledPreparationHasCorrelatedTerminalLog(t *testing.T) {
	m, _, u := downloadable(t)
	var output strings.Builder
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	m.Handle(w, httptest.NewRequest("GET", u, nil).WithContext(ctx))
	text := output.String()
	if w.Code != 503 || !strings.Contains(text, "event=download_prepare") || !strings.Contains(text, "event=download_response") || !strings.Contains(text, "context canceled") {
		t.Fatalf("missing terminal evidence: status=%d log=%s", w.Code, text)
	}
}

func downloadable(t *testing.T) (*Manager, string, string) {
	t.Helper()
	m, root, _ := fixture(t)
	p := filepath.Join(root, "中文.txt")
	if err := os.WriteFile(p, []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Configure(root, ""); err != nil {
		t.Fatal(err)
	}
	l, err := m.List(context.Background(), "", "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	u := "/api/library/content?" + url.Values{"path": {"中文.txt"}, "revision": {l.Entries[0].Revision}}.Encode()
	return m, p, u
}
func TestContentSupportsRangeHeadAndStrongContentConditions(t *testing.T) {
	m, p, u := downloadable(t)
	r := httptest.NewRequest("GET", u, nil)
	r.Header.Set("Range", "bytes=1-3")
	w := httptest.NewRecorder()
	m.Handle(w, r)
	if w.Code != 206 || w.Body.String() != "bcd" || w.Header().Get("Content-Range") != "bytes 1-3/6" {
		t.Fatalf("range %d %s %v", w.Code, w.Body.String(), w.Header())
	}
	oldTag := w.Header().Get("ETag")
	if !strings.HasPrefix(oldTag, `"sha256-`) || w.Header().Get("Content-Disposition") == "" {
		t.Fatal("content identity/disposition missing")
	}
	w = httptest.NewRecorder()
	m.Handle(w, httptest.NewRequest("HEAD", u, nil))
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "6" {
		t.Fatalf("head: %+v", w)
	}
	before, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, []byte("ABCDEF"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(p, time.Now(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", u, nil)
	r.Header.Set("If-Match", oldTag)
	w = httptest.NewRecorder()
	m.Handle(w, r)
	if w.Code != 412 {
		t.Fatalf("old content accepted despite same size/mtime: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", u, nil)
	r.Header.Set("Range", "bytes=1-3")
	r.Header.Set("If-Range", oldTag)
	w = httptest.NewRecorder()
	m.Handle(w, r)
	if w.Code != 200 || w.Body.String() != "ABCDEF" {
		t.Fatalf("If-Range did not restart with full new content: %d %q", w.Code, w.Body.String())
	}
}
func TestStaleListingCannotDownloadChangedFile(t *testing.T) {
	m, p, u := downloadable(t)
	if err := os.WriteFile(p, []byte("longer data"), 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	m.Handle(w, httptest.NewRequest("GET", u, nil))
	if w.Code != 412 {
		t.Fatalf("stale metadata accepted: %d", w.Code)
	}
	for _, raw := range []string{"/api/library/content?path=../private&revision=x", "/api/library/content?path=%2Fprivate&revision=x", "/api/library/content?path=a&path=b&revision=x"} {
		w = httptest.NewRecorder()
		m.Handle(w, httptest.NewRequest("GET", raw, nil))
		if w.Code < 400 || w.Code >= 500 {
			t.Errorf("unsafe request status %d: %s", w.Code, raw)
		}
	}
}
func TestConfigRequestRejectsUnknownFieldsAndListingsRetainStaleEvidence(t *testing.T) {
	m, root, _ := fixture(t)
	w := httptest.NewRecorder()
	m.Handle(w, httptest.NewRequest("POST", "/api/library/config", strings.NewReader(`{"root":"x","expected_revision":"","unexpected":true}`)))
	if w.Code != 400 {
		t.Fatalf("unknown fields: %d", w.Code)
	}
	if _, err := m.Configure(root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.List(context.Background(), "", "", 0, 50); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-offline"); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	m.Handle(w, httptest.NewRequest("GET", "/api/library/entries", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"stale":true`) || !strings.Contains(w.Body.String(), "observed_at_utc") {
		t.Fatalf("offline response: %d %s", w.Code, w.Body.String())
	}
}
