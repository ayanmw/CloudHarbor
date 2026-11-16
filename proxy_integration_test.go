package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ayanmw/CloudHarbor/internal/authorization"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/config"
	"github.com/ayanmw/CloudHarbor/internal/credentials"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
	"github.com/ayanmw/CloudHarbor/internal/management"
	"github.com/ayanmw/CloudHarbor/internal/webui"
)

func TestTLSProxyManagementAndWebSocket(t *testing.T) {
	var proxy *httputil.ReverseProxy
	ready := make(chan struct{})
	frontend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-ready
		proxy.ServeHTTP(w, r)
	}))
	defer frontend.Close()
	cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/preview", PublicOrigin: frontend.URL}
	var err error
	dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
	gate, err := management.NewWithOrigin("private-proxy-fixture-password", cfg.PublicOrigin)
	if err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(applicationHandler(gate, authorization.New(dl.Context(), "", "", auth, nil)))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	proxy = httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.Host = target.Host // Exercise proxies that also rewrite the backend Host.
	}
	close(ready)
	for _, tc := range []struct {
		origin string
		auth   bool
		want   int
	}{
		{frontend.URL, false, 401},
		{frontend.URL, true, 200},
		{"https://evil.example", true, 403},
	} {
		r, _ := http.NewRequest(http.MethodPost, frontend.URL+"/api/scan", nil)
		r.Header.Set("Origin", tc.origin)
		if tc.auth {
			r.Header.Set("Authorization", "Bearer private-proxy-fixture-password")
		}
		response, err := frontend.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != tc.want {
			t.Fatalf("TLS proxy origin %q auth=%v: status=%d want=%d", tc.origin, tc.auth, response.StatusCode, tc.want)
		}
	}
	pool := x509.NewCertPool()
	pool.AddCert(frontend.Certificate())
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{RootCAs: pool}, HandshakeTimeout: 3 * time.Second}
	headers := http.Header{"Authorization": {"Bearer private-proxy-fixture-password"}, "Origin": {frontend.URL}}
	conn, response, err := dialer.Dial("wss"+strings.TrimPrefix(frontend.URL, "https")+"/ws", headers)
	if err != nil {
		t.Fatalf("WSS through TLS termination failed: %v %v", err, response)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, body, err := conn.ReadMessage()
	conn.Close()
	if err != nil || !strings.Contains(string(body), `"type":"status"`) {
		t.Fatalf("WSS status missing: %v %s", err, body)
	}
	headers.Set("Origin", "https://evil.example")
	conn, response, err = dialer.Dial("wss"+strings.TrimPrefix(frontend.URL, "https")+"/ws", headers)
	if conn != nil {
		conn.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin WSS accepted: %v %v", err, response)
	}
	response.Body.Close()
}
