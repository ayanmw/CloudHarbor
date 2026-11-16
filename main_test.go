package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/authorization"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/config"
	"github.com/ayanmw/CloudHarbor/internal/credentials"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
	"github.com/ayanmw/CloudHarbor/internal/management"
	"github.com/ayanmw/CloudHarbor/internal/oauth"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
	"github.com/ayanmw/CloudHarbor/internal/webui"
)

func TestTaskStatusDTOIncludesLocalStorageSummary(t *testing.T) {
	checkedAt := time.Date(2026, 11, 2, 1, 2, 3, 0, time.UTC)
	dto := toTaskStatus(&downloader.Task{
		CanPause: true, CanResume: true,
		LocalStorage: &downloader.LocalStorageEvidence{Kind: "local_storage_blocked", Path: `D:\downloads`, RequiredBytes: 4096, AvailableBytes: -1, ReservedBytes: 512, CheckedAtUTC: checkedAt},
	})
	body, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	local, ok := decoded["local_storage"].(map[string]any)
	if !ok || decoded["can_pause"] != true || decoded["can_resume"] != true || local["path"] != `D:\downloads` || local["required_bytes"] != float64(4096) || local["available_bytes"] != float64(-1) || local["checked_at_utc"] != checkedAt.Format(time.RFC3339Nano) {
		t.Fatalf("task status local storage DTO = %#v", decoded["local_storage"])
	}
}

