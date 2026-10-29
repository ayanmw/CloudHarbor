package baiduapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCreateDirectoryUsesOfficialCreateEndpointWithoutRetry(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	calls := 0
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("method") != "create" {
			t.Fatalf("method = %q", r.URL.Query().Get("method"))
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("path") != "/queue/new" || r.Form.Get("isdir") != "1" || r.Form.Get("size") != "0" {
			t.Fatalf("create form = %v", r.Form)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0,"fs_id":123,"path":"/queue/new"}`)), Request: r}, nil
	})
	if err := NewClient("fixture", "").CreateDirectory("/queue/new"); err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
