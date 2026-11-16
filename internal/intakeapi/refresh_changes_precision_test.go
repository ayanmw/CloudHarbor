package intakeapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestRefreshChangesOptInKeepsAllIntegersExactAndLegacyUnchanged(t *testing.T) {
	n := int64(9007199254740993)
	at := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	backend := &fakeBackend{refreshChangePage: taskstore.CloudInventoryRefreshChangePage{Changes: []taskstore.CloudInventoryRefreshChange{{Sequence: n, Ordinal: n, Kind: "added", New: &taskstore.CloudInventoryRefreshChangeItem{FsID: n, Path: "/large", Name: "large", Size: n, ProviderMtime: n}, RecordedAtUTC: at, CloudObservedAtUTC: at}}, Counts: taskstore.CloudInventoryRefreshChangeCounts{Total: n, Added: n}, MatchedCount: n, NextBeforeSequence: n, HasMore: true}}
	for _, exact := range []bool{false, true} {
		url := "/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=9007199254740995&before_sequence=9007199254740997&kind=added&q=large"
		if exact {
			url += "&identity_format=string"
		}
		w := httptest.NewRecorder()
		New(backend, nil).HandleInventoryRefreshChanges(w, httptest.NewRequest("GET", url, nil))
		if w.Code != 200 || backend.refreshChangesEventSequence != 9007199254740995 || backend.refreshChangesBefore != 9007199254740997 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		d := json.NewDecoder(strings.NewReader(w.Body.String()))
		d.UseNumber()
		var body map[string]any
		if err := d.Decode(&body); err != nil {
			t.Fatal(err)
		}
		row := body["changes"].([]any)[0].(map[string]any)
		item := row["new"].(map[string]any)
		for _, value := range []any{row["sequence"], row["ordinal"], item["fs_id"], item["size"], item["provider_mtime"], body["matched_count"], body["next_before_sequence"], body["counts"].(map[string]any)["total"]} {
			if exact {
				if value != "9007199254740993" {
					t.Fatalf("rounded or numeric: %T %v", value, value)
				}
			} else {
				if value != json.Number("9007199254740993") {
					t.Fatalf("legacy changed: %T %v", value, value)
				}
			}
		}
		if exact && body["event_sequence"] != "9007199254740995" {
			t.Fatal(body["event_sequence"])
		}
	}
}
func TestRefreshChangesRejectsInvalidPrecisionBeforeReadingBackend(t *testing.T) {
	for _, format := range []string{"identity_format=number", "identity_format=string&identity_format=string"} {
		b := &fakeBackend{}
		w := httptest.NewRecorder()
		New(b, nil).HandleInventoryRefreshChanges(w, httptest.NewRequest("GET", "/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=2&"+format, nil))
		if w.Code != 400 || b.refreshChangesRunID != "" {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}

func TestRefreshChangesExactKeepsNullableBeforeAndObservationEvidence(t *testing.T) {
	at := time.Date(2026, 11, 6, 0, 0, 0, 123456789, time.UTC)
	page := taskstore.CloudInventoryRefreshChangePage{Changes: []taskstore.CloudInventoryRefreshChange{{Sequence: 3, Kind: "removed", Old: &taskstore.CloudInventoryRefreshChangeItem{FsID: 7, Path: "/old", Name: "old", Directory: true, ObservedAtUTC: &at}, RecordedAtUTC: at, CloudObservedAtUTC: at}}}
	raw, err := json.Marshal(refreshChangesExact("run-1", 11, "removed", "old", page))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{`"new":null`, `"fs_id":"7"`, `"size":"0"`, `"provider_mtime":"0"`, `"observed_at_utc":"2026-11-06T00:00:00.123456789Z"`, `"recorded_at_utc":"2026-11-06T00:00:00.123456789Z"`, `"cloud_observed_at_utc":"2026-11-06T00:00:00.123456789Z"`} {
		if !strings.Contains(string(raw), token) {
			t.Fatalf("missing %s: %s", token, raw)
		}
	}
	raw, err = json.Marshal(refreshChangesExact("run-1", 11, "", "", taskstore.CloudInventoryRefreshChangePage{}))
	if err != nil || !strings.Contains(string(raw), `"changes":[]`) {
		t.Fatalf("empty page: %s %v", raw, err)
	}
}

func TestRefreshChangesExportOptInPreservesExactScopeAndDefaultExport(t *testing.T) {
	n := int64(9007199254740993)
	at := time.Date(2026, 11, 6, 0, 0, 0, 1, time.UTC)
	b := &fakeBackend{refreshChangesExport: taskstore.CloudInventoryRefreshChangeExport{RunID: "run-1", EventSequence: n, RecordedAtUTC: at, CloudObservedAtUTC: at, Kind: "added", Query: "large", Counts: taskstore.CloudInventoryRefreshChangeCounts{Total: 1, Added: 1}, MatchedCount: 1, Changes: []taskstore.CloudInventoryRefreshChange{{Sequence: n, Ordinal: 1, Kind: "added", New: &taskstore.CloudInventoryRefreshChangeItem{FsID: n, Size: n, Path: "/large", Name: "large"}, RecordedAtUTC: at, CloudObservedAtUTC: at}}}}
	for _, exact := range []bool{false, true} {
		url := "/api/cloud-inventory/refresh/changes/export?run_id=run-1&event_sequence=9007199254740993&kind=added&q=large"
		if exact {
			url += "&identity_format=string"
		}
		w := httptest.NewRecorder()
		New(b, nil).HandleInventoryRefreshChangesExport(w, httptest.NewRequest("GET", url, nil))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		token := `"event_sequence":9007199254740993`
		if exact {
			token = `"event_sequence":"9007199254740993"`
		}
		if !strings.Contains(w.Body.String(), token) || !strings.Contains(w.Body.String(), `"exported_at_utc"`) {
			t.Fatal(w.Body.String())
		}
	}
}
