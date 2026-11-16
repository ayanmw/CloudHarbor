package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/config"
	"github.com/ayanmw/CloudHarbor/internal/credentials"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
	"github.com/ayanmw/CloudHarbor/internal/oauth"
)

type scannerTransport func(*http.Request) (*http.Response, error)

func (f scannerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestScannerAutomaticallyRecoversDownloadAfterRenewal(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "current_rejection", true: "late_rejection_after_fresh_scan"}[late], func(t *testing.T) {
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			newScanStarted := make(chan struct{}, 1)
			permitNewScan := make(chan struct{})
			var requests, actions atomic.Int32
			var wrongCredential atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 1 {
					if r.URL.Query().Get("access_token") != "private-access" {
						wrongCredential.Store(true)
					}
					started <- struct{}{}
					select {
					case <-release:
						w.WriteHeader(http.StatusUnauthorized)
					case <-r.Context().Done():
					}
					return
				}
				if r.URL.Query().Get("access_token") != "private-renewed" {
					wrongCredential.Store(true)
				}
				io.WriteString(w, "abcdef")
			}))
			defer srv.Close()
			priorTransport := http.DefaultTransport
			defer func() { http.DefaultTransport = priorTransport }()
			http.DefaultTransport = scannerTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "pan.baidu.com" {
					return priorTransport.RoundTrip(r)
				}
				var value any
				switch r.URL.Query().Get("method") {
				case "list":
					if late && r.URL.Query().Get("access_token") == "private-renewed" {
						select {
						case newScanStarted <- struct{}{}:
						case <-r.Context().Done():
							return nil, r.Context().Err()
						}
						select {
						case <-permitNewScan:
						case <-r.Context().Done():
							return nil, r.Context().Err()
						}
					}
					value = map[string]any{"errno": 0, "list": []baiduapi.FileInfo{{FsID: 1, Path: "/preview/a.txt", ServerFilename: "a.txt", Size: 6}}}
				case "filemetas":
					if r.URL.Query().Get("dlink") == "1" {
						value = map[string]any{"errno": 0, "list": []map[string]string{{"dlink": srv.URL}}}
					} else {
						value = map[string]any{"errno": 0, "list": []baiduapi.FileInfo{{FsID: 1, Path: "/preview/a.txt", ServerFilename: "a.txt", Size: 6}}}
					}
				case "filemanager":
					if r.URL.Query().Get("access_token") != "private-renewed" {
						wrongCredential.Store(true)
					}
					actions.Add(1)
					value = map[string]any{"errno": 0, "info": []map[string]int{{"errno": 0}}}
				default:
					t.Errorf("unexpected API method %q", r.URL.Query().Get("method"))
					value = map[string]int{"errno": -1}
				}
				body, _ := json.Marshal(value)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
			})
			cfg = &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/preview", ScanInterval: 3600, MaxParallel: 1, BaiduAppKey: "app"}
			scanRequests = make(chan struct{}, 1)
			lastScanResult.Store(nil)
			var err error
			dl, err = downloader.OpenDownloader(cfg, baiduapi.NewClient("", "app"))
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
			dl.SetAuthentication(baiduapi.NewClient("", "app"), false, time.Time{})
			auth, err = credentials.Open(dl, credentials.Options{AppKey: "app", SecretKey: "fixture-secret"}, localAuthorizationProvider{}, applyCredentialUpdate)
			if err != nil {
				t.Fatal(err)
			}
			dl.SetAuthenticationFailureHandler(auth.NotifyRejected)
			if err = auth.Apply(dl.Context(), &oauth.TokenResponse{AccessToken: "private-access", RefreshToken: "fixture-refresh", ExpiresIn: 3600}); err != nil {
				t.Fatal(err)
			}
			if err = dl.StartBackground(auth.Run); err != nil {
				t.Fatal(err)
			}
			if err = dl.StartBackground(runScanner); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("initial automatic scan did not start download")
			}
			if late {
				if err = auth.Refresh(dl.Context()); err != nil {
					t.Fatal(err)
				}
				select {
				case <-newScanStarted:
				case <-time.After(3 * time.Second):
					t.Fatal("ready callback did not start a scan using the new credential")
				}
				// The scanner is serial and blocked in the new-token list request.
				// Its next published result must belong to this scan, even if the
				// initial old scan published late after the download started.
				prior := lastScanResult.Load()
				close(permitNewScan)
				deadline := time.Now().Add(3 * time.Second)
				for result := lastScanResult.Load(); result == nil || result == prior || result.Error != "" || result.Success.IsZero(); result = lastScanResult.Load() {
					if time.Now().After(deadline) {
						t.Fatal("ready callback did not complete a fresh automatic scan")
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
			close(release)
			deadline := time.Now().Add(3 * time.Second)
			for task := dl.GetTaskByFsID(1); task == nil || task.Status != "done"; task = dl.GetTaskByFsID(1) {
				if time.Now().After(deadline) {
					t.Fatalf("automatic recovery stalled: %+v", task)
				}
				time.Sleep(5 * time.Millisecond)
			}
			content, err := os.ReadFile(filepath.Join(cfg.LocalDownDir, "a.txt"))
			if err != nil || string(content) != "abcdef" || requests.Load() != 2 || actions.Load() != 1 || wrongCredential.Load() {
				t.Fatalf("automatic recovery result: content=%q err=%v requests=%d actions=%d", content, err, requests.Load(), actions.Load())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err = dl.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			if auth.Status().State != "closed" {
				t.Fatal("shutdown did not drain the credential manager")
			}
			reopened, err := downloader.OpenDownloader(cfg, baiduapi.NewClient("", "app"))
			if err != nil {
				t.Fatalf("shutdown retained the shared store lock: %v", err)
			}
			if err = reopened.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
