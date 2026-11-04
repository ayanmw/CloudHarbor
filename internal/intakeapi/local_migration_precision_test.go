package intakeapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestLocalMigrationEventStringFormatKeepsCursorAndEvidenceExact(t *testing.T) {
	id := strings.Repeat("a", 32)
	f := &fakeBackend{localMigrationEvents: []taskstore.LocalDirectoryMigrationEvidenceEvent{{Sequence: 9007199254740993, Evidence: json.RawMessage(`{"size":9223372036854775807,"access_token":"private"}`)}}, localMigrationEventsNext: 9007199254740993, localMigrationEventsMore: true}
	s := New(f, nil)
	r := httptest.NewRequest("GET", "/api/settings/local-migrations/"+id+"/events?identity_format=string", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.HandleLocalDirectoryMigrationEvents(w, r)
	var result struct {
		Next   string `json:"next_before_sequence"`
		Events []struct {
			Sequence string `json:"sequence"`
			Evidence string `json:"evidence_json"`
		} `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Next != "9007199254740993" || len(result.Events) != 1 || result.Events[0].Sequence != result.Next || !strings.Contains(result.Events[0].Evidence, "9223372036854775807") || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("bad exact evidence: %s", w.Body.String())
	}
}

func TestLocalMigrationHistoryStringCountersKeepExactInt64(t *testing.T) {
	f := &fakeBackend{localMigrationList: []taskstore.LocalDirectoryMigration{{TotalBytes: 9223372036854775807, ProcessedBytes: 9007199254740993}}, localMigrationListTotal: 1}
	s := New(f, nil)
	for _, format := range []string{"", "string"} {
		w := httptest.NewRecorder()
		s.HandleLocalDirectoryMigrations(w, httptest.NewRequest("GET", "/api/settings/local-migrations?counter_format="+format, nil))
		want := `"processed_bytes":9007199254740993`
		if format == "string" {
			want = `"processed_bytes":"9007199254740993"`
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("format=%s status=%d body=%s", format, w.Code, w.Body.String())
		}
	}
	calls := f.localMigrationListCalls
	for _, query := range []string{"counter_format=bad", "counter_format=string&counter_format=string"} {
		w := httptest.NewRecorder()
		s.HandleLocalDirectoryMigrations(w, httptest.NewRequest("GET", "/api/settings/local-migrations?"+query, nil))
		if w.Code != 400 || f.localMigrationListCalls != calls {
			t.Fatalf("invalid format accepted: %s", query)
		}
	}
}
