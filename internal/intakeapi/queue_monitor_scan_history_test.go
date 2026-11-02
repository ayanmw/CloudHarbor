package intakeapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestQueueMonitorHistoryRejectsInvalidFiltersAndIDs(t *testing.T) {
	s := New(&fakeBackend{}, nil)
	for _, target := range []string{"/api/monitor-scans?status=bogus", "/api/monitor-scans?started_from_utc=2026-02-30", "/api/monitor-scans?limit=101", "/api/monitor-scans/events?run_id=not-an-id"} {
		r := httptest.NewRequest("GET", target, nil)
		w := httptest.NewRecorder()
		if strings.Contains(target, "/events?") {
			s.HandleQueueMonitorScanEvents(w, r)
		} else {
			s.HandleQueueMonitorScanRuns(w, r)
		}
		if w.Code != 400 {
			t.Fatalf("%s status=%d body=%s", target, w.Code, w.Body.String())
		}
	}
}

func TestQueueMonitorHistoryDTOSeparatesLocalAndCloudTimesWithoutAccount(t *testing.T) {
	local := time.Date(2026, 11, 2, 1, 2, 3, 0, time.UTC)
	cloud := local.Add(time.Second)
	body, err := json.Marshal(monitorRunDTO(taskstore.QueueMonitorScanRun{AccountFingerprint: "private-fingerprint", RunID: "00000000000000000000000000000001", Status: "success", StartedAtUTC: local, FinishedAtUTC: &cloud, CloudObservedAtUTC: &cloud, RecordedAtUTC: local}))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"started_at_utc", "finished_at_utc", "cloud_observed_at_utc", "recorded_at_utc"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
	if strings.Contains(text, "account_fingerprint") || strings.Contains(text, "private-fingerprint") {
		t.Fatalf("account identity leaked: %s", text)
	}
}
