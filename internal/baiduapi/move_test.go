package baiduapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMoveUsesSynchronousNoOverwriteAndRequiresSingleResult(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	for _, body := range []string{`{"errno":0,"info":[{"errno":0}]}`, `{"errno":0}`, `{"errno":0,"info":[{"errno":-8}]}`, `{"errno":0,"info":[{}]}`, `{"errno":0,"taskid":123,"info":[{"errno":0}]}`} {
		http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
			r.ParseForm()
			if r.Method != "POST" || r.URL.Query().Get("opera") != "move" || r.Form.Get("async") != "0" || r.Form.Get("ondup") != "fail" || !strings.Contains(r.Form.Get("filelist"), `"dest":"/queue"`) {
				t.Error("wrong move contract")
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		err := NewClient("private-access", "app").MoveFile("/source/a.txt", "/queue", "a.txt")
		if (err == nil) != (body == `{"errno":0,"info":[{"errno":0}]}`) {
			t.Fatalf("wrong move completion: %s err=%v", body, err)
		}
	}
}

func TestMoveNeverFollowsRedirectOrReplaysPost(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	calls := 0
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 307, Header: http.Header{"Location": {"https://pan.baidu.com/replayed"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if NewClient("token", "app").MoveFile("/source/a", "/queue", "a") == nil || calls != 1 {
		t.Fatalf("move replayed: calls=%d", calls)
	}
}

func TestAsyncTaskIDWinsOverAccompanyingFailureCode(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = apiTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":-9,"taskid":123,"info":[{"errno":-8}]}`))}, nil
	})
	err := NewClient("token", "app").MoveFile("/source/a", "/queue", "a")
	var async *AsyncMoveError
	if !errors.Is(err, ErrMoveAsync) || !errors.As(err, &async) || async.TaskID != "123" {
		t.Fatalf("async task classified as definitive failure: %v", err)
	}
}

func TestMoveWireContractAgainstLocalHTTPServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.URL.Query().Get("access_token") != "private-token" || r.Form.Get("async") != "0" || r.Form.Get("ondup") != "fail" || !strings.Contains(r.Form.Get("filelist"), `"path":"/source/中文 a.txt"`) {
			t.Error("wire move contract changed")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"errno":0,"info":[{"errno":0}]}`)
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		endpoint := *r.URL
		endpoint.Scheme = target.Scheme
		endpoint.Host = target.Host
		copy.URL = &endpoint
		return original.RoundTrip(copy)
	})
	if err := NewClient("private-token", "app").MoveFile("/source/中文 a.txt", "/queue", "中文 a.txt"); err != nil {
		t.Fatal(err)
	}
}
