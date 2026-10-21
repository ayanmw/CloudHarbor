package management

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagementAuthenticationAndOrigin(t *testing.T) {
	gate, err := New("a-strong-preview-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	handler := gate.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	tests := []struct {
		name, method, origin, fetch, bearer string
		basic                               bool
		want                                int
	}{
		{name: "anonymous", method: "GET", want: 401},
		{name: "basic page", method: "GET", basic: true, want: 204},
		{name: "bearer CLI", method: "POST", bearer: "a-strong-preview-admin-password", want: 204},
		{name: "wrong bearer", method: "POST", bearer: "wrong", want: 401},
		{name: "same origin", method: "POST", origin: "http://example.com", basic: true, want: 204},
		{name: "cross origin", method: "POST", origin: "http://evil.example", basic: true, want: 403},
		{name: "cross site", method: "POST", fetch: "cross-site", basic: true, want: 403},
		{name: "websocket origin", method: "GET", origin: "http://evil.example", basic: true, want: 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, "http://example.com/api/scan", nil)
			if tt.basic {
				r.SetBasicAuth("admin", "a-strong-preview-admin-password")
			}
			if tt.bearer != "" {
				r.Header.Set("Authorization", "Bearer "+tt.bearer)
			}
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.fetch != "" {
				r.Header.Set("Sec-Fetch-Site", tt.fetch)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status=%d want=%d", w.Code, tt.want)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private management page cacheable")
			}
		})
	}
}
