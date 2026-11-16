package intakeapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestCloudOperationHistoryErrorsAreLocalizedAndDoNotExposeBackendErrors(t *testing.T) {
	secretFailure := errors.New("sqlite unavailable; access_token=do-not-return")
	tests := []struct {
		name     string
		path     string
		want     string
		call     func(*Server, *httptest.ResponseRecorder, *http.Request)
		setError func(*fakeBackend)
	}{
		{name: "list", path: "/api/cloud/operations", want: "操作历史读取失败，请稍后重试。", setError: func(f *fakeBackend) { f.operationPageErr = secretFailure }, call: func(s *Server, w *httptest.ResponseRecorder, r *http.Request) { s.HandleCloudOperations(w, r) }},
		{name: "summary export", path: "/api/cloud/operations/export", want: "操作历史导出失败，请稍后重试。", setError: func(f *fakeBackend) { f.operationExportErr = secretFailure }, call: func(s *Server, w *httptest.ResponseRecorder, r *http.Request) { s.HandleCloudOperationExport(w, r) }},
		{name: "events", path: "/api/cloud/operations/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/events", want: "操作事件读取失败，请稍后重试。", setError: func(f *fakeBackend) { f.operationEventErr = secretFailure }, call: func(s *Server, w *httptest.ResponseRecorder, r *http.Request) {
			r.SetPathValue("id", strings.Repeat("a", 32))
			s.HandleCloudOperationEvents(w, r)
		}},
		{name: "evidence export", path: "/api/cloud/operations/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/export", want: "操作证据导出失败，请稍后重试。", setError: func(f *fakeBackend) { f.operationEvidenceExportErr = secretFailure }, call: func(s *Server, w *httptest.ResponseRecorder, r *http.Request) {
			r.SetPathValue("id", strings.Repeat("b", 32))
			s.HandleCloudOperationEvidenceExport(w, r)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := &fakeBackend{}
			test.setError(f)
			w := httptest.NewRecorder()
			test.call(New(f, nil), w, httptest.NewRequest(http.MethodGet, test.path, nil))
			if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), test.want) || strings.Contains(w.Body.String(), "do-not-return") || strings.Contains(w.Body.String(), "access_token") {
				t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
			}
		})
	}
}

func TestCloudOperationHistoryAPIIsReadOnlyAndValidatesPages(t *testing.T) {
	f := &fakeBackend{
		operationSummaries: []taskstore.RemoteOperationSummary{{ID: strings.Repeat("a", 32), Kind: "move", ObjectID: 55, SourcePath: "/source", DestinationPath: "/queue/source", Status: "unknown", CreatedAtUTC: time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)}},
		operationTotal:     12,
	}
	server := New(f, nil)
	w := httptest.NewRecorder()
	server.HandleCloudOperations(w, httptest.NewRequest(http.MethodGet, "/api/cloud/operations?status=unknown&kind=move&q=source&from=2026-10-30T01%3A00%3A00%2B08%3A00&to=2026-10-30T03%3A00%3A00Z&offset=10&limit=2", nil))
	wantFrom := time.Date(2026, 10, 29, 17, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 10, 30, 3, 0, 0, 0, time.UTC)
	if w.Code != http.StatusOK || f.operationFilter.Status != "unknown" || f.operationFilter.Kind != "move" || f.operationFilter.Query != "source" || f.operationFilter.FromUTC == nil || !f.operationFilter.FromUTC.Equal(wantFrom) || f.operationFilter.ToUTC == nil || !f.operationFilter.ToUTC.Equal(wantTo) || f.operationOffset != 10 || f.operationLimit != 2 || f.cloudCalls != 0 || f.mutationCalls != 0 {
		t.Fatalf("status=%d backend=%+v provider reads=%d writes=%d body=%s", w.Code, f, f.cloudCalls, f.mutationCalls, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "\"next_offset\":11") || strings.Contains(w.Body.String(), "\"account_fingerprint\"") || !strings.Contains(w.Body.String(), "\"source_path\":\"/source\"") {
		t.Fatalf("unexpected list response: %s", w.Body.String())
	}
	for _, target := range []string{"/api/cloud/operations?status=retry", "/api/cloud/operations?kind=copy", "/api/cloud/operations?offset=-1", "/api/cloud/operations?limit=51"} {
		w = httptest.NewRecorder()
		server.HandleCloudOperations(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid query %q status=%d body=%s", target, w.Code, w.Body.String())
		}
	}
	w = httptest.NewRecorder()
	server.HandleCloudOperations(w, httptest.NewRequest(http.MethodPost, "/api/cloud/operations", nil))
	if w.Code != http.StatusMethodNotAllowed || f.operationCalls != 1 {
		t.Fatalf("unexpected method status=%d list calls=%d", w.Code, f.operationCalls)
	}
}

