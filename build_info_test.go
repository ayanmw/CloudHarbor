package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/management"
)

func TestBuildInfoRouteIsAuthenticatedReadOnlyAndIndependentOfAccount(t *testing.T) {
	oldVersion, oldRevision, oldTime, oldSnapshot := version, revision, builtAtUTC, sourceSnapshot
	defer func() { version, revision, builtAtUTC, sourceSnapshot = oldVersion, oldRevision, oldTime, oldSnapshot }()
	version = "v0.8.31"
	revision = strings.Repeat("a", 40) + "-dirty-" + strings.Repeat("b", 16)
	builtAtUTC = "2026-11-06T01:00:00.1234567Z"
	sourceSnapshot = strings.Repeat("b", 64)
	gate, err := management.New("fixture-build-admin")
	if err != nil {
		t.Fatal(err)
	}
	handler := applicationHandler(gate, nil)
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, "http://example.com/api/build-info", nil))
		if w.Code != 401 {
			t.Fatalf("unauthenticated %s=%d", method, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://example.com/api/build-info", nil)
	r.Header.Set("Authorization", "Bearer fixture-build-admin")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var info buildInformation
	if err = json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || info.Version != version || info.Revision != revision || info.BuiltAtUTC != builtAtUTC || info.SourceSnapshot != sourceSnapshot || info.Runtime == "" || info.GoVersion == "" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if _, err = time.Parse(time.RFC3339Nano, info.ObservedAtUTC); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("POST", "http://example.com/api/build-info", nil)
	r.Header.Set("Authorization", "Bearer fixture-build-admin")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal(w.Code)
	}
}
func TestDevelopmentBuildDoesNotInventMissingBuildEvidence(t *testing.T) {
	oldTime, oldSnapshot := builtAtUTC, sourceSnapshot
	defer func() { builtAtUTC, sourceSnapshot = oldTime, oldSnapshot }()
	builtAtUTC = ""
	sourceSnapshot = ""
	info := currentBuildInformation()
	if info.BuiltAtUTC != "" || info.SourceSnapshot != "" {
		t.Fatal(info)
	}
}
