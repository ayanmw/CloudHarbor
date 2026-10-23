package baiduapi

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSourceOperationRequiresSingleSynchronousReceipt(t *testing.T) {
	base := http.DefaultTransport
	defer func() { http.DefaultTransport = base }()
	for _, op := range []string{"rename", "delete"} {
		for _, body := range []string{`{"errno":0}`, `{"errno":0,"info":[{}]}`, `{"errno":0,"taskid":123,"info":[{"errno":0}]}`, `{"errno":0,"info":[{"errno":0},{"errno":0}]}`} {
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			c := NewClient("fixture", "")
			var err error
			if op == "rename" {
				err = c.RenameFile("/a", "a.done")
			} else {
				err = c.DeleteFile("/a")
			}
			if !errors.Is(err, ErrSourceOperationUnconfirmed) {
				t.Errorf("%s accepted incomplete receipt %s: %v", op, body, err)
			}
		}
	}
}

func TestSourceOperationRedirectNeverReplaysPOST(t *testing.T) {
	base := http.DefaultTransport
	defer func() { http.DefaultTransport = base }()
	for _, op := range []string{"rename", "delete"} {
		requests := 0
		http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
			requests++
			return &http.Response{StatusCode: 307, Header: http.Header{"Location": {"https://pan.baidu.com/replay"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		})
		c := NewClient("fixture", "")
		if op == "rename" {
			c.RenameFile("/a", "a.done")
		} else {
			c.DeleteFile("/a")
		}
		if requests != 1 {
			t.Errorf("%s replayed %d POST requests", op, requests)
		}
	}
}