func TestCloudOperationHistoryCursorPageAndValidation(t *testing.T) {
	created := time.Date(2026, 10, 30, 1, 2, 3, 456789000, time.UTC)
	f := &fakeBackend{
		operationSummaries:  []taskstore.RemoteOperationSummary{{ID: strings.Repeat("b", 32), Kind: "move", Status: "unknown", CreatedAtUTC: created}},
		operationTotal:      21,
		operationNextCursor: &taskstore.RemoteOperationCursor{CreatedAtUTC: created, ID: strings.Repeat("b", 32)},
	}
	server := New(f, nil)
	w := httptest.NewRecorder()
	server.HandleCloudOperations(w, httptest.NewRequest(http.MethodGet, "/api/cloud/operations?status=unknown&limit=1", nil))
	if w.Code != http.StatusOK || f.operationCalls != 1 || f.operationCursor != nil || f.operationLimit != 1 || !strings.Contains(w.Body.String(), `"next_cursor":{"created_at_utc":"2026-10-30T01:02:03.456789Z","operation_id":"`+strings.Repeat("b", 32)+`"}`) {
		t.Fatalf("first cursor page status=%d backend=%+v body=%s", w.Code, f, w.Body.String())
	}
	createdValue := url.QueryEscape(created.Format(time.RFC3339Nano))
	target := "/api/cloud/operations?status=unknown&limit=2&before_created_at_utc=" + createdValue + "&before_operation_id=" + strings.Repeat("b", 32)
	w = httptest.NewRecorder()
	server.HandleCloudOperations(w, httptest.NewRequest(http.MethodGet, target, nil))
	if w.Code != http.StatusOK || f.operationCalls != 2 || f.operationCursor == nil || !f.operationCursor.CreatedAtUTC.Equal(created) || f.operationCursor.ID != strings.Repeat("b", 32) || f.operationLimit != 2 || f.operationFilter.Status != "unknown" {
		t.Fatalf("cursor page status=%d backend=%+v body=%s", w.Code, f, w.Body.String())
	}
	for _, invalid := range []string{
		"?before_created_at_utc=2026-10-30T01%3A02%3A03Z",
		"?before_operation_id=" + strings.Repeat("b", 32),
		"?before_created_at_utc=bad&before_operation_id=" + strings.Repeat("b", 32),
		"?before_created_at_utc=2026-10-30T01%3A02%3A03Z&before_created_at_utc=2026-10-30T01%3A02%3A03Z&before_operation_id=" + strings.Repeat("b", 32),
		"?offset=0&before_created_at_utc=2026-10-30T01%3A02%3A03Z&before_operation_id=" + strings.Repeat("b", 32),
	} {
		w = httptest.NewRecorder()
		server.HandleCloudOperations(w, httptest.NewRequest(http.MethodGet, "/api/cloud/operations"+invalid, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid cursor query %q status=%d body=%s", invalid, w.Code, w.Body.String())
		}
	}
	if f.operationCalls != 2 {
		t.Fatalf("invalid cursor reached backend: calls=%d", f.operationCalls)
	}
}

func TestCloudOperationHistoryRejectsInvalidSearchFilters(t *testing.T) {
	f := &fakeBackend{}
	server := New(f, nil)
	targets := []string{
		"/api/cloud/operations?q=" + url.QueryEscape(strings.Repeat("界", 129)),
		"/api/cloud/operations?from=not-a-time",
		"/api/cloud/operations?from=9999-01-01T00%3A00%3A00Z",
		"/api/cloud/operations?from=2026-10-31T00%3A00%3A00Z&to=2026-10-30T00%3A00%3A00Z",
	}
	for _, target := range targets {
		w := httptest.NewRecorder()
		server.HandleCloudOperations(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid filter %q status=%d body=%s", target, w.Code, w.Body.String())
		}
	}
	if f.operationCalls != 0 || f.cloudCalls != 0 || f.mutationCalls != 0 {
		t.Fatalf("invalid filters reached backend or provider: operations=%d cloud=%d mutations=%d", f.operationCalls, f.cloudCalls, f.mutationCalls)
	}
}

func TestCloudOperationHistoryExportReturnsFilteredAttachmentAndRejectsTooLarge(t *testing.T) {
	f := &fakeBackend{operationExports: []taskstore.RemoteOperationSummary{{ID: strings.Repeat("a", 32), Kind: "move", ObjectID: 55, SourcePath: "/source", DestinationPath: "/target", Status: "confirmed", CreatedAtUTC: time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)}}}
	server := New(f, nil)
	w := httptest.NewRecorder()
	server.HandleCloudOperationExport(w, httptest.NewRequest(http.MethodGet, "/api/cloud/operations/export?status=confirmed&kind=rename&q=source&from=2026-10-30T10%3A00%3A00%2B08%3A00&to=2026-10-30T02%3A00%3A00Z", nil))
	wantFrom := time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)
	if w.Code != http.StatusOK || f.operationExportCalls != 1 || f.operationExportFilter.Status != "confirmed" || f.operationExportFilter.Kind != "rename" || f.operationExportFilter.Query != "source" || f.operationExportFilter.FromUTC == nil || !f.operationExportFilter.FromUTC.Equal(wantFrom) || f.operationExportFilter.ToUTC == nil || !f.operationExportFilter.ToUTC.Equal(wantFrom) {
		t.Fatalf("status=%d backend=%+v body=%s", w.Code, f, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "application/json") || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"total":1`) || !strings.Contains(w.Body.String(), `"kind":"rename"`) || strings.Contains(w.Body.String(), "account_fingerprint") || strings.Contains(w.Body.String(), "evidence") {
		t.Fatalf("unexpected export response headers=%v body=%s", w.Header(), w.Body.String())
	}
	f.operationExportErr = taskstore.ErrRemoteOperationExportTooLarge
	w = httptest.NewRecorder()
	server.HandleCloudOperationExport(w, httptest.NewRequest(http.MethodGet, "/api/cloud/operations/export", nil))
	if w.Code != http.StatusRequestEntityTooLarge || strings.Contains(w.Body.String(), "source_path") {
		t.Fatalf("oversize export response status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleCloudOperationExport(w, httptest.NewRequest(http.MethodPost, "/api/cloud/operations/export", nil))
	if w.Code != http.StatusMethodNotAllowed || f.operationExportCalls != 2 {
		t.Fatalf("export method status=%d calls=%d", w.Code, f.operationExportCalls)
	}
}

func TestCloudOperationEventsMaskSecretsAndHideUnknownIDs(t *testing.T) {
	id := strings.Repeat("b", 32)
	f := &fakeBackend{operationEvents: []taskstore.RemoteOperationEvent{{Sequence: 3, Status: "unknown", RecordedAtUTC: time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC), Evidence: json.RawMessage("{\"reason\":\"recorded\",\"expected\":{\"path\":\"/file\"},\"access_token\":\"never-return-this\",\"nested\":[{\"SecretKey\":\"also-hidden\"}]}")}}}
	server := New(f, nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cloud/operations/"+id+"/events?limit=10&status=unknown&recorded_from_utc=2026-10-30&recorded_through_utc=2026-10-30", nil)
	req.SetPathValue("id", id)
	server.HandleCloudOperationEvents(w, req)
	if w.Code != http.StatusOK || f.operationEventID != id || f.operationEventStatus != "unknown" || f.operationEventFilter.RecordedFromUTC == nil || f.operationEventFilter.RecordedBeforeUTC == nil || f.operationBefore != 0 || f.operationEventLimit != 10 || f.cloudCalls != 0 || f.mutationCalls != 0 {
		t.Fatalf("status=%d backend=%+v provider reads=%d writes=%d body=%s", w.Code, f, f.cloudCalls, f.mutationCalls, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "never-return-this") || strings.Contains(w.Body.String(), "also-hidden") || !strings.Contains(w.Body.String(), "[REDACTED]") || !strings.Contains(w.Body.String(), "\"path\":\"/file\"") {
		t.Fatalf("sensitive evidence was not safely rendered: %s", w.Body.String())
	}
	f.operationEventErr = taskstore.ErrRemoteOperationNotFound
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/cloud/operations/"+strings.Repeat("c", 32)+"/events", nil)
	req.SetPathValue("id", strings.Repeat("c", 32))
	server.HandleCloudOperationEvents(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown operation status=%d body=%s", w.Code, w.Body.String())
	}
	for _, invalidQuery := range []string{"status=bad", "status=unknown&status=failed", "limit=10&limit=20", "recorded_from_utc=2026-02-30", "recorded_through_utc=2026-10-29&recorded_from_utc=2026-10-30", "recorded_from_utc=2026-10-30&recorded_from_utc=2026-10-31"} {
		w = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/cloud/operations/"+id+"/events?"+invalidQuery, nil)
		req.SetPathValue("id", id)
		server.HandleCloudOperationEvents(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("query %q status=%d body=%s", invalidQuery, w.Code, w.Body.String())
		}
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/cloud/operations/"+id+"/events", nil)
	req.SetPathValue("id", id)
	server.HandleCloudOperationEvents(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected event method status=%d", w.Code)
	}
}

func TestCloudOperationFilteredEvidenceExportUsesUTCFilterAndRedacts(t *testing.T) {
	id := strings.Repeat("a", 32)
	f := &fakeBackend{operationEvidenceExport: taskstore.RemoteOperationEvidenceBundle{Operation: taskstore.RemoteOperationSummary{ID: id, Kind: "move", ObjectID: 7, Status: "confirmed", CreatedAtUTC: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)}, Events: []taskstore.RemoteOperationEvent{{Sequence: 4, Status: "unknown", RecordedAtUTC: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), Evidence: json.RawMessage(`{"nested":{"access_token":"hidden"},"reason":"safe"}`)}}}}
	server := New(f, nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cloud/operations/"+id+"/events/export?status=unknown&recorded_from_utc=2026-10-30&recorded_through_utc=2026-10-30", nil)
	req.SetPathValue("id", id)
	server.HandleCloudOperationFilteredEvidenceExport(w, req)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), "remote-operation-events-") || f.operationEvidenceExportID != id || f.operationEventFilter.Status != "unknown" || f.operationEventFilter.RecordedBeforeUTC == nil {
		t.Fatalf("status=%d headers=%v backend=%+v body=%s", w.Code, w.Header(), f, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "hidden") || !strings.Contains(w.Body.String(), "[REDACTED]") || !strings.Contains(w.Body.String(), `"recorded_through_utc":"2026-10-30"`) {
		t.Fatalf("filtered export not safely represented: %s", w.Body.String())
	}
	f.operationEvidenceExportErr = taskstore.ErrRemoteOperationEvidenceExportTooLarge
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/cloud/operations/"+id+"/events/export?recorded_through_utc=2026-10-30", nil)
	req.SetPathValue("id", id)
	server.HandleCloudOperationFilteredEvidenceExport(w, req)
	if w.Code != http.StatusRequestEntityTooLarge || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("oversize status=%d headers=%v", w.Code, w.Header())
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/cloud/operations/"+id+"/events/export?recorded_from_utc=2262-04-11&recorded_through_utc=2262-04-11", nil)
	req.SetPathValue("id", id)
	server.HandleCloudOperationFilteredEvidenceExport(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("out-of-range date status=%d", w.Code)
	}
}

func TestCloudOperationEvidenceExportMasksSecretsAndRejectsOversize(t *testing.T) {
	id := strings.Repeat("d", 32)
	f := &fakeBackend{operationEvidenceExport: taskstore.RemoteOperationEvidenceBundle{
		Operation: taskstore.RemoteOperationSummary{ID: id, Kind: "move", ObjectID: 42, SourcePath: "/source", DestinationPath: "/target", Status: "unknown", CreatedAtUTC: time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)},
		Events:    []taskstore.RemoteOperationEvent{{Sequence: 1, Status: "unknown", RecordedAtUTC: time.Date(2026, 10, 30, 2, 1, 0, 0, time.UTC), Evidence: json.RawMessage(`{"access_token":"never-return-this","nested":[{"SecretKey":"also-hidden"}],"reason":"safe"}`)}},
	}}
	server := New(f, nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cloud/operations/"+id+"/export", nil)
	req.SetPathValue("id", id)
	server.HandleCloudOperationEvidenceExport(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") || f.operationEvidenceExportCalls != 1 || f.operationEvidenceExportID != id || f.cloudCalls != 0 || f.mutationCalls != 0 {
		t.Fatalf("status=%d backend=%+v response=%s", w.Code, f, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "never-return-this") || strings.Contains(w.Body.String(), "also-hidden") || strings.Contains(w.Body.String(), "private-account-fingerprint") || !strings.Contains(w.Body.String(), "[REDACTED]") || !strings.Contains(w.Body.String(), `"sequence":1`) {
		t.Fatalf("evidence export did not redact/retain event detail: %s", w.Body.String())
	}
	f.operationEvidenceExportErr = taskstore.ErrRemoteOperationEvidenceExportTooLarge
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/cloud/operations/"+id+"/export", nil)
	req.SetPathValue("id", id)
	server.HandleCloudOperationEvidenceExport(w, req)
	if w.Code != http.StatusRequestEntityTooLarge || strings.Contains(w.Body.String(), "never-return-this") {
		t.Fatalf("oversize response=%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/cloud/operations/"+id+"/export", nil)
	req.SetPathValue("id", id)
	server.HandleCloudOperationEvidenceExport(w, req)
	if w.Code != http.StatusMethodNotAllowed || f.operationEvidenceExportCalls != 2 {
		t.Fatalf("unexpected evidence export method status=%d calls=%d", w.Code, f.operationEvidenceExportCalls)
	}
}
