package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/management"
)

func TestCloudBatchPreviewRouteRequiresManagementAndSameOrigin(t *testing.T) {
	gate, err := management.New("fixture-preview-key")
	if err != nil {
		t.Fatal(err)
	}
	handler := applicationHandler(gate, nil)
	cases := []struct {
		method, bearer, origin string
		want                   int
	}{{"POST", "", "", 401}, {"GET", "fixture-preview-key", "", 405}, {"POST", "fixture-preview-key", "https://evil.example", 403}, {"POST", "fixture-preview-key", "", 400}}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, "http://example.com/api/cloud/mutations/preview", strings.NewReader(`{}`))
		if tc.bearer != "" {
			r.Header.Set("Authorization", "Bearer "+tc.bearer)
		}
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s origin=%q status=%d body=%s", tc.method, tc.origin, w.Code, w.Body.String())
		}
	}
}
