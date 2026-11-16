package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayanmw/CloudHarbor/internal/library"
	"github.com/ayanmw/CloudHarbor/internal/management"
)

func TestLocalLibraryRoutesAreAuthenticatedAndIndependentOfBaidu(t *testing.T) {
	old := libraryManager
	defer func() { libraryManager = old }()
	var err error
	libraryManager, err = library.Open(filepath.Join(t.TempDir(), "state"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer libraryManager.Close()
	gate, err := management.New("fixture-library-admin")
	if err != nil {
		t.Fatal(err)
	}
	h := applicationHandler(gate, nil)
	for _, p := range []string{"/api/library/config", "/api/library/entries", "/api/library/content?path=x&revision=y"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://example.com"+p, nil))
		if w.Code != 401 {
			t.Fatalf("unauth %s=%d", p, w.Code)
		}
	}
	root := t.TempDir()
	if err = os.WriteFile(filepath.Join(root, "read.txt"), []byte("local only"), 0600); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]string{"root": root, "expected_revision": ""})
	r := httptest.NewRequest("POST", "http://example.com/api/library/config", strings.NewReader(string(b)))
	r.Header.Set("Authorization", "Bearer fixture-library-admin")
	r.Header.Set("Origin", "https://other.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross origin config=%d", w.Code)
	}
	r = httptest.NewRequest("POST", "http://example.com/api/library/config", strings.NewReader(string(b)))
	r.Header.Set("Authorization", "Bearer fixture-library-admin")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("configure without Baidu=%d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "http://example.com/api/library/entries", nil)
	r.Header.Set("Authorization", "Bearer fixture-library-admin")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "read.txt") {
		t.Fatalf("list without Baidu=%d %s", w.Code, w.Body.String())
	}
}
