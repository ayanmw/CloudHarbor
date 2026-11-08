package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/management"
)

func TestDisplaySizeRouteRequiresManagementAndRejectsCrossOrigin(t *testing.T) {
	gate, err := management.New("fixture-size-key")
	if err != nil {
		t.Fatal(err)
	}
	h := applicationHandler(gate, nil)
	r := httptest.NewRequest("GET", "http://example.com/api/cloud-display-size", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("unguarded route %d", w.Code)
	}
	r = httptest.NewRequest("POST", "http://example.com/api/cloud-display-size", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer fixture-size-key")
	r.Header.Set("Origin", "https://other.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross origin %d", w.Code)
	}
	r = httptest.NewRequest("GET", "http://example.com/api/cloud-display-size?desc=invalid", nil)
	r.Header.Set("Authorization", "Bearer fixture-size-key")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("route not dispatched to parameter validation: %d", w.Code)
	}
}
