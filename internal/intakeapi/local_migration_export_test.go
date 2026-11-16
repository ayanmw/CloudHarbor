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

func TestLocalMigrationEvidenceExportIsReadOnlyAndBounded(t *testing.T) {
	id := strings.Repeat("a", 32)
	f := &fakeBackend{localMigrationEvidence: taskstore.LocalDirectoryMigrationEvidenceBundle{
		Migration: taskstore.LocalDirectoryMigration{ID: id, SourcePath: `D:\old`, TargetPath: `D:\new`, TombstonePath: `D:\old.moved`, Status: "completed", Phase: "complete", StartedAtUTC: time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC), UpdatedAtUTC: time.Date(2026, 10, 30, 1, 1, 0, 0, time.UTC)},
		Events:    []taskstore.LocalDirectoryMigrationEvidenceEvent{{Sequence: 1, Status: "completed", Phase: "complete", RecordedAtUTC: time.Date(2026, 10, 30, 1, 1, 0, 0, time.UTC), Evidence: json.RawMessage(`{"processed_files":2,"access_token":"must-not-export"}`)}},
	}}
	s := New(f, nil)
	w := httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvidenceExport(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migration/export", nil))
	if w.Code != http.StatusOK || f.localMigrationEvidenceCalls != 1 || f.cloudCalls != 0 || f.mutationCalls != 0 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), "local-migration-"+id+".json") || !strings.Contains(w.Body.String(), `"recorded_at_utc":"2026-10-30T01:01:00Z"`) || strings.Contains(w.Body.String(), "must-not-export") || !strings.Contains(w.Body.String(), "[REDACTED]") {
		t.Fatalf("status=%d calls=%d cloud=%d writes=%d headers=%v body=%s", w.Code, f.localMigrationEvidenceCalls, f.cloudCalls, f.mutationCalls, w.Header(), w.Body.String())
	}
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvidenceExport(w, httptest.NewRequest(http.MethodPost, "/api/settings/local-migration/export", nil))
	if w.Code != http.StatusMethodNotAllowed || f.localMigrationEvidenceCalls != 1 {
		t.Fatalf("non-GET status=%d calls=%d", w.Code, f.localMigrationEvidenceCalls)
	}
	f.localMigrationEvidenceErr = sql.ErrNoRows
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvidenceExport(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migration/export", nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "尚无本地目录迁移记录") {
		t.Fatalf("empty history status=%d body=%s", w.Code, w.Body.String())
	}
	f.localMigrationEvidenceErr = taskstore.ErrLocalMigrationEvidenceTooLarge
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvidenceExport(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migration/export", nil))
	if w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), "超过 100 条") {
		t.Fatalf("oversize status=%d body=%s", w.Code, w.Body.String())
	}
	f.localMigrationEvidenceErr = errors.New("internal path/key must not be exposed")
	w = httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvidenceExport(w, httptest.NewRequest(http.MethodGet, "/api/settings/local-migration/export", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "稍后重试") || strings.Contains(w.Body.String(), "internal path/key") {
		t.Fatalf("internal error leaked: status=%d body=%s", w.Code, w.Body.String())
	}
}
