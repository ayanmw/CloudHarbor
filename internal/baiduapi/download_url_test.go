package baiduapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadURLAddsTokenAsQueryParameter(t *testing.T) {
	for _, tc := range []struct {
		name, suffix, token, wantToken, wantRawPrefix string
	}{
		{"escaped token", "/file?sign=x%2fy&label=a%20b", "fixture+a&b=#%", "fixture+a&b=#%", "sign=x%2fy&label=a%20b&"},
		{"fragment", "/file?sign=x%2fy#section", "fixture-token", "fixture-token", "sign=x%2fy&"},
		{"similar parameter", "/file?not_access_token=marker", "fixture-token", "fixture-token", "not_access_token=marker&"},
		{"marker in path", "/access_token=marker/file", "fixture-token", "fixture-token", ""},
		{"existing token", "/file?access_token=link%2Btoken&sign=x%2fy", "fixture-token", "link+token", "access_token=link%2Btoken&sign=x%2fy"},
		{"encoded parameter name", "/file?access%5Ftoken=link-token", "fixture-token", "link-token", "access%5Ftoken=link-token"},
		{"explicit empty token", "/file?access_token=&sign=x%2fy", "fixture-token", "", "access_token=&sign=x%2fy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				values := r.URL.Query()["access_token"]
				if len(values) != 1 || values[0] != tc.wantToken {
					t.Errorf("download token changed or missing: got %q, want one %q", values, tc.wantToken)
				}
				if !strings.HasPrefix(r.URL.RawQuery, tc.wantRawPrefix) {
					t.Errorf("existing signed query rewritten: got %q, want prefix %q", r.URL.RawQuery, tc.wantRawPrefix)
				}
				io.WriteString(w, "abc")
			}))
			defer srv.Close()
			body, _, err := NewClient(tc.token, "fixture-app").DownloadFile(srv.URL + tc.suffix)
			if err != nil {
				t.Fatal(err)
			}
			defer body.Close()
			if _, err = io.Copy(io.Discard, body); err != nil {
				t.Fatal(err)
			}
		})
	}
}