func TestShouldReportRuntimeQueueScanFailure(t *testing.T) {
	cases := []struct {
		name       string
		result     *scanResult
		inProgress bool
		want       bool
	}{
		{name: "last runtime attempt failed", result: &scanResult{Error: "scan_failed"}, want: true},
		{name: "currently retrying after old failure", result: &scanResult{Error: "scan_failed"}, inProgress: true, want: false},
		{name: "runtime success", result: &scanResult{Success: time.Now()}, want: false},
		{name: "no runtime result", result: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldReportRuntimeQueueScanFailure(tc.result, tc.inProgress); got != tc.want {
				t.Fatalf("shouldReportRuntimeQueueScanFailure()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyQueueMonitorHistoryReadFailureDoesNotClaimNormal(t *testing.T) {
	result := &scanResult{Success: time.Date(2026, 11, 2, 12, 2, 3, 0, time.FixedZone("CST", 8*60*60))}
	response := map[string]interface{}{"status": "ok"}
	applyQueueMonitorHistoryReadFailure(response, result)
	if response["status"] != "degraded" || response["scan_history_available"] != false || response["scan_history_error"] != "history_unavailable" || response["last_successful_scan_source"] != "runtime" || response["last_successful_scan"] != "2026-11-02T04:02:03Z" {
		t.Fatalf("history read failure projection=%+v", response)
	}
	if _, exists := response["scan_error"]; exists {
		t.Fatalf("history read failure invented a scan error: %+v", response)
	}

	withoutRuntime := map[string]interface{}{"status": "ok"}
	applyQueueMonitorHistoryReadFailure(withoutRuntime, nil)
	if withoutRuntime["status"] != "degraded" || withoutRuntime["scan_history_available"] != false {
		t.Fatalf("history read failure without runtime evidence=%+v", withoutRuntime)
	}
}

func TestWebSocketCanReportQueueStateAfterDownloaderStops(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/queue", MaxParallel: 1}
	var err error
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	ui = webui.NewServer(dl).WithContext(appCtx)
	gate, err := management.New("private-administrator-password")
	if err != nil {
		t.Fatal(err)
	}
	gate.SetSessionStore(dl)
	srv := httptest.NewUnstartedServer(applicationHandler(gate, nil))
	srv.Config.BaseContext = func(net.Listener) context.Context { return appCtx }
	srv.Start()
	defer srv.Close()
	defer func() {
		if err := dl.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	}()

	headers := http.Header{"Authorization": {"Bearer private-administrator-password"}, "Origin": {srv.URL}}
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", headers)
	if err != nil {
		t.Fatalf("WebSocket connection failed: %v %v", err, response)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, body, err := conn.ReadMessage()
	if err != nil || !strings.Contains(string(body), `"queue_healthy":true`) {
		t.Fatalf("initial WebSocket queue state missing: %v %s", err, body)
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := dl.Shutdown(shutdownCtx); err != nil {
		shutdownCancel()
		t.Fatal(err)
	}
	shutdownCancel()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, body, err = conn.ReadMessage()
	if err != nil || !strings.Contains(string(body), `"queue_healthy":false`) {
		t.Fatalf("WebSocket did not report stopped queue: %v %s", err, body)
	}
}

func TestQueuePauseAPIRequiresAdminAndCannotClearMaintenance(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/queue", MaxParallel: 1}
	var err error
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := dl.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	gate, err := management.New("private-administrator-password")
	if err != nil {
		t.Fatal(err)
	}
	gate.SetSessionStore(dl)
	srv := httptest.NewServer(applicationHandler(gate, nil))
	defer srv.Close()

	request := func(path string, authenticated bool) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if authenticated {
			req.Header.Set("Authorization", "Bearer private-administrator-password")
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	response := request("/api/queue/pause", false)
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated pause status=%d, want 401", response.StatusCode)
	}
	response = request("/api/queue/pause", true)
	var result struct {
		QueuePause downloader.QueuePauseStatus `json:"queue_pause"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !result.QueuePause.Paused || result.QueuePause.UpdatedAtUTC == nil {
		t.Fatalf("pause response status=%d state=%+v", response.StatusCode, result.QueuePause)
	}
	statusRequest, err := http.NewRequest(http.MethodGet, srv.URL+"/api/status?offset=0&limit=10", nil)
	if err != nil {
		t.Fatal(err)
	}
	statusRequest.Header.Set("Authorization", "Bearer private-administrator-password")
	statusResponse, err := http.DefaultClient.Do(statusRequest)
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]json.RawMessage
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		statusResponse.Body.Close()
		t.Fatal(err)
	}
	statusResponse.Body.Close()
	var statusPause downloader.QueuePauseStatus
	if err := json.Unmarshal(status["queue_pause"], &statusPause); err != nil || !statusPause.Paused {
		t.Fatalf("status endpoint pause state=%+v err=%v", statusPause, err)
	}
	if string(status["queue_healthy"]) != "true" {
		t.Fatalf("status endpoint queue health=%s, want true", status["queue_healthy"])
	}
	if err := dl.TryBeginMaintenance(); err != nil {
		t.Fatal(err)
	}
	response = request("/api/queue/resume", true)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("resume while maintenance status=%d", response.StatusCode)
	}
	if err := dl.TryBeginMaintenance(); err == nil {
		dl.EndMaintenance()
		t.Fatal("HTTP resume bypassed maintenance gate")
	}
	dl.EndMaintenance()
	settingsPath := filepath.Join(cfg.LocalDownDir, ".nas-downloader", "queue", "settings.json")
	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(settingsPath, 0700); err != nil {
		t.Fatal(err)
	}
	response = request("/api/queue/pause", true)
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("pause without durable settings status=%d, want 503", response.StatusCode)
	}
	statusRequest, err = http.NewRequest(http.MethodGet, srv.URL+"/api/status?offset=0&limit=10", nil)
	if err != nil {
		t.Fatal(err)
	}
	statusRequest.Header.Set("Authorization", "Bearer private-administrator-password")
	statusResponse, err = http.DefaultClient.Do(statusRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		statusResponse.Body.Close()
		t.Fatal(err)
	}
	statusResponse.Body.Close()
	if err := json.Unmarshal(status["queue_pause"], &statusPause); err != nil || statusPause.Paused {
		t.Fatalf("failed pause altered state: %+v err=%v", statusPause, err)
	}
}

func TestTaskPauseAPIRequiresAdminAndReportsDurableState(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/queue", MaxParallel: 1}
	store, err := taskstore.Open(filepath.Join(cfg.LocalDownDir, ".nas-downloader", "queue"), cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	job := []byte(`{"version":1,"file":{"fs_id":991,"path":"/queue/pause-api.bin","server_filename":"pause-api.bin","size":42},"mode":"keep","status":"pending"}`)
	if err := store.Save(991, job); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := dl.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	gate, err := management.New("private-administrator-password")
	if err != nil {
		t.Fatal(err)
	}
	gate.SetSessionStore(dl)
	srv := httptest.NewServer(applicationHandler(gate, nil))
	defer srv.Close()
	request := func(method, path string, authenticated bool) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if authenticated {
			req.Header.Set("Authorization", "Bearer private-administrator-password")
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	response := request(http.MethodPost, "/api/tasks/991/pause", false)
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated pause status=%d, want 401", response.StatusCode)
	}
	response = request(http.MethodGet, "/api/tasks/991/pause", true)
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET pause status=%d, want 405", response.StatusCode)
	}
	response = request(http.MethodPost, "/api/tasks/991/pause", true)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("pause status=%d, want 200", response.StatusCode)
	}
	response = request(http.MethodPost, "/api/tasks/991/pause", true)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("idempotent pause status=%d, want 200", response.StatusCode)
	}
	response = request(http.MethodGet, "/api/status?offset=0&limit=10", true)
	var status struct {
		Tasks []struct {
			Status    string `json:"status"`
			CanPause  bool   `json:"can_pause"`
			CanResume bool   `json:"can_resume"`
		} `json:"tasks"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(status.Tasks) != 1 || status.Tasks[0].Status != "paused" || status.Tasks[0].CanPause || !status.Tasks[0].CanResume {
		t.Fatalf("paused task status DTO = %d %+v", response.StatusCode, status.Tasks)
	}
	response = request(http.MethodGet, "/api/status?offset=0&limit=10&status=paused", true)
	var filtered struct {
		Tasks []struct {
			Status string `json:"status"`
		} `json:"tasks"`
		TotalTasks int `json:"total_tasks"`
	}
	if err := json.NewDecoder(response.Body).Decode(&filtered); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || filtered.TotalTasks != 1 || len(filtered.Tasks) != 1 || filtered.Tasks[0].Status != "paused" {
		t.Fatalf("paused task filter status=%d result=%+v", response.StatusCode, filtered)
	}
	response = request(http.MethodPost, "/api/tasks/991/resume", true)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("resume status=%d, want 200", response.StatusCode)
	}
	response = request(http.MethodPost, "/api/tasks/991/resume", true)
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("resume of a non-paused task status=%d, want 409", response.StatusCode)
	}
	response = request(http.MethodPost, "/api/tasks/992/pause", true)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("pause of an unknown task status=%d, want 404", response.StatusCode)
	}
}

func TestManagementHTTPAndWebSocketIntegration(t *testing.T) {
	var err error
	cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/preview", ScanInterval: 30, MaxParallel: 1}
	store, err := taskstore.Open(filepath.Join(cfg.LocalDownDir, ".nas-downloader", "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	jobRecord := []byte(`{"version":1,"file":{"fs_id":3400000000000000123,"path":"/preview/a.txt","server_filename":"a.txt","size":3},"mode":"keep","status":"cancelled","local_storage":{"kind":"local_storage_blocked","path":"D:\\downloads","required_bytes":4096,"available_bytes":-1,"reserved_bytes":512,"checked_at_utc":"2026-11-02T01:02:03Z"}}`)
	if err = store.Save(3400000000000000123, jobRecord); err != nil {
		store.Close()
		t.Fatal(err)
	}
	for i := 1; i <= 21; i++ {
		event := taskstore.Event{Kind: "api-event-" + strconv.Itoa(i)}
		if i == 21 {
			event.Evidence = json.RawMessage(`{"nested":[{"access_token":"private-token","safe":"kept"}],"SIGN-KEY":"private-sign"}`)
		}
		if err = store.SaveWithEvent(3400000000000000123, jobRecord, event); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	store.Close()
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		// This integration test opens many authenticated HTTP/WebSocket routes; allow their handlers to drain under full-suite load.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := dl.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	dl.SetAuthentication(baiduapi.NewClient("", ""), false, time.Time{})
	auth, err = credentials.Open(dl, credentials.Options{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ui = webui.NewServer(dl).WithContext(dl.Context())
	gate, _ := management.New("private-administrator-password")
	gate.SetSessionStore(dl)
	srv := httptest.NewServer(applicationHandler(gate, authorization.New(dl.Context(), "", "", auth, nil)))
	defer srv.Close()
	noRedirectClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, path := range []string{"/", "/oauth", "/settings/paths", "/settings/migrations", "/intake", "/cloud", "/cloud/files", "/cloud/cache", "/cloud/cleanup", "/cloud/history", "/uploads", "/intake-history", "/api/uploads", "/operations", "/monitor-history", "/api/cloud", "/api/intakes", "/api/health", "/api/monitor-scans", "/api/monitor-scans/events?run_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "/api/monitor-scans/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/export", "/api/oauth/status", "/api/status", "/api/tasks/3400000000000000123/events", "/api/tasks/3400000000000000123/events/export", "/api/tasks/3400000000000000123/events/1/context", "/api/cloud/operations/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/events/export", "/api/cloud-inventory/refresh/runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/export", "/api/cloud-inventory/refresh/runs/export", "/api/cloud-inventory/refresh/changes/export", "/api/cloud-cleanup/duplicate-scan/events", "/res/cloud_browse_route.js", "/res/cloud_properties.js", "/res/cloud_actions.js", "/res/cloud_progress.js", "/res/cloud_scan_limits.js", "/res/shared.js", "/res/intake.js", "/res/monitor_history.js", "/ws"} {
		response, err := noRedirectClient.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := 401
		if !strings.HasPrefix(path, "/api/") && path != "/ws" {
			want = 303
		}
		if response.StatusCode != want {
			t.Fatalf("anonymous %s status=%d want=%d", path, response.StatusCode, want)
		}
	}
	loginRequest, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/login", strings.NewReader(`{"password":"private-administrator-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("Origin", srv.URL)
	loginResponse, err := http.DefaultClient.Do(loginRequest)
	if err != nil {
		t.Fatal(err)
	}
	loginBody, _ := io.ReadAll(loginResponse.Body)
	loginResponse.Body.Close()
	if loginResponse.StatusCode != 200 || strings.Contains(string(loginBody), "private-administrator-password") {
		t.Fatalf("login status=%d body=%s", loginResponse.StatusCode, loginBody)
	}
	cookies := loginResponse.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies=%d", len(cookies))
	}
	pathSettingsRequest, _ := http.NewRequest(http.MethodGet, srv.URL+"/settings/paths", nil)
	pathSettingsRequest.AddCookie(cookies[0])
	pathSettingsResponse, err := http.DefaultClient.Do(pathSettingsRequest)
	if err != nil {
		t.Fatal(err)
	}
	pathSettingsPage, _ := io.ReadAll(pathSettingsResponse.Body)
	pathSettingsResponse.Body.Close()
	if pathSettingsResponse.StatusCode != http.StatusOK || !strings.Contains(string(pathSettingsPage), `/app/assets/`) || pathSettingsResponse.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("path settings workspace status=%d", pathSettingsResponse.StatusCode)
	}
	monitorHistoryRequest, _ := http.NewRequest(http.MethodGet, srv.URL+"/monitor-history", nil)
	monitorHistoryRequest.AddCookie(cookies[0])
	monitorHistoryResponse, err := http.DefaultClient.Do(monitorHistoryRequest)
	if err != nil {
		t.Fatal(err)
	}
	monitorHistoryPage, _ := io.ReadAll(monitorHistoryResponse.Body)
	monitorHistoryResponse.Body.Close()
	// Vue now supplies the monitor UI; verify the authenticated bundle entry
	// here, while bundle behavior tests verify the actual monitor controls.
	if monitorHistoryResponse.StatusCode != http.StatusOK || !strings.Contains(string(monitorHistoryPage), `id="app"`) || !strings.Contains(string(monitorHistoryPage), "/app/assets/") || strings.Contains(string(monitorHistoryPage), "/res/monitor_history.js") || monitorHistoryResponse.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("monitor history page status=%d", monitorHistoryResponse.StatusCode)
	}
	monitorHistoryAPIRequest, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/monitor-scans", nil)
	monitorHistoryAPIRequest.AddCookie(cookies[0])
	monitorHistoryAPIResponse, err := http.DefaultClient.Do(monitorHistoryAPIRequest)
	if err != nil {
		t.Fatal(err)
	}
	monitorHistoryAPIResponse.Body.Close()
	if monitorHistoryAPIResponse.StatusCode != http.StatusConflict {
		t.Fatalf("monitor history API status=%d", monitorHistoryAPIResponse.StatusCode)
	}
	operationsRequest, _ := http.NewRequest(http.MethodGet, srv.URL+"/operations", nil)
	operationsRequest.AddCookie(cookies[0])
	operationsResponse, err := http.DefaultClient.Do(operationsRequest)
	if err != nil {
		t.Fatal(err)
	}
	operationsPage, _ := io.ReadAll(operationsResponse.Body)
	operationsResponse.Body.Close()
	// Vue owns the operation history markup now; behavior tests assert read-only
	// requests while this authenticated HTTP regression checks its embedded entry.
	if operationsResponse.StatusCode != http.StatusOK || !strings.Contains(string(operationsPage), `id="app"`) || !strings.Contains(string(operationsPage), "/app/assets/") || strings.Contains(string(operationsPage), "/res/cloud_operations.js") || operationsResponse.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("operations workspace status=%d body=%s", operationsResponse.StatusCode, operationsPage)
	}
	filteredEvidenceRequest, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/cloud/operations/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/events/export?recorded_through_utc=2026-10-30", nil)
	filteredEvidenceRequest.AddCookie(cookies[0])
	filteredEvidenceResponse, err := http.DefaultClient.Do(filteredEvidenceRequest)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, filteredEvidenceResponse.Body)
	filteredEvidenceResponse.Body.Close()
	if filteredEvidenceResponse.StatusCode != http.StatusNotFound || filteredEvidenceResponse.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("filtered evidence route status=%d cache=%q", filteredEvidenceResponse.StatusCode, filteredEvidenceResponse.Header.Get("Cache-Control"))
	}
	statusRequest, _ := http.NewRequest("GET", srv.URL+"/api/status", nil)
	statusRequest.AddCookie(cookies[0])
	statusResponse, err := http.DefaultClient.Do(statusRequest)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, statusResponse.Body)
	statusResponse.Body.Close()
	if statusResponse.StatusCode != 200 {
		t.Fatalf("cookie session status=%d", statusResponse.StatusCode)
	}
	logoutRequest, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/logout", nil)
	logoutRequest.AddCookie(cookies[0])
	logoutRequest.Header.Set("Origin", srv.URL)
	logoutResponse, err := http.DefaultClient.Do(logoutRequest)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, logoutResponse.Body)
	logoutResponse.Body.Close()
	if logoutResponse.StatusCode != 200 {
		t.Fatalf("logout status=%d", logoutResponse.StatusCode)
	}
	replayRequest, _ := http.NewRequest("GET", srv.URL+"/api/status", nil)
	replayRequest.AddCookie(cookies[0])
	replayResponse, err := noRedirectClient.Do(replayRequest)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, replayResponse.Body)
	replayResponse.Body.Close()
	if replayResponse.StatusCode != 401 {
		t.Fatalf("revoked session replay status=%d", replayResponse.StatusCode)
	}
	scanInProgress.Store(true)
	t.Cleanup(func() { scanInProgress.Store(false) })
	req, _ := http.NewRequest("GET", srv.URL+"/api/health", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(body), `"status":"waiting_authorization"`) || !strings.Contains(string(body), `"ready":false`) || !strings.Contains(string(body), `"cancelled":1`) || !strings.Contains(string(body), `"scan_in_progress":true`) {
		t.Fatalf("health invented account readiness: %s", body)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	var status struct {
		Tasks []struct {
			ID        string `json:"task_id"`
			CanRetry  bool   `json:"can_retry"`
			CanCancel bool   `json:"can_cancel"`
		}
	}
	if json.Unmarshal(body, &status) != nil || len(status.Tasks) != 1 || status.Tasks[0].ID != "3400000000000000123" || status.Tasks[0].CanRetry || status.Tasks[0].CanCancel {
		t.Fatalf("incorrect action DTO: %s", body)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/status?offset=0&limit=1&sort_by=name", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if json.Unmarshal(body, &status) != nil || len(status.Tasks) != 1 || status.Tasks[0].ID != "3400000000000000123" || status.Tasks[0].CanRetry || status.Tasks[0].CanCancel {
		t.Fatalf("incorrect paged action DTO: %s", body)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/tasks/3400000000000000123/events?limit=1", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	var eventPage struct {
		Events             []taskstore.Event `json:"events"`
		NextBeforeSequence *int64            `json:"next_before_sequence"`
	}
	if response.StatusCode != http.StatusOK || json.Unmarshal(body, &eventPage) != nil || len(eventPage.Events) != 1 || eventPage.Events[0].Kind != "api-event-21" || eventPage.NextBeforeSequence == nil {
		t.Fatalf("task events were not returned in the authenticated response: status=%d body=%s", response.StatusCode, body)
	}
	for _, format := range []string{"string", "invalid"} {
		req, _ := http.NewRequest("GET", srv.URL+"/api/tasks/3400000000000000123/events?limit=1&sequence_format="+format, nil)
		req.Header.Set("Authorization", "Bearer private-administrator-password")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if format == "invalid" {
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("invalid sequence format: %d", res.StatusCode)
			}
			continue
		}
		var page struct {
			Events []struct {
				Sequence string `json:"sequence"`
				JobID    string `json:"job_id"`
			} `json:"events"`
			Next string `json:"next_before_sequence"`
		}
		if res.StatusCode != http.StatusOK || json.Unmarshal(payload, &page) != nil || len(page.Events) != 1 || page.Events[0].JobID != "3400000000000000123" || page.Next != strconv.FormatInt(*eventPage.NextBeforeSequence, 10) {
			t.Fatalf("string event response: %d %s", res.StatusCode, payload)
		}
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/tasks/3400000000000000123/events?limit=1&before_sequence="+strconv.FormatInt(*eventPage.NextBeforeSequence, 10), nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || json.Unmarshal(body, &eventPage) != nil || len(eventPage.Events) != 1 || eventPage.Events[0].Kind != "api-event-20" {
		t.Fatalf("older event page was not returned in sequence: status=%d body=%s", response.StatusCode, body)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/tasks/43/events", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing task event history status=%d, want 404", response.StatusCode)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/tasks/3400000000000000123/events/export", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	var export struct {
		TaskID      string            `json:"task_id"`
		TotalEvents int               `json:"total_events"`
		Events      []taskstore.Event `json:"events"`
	}
	if response.StatusCode != http.StatusOK || json.Unmarshal(body, &export) != nil || export.TaskID != "3400000000000000123" || export.TotalEvents != 22 || len(export.Events) != 22 || export.Events[0].Kind != "job_registered" || export.Events[21].Kind != "api-event-21" {
		t.Fatalf("task event export status=%d body=%s", response.StatusCode, body)
	}
	if response.Header.Get("Cache-Control") != "no-store" || !strings.Contains(response.Header.Get("Content-Disposition"), `attachment; filename="task-events-3400000000000000123-`) || !strings.Contains(string(body), `"access_token":"[REDACTED]"`) || !strings.Contains(string(body), `"SIGN-KEY":"[REDACTED]"`) || strings.Contains(string(body), "private-token") || strings.Contains(string(body), "private-sign") {
		t.Fatalf("task event export headers or recursive masking invalid: headers=%v body=%s", response.Header, body)
	}
	for _, target := range []string{"/api/tasks/not-a-number/events/export", "/api/tasks/43/events/export"} {
		req, _ = http.NewRequest("GET", srv.URL+target, nil)
		req.Header.Set("Authorization", "Bearer private-administrator-password")
		response, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest && !(target == "/api/tasks/43/events/export" && response.StatusCode == http.StatusNotFound) {
			t.Fatalf("task event export %s status=%d", target, response.StatusCode)
		}
	}
	for _, target := range []string{"/api/tasks/not-a-number/events", "/api/tasks/3400000000000000123/events?before_sequence=bad"} {
		req, _ = http.NewRequest("GET", srv.URL+target, nil)
		req.Header.Set("Authorization", "Bearer private-administrator-password")
		response, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid event query %s status=%d, want 400", target, response.StatusCode)
		}
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/tasks/3400000000000000123/events?limit=51", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized event page accepted: %d", response.StatusCode)
	}
	req, _ = http.NewRequest("POST", srv.URL+"/api/tasks/3400000000000000123/events", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("event mutation method status=%d, want 405", response.StatusCode)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/status?offset=1&limit=1&sort_by=name", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	var pagedStatus struct {
		Tasks      []json.RawMessage `json:"tasks"`
		TotalTasks int               `json:"total_tasks"`
		Counts     map[string]int    `json:"status_counts"`
		Offset     int               `json:"offset"`
		HasMore    bool              `json:"has_more"`
	}
	if json.Unmarshal(body, &pagedStatus) != nil || response.StatusCode != http.StatusOK || len(pagedStatus.Tasks) != 0 || pagedStatus.TotalTasks != 1 || pagedStatus.Counts["cancelled"] != 1 || pagedStatus.Offset != 1 || pagedStatus.HasMore {
		t.Fatalf("incorrect paged status: %d %s", response.StatusCode, body)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/status?offset=0&limit=1&sort_by=name&status=cancelled&q=A.TXT", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	var filteredStatus struct {
		Tasks []struct {
			ID string `json:"task_id"`
		} `json:"tasks"`
		TotalTasks int            `json:"total_tasks"`
		QueueTotal int            `json:"queue_total_tasks"`
		Counts     map[string]int `json:"status_counts"`
	}
	if response.StatusCode != http.StatusOK || json.Unmarshal(body, &filteredStatus) != nil || len(filteredStatus.Tasks) != 1 || filteredStatus.Tasks[0].ID != "3400000000000000123" || filteredStatus.TotalTasks != 1 || filteredStatus.QueueTotal != 1 || filteredStatus.Counts["cancelled"] != 1 {
		t.Fatalf("status/search filters changed global counts or missed task: status=%d body=%s", response.StatusCode, body)
	}
	for _, target := range []string{
		"/api/status?offset=0&limit=1&status=invalid",
		"/api/status?offset=0&limit=1&q=a&q=b",
		"/api/status?offset=0&limit=1&q=" + strings.Repeat("x", 129),
	} {
		req, _ = http.NewRequest("GET", srv.URL+target, nil)
		req.Header.Set("Authorization", "Bearer private-administrator-password")
		response, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid task filters accepted: %s status=%d", target, response.StatusCode)
		}
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/status?limit=201", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized status page accepted: %d", response.StatusCode)
	}
	req, _ = http.NewRequest("POST", srv.URL+"/api/tasks/3400000000000000123/retry", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 409 {
		t.Fatalf("large task ID did not identify existing ineligible task: %d", response.StatusCode)
	}
	req, _ = http.NewRequest("POST", srv.URL+"/api/scan", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	req.Header.Set("Origin", "https://evil.example")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross-origin management request allowed")
	}
	headers := http.Header{"Authorization": {"Bearer private-administrator-password"}, "Origin": {srv.URL}}
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", headers)
	if err != nil {
		t.Fatalf("authenticated WebSocket failed: %v %v", err, response)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, body, err = conn.ReadMessage()
	if err != nil || !strings.Contains(string(body), `"type":"status"`) {
		t.Fatalf("missing WebSocket status: %v %s", err, body)
	}
	if !strings.Contains(string(body), `"task_id":"3400000000000000123"`) {
		t.Fatal("WebSocket lost exact task ID")
	}
	if !strings.Contains(string(body), `"can_pause":false,"can_resume":false`) {
		t.Fatalf("WebSocket status omitted task control capabilities: %s", body)
	}
	if !strings.Contains(string(body), `"local_storage":{"kind":"local_storage_blocked"`) {
		t.Fatalf("WebSocket status omitted persisted local storage diagnosis: %s", body)
	}
	if !strings.Contains(string(body), `"queue_pause":{"paused":false,"active_tasks":0}`) {
		t.Fatalf("WebSocket status omitted queue pause state: %s", body)
	}
	if !strings.Contains(string(body), `"queue_healthy":true`) {
		t.Fatalf("WebSocket status omitted queue operational state: %s", body)
	}
	if err := conn.WriteJSON(map[string]interface{}{"type": "status_page", "offset": 0, "limit": 100, "sort_by": "name", "status": "cancelled", "q": "A.TXT", "request_id": 77}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, body, err = conn.ReadMessage()
	if err != nil || !strings.Contains(string(body), `"total_tasks":1`) || !strings.Contains(string(body), `"queue_total_tasks":1`) || !strings.Contains(string(body), `"request_id":77`) || !strings.Contains(string(body), `"path":"/preview/a.txt"`) {
		t.Fatalf("WebSocket page request failed: %v %s", err, body)
	}
	if err := conn.WriteJSON(map[string]interface{}{"type": "status_page", "offset": 0, "limit": 100, "sort_by": "name", "status": "invalid", "q": "stale"}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, body, err = conn.ReadMessage()
	if err != nil || !strings.Contains(string(body), `"request_id":77`) || !strings.Contains(string(body), `"path":"/preview/a.txt"`) {
		t.Fatalf("invalid WebSocket filters changed the active page: %v %s", err, body)
	}
	conn.Close()
	headers.Set("Origin", "http://evil.example")
	conn, response, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", headers)
	if err == nil {
		conn.Close()
		t.Fatal("cross-origin WebSocket allowed")
	}
	if response == nil || response.StatusCode != 403 {
		t.Fatalf("wrong origin refusal: %v", err)
	}
}

func TestTaskEventEvidenceExportRejectsOversizedHistoryAsAWhole(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	root := t.TempDir()
	cfg = &config.Config{LocalDownDir: root, QueueDir: "/preview", MaxParallel: 1}
	store, err := taskstore.Open(filepath.Join(root, ".nas-downloader", "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	record := []byte(`{"version":1,"file":{"fs_id":42,"path":"/preview/a.txt","server_filename":"a.txt","size":3},"mode":"keep","status":"pending"}`)
	for i := 0; i <= taskstore.TaskEventEvidenceExportLimit; i++ {
		if err = store.SaveWithEvent(42, record, taskstore.Event{Kind: "download_attempt_started"}); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dl.Shutdown(context.Background()) })
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/42/events/export", nil)
	req.SetPathValue("id", "42")
	response := httptest.NewRecorder()
	handleTaskEventEvidenceExport(response, req)
	if response.Code != http.StatusRequestEntityTooLarge || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), `"events"`) {
		t.Fatalf("oversized history was not rejected all-or-nothing: status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	filteredRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/42/events/export?kind=download_attempt_started", nil)
	filteredRequest.SetPathValue("id", "42")
	filteredResponse := httptest.NewRecorder()
	handleTaskEventEvidenceExport(filteredResponse, filteredRequest)
	if filteredResponse.Code != http.StatusRequestEntityTooLarge || strings.Contains(filteredResponse.Body.String(), `"events"`) || !strings.Contains(filteredResponse.Body.String(), "当前筛选匹配超过 100 条") {
		t.Fatalf("oversized filtered history was not rejected all-or-nothing: status=%d body=%s", filteredResponse.Code, filteredResponse.Body.String())
	}
	post := httptest.NewRecorder()
	postRequest := httptest.NewRequest(http.MethodPost, "/api/tasks/42/events/export", nil)
	postRequest.SetPathValue("id", "42")
	handleTaskEventEvidenceExport(post, postRequest)
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("task event export accepted non-GET: status=%d headers=%v", post.Code, post.Header())
	}
}

func TestTaskEventFilterUsesRecordedUTCAndExportsOnlyMatches(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	root := t.TempDir()
	cfg = &config.Config{LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, ".nas-downloader"), QueueDir: "/preview", MaxParallel: 1}
	store, err := taskstore.Open(filepath.Join(root, ".nas-downloader", "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	record := []byte(`{"version":1,"file":{"fs_id":42,"path":"/preview/a.txt","server_filename":"a.txt","size":3},"mode":"keep","status":"pending"}`)
	oldOccurred := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	futureOccurred := time.Now().UTC().AddDate(1, 0, 0)
	for _, event := range []taskstore.Event{
		{Kind: "download_attempt_started", OccurredAtUTC: &oldOccurred, EvidenceType: "test", Evidence: json.RawMessage(`{"access_token":"sensitive"}`)},
		{Kind: "download_attempt_finished", OccurredAtUTC: &futureOccurred, EvidenceType: "test", Evidence: json.RawMessage(`{}`)},
	} {
		if err = store.SaveWithEvent(42, record, event); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dl.Shutdown(context.Background()) })
	date := time.Now().UTC().Format("2006-01-02")
	query := "?kind=download_attempt_started&from_utc=" + date + "&through_utc=" + date
	request := httptest.NewRequest(http.MethodGet, "/api/tasks/42/events"+query, nil)
	request.SetPathValue("id", "42")
	response := httptest.NewRecorder()
	handleTaskEvents(response, request)
	var page struct {
		Events       []taskstore.Event `json:"events"`
		TotalMatches int64             `json:"total_matches"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.TotalMatches != 1 || len(page.Events) != 1 || page.Events[0].Kind != "download_attempt_started" || !page.Events[0].OccurredAtUTC.Equal(oldOccurred) {
		t.Fatalf("filtered event page status=%d body=%s", response.Code, response.Body.String())
	}
	exportRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/42/events/export"+query, nil)
	exportRequest.SetPathValue("id", "42")
	exportResponse := httptest.NewRecorder()
	handleTaskEventEvidenceExport(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK || strings.Contains(exportResponse.Body.String(), "sensitive") || !strings.Contains(exportResponse.Body.String(), `"from_utc":"`+date+`"`) || !strings.Contains(exportResponse.Body.String(), `"total_events":1`) || !strings.Contains(exportResponse.Body.String(), `"access_token":"[REDACTED]"`) {
		t.Fatalf("filtered event export status=%d body=%s", exportResponse.Code, exportResponse.Body.String())
	}
	for _, target := range []string{
		"/api/tasks/42/events?kind=unlisted_event",
		"/api/tasks/42/events?kind=download_attempt_started&kind=task_retry_requested",
		"/api/tasks/42/events?from_utc=2026-11-03&through_utc=2026-11-02",
		"/api/tasks/42/events?from_utc=2026-02-30",
	} {
		invalid := httptest.NewRecorder()
		badRequest := httptest.NewRequest(http.MethodGet, target, nil)
		badRequest.SetPathValue("id", "42")
		handleTaskEvents(invalid, badRequest)
		if invalid.Code != http.StatusBadRequest {
			t.Errorf("invalid event filter %q returned %d, want 400", target, invalid.Code)
		}
	}
}

func TestTaskEventCorrelationFilterQueryValidation(t *testing.T) {
	valid, err := parseTaskEventFilter(url.Values{"operation_id": {"op/+'"}, "attempt_id": {"attempt-1"}}, true)
	if err != nil || valid.OperationID != "op/+'" || valid.AttemptID != "attempt-1" {
		t.Fatalf("valid correlation filter=%#v err=%v", valid, err)
	}
	for _, query := range []url.Values{
		{"operation_id": {""}},
		{"operation_id": {" op"}},
		{"attempt_id": {"attempt "}},
		{"operation_id": {"bad\x01id"}},
		{"attempt_id": {strings.Repeat("a", 129)}},
		{"operation_id": {"one", "two"}},
		{"attempt_id": {string([]byte{0xff})}},
		{"other_id": {"operation"}},
	} {
		if _, err = parseTaskEventFilter(query, true); err == nil {
			t.Errorf("invalid correlation query %q accepted", query.Encode())
		}
	}
	max := strings.Repeat("x", 128)
	if parsed, parseErr := parseTaskEventFilter(url.Values{"operation_id": {max}}, true); parseErr != nil || parsed.OperationID != max {
		t.Fatalf("128-byte operation ID rejected: %#v, %v", parsed, parseErr)
	}
}

func TestTaskEventExactUTCFilterValidation(t *testing.T) {
	valid, err := parseTaskEventFilter(url.Values{"recorded_from_utc": {"2026-11-02T10:00:00Z"}, "recorded_before_utc": {"2026-11-02T10:00:01.123456Z"}}, false)
	if err != nil || valid.RecordedExactFromUTC == nil || valid.RecordedExactBeforeUTC == nil || valid.RecordedExactBeforeUTC.Nanosecond() != 123456000 {
		t.Fatalf("exact UTC filter=%#v err=%v", valid, err)
	}
	for _, query := range []url.Values{
		{"recorded_from_utc": {"2026-11-02T10:00:00+08:00"}},
		{"recorded_from_utc": {"not-a-time"}},
		{"recorded_from_utc": {"2026-11-02T10:00:00.1234567Z"}},
		{"recorded_from_utc": {"2026-11-02T10:00:02Z"}, "recorded_before_utc": {"2026-11-02T10:00:01Z"}},
		{"from_utc": {"2026-11-02"}, "recorded_from_utc": {"2026-11-02T10:00:00Z"}},
	} {
		if _, err = parseTaskEventFilter(query, false); err == nil {
			t.Errorf("invalid exact UTC query accepted: %q", query.Encode())
		}
	}
}

func TestGlobalTaskEventHistoryAPIUsesSnapshotFiltersAndRedactsEvidence(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	root := t.TempDir()
	cfg = &config.Config{LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, ".nas-downloader"), QueueDir: "/preview", MaxParallel: 1}
	store, err := taskstore.Open(filepath.Join(root, ".nas-downloader", "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	record := []byte(`{"version":1,"file":{"fs_id":42,"path":"/preview/a.txt","server_filename":"a.txt","size":3},"mode":"keep","status":"pending"}`)
	record43 := []byte(strings.Replace(string(record), `"fs_id":42`, `"fs_id":43`, 1))
	start := time.Now().UTC().Truncate(time.Second)
	for _, item := range []struct {
		id                       int64
		kind, operation, attempt string
		at                       time.Time
		evidence                 string
	}{
		{42, "source_operation_unknown", "op-a", "attempt-a", start, `{"access_token":"private-token"}`},
		{43, "download_attempt_started", "op-a", "attempt-b", start.Add(time.Microsecond), `{"ok":true}`},
		{42, "source_operation_confirmed", "op-b", "attempt-a", start.Add(time.Second), `{"ok":true}`},
	} {
		jobRecord := record
		if item.id == 43 {
			jobRecord = record43
		}
		if err = store.SaveWithEvent(item.id, jobRecord, taskstore.Event{Kind: item.kind, OperationID: item.operation, AttemptID: item.attempt, RecordedAtUTC: item.at, EvidenceType: "test", Evidence: json.RawMessage(item.evidence)}); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dl.Shutdown(context.Background()) })
	from := start.Format(time.RFC3339Nano)
	before := start.Add(time.Second).Format(time.RFC3339Nano)
	request := httptest.NewRequest(http.MethodGet, "/api/task-events?task_id=42&operation_id=op-a&recorded_from_utc="+url.QueryEscape(from)+"&recorded_before_utc="+url.QueryEscape(before)+"&limit=1", nil)
	response := httptest.NewRecorder()
	handleTaskEventHistory(response, request)
	var page struct {
		Events       []globalTaskEventDTO `json:"events"`
		TotalMatches int64                `json:"total_matches"`
		AsOfSequence string               `json:"as_of_sequence"`
	}
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.TotalMatches != 1 || page.AsOfSequence != "3" || len(page.Events) != 1 || page.Events[0].JobID != "42" || page.Events[0].Kind != "source_operation_unknown" || strings.Contains(response.Body.String(), "private-token") || !strings.Contains(response.Body.String(), `"access_token":"[REDACTED]"`) {
		t.Fatalf("global history status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	exportRequest := httptest.NewRequest(http.MethodGet, "/api/task-events/export?task_id=42&operation_id=op-a&as_of_sequence=3&recorded_from_utc="+url.QueryEscape(from)+"&recorded_before_utc="+url.QueryEscape(before), nil)
	exportResponse := httptest.NewRecorder()
	handleTaskEventHistoryExport(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK || exportResponse.Header().Get("Cache-Control") != "no-store" || !strings.Contains(exportResponse.Header().Get("Content-Disposition"), `attachment; filename="task-events-global-`) || strings.Contains(exportResponse.Body.String(), "private-token") || !strings.Contains(exportResponse.Body.String(), `"as_of_sequence":"3"`) || !strings.Contains(exportResponse.Body.String(), `"task_id":"42"`) {
		t.Fatalf("global evidence export status=%d headers=%v body=%s", exportResponse.Code, exportResponse.Header(), exportResponse.Body.String())
	}
}

func TestGlobalTaskEventHistoryAPIRejectsInvalidCursorsAndFilters(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	root := t.TempDir()
	cfg = &config.Config{LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, ".nas-downloader"), QueueDir: "/preview", MaxParallel: 1}
	var err error
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dl.Shutdown(context.Background()) })
	for _, target := range []string{
		"/api/task-events?task_id=0",
		"/api/task-events?kind=unknown-event",
		"/api/task-events?before_sequence=2&as_of_sequence=1",
		"/api/task-events?as_of_sequence=1",
		"/api/task-events?from_utc=2026-11-02&recorded_from_utc=2026-11-02T00%3A00%3A00Z",
		"/api/task-events?operation_id=x&operation_id=y",
	} {
		response := httptest.NewRecorder()
		handleTaskEventHistory(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("invalid global event query %q status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
	post := httptest.NewRecorder()
	handleTaskEventHistory(post, httptest.NewRequest(http.MethodPost, "/api/task-events", nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("global event API accepted non-GET: status=%d headers=%v", post.Code, post.Header())
	}
}

func TestGlobalTaskEventHistoryPageAndAPIRequireManagementAuthentication(t *testing.T) {
	oldCfg, oldDL, oldUI := cfg, dl, ui
	t.Cleanup(func() { cfg, dl, ui = oldCfg, oldDL, oldUI })
	root := t.TempDir()
	cfg = &config.Config{LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, ".nas-downloader"), QueueDir: "/preview", MaxParallel: 1}
	var err error
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dl.Shutdown(context.Background()) })
	ui = webui.NewServer(dl)
	gate, err := management.New("private-administrator-password")
	if err != nil {
		t.Fatal(err)
	}
	gate.SetSessionStore(dl)
	handler := applicationHandler(gate, nil)
	for _, target := range []struct {
		path string
		want int
	}{
		{"/task-events", http.StatusSeeOther},
		{"/api/task-events", http.StatusUnauthorized},
		// Upload recovery history uses the same management gate, including before service readiness.
		{"/api/uploads/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/recovery", http.StatusUnauthorized},
		{"/api/uploads/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/items/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/session", http.StatusUnauthorized},
		{"/api/uploads/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/items/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/verify-receipt", http.StatusUnauthorized},
		{"/api/uploads/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/transfer-statistics", http.StatusUnauthorized},
		{"/api/uploads/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/pause", http.StatusUnauthorized},
		{"/api/uploads/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/resume", http.StatusUnauthorized},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target.path, nil))
		if response.Code != target.want {
			t.Errorf("unauthenticated %s status=%d, want %d", target.path, response.Code, target.want)
		}
	}
	authorized := httptest.NewRequest(http.MethodGet, "/api/task-events", nil)
	authorized.Header.Set("Authorization", "Bearer private-administrator-password")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authorized)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"events":[]`) {
		t.Fatalf("authenticated global event page status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTaskEventCorrelationFiltersApplyToPageAndExport(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	root := t.TempDir()
	stateDir := filepath.Join(root, ".nas-downloader")
	cfg = &config.Config{LocalDownDir: filepath.Join(root, "downloads"), StateDir: stateDir, QueueDir: "/preview", MaxParallel: 1}
	store, err := taskstore.Open(filepath.Join(stateDir, "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{42, 43} {
		record, marshalErr := json.Marshal(map[string]any{"version": 1, "file": map[string]any{"fs_id": id, "path": "/preview/a.txt", "server_filename": "a.txt", "size": 3}, "mode": "keep", "status": "pending"})
		if marshalErr != nil {
			store.Close()
			t.Fatal(marshalErr)
		}
		if err = store.SaveWithEvent(id, record, taskstore.Event{Kind: "source_operation_confirmed", OperationID: "operation-1", AttemptID: "attempt-1", EvidenceType: "test", Evidence: json.RawMessage(`{"access_token":"secret"}`)}); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	record, err := json.Marshal(map[string]any{"version": 1, "file": map[string]any{"fs_id": int64(42), "path": "/preview/a.txt", "server_filename": "a.txt", "size": 3}, "mode": "keep", "status": "pending"})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err = store.SaveWithEvent(42, record, taskstore.Event{Kind: "download_attempt_started", AttemptID: "attempt-2", EvidenceType: "test", Evidence: json.RawMessage(`{}`)}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dl.Shutdown(context.Background()) })
	date := time.Now().UTC().Format("2006-01-02")
	query := "?kind=source_operation_confirmed&operation_id=operation-1&attempt_id=attempt-1&from_utc=" + date + "&through_utc=" + date
	request := httptest.NewRequest(http.MethodGet, "/api/tasks/42/events"+query, nil)
	request.SetPathValue("id", "42")
	response := httptest.NewRecorder()
	handleTaskEvents(response, request)
	var page struct {
		Events       []taskstore.Event              `json:"events"`
		TotalMatches int64                          `json:"total_matches"`
		KindCounts   []taskstore.TaskEventKindCount `json:"kind_counts"`
	}
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"access_token":"secret"`) || !strings.Contains(response.Body.String(), `"access_token":"[REDACTED]"`) || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.TotalMatches != 1 || len(page.Events) != 1 || page.Events[0].OperationID != "operation-1" || page.Events[0].AttemptID != "attempt-1" || !hasTaskEventKindCount(page.KindCounts, "source_operation_confirmed", 1) {
		t.Fatalf("correlation page status=%d body=%s", response.Code, response.Body.String())
	}
	exportRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/42/events/export"+query, nil)
	exportRequest.SetPathValue("id", "42")
	exportResponse := httptest.NewRecorder()
	handleTaskEventEvidenceExport(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK || strings.Contains(exportResponse.Body.String(), "secret") || !strings.Contains(exportResponse.Body.String(), `"operation_id":"operation-1"`) || !strings.Contains(exportResponse.Body.String(), `"attempt_id":"attempt-1"`) || !strings.Contains(exportResponse.Body.String(), `"filters":{"attempt_id":"attempt-1","from_utc":"`+date+`","kind":"source_operation_confirmed","operation_id":"operation-1","through_utc":"`+date+`"}`) {
		t.Fatalf("correlation export status=%d body=%s", exportResponse.Code, exportResponse.Body.String())
	}
	exactStart := page.Events[0].RecordedAtUTC.Format(time.RFC3339Nano)
	exactBefore := page.Events[0].RecordedAtUTC.Add(time.Second).Format(time.RFC3339Nano)
	exactQuery := "?operation_id=operation-1&attempt_id=attempt-1&recorded_from_utc=" + url.QueryEscape(exactStart) + "&recorded_before_utc=" + url.QueryEscape(exactBefore)
	exactPage := httptest.NewRecorder()
	exactRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/42/events"+exactQuery, nil)
	exactRequest.SetPathValue("id", "42")
	handleTaskEvents(exactPage, exactRequest)
	if exactPage.Code != http.StatusOK || !strings.Contains(exactPage.Body.String(), `"total_matches":1`) || exactPage.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("exact UTC event page status=%d headers=%v body=%s", exactPage.Code, exactPage.Header(), exactPage.Body.String())
	}
	exactExport := httptest.NewRecorder()
	exactExportRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/42/events/export"+exactQuery, nil)
	exactExportRequest.SetPathValue("id", "42")
	handleTaskEventEvidenceExport(exactExport, exactExportRequest)
	if exactExport.Code != http.StatusOK || !strings.Contains(exactExport.Body.String(), `"recorded_from_utc":"`+exactStart+`"`) || !strings.Contains(exactExport.Body.String(), `"recorded_before_utc":"`+exactBefore+`"`) {
		t.Fatalf("exact UTC event export status=%d body=%s", exactExport.Code, exactExport.Body.String())
	}
	zeroRequest := httptest.NewRequest(http.MethodGet, "/api/tasks/42/events?attempt_id=missing", nil)
	zeroRequest.SetPathValue("id", "42")
	zeroResponse := httptest.NewRecorder()
	handleTaskEvents(zeroResponse, zeroRequest)
	if zeroResponse.Code != http.StatusOK || !strings.Contains(zeroResponse.Body.String(), `"total_matches":0`) {
		t.Fatalf("zero correlation result status=%d body=%s", zeroResponse.Code, zeroResponse.Body.String())
	}
	notFound := httptest.NewRecorder()
	missingTask := httptest.NewRequest(http.MethodGet, "/api/tasks/999/events?operation_id=operation-1", nil)
	missingTask.SetPathValue("id", "999")
	handleTaskEvents(notFound, missingTask)
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("missing task returned %d, want 404", notFound.Code)
	}
}

