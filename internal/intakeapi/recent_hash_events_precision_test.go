package intakeapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestRecentHashEventsExactAndLegacy(t *testing.T) {
	b := &fakeBackend{duplicateEvents: []taskstore.CloudDuplicateScanEvent{{Sequence: 9007199254740993, RunID: "legacy-run", Status: "failed", Phase: "failed", Evidence: json.RawMessage(`{"fs_id":9007199254740993,"access_token":"private","nested":[{"fs_id":9007199254740995,"refresh_token":"nested-private"}]}`)}}, duplicateEventsNext: 9007199254740993, duplicateEventsMore: true}
	w := httptest.NewRecorder()
	New(b, nil).HandleDuplicateScanEvents(w, httptest.NewRequest("GET", "/api/cloud-cleanup/duplicate-scan/events?identity_format=string&before_sequence=9007199254740994&limit=20", nil))
	var d struct {
		Rows []struct {
			Sequence string `json:"sequence"`
			RunID    string `json:"run_id"`
			Evidence string `json:"evidence_json"`
		} `json:"rows"`
		Next string `json:"next_before_sequence"`
		More bool   `json:"has_more"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || w.Code != 200 || len(d.Rows) != 1 || d.Rows[0].Sequence != "9007199254740993" || d.Rows[0].RunID != "legacy-run" || !strings.Contains(d.Rows[0].Evidence, `9007199254740993`) || !strings.Contains(d.Rows[0].Evidence, `9007199254740995`) || strings.Contains(d.Rows[0].Evidence, "private") || !strings.Contains(d.Rows[0].Evidence, "[REDACTED]") || d.Next != "9007199254740993" || !d.More || b.cloudCalls != 0 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %s error=%v", w.Code, w.Body.String(), err)
	}
	w = httptest.NewRecorder()
	New(b, nil).HandleDuplicateScanEvents(w, httptest.NewRequest("GET", "/api/cloud-cleanup/duplicate-scan/events", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"events"`) || !strings.Contains(w.Body.String(), `"next_before_sequence":9007199254740993`) || strings.Contains(w.Body.String(), `"evidence_json"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestRecentHashEventsExactValidation(t *testing.T) {
	for _, q := range []string{"identity_format=bad", "identity_format=string&identity_format=string", "identity_format=string&before_sequence=01", "identity_format=string&run_id=other", "identity_format=string&before_sequence=9223372036854775808"} {
		b := &fakeBackend{}
		w := httptest.NewRecorder()
		New(b, nil).HandleDuplicateScanEvents(w, httptest.NewRequest("GET", "/api/cloud-cleanup/duplicate-scan/events?"+q, nil))
		if w.Code != 400 || b.duplicateEventsLimit != 0 {
			t.Fatal(q, w.Code, w.Body.String())
		}
	}
}
