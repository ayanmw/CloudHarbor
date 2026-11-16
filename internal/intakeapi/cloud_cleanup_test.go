package intakeapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

type cleanupHTTPBackend struct {
	*fakeBackend
	calls int
	items []taskstore.CleanupBatchItem
	err   error
	after int64
}

func (b *cleanupHTTPBackend) CreateCloudCleanupBatch(_ context.Context, id string, items []taskstore.CleanupBatchItem) (taskstore.CleanupBatchRecord, error) {
	b.calls++
	b.items = items
	return taskstore.CleanupBatchRecord{ID: id, Status: "queued"}, b.err
}
func (b *cleanupHTTPBackend) CloudCleanupBatches(context.Context, int, int) ([]taskstore.CleanupBatchSummary, int, error) {
	return []taskstore.CleanupBatchSummary{}, 0, b.err
}
func (b *cleanupHTTPBackend) CloudCleanupBatch(context.Context, string) (taskstore.CleanupBatchRecord, error) {
	return taskstore.CleanupBatchRecord{Status: "queued"}, b.err
}
func (b *cleanupHTTPBackend) CloudCleanupItems(context.Context, string, int, int) ([]taskstore.CleanupItemRecord, int, error) {
	return []taskstore.CleanupItemRecord{}, 0, b.err
}
func (b *cleanupHTTPBackend) CloudCleanupEvents(_ context.Context, _ string, after int64, _ int) ([]taskstore.CleanupBatchEvent, bool, error) {
	b.after = after
	return []taskstore.CleanupBatchEvent{{Sequence: 9007199254740993, Kind: "created"}}, false, b.err
}
func (b *cleanupHTTPBackend) StopCloudCleanupBatch(context.Context, string) error {
	b.calls++
	return b.err
}

const cleanupJSON = `{"batch_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","items":[{"kind":"empty","expected":{"fs_id":"9007199254740993","path":"/empty","name":"empty","directory":true,"size":"0","provider_mtime":"123"},"client_preview_observed_at_utc":"2026-11-06T00:00:00Z"}]}`

func TestCleanupHTTPExactSelectionAndRejectsClientScope(t *testing.T) {
	b := &cleanupHTTPBackend{fakeBackend: &fakeBackend{}}
	s := New(b, nil)
	w := httptest.NewRecorder()
	s.HandleCloudCleanupBatches(w, httptest.NewRequest("POST", "/api/cloud/cleanup-batches", strings.NewReader(cleanupJSON)))
	if w.Code != 202 || b.calls != 1 || b.items[0].Expected.FsID != 9007199254740993 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, b.calls, w.Body.String())
	}
	for _, body := range []string{strings.Replace(cleanupJSON, `"9007199254740993"`, `9007199254740993`, 1), strings.TrimSuffix(cleanupJSON, "}") + `,"account_fingerprint":"spoof"}`, strings.Replace(cleanupJSON, `"directory":true`, `"directory":false`, 1), cleanupJSON + ` {}`} {
		w = httptest.NewRecorder()
		s.HandleCloudCleanupBatches(w, httptest.NewRequest("POST", "/api/cloud/cleanup-batches", strings.NewReader(body)))
		if w.Code != 400 || b.calls != 1 {
			t.Fatalf("accepted bad request status=%d calls=%d", w.Code, b.calls)
		}
	}
}

func TestCleanupHTTPExactEventCursorAndErrorRedaction(t *testing.T) {
	b := &cleanupHTTPBackend{fakeBackend: &fakeBackend{}}
	s := New(b, nil)
	r := httptest.NewRequest("GET", "/events?after_sequence=9007199254740992&limit=1", nil)
	r.SetPathValue("id", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	w := httptest.NewRecorder()
	s.HandleCloudCleanupEvents(w, r)
	if w.Code != 200 || b.after != 9007199254740992 || !strings.Contains(w.Body.String(), `"next_sequence":"9007199254740993"`) {
		t.Fatalf("cursor %d %s", w.Code, w.Body.String())
	}
	b.err = errors.New("access_token=must-not-leak")
	w = httptest.NewRecorder()
	s.HandleCloudCleanupBatches(w, httptest.NewRequest("POST", "/", strings.NewReader(cleanupJSON)))
	if w.Code != 502 || strings.Contains(w.Body.String(), "must-not-leak") || !strings.Contains(w.Body.String(), "相同 ID 和清单") {
		t.Fatalf("leaked error %d %s", w.Code, w.Body.String())
	}
}

func TestCleanupStopFailureDoesNotSuggestCreatingBatchAgain(t *testing.T) {
	b := &cleanupHTTPBackend{fakeBackend: &fakeBackend{}, err: errors.New("private-stop-detail")}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/stop", strings.NewReader(`{}`))
	r.SetPathValue("id", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	New(b, nil).HandleCloudCleanupStop(w, r)
	body := w.Body.String()
	if w.Code != 502 || !strings.Contains(body, "停止请求") || !strings.Contains(body, "查询") || strings.Contains(body, "清单") || strings.Contains(body, "private-stop-detail") {
		t.Fatalf("misleading stop failure: %d %s", w.Code, body)
	}
}

func TestCleanupRecordReadFailuresOnlySuggestRefreshing(t *testing.T) {
	b := &cleanupHTTPBackend{fakeBackend: &fakeBackend{}, err: errors.New("private-read-detail")}
	s := New(b, nil)
	for name, handler := range map[string]func(http.ResponseWriter, *http.Request){"batches": s.HandleCloudCleanupBatches, "batch": s.HandleCloudCleanupBatch, "items": s.HandleCloudCleanupItems, "events": s.HandleCloudCleanupEvents} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)
			r.SetPathValue("id", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			handler(w, r)
			body := w.Body.String()
			if w.Code != 502 || !strings.Contains(body, "读取失败") || strings.Contains(body, "提交") || strings.Contains(body, "private-read-detail") || b.calls != 0 {
				t.Fatalf("read response: %d %s writes=%d", w.Code, body, b.calls)
			}
		})
	}
}
