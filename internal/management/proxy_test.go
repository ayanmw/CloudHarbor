package management

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfiguredHTTPSOriginThroughHTTPProxy(t *testing.T) {
	gate, err := NewWithOrigin("private-preview-password", "https://nas.example:8443")
	if err != nil {
		t.Fatal(err)
	}
	// The inner check represents the WebSocket upgrader's second origin check.
	handler := gate.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !CheckOrigin(r) {
			http.Error(w, "inner origin refused", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		origin string
		want   int
	}{
		{"https://nas.example:8443", 204},
		{"https://evil.example", 403},
		{"http://nas.example:8443", 403},
		{"https://nas.example", 403},
		{"", 204},
	} {
		r := httptest.NewRequest("POST", "http://backend.internal:8080/api/scan", nil)
		r.Header.Set("Authorization", "Bearer private-preview-password")
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "evil.example")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("origin %q: status=%d want=%d", tc.origin, w.Code, tc.want)
		}
	}
}

func TestPublicOriginValidationAndUnconfiguredProxyHeaders(t *testing.T) {
	for _, origin := range []string{"http://nas.example", "https://", "https://user:password@nas.example", "https://nas.example/prefix", "https://nas.example?", "https://nas.example#", "https://nas.example:99999", "https://nas.example:"} {
		if _, err := NewWithOrigin("private-preview-password", origin); err == nil {
			t.Errorf("invalid public origin accepted: %q", origin)
		}
	}
	r := httptest.NewRequest("POST", "http://backend.internal:8080/api/scan", nil)
	r.Header.Set("Origin", "https://nas.example")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "nas.example")
	r.Header.Set("Forwarded", "proto=https;host=nas.example")
	if CheckOrigin(r) {
		t.Fatal("unconfigured origin trusted forwarded headers")
	}
}

func TestPublicOriginCanonicalPort(t *testing.T) {
	for _, tc := range []struct{ configured, browser string }{
		{"https://nas.example:443", "https://nas.example"},
		{"https://nas.example:0443/", "https://nas.example"},
		{"https://nas.example:08443", "https://nas.example:8443"},
		{"https://[::1]:443", "https://[::1]"},
	} {
		gate, err := NewWithOrigin("private-preview-password", tc.configured)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "http://backend.internal/api/scan", nil)
		r.Header.Set("Authorization", "Bearer private-preview-password")
		r.Header.Set("Origin", tc.browser)
		w := httptest.NewRecorder()
		gate.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
		if w.Code != 204 {
			t.Fatalf("configured %q browser %q: status=%d", tc.configured, tc.browser, w.Code)
		}
	}
}
