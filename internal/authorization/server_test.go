package authorization

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/credentials"
	"web_browser_baidu_onekey_upload/internal/oauth"
)

type fakeManager struct{ applied string }

func (m *fakeManager) Apply(ctx context.Context, t *oauth.TokenResponse) error {
	m.applied = t.AccessToken
	return nil
}
func (m *fakeManager) Refresh(context.Context) error { return nil }
func (m *fakeManager) Status() credentials.Status {
	return credentials.Status{State: "ready", Ready: true}
}

type fakeProvider struct {
	device func(context.Context, string) (*oauth.DeviceCodeResponse, error)
	poll   func(context.Context, string, string, string, int, int) (*oauth.TokenResponse, error)
}

func (p fakeProvider) Device(ctx context.Context, k string) (*oauth.DeviceCodeResponse, error) {
	return p.device(ctx, k)
}
func (p fakeProvider) Poll(ctx context.Context, k, s, c string, i, e int) (*oauth.TokenResponse, error) {
	return p.poll(ctx, k, s, c, i, e)
}

func TestDeviceSessionOwnsPollingAndAppliesWithoutReturningTokens(t *testing.T) {
	manager := &fakeManager{}
	p := fakeProvider{device: func(context.Context, string) (*oauth.DeviceCodeResponse, error) {
		return &oauth.DeviceCodeResponse{DeviceCode: "server-code", UserCode: "123", Interval: 7, ExpiresIn: 120, VerificationURL: "https://openapi.baidu.com/device", QRCodeURL: "https://openapi.baidu.com/device/qrcode"}, nil
	}, poll: func(ctx context.Context, k, s, c string, i, e int) (*oauth.TokenResponse, error) {
		if c != "server-code" || i != 7 || e < 110 || e > 120 {
			t.Fatalf("browser changed server polling: %s %d %d", c, i, e)
		}
		return &oauth.TokenResponse{AccessToken: "private-access", RefreshToken: "private-refresh", ExpiresIn: 3600}, nil
	}}
	server := New(context.Background(), "app", "secret", manager, p)
	w := httptest.NewRecorder()
	server.HandleDeviceCode(w, httptest.NewRequest(http.MethodPost, "/api/oauth/device-code", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleToken(w, httptest.NewRequest(http.MethodPost, "/api/oauth/token", strings.NewReader(`{"device_code":"server-code","interval":1,"expires_in":3600}`)))
	if w.Code != 200 || manager.applied != "private-access" || strings.Contains(w.Body.String(), "private-") {
		t.Fatalf("authorization not applied or leaked: %d %s", w.Code, w.Body.String())
	}
	var status credentials.Status
	if json.Unmarshal(w.Body.Bytes(), &status) != nil || !status.Ready {
		t.Fatal("missing credential status")
	}
	w = httptest.NewRecorder()
	server.HandleToken(w, httptest.NewRequest(http.MethodPost, "/api/oauth/token", strings.NewReader(`{"device_code":"server-code"}`)))
	if w.Code != 409 {
		t.Fatal("consumed device session reused")
	}
}

func TestNewDeviceSessionCancelsPreviousPoll(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	p := fakeProvider{device: func(context.Context, string) (*oauth.DeviceCodeResponse, error) {
		return &oauth.DeviceCodeResponse{DeviceCode: "code", Interval: 1, ExpiresIn: 60}, nil
	}, poll: func(ctx context.Context, _, _, _ string, _, _ int) (*oauth.TokenResponse, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}}
	server := New(context.Background(), "app", "secret", &fakeManager{}, p)
	server.HandleDeviceCode(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil))
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.HandleToken(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(`{"device_code":"code"}`)))
	}()
	<-started
	server.HandleDeviceCode(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil))
	<-cancelled
	<-done
}