func hasTaskEventKindCount(counts []taskstore.TaskEventKindCount, kind string, expected int64) bool {
	for _, count := range counts {
		if count.Kind == kind {
			return count.Count == expected
		}
	}
	return false
}

func TestTaskEventContextAPIIsTaskScopedAndMasksEvidence(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	root := t.TempDir()
	cfg = &config.Config{LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, ".nas-downloader"), QueueDir: "/preview", MaxParallel: 1}
	store, err := taskstore.Open(filepath.Join(cfg.StateDir, "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{42, 43} {
		record, _ := json.Marshal(map[string]any{"version": 1, "file": map[string]any{"fs_id": id, "path": "/preview/a.txt", "server_filename": "a.txt", "size": 3}, "mode": "keep", "status": "pending"})
		if err = store.SaveWithEvent(id, record, taskstore.Event{Kind: "source_operation_unknown", EvidenceType: "test", Evidence: json.RawMessage(`{"access_token":"private"}`)}); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	events, _, _, err := store.EventPage(42, 0, 10)
	if err != nil || len(events) == 0 {
		store.Close()
		t.Fatalf("events=%#v err=%v", events, err)
	}
	sequence := events[len(events)-1].Sequence
	otherEvents, _, _, err := store.EventPage(43, 0, 10)
	if err != nil || len(otherEvents) == 0 {
		store.Close()
		t.Fatalf("other task events=%#v err=%v", otherEvents, err)
	}
	otherSequence := otherEvents[len(otherEvents)-1].Sequence
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dl.Shutdown(context.Background()) })
	request := httptest.NewRequest(http.MethodGet, "/api/tasks/42/events/"+strconv.FormatInt(sequence, 10)+"/context", nil)
	request.SetPathValue("id", "42")
	request.SetPathValue("sequence", strconv.FormatInt(sequence, 10))
	response := httptest.NewRecorder()
	handleTaskEventContext(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private") || !strings.Contains(response.Body.String(), "[REDACTED]") || !strings.Contains(response.Body.String(), `"anchor"`) {
		t.Fatalf("context response status=%d body=%s", response.Code, response.Body.String())
	}
	wrongTask := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/tasks/42/events/"+strconv.FormatInt(otherSequence, 10)+"/context", nil)
	request.SetPathValue("id", "42")
	request.SetPathValue("sequence", strconv.FormatInt(otherSequence, 10))
	handleTaskEventContext(wrongTask, request)
	if wrongTask.Code != http.StatusNotFound {
		t.Fatalf("other task event sequence returned %d", wrongTask.Code)
	}
	missing := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/tasks/42/events/999/context", nil)
	request.SetPathValue("id", "42")
	request.SetPathValue("sequence", "999")
	handleTaskEventContext(missing, request)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("cross-task/missing anchor status=%d", missing.Code)
	}
}

