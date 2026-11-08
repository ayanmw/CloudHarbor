package intakeapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

type cleanupChangeBackend struct {
	*fakeBackend
	calls int
	err   error
}

func (b *cleanupChangeBackend) CloudCleanupRevision(context.Context) (string, error) {
	b.calls++
	return strings.Repeat("a", 64), b.err
}
func TestCleanupRevisionReadOnlyAndSanitized(t *testing.T) {
	b := &cleanupChangeBackend{fakeBackend: &fakeBackend{}}
	s := New(b, nil)
	w := httptest.NewRecorder()
	s.HandleCloudCleanupRevision(w, httptest.NewRequest("GET", "/api/cloud/cleanup-revision", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), strings.Repeat("a", 64)) || !strings.Contains(w.Body.String(), "observed_at_utc") {
		t.Fatalf("response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.HandleCloudCleanupRevision(w, httptest.NewRequest("POST", "/api/cloud/cleanup-revision", nil))
	if w.Code != 405 || b.calls != 1 {
		t.Fatal("non-read reached backend")
	}
	b.err = errors.New("private-token-private-path")
	w = httptest.NewRecorder()
	s.HandleCloudCleanupRevision(w, httptest.NewRequest("GET", "/api/cloud/cleanup-revision", nil))
	if w.Code != 502 || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("error leak %s", w.Body.String())
	}
}

func TestCleanupRevisionReadFailureDoesNotSuggestResubmission(t *testing.T) {
	b := &cleanupChangeBackend{fakeBackend: &fakeBackend{}, err: errors.New("private-token")}
	w := httptest.NewRecorder()
	New(b, nil).HandleCloudCleanupRevision(w, httptest.NewRequest("GET", "/api/cloud/cleanup-revision", nil))
	body := w.Body.String()
	if w.Code != 502 || !strings.Contains(body, "读取失败") || strings.Contains(body, "提交") || strings.Contains(body, "private-token") {
		t.Fatalf("misleading read failure: %d %s", w.Code, body)
	}
}
