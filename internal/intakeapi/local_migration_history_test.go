package intakeapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestLocalMigrationHistoryPageValidatesAndReportsStablePageMetadata(t *testing.T) {
	id1, id2 := strings.Repeat("a", 32), strings.Repeat("b", 32)
	f := &fakeBackend{localMigrationList: []taskstore.LocalDirectoryMigration{{ID: id2, Status: "completed"}, {ID: id1, Status: "failed"}}, localMigrationListTotal: 3}
	s := New(f, nil)
	w := httptest.NewRecorder()
	s.HandleLocalDirectoryMigrations(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations?offset=2&limit=2&status=completed&q=%20%2Fold%20", nil))
	var body struct {
		Entries    []taskstore.LocalDirectoryMigration `json:"entries"`
		Total      int                                 `json:"total"`
		Offset     int                                 `json:"offset"`
		Limit      int                                 `json:"limit"`
		NextOffset *int                                `json:"next_offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || f.localMigrationListCalls != 1 || f.localMigrationListOffset != 2 || f.localMigrationListLimit != 2 || f.localMigrationFilter != (taskstore.LocalDirectoryMigrationFilter{Status: "completed", Query: "/old"}) || f.cloudCalls != 0 || f.mutationCalls != 0 || body.Total != 3 || body.Offset != 2 || body.Limit != 2 || body.NextOffset != nil || len(body.Entries) != 2 || body.Entries[0].ID != id2 {
		t.Fatalf("status=%d calls=%d args=%d/%d body=%+v", w.Code, f.localMigrationListCalls, f.localMigrationListOffset, f.localMigrationListLimit, body)
	}
	for _, query := range []string{"offset=-1", "limit=0", "limit=51", "offset=bad", "limit=bad", "status=unknown", "q=%FF", "status=failed&status=running", "q=" + strings.Repeat("x", 257)} {
		w = httptest.NewRecorder()
		s.HandleLocalDirectoryMigrations(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations?"+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("query %q status=%d body=%s", query, w.Code, w.Body.String())
		}
	}
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrations(w, httptest.NewRequest(http.MethodPost, "/api/settings/local-migrations", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("non-GET status=%d", w.Code)
	}
}

func TestLocalMigrationHistoryDateFiltersAreStrictAndSharedWithExport(t *testing.T) {
	filter := taskstore.LocalDirectoryMigrationFilter{Status: "failed", Query: `D:\old`, StartedFromUTC: "2024-02-29", StartedThroughUTC: "2024-03-01"}
	f := &fakeBackend{}
	s := New(f, nil)
	w := httptest.NewRecorder()
	s.HandleLocalDirectoryMigrations(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations?status=failed&q=D%3A%5Cold&started_from_utc=2024-02-29&started_through_utc=2024-03-01&offset=10&limit=5", nil))
	if w.Code != http.StatusOK || f.localMigrationFilter != filter || f.localMigrationListOffset != 10 || f.localMigrationListLimit != 5 {
		t.Fatalf("list status=%d filter=%+v offset/limit=%d/%d", w.Code, f.localMigrationFilter, f.localMigrationListOffset, f.localMigrationListLimit)
	}
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationSummaryExport(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations/export?status=failed&q=D%3A%5Cold&started_from_utc=2024-02-29&started_through_utc=2024-03-01", nil))
	if w.Code != http.StatusOK || f.localMigrationSummaryFilter != filter || !strings.Contains(w.Body.String(), `"started_from_utc":"2024-02-29"`) || !strings.Contains(w.Body.String(), `"started_through_utc":"2024-03-01"`) {
		t.Fatalf("export status=%d filter=%+v body=%s", w.Code, f.localMigrationSummaryFilter, w.Body.String())
	}
	for _, query := range []string{
		"started_from_utc=2024-02-30", "started_through_utc=2024-2-9",
		"started_from_utc=2024-03-01&started_through_utc=2024-02-29",
		"started_from_utc=2024-02-29&started_from_utc=2024-03-01",
	} {
		w = httptest.NewRecorder()
		s.HandleLocalDirectoryMigrations(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations?"+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("query %q status=%d body=%s", query, w.Code, w.Body.String())
		}
	}
}

func TestLocalMigrationHistoryExportUsesExactIDAndRedactsEvidence(t *testing.T) {
	id := strings.Repeat("c", 32)
	f := &fakeBackend{localMigrationByID: taskstore.LocalDirectoryMigrationEvidenceBundle{
		Migration: taskstore.LocalDirectoryMigration{ID: id, SourcePath: `D:\old`, TargetPath: `D:\new`, TombstonePath: `D:\old.moved`, Status: "completed", Phase: "complete"},
		Events:    []taskstore.LocalDirectoryMigrationEvidenceEvent{{Sequence: 1, RecordedAtUTC: time.Date(2026, 10, 30, 1, 1, 0, 0, time.UTC), Evidence: json.RawMessage(`{"app_secret":"hidden"}`)}},
	}}
	s := New(f, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations/"+id+"/export", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvidenceExportByID(w, r)
	if w.Code != http.StatusOK || f.localMigrationByIDCalls != 1 || f.localMigrationByIDValue != id || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), "local-migration-"+id+".json") || strings.Contains(w.Body.String(), "hidden") || !strings.Contains(w.Body.String(), "[REDACTED]") {
		t.Fatalf("status=%d id=%q headers=%v body=%s", w.Code, f.localMigrationByIDValue, w.Header(), w.Body.String())
	}
	r = httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations/"+strings.Repeat("d", 32)+"/export", nil)
	r.SetPathValue("id", strings.Repeat("d", 32))
	f.localMigrationByIDErr = sql.ErrNoRows
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvidenceExportByID(w, r)
	if w.Code != http.StatusNotFound || f.localMigrationByIDValue != strings.Repeat("d", 32) {
		t.Fatalf("unknown id status=%d requested=%q", w.Code, f.localMigrationByIDValue)
	}
	r.SetPathValue("id", "invalid")
	f.localMigrationByIDErr = errors.New("internal key")
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvidenceExportByID(w, r)
	if w.Code != http.StatusBadRequest || f.localMigrationByIDCalls != 2 {
		t.Fatalf("invalid id reached backend: status=%d calls=%d", w.Code, f.localMigrationByIDCalls)
	}
}

func TestLocalMigrationSummaryExportReusesFiltersAndRejectsErrors(t *testing.T) {
	id := strings.Repeat("e", 32)
	f := &fakeBackend{localMigrationSummaries: []taskstore.LocalDirectoryMigration{{ID: id, SourcePath: `D:\old`, TargetPath: `D:\new`, Status: "failed", Phase: "copying", StartedAtUTC: time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)}}}
	s := New(f, nil)
	w := httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationSummaryExport(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations/export?status=failed&q=%20D%3A%5Cold%20", nil))
	var body struct {
		ExportedAt string                                  `json:"exported_at_utc"`
		Filter     taskstore.LocalDirectoryMigrationFilter `json:"filter"`
		Total      int                                     `json:"total"`
		Migrations []taskstore.LocalDirectoryMigration     `json:"migrations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || f.localMigrationSummaryCalls != 1 || f.localMigrationSummaryFilter != (taskstore.LocalDirectoryMigrationFilter{Status: "failed", Query: `D:\old`}) || f.cloudCalls != 0 || f.mutationCalls != 0 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), "local-migration-history.json") || body.ExportedAt == "" || body.Filter.Query != `D:\old` || body.Total != 1 || len(body.Migrations) != 1 || body.Migrations[0].ID != id || strings.Contains(w.Body.String(), `"events"`) {
		t.Fatalf("status=%d calls=%d filter=%+v headers=%v body=%s", w.Code, f.localMigrationSummaryCalls, f.localMigrationSummaryFilter, w.Header(), w.Body.String())
	}
	for _, query := range []string{"status=unknown", "q=%FF", "q=" + strings.Repeat("x", 257)} {
		w = httptest.NewRecorder()
		s.HandleLocalDirectoryMigrationSummaryExport(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations/export?"+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("query %q status=%d body=%s", query, w.Code, w.Body.String())
		}
	}
	f.localMigrationSummariesErr = taskstore.ErrLocalMigrationSummaryExportTooLarge
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationSummaryExport(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations/export", nil))
	if w.Code != http.StatusRequestEntityTooLarge || strings.Contains(w.Body.String(), "internal") {
		t.Fatalf("oversized export status=%d body=%s", w.Code, w.Body.String())
	}
	f.localMigrationSummariesErr = errors.New("internal sqlite path")
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationSummaryExport(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations/export", nil))
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "internal sqlite path") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("internal error leaked or cached: status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
}

func TestLocalMigrationEventPageIsIDScopedValidatedAndRedacted(t *testing.T) {
	id := strings.Repeat("f", 32)
	f := &fakeBackend{localMigrationEvents: []taskstore.LocalDirectoryMigrationEvidenceEvent{{Sequence: 8, Status: "running", Phase: "copying", RecordedAtUTC: time.Date(2026, 10, 30, 2, 3, 4, 0, time.UTC), Evidence: json.RawMessage(`{"nested":{"access_token":"hidden","processed_files":2}}`)}}, localMigrationEventsNext: 4, localMigrationEventsMore: true}
	s := New(f, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations/"+id+"/events?before_sequence=9&limit=1", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvents(w, r)
	if w.Code != http.StatusOK || f.localMigrationEventsCalls != 1 || f.localMigrationEventsID != id || f.localMigrationEventsBefore != 9 || f.localMigrationEventsLimit != 1 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "hidden") || !strings.Contains(w.Body.String(), "[REDACTED]") || !strings.Contains(w.Body.String(), `"recorded_at_utc":"2026-10-30T02:03:04Z"`) || !strings.Contains(w.Body.String(), `"next_before_sequence":4`) {
		t.Fatalf("status=%d id=%q cursor=%d limit=%d headers=%v body=%s", w.Code, f.localMigrationEventsID, f.localMigrationEventsBefore, f.localMigrationEventsLimit, w.Header(), w.Body.String())
	}
	for _, target := range []string{"/api/settings/local-migrations/invalid/events", "/api/settings/local-migrations/" + id + "/events?before_sequence=-1", "/api/settings/local-migrations/" + id + "/events?limit=51", "/api/settings/local-migrations/" + id + "/events?limit=bad"} {
		r = httptest.NewRequest(http.MethodGet, target, nil)
		pathID := id
		if strings.Contains(target, "/invalid/") {
			pathID = "invalid"
		}
		r.SetPathValue("id", pathID)
		w = httptest.NewRecorder()
		s.HandleLocalDirectoryMigrationEvents(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("target %q status=%d body=%s", target, w.Code, w.Body.String())
		}
	}
	f.localMigrationEventsErr = sql.ErrNoRows
	r = httptest.NewRequest(http.MethodGet, "/api/settings/local-migrations/"+id+"/events", nil)
	r.SetPathValue("id", id)
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvents(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing record status=%d body=%s", w.Code, w.Body.String())
	}
	f.localMigrationEventsErr = errors.New("internal sqlite evidence")
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvents(w, r)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "internal sqlite evidence") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("internal error leaked: status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
}