func TestTaskEventCorrelationReadStoreUnavailableReturns503(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	root := t.TempDir()
	cfg = &config.Config{LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, ".nas-downloader"), QueueDir: "/preview", MaxParallel: 1}
	var err error
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	if err = dl.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/tasks/42/events?operation_id=operation-1", nil)
	request.SetPathValue("id", "42")
	response := httptest.NewRecorder()
	handleTaskEvents(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed event store returned %d, want 503: %s", response.Code, response.Body.String())
	}
}

func TestManagedPathsHTTPReadAndSave(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	root := t.TempDir()
	cfg = &config.Config{LocalDownDir: filepath.Join(root, "downloads"), QueueDir: "/old", StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	var err error
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dl.Shutdown(context.Background()) })
	get := httptest.NewRecorder()
	handleManagedPaths(get, httptest.NewRequest(http.MethodGet, "/api/settings/paths", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"queue_dir":"/old"`) {
		t.Fatalf("path settings GET: %d %s", get.Code, get.Body.String())
	}
	if !strings.Contains(get.Body.String(), `"queue_source":"startup"`) || !strings.Contains(get.Body.String(), `"local_download_source":"startup"`) {
		t.Fatalf("missing startup sources: %s", get.Body.String())
	}
	requestBody, err := json.Marshal(downloader.ManagedPaths{QueueDir: "/new", LocalDownDir: filepath.Join(root, "new-downloads")})
	if err != nil {
		t.Fatal(err)
	}
	post := httptest.NewRecorder()
	handleManagedPaths(post, httptest.NewRequest(http.MethodPost, "/api/settings/paths", strings.NewReader(string(requestBody))))
	if post.Code != http.StatusOK || !strings.Contains(post.Body.String(), `"queue_dir":"/new"`) {
		t.Fatalf("path settings POST: %d %s", post.Code, post.Body.String())
	}
	after := httptest.NewRecorder()
	handleManagedPaths(after, httptest.NewRequest(http.MethodGet, "/api/settings/paths", nil))
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), `"queue_source":"saved"`) || !strings.Contains(after.Body.String(), `"local_download_source":"saved"`) {
		t.Fatalf("saved sources missing: %d %s", after.Code, after.Body.String())
	}

}

func TestManagedPathsHTTPImpactPreviewAndConfirmation(t *testing.T) {
	oldCfg, oldDL := cfg, dl
	t.Cleanup(func() { cfg, dl = oldCfg, oldDL })
	root := t.TempDir()
	cfg = &config.Config{LocalDownDir: filepath.Join(root, "downloads"), QueueDir: "/old", StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	store, err := taskstore.Open(filepath.Join(cfg.StateDir, "queue"), cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(81, []byte(`{"version":1,"file":{"fs_id":81,"path":"/old/pending.txt","server_filename":"pending.txt","size":7},"mode":"keep","status":"pending"}`)); err != nil {
		store.Close()
		t.Fatal(err)
	}
	for _, id := range []int64{82, 83} {
		if err = store.Save(id, []byte(fmt.Sprintf(`{"version":1,"file":{"fs_id":%d,"path":"/old/pending-%d.txt","server_filename":"pending-%d.txt","size":7},"mode":"keep","status":"pending"}`, id, id, id))); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dl.Shutdown(context.Background()) })
	getImpact := func(offset int) downloader.ManagedPathImpact {
		t.Helper()
		get := httptest.NewRecorder()
		handleManagedPaths(get, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/settings/paths?queue_dir=/new&offset=%d&limit=1", offset), nil))
		var body struct {
			Impact downloader.ManagedPathImpact `json:"queue_change_impact"`
		}
		if get.Code != http.StatusOK || json.Unmarshal(get.Body.Bytes(), &body) != nil || body.Impact.Count != 3 || len(body.Impact.Tasks) != 1 || body.Impact.Tasks[0].FsID != int64(81+offset) {
			t.Fatalf("path impact GET: status=%d body=%s", get.Code, get.Body.String())
		}
		return body.Impact
	}
	impact := getImpact(0)
	secondPage := getImpact(1)
	thirdPage := getImpact(2)
	if !impact.HasMore || !secondPage.HasMore || thirdPage.HasMore || impact.Digest != secondPage.Digest || secondPage.Digest != thirdPage.Digest || impact.Offset != 0 || secondPage.Offset != 1 || thirdPage.Offset != 2 {
		t.Fatalf("path impact pages do not share a stable snapshot: first=%+v second=%+v third=%+v", impact, secondPage, thirdPage)
	}
	for _, query := range []string{"offset=-1", "offset=invalid", "limit=0", "limit=51"} {
		get := httptest.NewRecorder()
		handleManagedPaths(get, httptest.NewRequest(http.MethodGet, "/api/settings/paths?queue_dir=/new&"+query, nil))
		if get.Code != http.StatusBadRequest {
			t.Fatalf("invalid impact pagination accepted (%s): %d %s", query, get.Code, get.Body.String())
		}
	}
	largeOffset := httptest.NewRecorder()
	handleManagedPaths(largeOffset, httptest.NewRequest(http.MethodGet, "/api/settings/paths?queue_dir=/new&offset=9223372036854775807&limit=1", nil))
	var clamped struct {
		Impact downloader.ManagedPathImpact `json:"queue_change_impact"`
	}
	if largeOffset.Code != http.StatusOK || json.Unmarshal(largeOffset.Body.Bytes(), &clamped) != nil || clamped.Impact.Offset != clamped.Impact.Count || len(clamped.Impact.Tasks) != 0 {
		t.Fatalf("very large impact offset was not safely clamped: %d %s", largeOffset.Code, largeOffset.Body.String())
	}
	postPath := func(digest string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"queue_dir": "/new", "local_download_dir": cfg.LocalDownDir, "queue_change_preview_digest": digest})
		post := httptest.NewRecorder()
		handleManagedPaths(post, httptest.NewRequest(http.MethodPost, "/api/settings/paths", strings.NewReader(string(body))))
		return post
	}
	missing := postPath("")
	if missing.Code != http.StatusConflict || !strings.Contains(missing.Body.String(), `"queue_change_impact"`) || dl.GetManagedPaths().QueueDir != "/old" {
		t.Fatalf("missing impact confirmation was not rejected with a preview: status=%d body=%s", missing.Code, missing.Body.String())
	}
	stale := postPath("stale-preview-digest")
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "再次确认") || dl.GetManagedPaths().QueueDir != "/old" {
		t.Fatalf("stale impact confirmation was not rejected: status=%d body=%s", stale.Code, stale.Body.String())
	}
	post := postPath(impact.Digest)
	if post.Code != http.StatusOK || dl.GetManagedPaths().QueueDir != "/new" {
		t.Fatalf("confirmed path save: status=%d body=%s paths=%+v", post.Code, post.Body.String(), dl.GetManagedPaths())
	}
}

func TestLoadMainConfigStoresPackageConfig(t *testing.T) {
	previous := cfg
	t.Cleanup(func() { cfg = previous })

	if err := loadMainConfig(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("main config was not stored for credential callbacks")
	}
}

type localAuthorizationProvider struct{}

func (localAuthorizationProvider) Account(context.Context, string, string) (string, error) {
	return "123", nil
}
func (localAuthorizationProvider) Refresh(context.Context, string, string, string) (*oauth.TokenResponse, error) {
	return &oauth.TokenResponse{AccessToken: "private-renewed", RefreshToken: "private-refresh", ExpiresIn: 3600}, nil
}
func (localAuthorizationProvider) Device(context.Context, string) (*oauth.DeviceCodeResponse, error) {
	return &oauth.DeviceCodeResponse{DeviceCode: "local-device-code", UserCode: "123456", Interval: 1, ExpiresIn: 120}, nil
}
func (localAuthorizationProvider) Poll(context.Context, string, string, string, int, int) (*oauth.TokenResponse, error) {
	return &oauth.TokenResponse{AccessToken: "private-access", RefreshToken: "private-refresh", ExpiresIn: 3600}, nil
}

func TestWebAuthorizationAppliesDurableCredentialsToDownloader(t *testing.T) {
	var err error
	cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/preview", ScanInterval: 30, MaxParallel: 1, BaiduAppKey: "app", BaiduSecretKey: "private-secret"}
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := dl.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	dl.SetAuthentication(baiduapi.NewClient("", "app"), false, time.Time{})
	p := localAuthorizationProvider{}
	auth, err = credentials.Open(dl, credentials.Options{AppKey: "app", SecretKey: "private-secret"}, p, func(u credentials.Update) {
		dl.SetAuthentication(baiduapi.NewClient(u.AccessToken, "app"), u.Ready, u.ExpiresAt)
	})
	if err != nil {
		t.Fatal(err)
	}
	ui = webui.NewServer(dl).WithContext(dl.Context())
	gate, _ := management.New("private-administrator-password")
	gate.SetSessionStore(dl)
	srv := httptest.NewServer(applicationHandler(gate, authorization.New(dl.Context(), "app", "private-secret", auth, p)))
	defer srv.Close()
	for _, path := range []string{"/api/oauth/device-code", "/api/oauth/token"} {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(`{"device_code":"local-device-code"}`))
		req.Header.Set("Authorization", "Bearer private-administrator-password")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || strings.Contains(string(body), "private-") {
			t.Fatalf("authorization HTTP status=%d leaked=%v", response.StatusCode, strings.Contains(string(body), "private-"))
		}
	}
	if !auth.Status().Ready || !dl.AuthenticationReady() {
		t.Fatal("web authorization did not enable downloader")
	}
	body, err := dl.LoadCredentials()
	if err != nil || !strings.Contains(string(body), "private-access") {
		t.Fatal("web authorization was not durably saved")
	}
}

func TestAppConfigCanBeSetOnceAndOnlyReturnsMasks(t *testing.T) {
	priorConfig, priorDL, priorAuth, priorAuthorization := cfg, dl, auth, authorizationHTTP
	defer func() { cfg, dl, auth, authorizationHTTP = priorConfig, priorDL, priorAuth, priorAuthorization }()
	cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/queue", ScanInterval: 30, MaxParallel: 1, MaxRetries: 2, RetryBackoffSeconds: 1}
	var err error
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := dl.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	auth, err = credentials.Open(dl, credentials.Options{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ui = webui.NewServer(dl).WithContext(dl.Context())
	authorizationHTTP = authorization.New(dl.Context(), "", "", auth, nil)
	gate, err := management.New("private-config-test-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	gate.SetSessionStore(dl)
	handler := applicationHandler(gate, authorizationHTTP)
	request := httptest.NewRequest("GET", "http://example.test/api/app-config", nil)
	request.Header.Set("Authorization", "Bearer private-config-test-admin-password")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var status map[string]any
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &status) != nil || status["editable"] != true || status["configured"] != false {
		t.Fatalf("empty app config state: %d %s", response.Code, response.Body.String())
	}
	payload := `{"app_id":"app-id-private-4821","app_key":"app-key-private-4821","secret_key":"secret-private-4821","sign_key":"sign-private-4821"}`
	request = httptest.NewRequest("POST", "http://example.test/api/app-config", strings.NewReader(payload))
	request.Header.Set("Authorization", "Bearer private-config-test-admin-password")
	request.Header.Set("Origin", "http://example.test")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("save status=%d body=%s", response.Code, response.Body.String())
	}
	for _, secret := range []string{"secret-private-4821", "sign-private-4821"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("response leaked %s", secret)
		}
	}
	request = httptest.NewRequest("GET", "http://example.test/api/app-config", nil)
	request.Header.Set("Authorization", "Bearer private-config-test-admin-password")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &status) != nil || status["configured"] != true || status["editable"] != false || status["source"] != "queue" {
		t.Fatalf("saved app config state: %d %s", response.Code, response.Body.String())
	}
	for _, secret := range []string{"secret-private-4821", "sign-private-4821"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("status leaked %s", secret)
		}
	}
	request = httptest.NewRequest("POST", "http://example.test/api/app-config", strings.NewReader(payload))
	request.Header.Set("Authorization", "Bearer private-config-test-admin-password")
	request.Header.Set("Origin", "http://example.test")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate save status=%d body=%s", response.Code, response.Body.String())
	}
}
