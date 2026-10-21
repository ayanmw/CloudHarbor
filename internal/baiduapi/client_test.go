package baiduapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
