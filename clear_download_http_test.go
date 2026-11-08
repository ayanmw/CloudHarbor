package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/management"
)

func TestClearDownloadRecordHTTPRequiresManagementAndExplicitConfirmation(t *testing.T) {
	gate, err := management.New("clear-fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	h := applicationHandler(gate, nil)
	for _, tc := range []struct {
		method, body, auth, origin string
		want                       int
	}{
		{"POST", `{}`, "", "", 401},
		{"POST", `{}`, "clear-fixture-key", "https://other.example", 403},
		{"GET", ``, "clear-fixture-key", "", 405},
		{"POST", `{}`, "clear-fixture-key", "", 400},
		{"POST", `{"acknowledge_reset":false,"clear_token":"` + strings.Repeat("a", 64) + `"}`, "clear-fixture-key", "", 400},
		{"POST", `{"acknowledge_reset":true,"clear_token":"bad"}`, "clear-fixture-key", "", 400},
		{"POST", `{"acknowledge_reset":true,"clear_token":"` + strings.Repeat("a", 64) + `"} {}`, "clear-fixture-key", "", 400},
	} {
		r := httptest.NewRequest(tc.method, "http://example.com/api/tasks/42/clear", strings.NewReader(tc.body))
		if tc.auth != "" {
			r.Header.Set("Authorization", "Bearer "+tc.auth)
		}
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s want=%d got=%d %s", tc.method, tc.want, w.Code, w.Body.String())
		}
	}
}
