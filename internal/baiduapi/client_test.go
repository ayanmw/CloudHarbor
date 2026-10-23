package baiduapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type apiTransport func(*http.Request) (*http.Response, error)

func (f apiTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestListDirReadsAllPages(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	calls := 0
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		start := r.URL.Query().Get("start")
		var files []FileInfo
		if start == "0" {
			for id := int64(1); id <= 1000; id++ {
				files = append(files, FileInfo{FsID: id})
			}
		} else if start == "1000" {
			files = []FileInfo{{FsID: 1001}}
		} else {
			t.Errorf("unexpected start %q", start)
		}
		b, _ := json.Marshal(ListResponse{List: files})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b))), Request: r}, nil
	})
	files, err := NewClient("test", "test").ListDir("/queue")
	if err != nil || len(files) != 1001 || calls != 2 {
		t.Fatalf("files=%d calls=%d err=%v", len(files), calls, err)
	}
}

func TestListDirPageRequestsOnlyRequestedWindow(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	calls := 0
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("dir") != "/queue" || r.URL.Query().Get("start") != "50" || r.URL.Query().Get("limit") != "3" {
			t.Fatalf("unexpected listing query: %s", r.URL.RawQuery)
		}
		files := []FileInfo{
			{FsID: 51, Path: "/queue/a", ServerFilename: "a"},
			{FsID: 52, Path: "/queue/b", ServerFilename: "b"},
			{FsID: 53, Path: "/queue/c", ServerFilename: "c"},
		}
		b, _ := json.Marshal(ListResponse{List: files})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b))), Request: r}, nil
	})
	files, hasMore, err := NewClient("test", "test").ListDirPage("/queue", 50, 2)
	if err != nil || len(files) != 2 || !hasMore || calls != 1 || files[0].FsID != 51 || files[1].FsID != 52 {
		t.Fatalf("files=%+v hasMore=%v calls=%d err=%v", files, hasMore, calls, err)
	}
}

func TestGetFileInfoReadsCurrentIdentityByFSID(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") != "filemetas" || r.URL.Query().Get("fsids") != "[123]" || r.URL.Query().Get("dlink") != "0" {
			t.Fatalf("unexpected metadata query: %s", r.URL.RawQuery)
		}
		info := FileInfo{FsID: 123, Path: "/queue/a.txt", ServerFilename: "a.txt", Size: 3, Mtime: 42}
		body, _ := json.Marshal(map[string]any{"errno": 0, "list": []FileInfo{info}})
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
	})
	got, err := NewClient("test", "app").GetFileInfo(123)
	if err != nil || got != (FileInfo{FsID: 123, Path: "/queue/a.txt", ServerFilename: "a.txt", Size: 3, Mtime: 42}) {
		t.Fatalf("metadata=%+v err=%v", got, err)
	}
}

func TestDownloadRequestCanBeCancelled(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		body, _, err := NewClient("test", "test").WithContext(ctx).DownloadFileWithRange(srv.URL, 0)
		if body != nil {
			body.Close()
		}
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel cause lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not unblock HTTP")
	}
}

func TestHTTPRetryClassification(t *testing.T) {
	for _, status := range []int{401, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
			})
			_, err := NewClient("test", "test").ListDir("/queue")
			if err == nil || IsRetryable(err) != (status != 401) {
				t.Fatalf("status=%d retry=%v err=%v", status, IsRetryable(err), err)
			}
		})
	}
}

func TestFileManagerRejectsPerItemFailure(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0,"info":[{"errno":-9,"path":"/queue/a"}]}`)), Request: r}, nil
	})
	c := NewClient("test", "test")
	if err := c.RenameFile("/queue/a", "b"); err == nil {
		t.Error("rename item error ignored")
	}
	if err := c.DeleteFile("/queue/a"); err == nil {
		t.Error("delete item error ignored")
	}
}

func TestListDirRejectsInvalidSuccessResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"HTTP failure", 503, `{}`}, {"missing errno", 200, `{"list":[]}`}, {"missing list", 200, `{"errno":0}`}, {"null list", 200, `{"errno":0,"list":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})
			if _, err := NewClient("test", "test").ListDir("/queue"); err == nil {
				t.Fatal("invalid response treated as empty directory")
			}
		})
	}
}

func TestListDirRejectsOversizedResponse(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	body := `{"errno":0,"list":[]}` + strings.Repeat(" ", 16*1024*1024)
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	if _, err := NewClient("test", "test").ListDir("/queue"); err == nil {
		t.Fatal("oversized directory response treated as complete")
	}
}

func TestGetDownloadLinkRejectsOversizedResponse(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	body := `{"errno":0,"list":[{"dlink":"https://download.example/file"}]}` + strings.Repeat(" ", 1024*1024)
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	if _, err := NewClient("test", "test").GetDownloadLink(1); err == nil {
		t.Fatal("oversized download-link response treated as complete")
	}
}

