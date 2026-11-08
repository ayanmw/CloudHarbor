package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/management"
	"web_browser_baidu_onekey_upload/internal/uploads"
	"web_browser_baidu_onekey_upload/internal/webui"
)

func TestUploadConcurrencyRouteUsesManagementAuthentication(t *testing.T) {
	oldCfg, oldDL, oldUI, oldUploads := cfg, dl, ui, uploadManager
	defer func() { cfg, dl, ui, uploadManager = oldCfg, oldDL, oldUI, oldUploads }()
	cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/queue", MaxParallel: 1}
	var err error
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Shutdown(context.Background())
	ui = webui.NewServer(dl)
	uploadManager, err = uploads.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer uploadManager.Close()
	gate, err := management.New("fixture-upload-administrator")
	if err != nil {
		t.Fatal(err)
	}
	gate.SetSessionStore(dl)
	handler := applicationHandler(gate, nil)
	for _, method := range []string{"GET", "POST"} {
		r := httptest.NewRequest(method, "http://example.com/api/uploads/settings/concurrency", strings.NewReader(`{"limit":2}`))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 || strings.Contains(w.Header().Get("WWW-Authenticate"), "Basic") {
			t.Fatalf("unauthenticated %s=%d", method, w.Code)
		}
	}
	if uploadManager.ConcurrencyStatus().Limit != 1 {
		t.Fatal("unauthenticated write changed setting")
	}
	r := httptest.NewRequest("POST", "http://example.com/api/uploads/settings/concurrency", strings.NewReader(`{"limit":2}`))
	r.Header.Set("Authorization", "Bearer fixture-upload-administrator")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || uploadManager.ConcurrencyStatus().Limit != 2 {
		t.Fatalf("authenticated route=%d %s", w.Code, w.Body.String())
	}
	// A literal storage route must not be interpreted as a batch ID or bypass the gate.
	for _, authenticated := range []bool{false, true} {
		r := httptest.NewRequest("GET", "http://example.com/api/uploads/storage", nil)
		if authenticated {
			r.Header.Set("Authorization", "Bearer fixture-upload-administrator")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		expected := 401
		if authenticated {
			expected = 200
		}
		if w.Code != expected {
			t.Fatalf("storage auth=%v code=%d body=%s", authenticated, w.Code, w.Body.String())
		}
		if authenticated && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("storage response cached")
		}
	}

}
