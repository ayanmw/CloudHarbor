package main

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/authorization"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/credentials"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/management"
	"web_browser_baidu_onekey_upload/internal/oauth"
	"web_browser_baidu_onekey_upload/internal/taskstore"
	"web_browser_baidu_onekey_upload/internal/webui"
)

func TestManagementHTTPAndWebSocketIntegration(t *testing.T) {
	var err error
	cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/preview", ScanInterval: 30, MaxParallel: 1}
	store, err := taskstore.Open(filepath.Join(cfg.LocalDownDir, ".nas-downloader", "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	jobRecord := []byte(`{"version":1,"file":{"fs_id":3400000000000000123,"path":"/preview/a.txt","server_filename":"a.txt","size":3},"mode":"keep","status":"cancelled"}`)
	if err = store.Save(3400000000000000123, jobRecord); err != nil {
		store.Close()
		t.Fatal(err)
	}
	for i := 1; i <= 21; i++ {
		if err = store.SaveWithEvent(3400000000000000123, jobRecord, taskstore.Event{Kind: "api-event-" + strconv.Itoa(i)}); err != nil {
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
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
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
	for _, path := range []string{"/", "/oauth", "/intake", "/api/cloud", "/api/intakes", "/api/health", "/api/oauth/status", "/api/status", "/api/tasks/3400000000000000123/events", "/res/shared.js", "/res/intake.js", "/ws"} {
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
	req, _ := http.NewRequest("GET", srv.URL+"/api/health", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(body), `"status":"waiting_authorization"`) || !strings.Contains(string(body), `"ready":false`) || !strings.Contains(string(body), `"cancelled":1`) {
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
	if err := conn.WriteJSON(map[string]interface{}{"type": "status_page", "offset": 1, "limit": 100, "sort_by": "name"}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, body, err = conn.ReadMessage()
	if err != nil || !strings.Contains(string(body), `"total_tasks":1`) || !strings.Contains(string(body), `"tasks":[]`) {
		t.Fatalf("WebSocket page request failed: %v %s", err, body)
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
	requestBody, err := json.Marshal(downloader.ManagedPaths{QueueDir: "/new", LocalDownDir: filepath.Join(root, "new-downloads")})
	if err != nil {
		t.Fatal(err)
	}
	post := httptest.NewRecorder()
	handleManagedPaths(post, httptest.NewRequest(http.MethodPost, "/api/settings/paths", strings.NewReader(string(requestBody))))
	if post.Code != http.StatusOK || !strings.Contains(post.Body.String(), `"queue_dir":"/new"`) {
		t.Fatalf("path settings POST: %d %s", post.Code, post.Body.String())
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
