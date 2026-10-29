package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/credentials"
	"web_browser_baidu_onekey_upload/internal/oauth"
)

type renewalFixture struct{ refreshes atomic.Int32 }

func (p *renewalFixture) Refresh(_ context.Context, key, secret, refresh string) (*oauth.TokenResponse, error) {
	p.refreshes.Add(1)
	if key != "app" || secret != "fixture-secret" || refresh != "old-refresh" {
		return nil, fmt.Errorf("unexpected renewal input")
	}
	return &oauth.TokenResponse{AccessToken: "new", RefreshToken: "new-refresh", ExpiresIn: 3600}, nil
}

func (*renewalFixture) Account(_ context.Context, token, _ string) (string, error) {
	if token != "old" && token != "new" {
		return "", fmt.Errorf("unexpected account credential")
	}
	return "123", nil
}

func TestRenewalAndRejectedDownloadRecoverThroughRealManager(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejection_triggers_renewal", true: "late_rejection_after_rotation"}[late], func(t *testing.T) {
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			var requests atomic.Int32
			var wrongCredential atomic.Bool
			original, cloud := setup(t, func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if n == 1 {
					if r.URL.Query().Get("access_token") != "old" {
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
				if r.URL.Query().Get("access_token") != "new" {
					wrongCredential.Store(true)
				}
				io.WriteString(w, "abcdef")
			})
			base := http.DefaultTransport
			http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("method") == "filemanager" && r.URL.Query().Get("access_token") != "new" {
					wrongCredential.Store(true)
				}
				return base.RoundTrip(r)
			})
			cloud.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 6)}
			d, err := OpenDownloader(original.cfg, original.getClient())
			if err != nil {
				t.Fatal(err)
			}
			defer shutdown(t, d)
			d.SetAuthentication(baiduapi.NewClient("", "app"), false, time.Time{})
			provider := &renewalFixture{}
			manager, err := credentials.Open(d, credentials.Options{AppKey: "app", SecretKey: "fixture-secret"}, provider, func(update credentials.Update) {
				d.SetAuthentication(baiduapi.NewClient(update.AccessToken, "app"), update.Ready, update.ExpiresAt)
			})
			if err != nil {
				t.Fatal(err)
			}
			d.SetAuthenticationFailureHandler(manager.NotifyRejected)
			if err = manager.Apply(d.Context(), &oauth.TokenResponse{AccessToken: "old", RefreshToken: "old-refresh", ExpiresIn: 3600}); err != nil {
				t.Fatal(err)
			}
			if err = d.StartBackground(manager.Run); err != nil {
				t.Fatal(err)
			}
			if err = d.ScanAndDownload(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("old-credential download did not start")
			}
			if late {
				if err = manager.Refresh(d.Context()); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			waitForStatus(t, d, 1, "auth_wait")
			deadline := time.Now().Add(3 * time.Second)
			for !manager.Status().Ready || d.getClient().Fingerprint() != baiduapi.NewClient("new", "app").Fingerprint() {
				if time.Now().After(deadline) {
					t.Fatalf("renewal did not restore new admission: %+v", manager.Status())
				}
				time.Sleep(5 * time.Millisecond)
			}
			// Observe beyond the worker's 100 ms polling interval before granting
			// fresh scan eligibility; checking only at publication misses late claims.
			time.Sleep(150 * time.Millisecond)
			d.queue.mu.Lock()
			failures := d.queue.jobs[1].Failures
			_, eligible := d.queue.eligible[1]
			d.queue.mu.Unlock()
			if failures != 0 || eligible || !d.AuthenticationReady() || requests.Load() != 1 || provider.refreshes.Load() != 1 {
				t.Fatal("credential rejection consumed budget, bypassed scan, or revoked the new token")
			}
			cloud.mu.Lock()
			actionsBeforeScan := len(cloud.actions)
			cloud.mu.Unlock()
			if actionsBeforeScan != 0 {
				t.Fatal("source processed before fresh scan and successful download")
			}
			if err = d.ScanAndDownload(); err != nil {
				t.Fatal(err)
			}
			waitForStatus(t, d, 1, "done")
			body, err := d.LoadCredentials()
			var record credentials.Record
			if err != nil || json.Unmarshal(body, &record) != nil || record.AccessToken != "new" || record.RefreshToken != "new-refresh" || record.AccountID != "123" || record.Generation != 2 {
				t.Fatal("rotated credentials were not persisted with the bound account")
			}
			shutdown(t, d)
			content, err := os.ReadFile(filepath.Join(d.cfg.LocalDownDir, "a.txt"))
			if err != nil || string(content) != "abcdef" || len(cloud.actions) != 1 || requests.Load() != 2 || wrongCredential.Load() {
				t.Fatalf("renewed download did not complete exactly once: content=%q err=%v requests=%d actions=%v", content, err, requests.Load(), cloud.actions)
			}
		})
	}
}
