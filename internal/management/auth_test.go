package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestManagementAuthenticationAndOrigin(t *testing.T) {
	gate, err := New("a-strong-preview-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	handler := gate.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && !CheckOrigin(r) {
			http.Error(w, "cross origin", 403)
			return
		}
		w.WriteHeader(204)
	}))
	tests := []struct {
		name, path, method, origin, fetch, bearer, basic string
		want                                             int
		challenge                                        string
	}{
		{name: "anonymous api", path: "/api/scan", method: "GET", want: 401},
		{name: "anonymous page redirects", path: "/", method: "GET", want: 303},
		{name: "basic rejected", path: "/api/scan", method: "GET", basic: "a-strong-preview-admin-password", want: 401},
		{name: "bearer CLI", path: "/api/scan", method: "POST", bearer: "a-strong-preview-admin-password", want: 204},
		{name: "wrong bearer", path: "/api/scan", method: "POST", bearer: "wrong", want: 401},
		{name: "same origin", path: "/api/scan", method: "POST", origin: "http://example.com", bearer: "a-strong-preview-admin-password", want: 204},
		{name: "cross origin", path: "/api/scan", method: "POST", origin: "http://evil.example", bearer: "a-strong-preview-admin-password", want: 403},
		{name: "cross site", path: "/api/scan", method: "POST", fetch: "cross-site", bearer: "a-strong-preview-admin-password", want: 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, "http://example.com"+tt.path, nil)
			if tt.basic != "" {
				r.SetBasicAuth("admin", tt.basic)
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
				t.Fatalf("status=%d want=%d body=%s", w.Code, tt.want, w.Body.String())
			}
			if w.Header().Get("WWW-Authenticate") != "" {
				t.Fatal("Basic challenge must not be returned")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private management page cacheable")
			}
		})
	}
}

func TestSessionLoginSurvivesGateRecreationAndLogoutRevokes(t *testing.T) {
	store, err := taskstore.Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	password := "a-strong-preview-admin-password"
	gate, _ := New(password)
	gate.SetSessionStore(store)
	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "https://nas.example/api/login", strings.NewReader(`{"password":"a-strong-preview-admin-password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://nas.example")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	gate.HandleLogin(login, req)
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	var response map[string]bool
	if json.Unmarshal(login.Body.Bytes(), &response) != nil || !response["ok"] {
		t.Fatal("missing login success response")
	}
	cookie := login.Result().Cookies()[0]
	if cookie.Name != sessionCookieName || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || !cookie.Secure || cookie.Expires.Year() != 9999 {
		t.Fatalf("unexpected cookie attributes: %#v", cookie)
	}
	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	gateAfterRestart, _ := New(password)
	gateAfterRestart.SetSessionStore(store)
	check := httptest.NewRecorder()
	checkReq := httptest.NewRequest("GET", "https://nas.example/api/status", nil)
	checkReq.AddCookie(cookie)
	gateAfterRestart.Wrap(protected).ServeHTTP(check, checkReq)
	if check.Code != 204 {
		t.Fatalf("session did not survive gate recreation: %d", check.Code)
	}
	logout := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodPost, "https://nas.example/api/logout", nil)
	logoutReq.AddCookie(cookie)
	logoutReq.Header.Set("Origin", "https://nas.example")
	gateAfterRestart.Wrap(http.HandlerFunc(gateAfterRestart.HandleLogout)).ServeHTTP(logout, logoutReq)
	if logout.Code != http.StatusOK || len(logout.Result().Cookies()) != 1 || logout.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout did not clear cookie: %d %#v", logout.Code, logout.Result().Cookies())
	}
	replay := httptest.NewRecorder()
	replayReq := httptest.NewRequest("GET", "https://nas.example/api/status", nil)
	replayReq.AddCookie(cookie)
	gateAfterRestart.Wrap(protected).ServeHTTP(replay, replayReq)
	if replay.Code != 401 {
		t.Fatalf("revoked cookie replay status=%d", replay.Code)
	}
}

func TestLoginRequiresSameOriginAndBoundsFailedAttempts(t *testing.T) {
	store, err := taskstore.Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gate, _ := New("a-strong-preview-admin-password")
	gate.SetSessionStore(store)
	for attempt := 0; attempt < 10; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "http://nas.example/api/login", strings.NewReader(`{"password":"wrong-password-value"}`))
		req.Header.Set("Origin", "http://nas.example")
		w := httptest.NewRecorder()
		gate.HandleLogin(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("failed login %d status=%d", attempt, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "http://nas.example/api/login", strings.NewReader(`{"password":"a-strong-preview-admin-password"}`))
	req.Header.Set("Origin", "http://nas.example")
	w := httptest.NewRecorder()
	gate.HandleLogin(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit status=%d", w.Code)
	}
	badOrigin := httptest.NewRequest(http.MethodPost, "http://nas.example/api/login", strings.NewReader(`{"password":"wrong-password-value"}`))
	badOrigin.Header.Set("Origin", "http://evil.example")
	blocked := httptest.NewRecorder()
	gate.HandleLogin(blocked, badOrigin)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login status=%d", blocked.Code)
	}
}
