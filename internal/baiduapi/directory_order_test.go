package baiduapi

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOrderedDirectoryPageUsesProviderOrderAndLookahead(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	for _, field := range []string{"name", "size", "time"} {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprint(field, desc), func(t *testing.T) {
				calls := 0
				http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					q := r.URL.Query()
					wantDesc := "0"
					if desc {
						wantDesc = "1"
					}
					if q.Get("order") != field || q.Get("desc") != wantDesc || q.Get("start") != "100" || q.Get("limit") != "3" {
						t.Fatalf("wrong query: %v", q)
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0,"list":[{"fs_id":3},{"fs_id":1},{"fs_id":2}]}`)), Request: r}, nil
				})
				page, more, boundary, err := NewClient("test", "app").ListDirPageOrdered("/", 100, 2, DirectoryOrder{Field: field, Descending: desc})
				if err != nil || !more || len(page) != 2 || page[0].FsID != 3 || boundary == nil || boundary.FsID != 2 || calls != 1 {
					t.Fatalf("page=%v more=%v boundary=%v calls=%d err=%v", page, more, boundary, calls, err)
				}
			})
		}
	}
}

func TestOrderedDirectoryPageRejectsInvalidOrderBeforeRequest(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = apiTransport(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected provider request"); return nil, nil })
	for _, field := range []string{"", "type", "NAME", "name&desc=1"} {
		if _, _, _, err := NewClient("test", "app").ListDirPageOrdered("/", 0, 100, DirectoryOrder{Field: field}); err == nil {
			t.Fatalf("accepted %q", field)
		}
	}
}