func TestListDirOversizedHTTPFailureRemainsRetryable(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	body := strings.Repeat(" ", 16*1024*1024+1)
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	_, err := NewClient("test", "test").ListDir("/queue")
	if err == nil || !IsRetryable(err) {
		t.Fatalf("oversized HTTP failure lost retry classification: %v", err)
	}
}

func TestFileVersionUsesOfficialServerMtime(t *testing.T) {
	var f FileInfo
	if err := json.Unmarshal([]byte(`{"fs_id":1,"server_mtime":987,"size":6}`), &f); err != nil {
		t.Fatal(err)
	}
	if f.Mtime != 987 {
		t.Fatalf("server_mtime lost: %+v", f)
	}
}

func TestTransportErrorsDoNotExposeAccessToken(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) { return nil, fmt.Errorf("network unavailable") })
	c := NewClient("private-token", "test")
	_, err := c.ListDir("/queue")
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("credential in transport error: %v", err)
	}
}

func TestMalformedDownloadLinkDoesNotExposeAccessToken(t *testing.T) {
	c := NewClient("private-token", "test")
	for _, download := range []func(string) (io.ReadCloser, int64, error){c.DownloadFile, func(link string) (io.ReadCloser, int64, error) { return c.DownloadFileWithRange(link, 3) }} {
		body, _, err := download("https://example.test/%zz")
		if body != nil {
			body.Close()
		}
		if err == nil || strings.Contains(err.Error(), "private-token") {
			t.Errorf("credential in malformed URL error: %v", err)
		}
	}
}

func TestMetadataFailuresPreserveRetryCause(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		t.Run(fmt.Sprint(truncated), func(t *testing.T) {
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				status := 503
				body := io.ReadCloser(io.NopCloser(strings.NewReader("upstream unavailable")))
				if truncated {
					status = 200
					body = &brokenMetadataBody{}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: body, Request: r}, nil
			})
			c := NewClient("test", "test")
			_, linkErr := c.GetDownloadLink(1)
			for _, err := range []error{linkErr, c.RenameFile("/a", "b"), c.DeleteFile("/a")} {
				if err == nil || !IsRetryable(err) {
					t.Errorf("retry cause lost: %v", err)
				}
			}
		})
	}
}

func TestIdleDownloadTimesOutAndIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "6")
		io.WriteString(w, "abc")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	body, _, err := NewClient("test", "test").WithIdleTimeout(30*time.Millisecond).DownloadFileWithRange(srv.URL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	_, err = io.ReadAll(body)
	if err == nil || !IsRetryable(err) {
		t.Fatalf("idle connection did not return retryable timeout: %v", err)
	}
}

func TestProgressingDownloadCanOutliveIdleLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		for _, b := range []byte("abcde") {
			if _, err := w.Write([]byte{b}); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			time.Sleep(150 * time.Millisecond)
		}
	}))
	defer srv.Close()
	started := time.Now()
	body, _, err := NewClient("test", "test").WithIdleTimeout(500*time.Millisecond).DownloadFileWithRange(srv.URL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	bytes, err := io.ReadAll(body)
	if err != nil || string(bytes) != "abcde" || time.Since(started) <= 500*time.Millisecond {
		t.Fatalf("progressing long download failed: %q %v", bytes, err)
	}
}

func TestUserIDPreservesFullIntegerAndRejectsAuthenticationFailure(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	body := `{"errno":0,"uk":3400000000000000123}`
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	c := NewClient("test", "test")
	id, err := c.UserID()
	if err != nil || id != "3400000000000000123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	body = `{"error_code":111,"error_msg":"token expired"}`
	_, err = c.UserID()
	if !IsAuthenticationError(err) {
		t.Fatalf("authentication failure not classified: %v", err)
	}
	body = `{"errno":0,"uk":0}`
	if _, err = c.UserID(); err == nil {
		t.Fatal("zero account accepted")
	}
}

type brokenMetadataBody struct{}

func (*brokenMetadataBody) Read(p []byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (*brokenMetadataBody) Close() error               { return nil }

func TestDownloadRangeContract(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		contentRange string
		offset       int64
		wantErr      bool
	}{
		{"ignored range", 200, "", 3, true},
		{"wrong start", 206, "bytes 0-2/6", 3, true},
		{"missing range", 206, "", 3, true},
		{"impossible total", 206, "bytes 3-5/5", 3, true},
		{"partial suffix", 206, "bytes 3-4/6", 3, true},
		{"valid suffix", 206, "bytes 3-5/6", 3, false},
		{"fresh full", 200, "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.offset > 0 && r.Header.Get("Range") != "bytes=3-" {
					t.Errorf("Range=%q", r.Header.Get("Range"))
				}
				if tc.contentRange != "" {
					w.Header().Set("Content-Range", tc.contentRange)
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, "def")
			}))
			defer srv.Close()
			body, _, err := NewClient("test", "test").DownloadFileWithRange(srv.URL, tc.offset)
			if body != nil {
				defer body.Close()
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v, wantErr=%v", err, tc.wantErr)
			}
			if err == nil {
				b, _ := io.ReadAll(body)
				if string(b) != "def" {
					t.Fatalf("body=%q", b)
				}
			}
		})
	}
}
