package intakeapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestHistoryCoverageAndRefreshEvidencePreserveNullAndExactValues(t *testing.T) {
	count := int64(9007199254740993)
	delta := -count
	run := taskstore.CloudScanRun{RunID: strings.Repeat("a", 32), CoverageIncompleteDirectories: &count, CoverageReasons: &taskstore.CloudInventoryCoverageReasons{MissingRoot: count}, CoverageComparison: &taskstore.CloudScanCoverageComparison{Status: "comparable", PreviousSequence: count, IncompleteDirectoriesDelta: &delta}}
	body, err := json.Marshal(scanEvidencePage([]taskstore.CloudScanRun{run}, 0, false))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{`"coverage_status":null`, `"coverage_incomplete_directories":"9007199254740993"`, `"missing_root":"9007199254740993"`, `"previous_sequence":"9007199254740993"`, `"incomplete_directories_delta":"-9007199254740993"`} {
		if !strings.Contains(string(body), token) {
			t.Fatalf("missing %s: %s", token, body)
		}
	}
	body, err = json.Marshal(scanEvidencePage([]taskstore.CloudInventoryRefreshEvent{{RestartFSID: &count}}, 0, false))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{`"restart_fs_id":"9007199254740993"`, `"added_count":null`, `"directory_changed":null`} {
		if !strings.Contains(string(body), token) {
			t.Fatalf("missing %s: %s", token, body)
		}
	}
}
func TestHashHistoryOptInPrecisionAndRedactedEvidenceText(t *testing.T) {
	runID := strings.Repeat("a", 32)
	f := &fakeBackend{duplicateRuns: []taskstore.CloudDuplicateScanRun{{RunID: runID, StartSequence: 9007199254740993, TotalBytes: 9007199254740995}}, duplicateRunEvents: []taskstore.CloudDuplicateScanEvent{{RunID: runID, Sequence: 9007199254740993, Evidence: json.RawMessage(`{"fs_id":9007199254740993,"access_token":"secret"}`)}}}
	s := New(f, nil)
	w := httptest.NewRecorder()
	s.HandleDuplicateScanRuns(w, httptest.NewRequest("GET", "/api/cloud-cleanup/duplicate-scan/runs?identity_format=string&before_start_sequence=9007199254740995", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"sequence":"9007199254740993"`) || strings.Contains(w.Body.String(), "account_fingerprint") || f.duplicateRunsBefore != 9007199254740995 {
		t.Fatalf("runs %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/cloud-cleanup/duplicate-scan/runs/"+runID+"/events?identity_format=string", nil)
	r.SetPathValue("id", runID)
	s.HandleDuplicateScanRunEvents(w, r)
	var result struct {
		Rows []struct {
			Evidence string `json:"evidence_json"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(result.Rows) != 1 || strings.Contains(result.Rows[0].Evidence, "secret") || !strings.Contains(result.Rows[0].Evidence, "9007199254740993") || !strings.Contains(result.Rows[0].Evidence, "[REDACTED]") {
		t.Fatalf("events %d %s", w.Code, w.Body.String())
	}
}

func TestHashHistoryRejectsInvalidIdentityFormatsBeforeBackendRead(t *testing.T) {
	for _, query := range []string{"identity_format=number", "identity_format=string&identity_format=string"} {
		f := &fakeBackend{}
		s := New(f, nil)
		w := httptest.NewRecorder()
		s.HandleDuplicateScanRuns(w, httptest.NewRequest("GET", "/api/cloud-cleanup/duplicate-scan/runs?before_start_sequence=17&"+query, nil))
		if w.Code != 400 || f.duplicateRunsBefore != 0 {
			t.Fatalf("invalid query reached backend: %d %s", w.Code, w.Body.String())
		}
	}
}
