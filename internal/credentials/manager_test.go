package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/oauth"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type provider struct {
	refresh func(context.Context, string, string, string) (*oauth.TokenResponse, error)
	account func(context.Context, string, string) (string, error)
}

func (p provider) Refresh(ctx context.Context, key, secret, token string) (*oauth.TokenResponse, error) {
	return p.refresh(ctx, key, secret, token)
}
func (p provider) Account(ctx context.Context, token, key string) (string, error) {
	return p.account(ctx, token, key)
}
func newStore(t *testing.T) *taskstore.Store {
	t.Helper()
	s, err := taskstore.Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestApplyPersistsBeforePublishingAndRejectsAccountSwitch(t *testing.T) {
	store := newStore(t)
	p := provider{account: func(ctx context.Context, token, key string) (string, error) {
		if token == "other-access" {
			return "999", nil
		}
		return "123", nil
	}}
	var applied []string
	manager, err := Open(store, Options{AppKey: "app", SecretKey: "private-secret"}, p, func(update Update) {
		if !update.Ready {
			return
		}
		body, readErr := store.LoadCredentials()
		if readErr != nil || !strings.Contains(string(body), update.AccessToken) {
			t.Error("published before durable credentials")
		}
		applied = append(applied, update.AccessToken)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	if err = manager.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "other-access", RefreshToken: "other-refresh", ExpiresIn: 3600}); !errors.Is(err, taskstore.ErrAccountMismatch) {
		t.Fatalf("account switch accepted: %v", err)
	}
	body, _ := store.LoadCredentials()
	if strings.Contains(string(body), "other-access") || strings.Contains(string(body), "private-secret") || len(applied) == 0 || applied[len(applied)-1] != "new-access" {
		t.Fatalf("wrong credential persistence/application: %s %v", body, applied)
	}
	status, _ := json.Marshal(manager.Status())
	if strings.Contains(string(status), "new-access") || strings.Contains(string(status), "new-refresh") {
		t.Fatalf("status leaked secrets: %s", status)
	}
}

func TestSavedRotationWinsOverStaleEnvironmentAndRefreshesOnlyOnce(t *testing.T) {
	store := newStore(t)
	var refreshes atomic.Int32
	p := provider{account: func(context.Context, string, string) (string, error) { return "123", nil }, refresh: func(ctx context.Context, key, secret, token string) (*oauth.TokenResponse, error) {
		refreshes.Add(1)
		return &oauth.TokenResponse{AccessToken: "rotated-access", RefreshToken: "rotated-refresh", ExpiresIn: 3600}, nil
	}}
	manager, err := Open(store, Options{AppKey: "app", SecretKey: "secret", AccessToken: "old-access", RefreshToken: "old-refresh"}, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := manager.Ensure(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if refreshes.Load() != 1 {
		t.Fatalf("parallel renewal duplicated: %d", refreshes.Load())
	}
	var active string
	manager, err = Open(store, Options{AppKey: "app", SecretKey: "secret", AccessToken: "stale-environment"}, p, func(update Update) {
		if update.Ready {
			active = update.AccessToken
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if active != "rotated-access" || refreshes.Load() != 1 {
		t.Fatalf("restart reverted credentials: %q refreshes=%d", active, refreshes.Load())
	}
}

func TestRotationSurvivesTemporaryAccountLookupFailure(t *testing.T) {
	store := newStore(t)
	var unavailable atomic.Bool
	unavailable.Store(true)
	p := provider{refresh: func(context.Context, string, string, string) (*oauth.TokenResponse, error) {
		return &oauth.TokenResponse{AccessToken: "rotated-access", RefreshToken: "rotated-refresh", ExpiresIn: 3600}, nil
	}, account: func(context.Context, string, string) (string, error) {
		if unavailable.Load() {
			return "", context.DeadlineExceeded
		}
		return "123", nil
	}}
	manager, err := Open(store, Options{AppKey: "app", SecretKey: "secret", AccessToken: "old-access", RefreshToken: "old-refresh"}, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Ensure(context.Background()); err == nil {
		t.Fatal("lookup failure ignored")
	}
	body, _ := store.LoadCredentials()
	if !strings.Contains(string(body), "rotated-refresh") || manager.Status().Ready {
		t.Fatalf("rotation lost/applied without verification: %s %+v", body, manager.Status())
	}
	unavailable.Store(false)
	manager, err = Open(store, Options{AppKey: "app", SecretKey: "secret"}, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Ensure(context.Background()); err != nil || !manager.Status().Ready {
		t.Fatalf("persisted rotation did not recover: %v", err)
	}
}

func TestStorageFailureDoesNotApplyNewCredentials(t *testing.T) {
	store := newStore(t)
	published := false
	p := provider{account: func(context.Context, string, string) (string, error) { return "123", nil }}
	manager, err := Open(store, Options{AppKey: "app"}, p, func(update Update) { published = update.Ready })
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err = manager.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "new-access", ExpiresIn: 3600}); err == nil || published {
		t.Fatalf("applied unsaved credentials: %v", err)
	}
}

func TestCancelledRenewalAndUnknownExpiryAreExplicit(t *testing.T) {
	p := provider{account: func(context.Context, string, string) (string, error) { return "123", nil }}
	manager, err := Open(newStore(t), Options{AccessToken: "manual-access"}, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := manager.Status(); !status.Ready || status.State != "expiry_unknown" {
		t.Fatalf("invented expiry: %+v", status)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = manager.Ensure(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission: %v", err)
	}
}

func TestRejectedManualCandidateDoesNotDisableWorkingCredential(t *testing.T) {
	p := provider{account: func(ctx context.Context, token, key string) (string, error) {
		if token == "bad-candidate" {
			return "", &baiduapi.APIError{Code: 111}
		}
		return "123", nil
	}}
	manager, err := Open(newStore(t), Options{}, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "working-access", ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	if err = manager.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "bad-candidate", ExpiresIn: 3600}); err == nil {
		t.Fatal("bad candidate accepted")
	}
	if err = manager.Ensure(context.Background()); err != nil || !manager.Status().Ready {
		t.Fatalf("bad candidate disabled previous token: %v %+v", err, manager.Status())
	}
}

func TestBackgroundRecoversRejectedSavedToken(t *testing.T) {
	store := newStore(t)
	now := time.Now()
	saved := Record{Version: 1, AppKey: "app", AccessToken: "rejected-access", RefreshToken: "working-refresh", AccountID: "123", IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	body, _ := json.Marshal(saved)
	if err := store.SaveCredentials(body); err != nil {
		t.Fatal(err)
	}
	p := provider{account: func(ctx context.Context, token, key string) (string, error) {
		if token == "rejected-access" {
			return "", &baiduapi.APIError{Code: 111}
		}
		return "123", nil
	}, refresh: func(context.Context, string, string, string) (*oauth.TokenResponse, error) {
		return &oauth.TokenResponse{AccessToken: "renewed-access", RefreshToken: "renewed-refresh", ExpiresIn: 3600}, nil
	}}
	ready := make(chan struct{}, 1)
	manager, err := Open(store, Options{AppKey: "app", SecretKey: "secret"}, p, func(update Update) {
		if update.Ready {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); manager.Run(ctx) }()
	select {
	case <-ready:
		cancel()
		<-done
	case <-ctx.Done():
		<-done
		t.Fatalf("rejected saved credential never renewed: %+v", manager.Status())
	}
}

func TestInFlightCancelledRefreshPreservesFutureRenewal(t *testing.T) {
	started := make(chan struct{})
	p := provider{account: func(context.Context, string, string) (string, error) { return "123", nil }, refresh: func(ctx context.Context, _, _, _ string) (*oauth.TokenResponse, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	m, err := Open(newStore(t), Options{AppKey: "app", SecretKey: "secret"}, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "working", RefreshToken: "refresh", ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.Refresh(ctx) }()
	<-started
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = m.Ensure(context.Background()); err != nil || !m.Status().Ready {
		t.Fatalf("cancel permanently blocked healthy credentials: %v %+v", err, m.Status())
	}
}

type bindingFailureStore struct {
	Store
	fail bool
}

func (s *bindingFailureStore) BindAccount(id string) error {
	if s.fail {
		s.fail = false
		return errors.New("temporary storage failure")
	}
	return s.Store.BindAccount(id)
}

func TestBindingFailureRetainsManualCandidate(t *testing.T) {
	store := &bindingFailureStore{Store: newStore(t), fail: true}
	p := provider{account: func(context.Context, string, string) (string, error) { return "123", nil }}
	m, err := Open(store, Options{}, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "candidate", ExpiresIn: 3600}); err == nil {
		t.Fatal("binding failure ignored")
	}
	// Skip the already tested wall-clock backoff to exercise its retry outcome.
	m.mu.Lock()
	m.nextAttempt = time.Time{}
	m.mu.Unlock()
	if err = m.Ensure(context.Background()); err != nil || !m.Status().Ready {
		t.Fatalf("candidate lost on storage recovery: %v %+v", err, m.Status())
	}
	body, _ := store.LoadCredentials()
	if !strings.Contains(string(body), "candidate") {
		t.Fatal("recovery did not persist candidate")
	}
}

func TestBackgroundRevokesExpiredCredentialDuringRefreshBackoff(t *testing.T) {
	var refreshes atomic.Int32
	updates := make(chan Update, 16)
	p := provider{account: func(context.Context, string, string) (string, error) { return "123", nil }, refresh: func(context.Context, string, string, string) (*oauth.TokenResponse, error) {
		refreshes.Add(1)
		return nil, context.DeadlineExceeded
	}}
	m, err := Open(newStore(t), Options{AppKey: "app", SecretKey: "secret"}, p, func(u Update) { updates <- u })
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "short-lived", RefreshToken: "refresh", ExpiresIn: 1}); err != nil {
		t.Fatal(err)
	}
	if err = m.Refresh(context.Background()); err == nil {
		t.Fatal("refresh failure ignored")
	}
	for len(updates) > 0 {
		<-updates
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.Run(ctx) }()
	defer func() { cancel(); <-done }()
	timer := time.NewTimer(2500 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case u := <-updates:
			if !u.Ready {
				if refreshes.Load() != 1 {
					t.Fatal("expiry bypassed refresh backoff")
				}
				return
			}
		case <-timer.C:
			t.Fatal("expiry did not revoke downloader admission during backoff")
		}
	}
}

func TestManualRefreshFailureWakesSleepingBackgroundRenewal(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var refreshes atomic.Int32
	p := provider{account: func(context.Context, string, string) (string, error) { return "123", nil }, refresh: func(context.Context, string, string, string) (*oauth.TokenResponse, error) {
		refreshes.Add(1)
		if fail.Load() {
			return nil, context.DeadlineExceeded
		}
		return &oauth.TokenResponse{AccessToken: "renewed", RefreshToken: "renewed-refresh", ExpiresIn: 3600}, nil
	}}
	ready := make(chan string, 16)
	m, err := Open(newStore(t), Options{AppKey: "app", SecretKey: "secret"}, p, func(u Update) {
		if u.Ready {
			ready <- u.AccessToken
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "old", RefreshToken: "refresh", ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.Run(ctx) }()
	defer func() { cancel(); <-done }()
	// Run consumes the initial publication wake and sleeps until normal renewal.
	time.Sleep(100 * time.Millisecond)
	if err = m.Refresh(context.Background()); err == nil {
		t.Fatal("refresh failure ignored")
	}
	fail.Store(false)
	timer := time.NewTimer(7 * time.Second)
	defer timer.Stop()
	for {
		select {
		case token := <-ready:
			if token == "renewed" {
				if refreshes.Load() != 2 {
					t.Fatalf("unexpected refresh count %d", refreshes.Load())
				}
				return
			}
		case <-timer.C:
			t.Fatalf("manual failure did not reschedule background retry: calls=%d", refreshes.Load())
		}
	}
}

func TestLateOldTokenRejectionDoesNotRevokeNewCredential(t *testing.T) {
	p := provider{account: func(context.Context, string, string) (string, error) { return "123", nil }}
	m, err := Open(newStore(t), Options{}, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"old", "new"} {
		if err = m.Apply(context.Background(), &oauth.TokenResponse{AccessToken: token, ExpiresIn: 3600}); err != nil {
			t.Fatal(err)
		}
	}
	m.NotifyRejected(fingerprint("old"))
	if !m.Status().Ready {
		t.Fatal("old request revoked new credentials")
	}
	m.NotifyRejected(fingerprint("new"))
	if m.Status().Ready {
		t.Fatal("active rejection did not revoke credentials")
	}
}

func TestCancelledLookupAfterRotationRetainsSavedPair(t *testing.T) {
	store := newStore(t)
	var lookupCancelled atomic.Bool
	p := provider{refresh: func(context.Context, string, string, string) (*oauth.TokenResponse, error) {
		return &oauth.TokenResponse{AccessToken: "rotated", RefreshToken: "rotated-refresh", ExpiresIn: 3600}, nil
	}, account: func(ctx context.Context, token, _ string) (string, error) {
		if token == "rotated" && !lookupCancelled.Swap(true) {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return "123", nil
	}}
	m, err := Open(store, Options{AppKey: "app", SecretKey: "secret"}, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "old", RefreshToken: "old-refresh", ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err = m.Refresh(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lookup did not cancel: %v", err)
	}
	body, _ := store.LoadCredentials()
	if !strings.Contains(string(body), "rotated-refresh") || m.Status().Ready {
		t.Fatal("cancel lost rotation or published unchecked pair")
	}
	if err = m.Ensure(context.Background()); err != nil || !m.Status().Ready {
		t.Fatalf("rotation did not recover after cancel: %v", err)
	}
}

func TestRejectionRevokesOldCredentialDuringManualRefreshBackoff(t *testing.T) {
	p := provider{account: func(context.Context, string, string) (string, error) { return "123", nil }, refresh: func(context.Context, string, string, string) (*oauth.TokenResponse, error) {
		return nil, context.DeadlineExceeded
	}}
	m, err := Open(newStore(t), Options{AppKey: "app", SecretKey: "secret"}, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "active", RefreshToken: "refresh", ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	if err = m.Refresh(context.Background()); err == nil {
		t.Fatal("refresh failure ignored")
	}
	before := m.Status()
	if !before.Ready || before.NextAttempt == nil {
		t.Fatal("expected usable credential during backoff")
	}
	m.NotifyRejected(fingerprint("active"))
	after := m.Status()
	if after.Ready || after.NextAttempt == nil || !after.NextAttempt.Equal(*before.NextAttempt) {
		t.Fatalf("rejection failed to revoke or changed backoff: before=%+v after=%+v", before, after)
	}
}

func TestTransientRenewalFailureCannotReviveRejectedActiveToken(t *testing.T) {
	p := provider{account: func(context.Context, string, string) (string, error) { return "123", nil }, refresh: func(context.Context, string, string, string) (*oauth.TokenResponse, error) {
		return nil, context.DeadlineExceeded
	}}
	m, err := Open(newStore(t), Options{AppKey: "app", SecretKey: "secret"}, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Apply(context.Background(), &oauth.TokenResponse{AccessToken: "rejected", RefreshToken: "refresh", ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	m.NotifyRejected(fingerprint("rejected"))
	if err = m.Refresh(context.Background()); err == nil {
		t.Fatal("refresh failure ignored")
	}
	if m.Status().Ready {
		t.Fatal("transient renewal failure revived rejected credentials")
	}
}
