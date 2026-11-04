package intakeapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudScanStatusStringFormatHasExactCountersWithoutAccountFingerprint(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		w := httptest.NewRecorder()
		s := New(&fakeBackend{}, nil)
		r := httptest.NewRequest("GET", "/api/cloud-inventory/scan?identity_format=string", nil)
		if refresh {
			s.HandleInventoryRefresh(w, r)
		} else {
			s.HandleInventoryScan(w, r)
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"pages_processed":"`) || strings.Contains(w.Body.String(), "account_fingerprint") {
			t.Fatalf("refresh=%v response=%s", refresh, w.Body.String())
		}
	}
}

func TestCloudScanEvidenceStringFormat(t *testing.T) {
	for _, route := range []string{"/api/cloud-inventory/scan/runs", "/api/cloud-inventory/scan/events?run_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "/api/cloud-inventory/refresh/runs", "/api/cloud-inventory/refresh/events?run_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		separator := "?"
		if strings.Contains(route, "?") {
			separator = "&"
		}
		r := httptest.NewRequest("GET", route+separator+"identity_format=string", nil)
		w := httptest.NewRecorder()
		s := New(&fakeBackend{}, nil)
		switch {
		case strings.Contains(route, "scan/runs"):
			s.HandleCloudScanHistoryRuns(w, r)
		case strings.Contains(route, "scan/events"):
			s.HandleCloudScanHistoryEvents(w, r)
		case strings.Contains(route, "refresh/runs"):
			s.HandleInventoryRefreshRuns(w, r)
		default:
			s.HandleInventoryRefreshEvents(w, r)
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"next_before_sequence":"`) || strings.Contains(w.Body.String(), "account_fingerprint") {
			t.Fatalf("%s: %d %s", route, w.Code, w.Body.String())
		}
	}
}
