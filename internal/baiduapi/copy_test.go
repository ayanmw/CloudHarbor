package baiduapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCopyWireContractAgainstLocalServer(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		var items []map[string]string
		if r.Method != "POST" || r.URL.Query().Get("opera") != "copy" || r.PostForm.Get("async") != "0" || r.PostForm.Get("ondup") != "fail" || json.Unmarshal([]byte(r.PostForm.Get("filelist")), &items) != nil || len(items) != 1 || items[0]["path"] != "/来源/中文 a.pdf" || items[0]["dest"] != "/目标" || items[0]["newname"] != "新名称.pdf" {
			t.Error("wire copy contract changed")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"errno":0,"info":[{"errno":0}]}`)
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		request := r.Clone(r.Context())
		endpoint := *r.URL
		endpoint.Scheme, endpoint.Host = target.Scheme, target.Host
		request.URL = &endpoint
		return original.RoundTrip(request)
	})
	if err := NewClient("test-token", "app").CopyFile("/来源/中文 a.pdf", "/目标", "新名称.pdf"); err != nil || requests != 1 {
		t.Fatalf("requests=%d err=%v", requests, err)
	}
}

func TestCopySynchronousReceiptAndNoOverwrite(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	for _, tc := range []struct {
		body string
		want error
	}{
		{`{"errno":0,"info":[{"errno":0}]}`, nil},
		{`{"errno":0}`, ErrCopyUnconfirmed},
		{`{"errno":0,"info":[{}]}`, ErrCopyUnconfirmed},
		{`{"errno":0,"info":[{"errno":0},{"errno":0}]}`, ErrCopyUnconfirmed},
		{`{"errno":0,"taskid":123,"info":[{"errno":0}]}`, ErrCopyAsync},
		{`{"errno":-9,"taskid":123,"info":[{"errno":-8}]}`, ErrCopyAsync},
		{`{"errno":111,"info":[{"errno":0}]}`, ErrCopyAsync},
		{`{"errno":0,"info":[{"errno":111}]}`, ErrCopyAsync},
		{`{"info":[{"errno":0}]}`, ErrCopyUnconfirmed},
		{`broken JSON`, ErrCopyUnconfirmed},
	} {
		t.Run(tc.body, func(t *testing.T) {
			calls := 0
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Method != "POST" || r.URL.Query().Get("opera") != "copy" || r.PostForm.Get("async") != "0" || r.PostForm.Get("ondup") != "fail" {
					t.Fatal("copy protocol mismatch")
				}
				var items []map[string]string
				if json.Unmarshal([]byte(r.PostForm.Get("filelist")), &items) != nil || len(items) != 1 || items[0]["path"] != "/来源/中文 a.pdf" || items[0]["dest"] != "/目标" || items[0]["newname"] != "新名称.pdf" || items[0]["ondup"] != "fail" {
					t.Fatal("copy identity mismatch")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			err := NewClient("private-test-token", "app").CopyFile("/来源/中文 a.pdf", "/目标", "新名称.pdf")
			if calls != 1 || (tc.want == nil && err != nil) || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("calls=%d err=%v want=%v", calls, err, tc.want)
			}
		})
	}
}

func TestCopyFailureRetainsOperationAndDoesNotExposeToken(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	for _, body := range []string{`{"errno":-9}`, `{"errno":0,"info":[{"errno":-8}]}`} {
		http.DefaultTransport = apiTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		err := NewClient("private-copy-token", "app").CopyFile("/a", "/b", "a")
		var provider *APIError
		if !errors.As(err, &provider) || provider.Operation != "copy" || strings.Contains(err.Error(), "private-copy-token") {
			t.Fatalf("copy failure lost operation or exposed token: %v", err)
		}
	}
}

func TestCopyNeverFollowsRedirectOrRetriesNetworkFailure(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	for _, status := range []int{307, 308, 0} {
		calls := 0
		http.DefaultTransport = apiTransport(func(*http.Request) (*http.Response, error) {
			calls++
			if status == 0 {
				return nil, errors.New("reply lost")
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://pan.baidu.com/replay"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})
		if err := NewClient("token", "app").CopyFile("/a", "/b", "a"); err == nil || calls != 1 {
			t.Fatalf("status=%d calls=%d err=%v", status, calls, err)
		}
	}
}
