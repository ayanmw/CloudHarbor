package baiduapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProviderErrorClassificationPreservesEnvelopeAndFingerprint(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	var body string
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	c := NewClient("private-fixture-token", "")
	for _, tc := range []struct {
		field       string
		code        int
		auth, retry bool
	}{
		{"error_code", 110, true, false}, {"error_code", 111, true, false}, {"errno", 110, true, false},
		{"errno", 20016, true, false}, {"errno", 20017, true, false}, {"errno", 31045, true, false},
		{"error_code", 20016, true, false}, {"errno", 111, false, false},
		{"errno", 31034, false, true}, {"errno", -6, false, false}, {"errno", -9, false, false},
	} {
		t.Run(fmt.Sprintf("%s_%d", tc.field, tc.code), func(t *testing.T) {
			body = fmt.Sprintf(`{"%s":%d}`, tc.field, tc.code)
			_, err := c.ListDir("/queue")
			wrapped := errors.Join(errors.New("unrelated folder failure"), fmt.Errorf("metadata: %w", err))
			fp, auth := AuthenticationFingerprint(wrapped)
			if auth != tc.auth || (auth && fp != c.Fingerprint()) {
				t.Fatalf("auth=%v fingerprint=%q error=%v", auth, fp, err)
			}
			if got := IsRetryable(err); got != tc.retry {
				t.Fatalf("retry=%v wanted %v: %v", got, tc.retry, err)
			}
			if strings.Contains(err.Error(), c.AccessToken) {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestPostprocessingItemErrnoDoesNotBecomeOAuthError(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	var body string
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	c := NewClient("fixture", "")
	for name, action := range map[string]func() error{"rename": func() error { return c.RenameFile("/a", "a.done") }, "delete": func() error { return c.DeleteFile("/a") }} {
		for _, tc := range []struct {
			code        int
			auth, retry bool
		}{{111, false, false}, {20016, true, false}, {31034, false, true}} {
			t.Run(fmt.Sprintf("%s_%d", name, tc.code), func(t *testing.T) {
				body = fmt.Sprintf(`{"errno":0,"info":[{"errno":%d}]}`, tc.code)
				err := action()
				if err == nil || IsAuthenticationError(err) != tc.auth || IsRetryable(err) != tc.retry {
					t.Fatalf("item namespace lost: %v", err)
				}
			})
		}
	}
}

func TestMoveRunningErrnoIsUncertainWithoutTaskID(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	for _, body := range []string{`{"errno":111}`, `{"errno":111,"error_code":111}`, `{"errno":0,"info":[{"errno":111}]}`, `{"errno":0,"info":[{"errno":-9},{"errno":111}]}`} {
		http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
		err := NewClient("fixture", "").MoveFile("/a", "/queue", "a")
		if !errors.Is(err, ErrMoveAsync) || IsAuthenticationError(err) {
			t.Fatalf("running move misclassified: %v", err)
		}
	}
}
