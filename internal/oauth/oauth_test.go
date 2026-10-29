package oauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type oauthTransport func(*http.Request) (*http.Response, error)

func (f oauthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func replaceTransport(t *testing.T, fn oauthTransport) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = fn
	t.Cleanup(func() { http.DefaultTransport = old })
}
func response(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestRefreshValidatesTokenAndDoesNotExposeProviderBody(t *testing.T) {
	replaceTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("refresh_token") != "private-refresh" {
			t.Error("wrong refresh token")
		}
		return response(`{"error":"invalid_grant","error_description":"private-refresh private-secret"}`, 400), nil
	})
	_, err := RefreshTokenContext(context.Background(), "app", "private-secret", "private-refresh")
	var provider *ProviderError
	if !errors.As(err, &provider) || provider.Code != "invalid_grant" || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe/wrong error: %v", err)
	}
}

func TestRefreshCancellationHasNoCredentialURL(t *testing.T) {
	started := make(chan struct{})
	replaceTransport(t, func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := RefreshTokenContext(ctx, "app", "private-secret", "private-refresh"); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "private") {
			t.Fatalf("cancel cause/url: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh request did not cancel")
	}
}

func TestPollSlowDownDoesNotPollAgainTooSoon(t *testing.T) {
	var calls atomic.Int32
	replaceTransport(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(`{"error":"slow_down"}`, 400), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_, err := PollForTokenContext(ctx, "app", "secret", "code", 1, 60)
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}

func TestInvalidSuccessCannotBecomeCredentials(t *testing.T) {
	for _, body := range []string{`{}`, `{"access_token":"a","expires_in":0}`, `{"access_token":"a","expires_in":-1}`} {
		replaceTransport(t, func(r *http.Request) (*http.Response, error) { return response(body, 200), nil })
		if _, err := RefreshTokenContext(context.Background(), "app", "secret", "refresh"); err == nil {
			t.Fatalf("accepted invalid token: %s", body)
		}
	}
}

func TestPendingAuthorizationWaitsThenAcceptsToken(t *testing.T) {
	var calls int
	var first time.Time
	replaceTransport(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			first = time.Now()
			return response(`{"error":"authorization_pending"}`, 400), nil
		}
		if time.Since(first) < 900*time.Millisecond {
			t.Error("authorization_pending bypassed polling interval")
		}
		return response(`{"access_token":"private-access","refresh_token":"private-refresh","expires_in":3600}`, 200), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	token, err := PollForTokenContext(ctx, "app", "secret", "code", 1, 60)
	if err != nil || token == nil || token.AccessToken != "private-access" || calls != 2 {
		t.Fatalf("pending authorization failed: err=%v calls=%d", err, calls)
	}
}
