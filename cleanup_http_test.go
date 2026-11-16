package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/management"
)

func TestCleanupRoutesRequireManagementAndSameOrigin(t *testing.T) {
	gate, err := management.New("fixture-cleanup-key")
	if err != nil {
		t.Fatal(err)
	}
	handler := applicationHandler(gate, nil)
	for _, path := range []string{"/api/cloud/cleanup-revision", "/api/cloud/cleanup-batches", "/api/cloud/cleanup-batches/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "/api/cloud/cleanup-batches/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/items", "/api/cloud/cleanup-batches/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/events", "/api/cloud/cleanup-batches/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/stop"} {
		r := httptest.NewRequest("GET", "http://example.com"+path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("unguarded %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/cloud/cleanup-batches", "/api/cloud/cleanup-batches/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/stop"} {
		r := httptest.NewRequest("POST", "http://example.com"+path, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer fixture-cleanup-key")
		r.Header.Set("Origin", "https://evil.example")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("cross origin %s: %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "http://example.com/api/cloud/cleanup-batches", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer fixture-cleanup-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("route not dispatched: %d %s", w.Code, w.Body.String())
	}
}
