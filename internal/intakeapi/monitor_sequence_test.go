package intakeapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

type preciseMonitorBackend struct {
	fakeBackend
	before int64
}

func (b *preciseMonitorBackend) QueueMonitorScanHistoryRuns(_ context.Context, before int64, _ taskstore.QueueMonitorScanFilter, _ int) ([]taskstore.QueueMonitorScanRun, int64, bool, error) {
	b.before = before
	return []taskstore.QueueMonitorScanRun{{Sequence: 9007199254740993, RunID: strings.Repeat("a", 32), AccountFingerprint: "private"}}, 9007199254740993, true, nil
}
func (b *preciseMonitorBackend) QueueMonitorScanHistoryEvents(_ context.Context, _ string, before int64, _ int) ([]taskstore.QueueMonitorScanEvent, int64, bool, error) {
	b.before = before
	return []taskstore.QueueMonitorScanEvent{{Sequence: 9007199254740993, RunID: strings.Repeat("a", 32), AccountFingerprint: "private"}}, 9007199254740993, true, nil
}

func TestMonitorHistoryStringSequencesPreservePrecisionAndNumericCompatibility(t *testing.T) {
	for _, events := range []bool{false, true} {
		for _, format := range []string{"", "string", "invalid", "string&sequence_format=string"} {
			backend := &preciseMonitorBackend{}
			server := New(backend, nil)
			target := "/api/monitor-scans?before_sequence=9223372036854775806"
			if events {
				target = "/api/monitor-scans/events?run_id=" + strings.Repeat("a", 32) + "&before_sequence=9223372036854775806"
			}
			if format != "" {
				target += "&sequence_format=" + format
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest("GET", target, nil)
			if events {
				server.HandleQueueMonitorScanEvents(response, request)
			} else {
				server.HandleQueueMonitorScanRuns(response, request)
			}
			if strings.HasPrefix(format, "invalid") || strings.Contains(format, "&") {
				if response.Code != 400 {
					t.Fatalf("invalid format: %d", response.Code)
				}
				continue
			}
			if response.Code != 200 || backend.before != 9223372036854775806 {
				t.Fatalf("precision request: %d %s", response.Code, response.Body.String())
			}
			payload := response.Body.String()
			var body map[string]any
			decoder := json.NewDecoder(response.Body)
			decoder.UseNumber()
			if err := decoder.Decode(&body); err != nil {
				t.Fatal(err)
			}
			key := "runs"
			if events {
				key = "events"
			}
			row := body[key].([]any)[0].(map[string]any)
			if format == "string" {
				if row["sequence"] != "9007199254740993" || body["next_before_sequence"] != "9007199254740993" {
					t.Fatalf("string precision: %#v", body)
				}
			} else {
				if row["sequence"] != json.Number("9007199254740993") || body["next_before_sequence"] != json.Number("9007199254740993") {
					t.Fatalf("numeric compatibility: %#v", body)
				}
			}
			if strings.Contains(payload, "private") {
				t.Fatal("private identity leaked")
			}
		}
	}
}

func TestMonitorHistoryStringTerminalCursorIsNull(t *testing.T) {
	server := New(&fakeBackend{}, nil)
	for _, events := range []bool{false, true} {
		target := "/api/monitor-scans?sequence_format=string"
		if events {
			target = "/api/monitor-scans/events?sequence_format=string&run_id=" + strings.Repeat("a", 32)
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET", target, nil)
		if events {
			server.HandleQueueMonitorScanEvents(response, request)
		} else {
			server.HandleQueueMonitorScanRuns(response, request)
		}
		var body map[string]any
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body["next_before_sequence"] != nil || body["has_more"] != false {
			t.Fatalf("terminal page: %d %s", response.Code, response.Body.String())
		}
	}
}
