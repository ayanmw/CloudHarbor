package main

import (
	"context"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
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
	"web_browser_baidu_onekey_upload/internal/webui"
)

func TestManagementHTTPAndWebSocketIntegration(t *testing.T) {
	var err error
	cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/preview", ScanInterval: 30, MaxParallel: 1}
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
	srv := httptest.NewServer(applicationHandler(gate, authorization.New(dl.Context(), "", "", auth, nil)))
	defer srv.Close()
	for _, path := range []string{"/", "/oauth", "/api/health", "/api/oauth/status", "/api/status", "/res/shared.js", "/ws"} {
		response, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatalf("anonymous %s status=%d", path, response.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/health", nil)
	req.Header.Set("Authorization", "Bearer private-administrator-password")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(body), `"status":"waiting_authorization"`) || !strings.Contains(string(body), `"ready":false`) {
		t.Fatalf("health invented account readiness: %s", body)
	}
	req, _ = http.NewRequest("POST", srv.URL+"/api/scan", nil)
	req.SetBasicAuth("admin", "private-administrator-password")
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
	conn.Close()
	if err != nil || !strings.Contains(string(body), `"type":"status"`) {
		t.Fatalf("missing WebSocket status: %v %s", err, body)
	}
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
	srv := httptest.NewServer(applicationHandler(gate, authorization.New(dl.Context(), "app", "private-secret", auth, p)))
	defer srv.Close()
	for _, path := range []string{"/api/oauth/device-code", "/api/oauth/token"} {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(`{"device_code":"local-device-code"}`))
		req.SetBasicAuth("admin", "private-administrator-password")
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
