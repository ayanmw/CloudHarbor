package intakeapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestOperationEvidenceMaskPreservesExactNumbers(t *testing.T) {
	raw := json.RawMessage(`{"fs_id":9007199254740993,"nested":[{"size":9223372036854775807,"token":"private-value"}]}`)
	masked, err := MaskSensitiveEvidence(raw)
	if err != nil || !strings.Contains(string(masked), "9007199254740993") || !strings.Contains(string(masked), "9223372036854775807") || strings.Contains(string(masked), "private-value") || !strings.Contains(string(masked), "[REDACTED]") {
		t.Fatalf("mask altered evidence: %s %v", masked, err)
	}
	for _, invalid := range []string{`{} {}`, `{} garbage`} {
		if _, err = MaskSensitiveEvidence(json.RawMessage(invalid)); err == nil {
			t.Fatalf("accepted trailing evidence: %s", invalid)
		}
	}
}

type preciseOperationBackend struct{ fakeBackend }

func (b *preciseOperationBackend) CloudOperationEventPage(_ context.Context, _ string, _ taskstore.RemoteOperationEventFilter, before int64, _ int) ([]taskstore.RemoteOperationEvent, int64, bool, error) {
	b.operationBefore = before
	b.operationEventCalls++
	return b.operationEvents, 9007199254740993, true, nil
}

func TestOperationStringIdentityFormatPreservesIDsEventsAndNumericCompatibility(t *testing.T) {
	for _, endpoint := range []string{"list", "offset", "events"} {
		for _, format := range []string{"", "string", "invalid", "string&identity_format=string"} {
			backend := &preciseOperationBackend{fakeBackend: fakeBackend{operationSummaries: []taskstore.RemoteOperationSummary{{ID: strings.Repeat("a", 32), ObjectID: 9007199254740993, Kind: "move", Status: "unknown", CreatedAtUTC: time.Unix(1, 0).UTC()}}, operationTotal: 1, operationEvents: []taskstore.RemoteOperationEvent{{Sequence: 9007199254740993, Status: "unknown", Evidence: json.RawMessage(`{"fs_id":9007199254740993,"token":"private-value"}`)}}}}
			target := "/api/cloud/operations?limit=1"
			if endpoint == "offset" {
				target += "&offset=0"
			}
			if endpoint == "events" {
				target = "/api/cloud/operations/" + strings.Repeat("a", 32) + "/events?limit=1&before_sequence=9223372036854775806"
			}
			if format != "" {
				target += "&identity_format=" + format
			}
			server := New(backend, nil)
			response := httptest.NewRecorder()
			request := httptest.NewRequest("GET", target, nil)
			request.SetPathValue("id", strings.Repeat("a", 32))
			if endpoint == "events" {
				server.HandleCloudOperationEvents(response, request)
			} else {
				server.HandleCloudOperations(response, request)
			}
			if strings.HasPrefix(format, "invalid") || strings.Contains(format, "&") {
				if response.Code != 400 || backend.operationCalls+backend.operationEventCalls != 0 {
					t.Fatalf("invalid identity format: %d", response.Code)
				}
				continue
			}
			if response.Code != 200 {
				t.Fatalf("response: %d %s", response.Code, response.Body.String())
			}
			payload := response.Body.String()
			if strings.Contains(payload, "private-value") {
				t.Fatal("secret leaked")
			}
			var body map[string]any
			decoder := json.NewDecoder(strings.NewReader(payload))
			decoder.UseNumber()
			if err := decoder.Decode(&body); err != nil {
				t.Fatal(err)
			}
			key, field := "operations", "object_id"
			if endpoint == "events" {
				key, field = "events", "sequence"
				if backend.operationBefore != 9223372036854775806 {
					t.Fatal("request cursor changed")
				}
			}
			row := body[key].([]any)[0].(map[string]any)
			if format == "string" {
				if row[field] != "9007199254740993" {
					t.Fatalf("string identity changed: %s", payload)
				}
				if endpoint == "events" {
					if body["next_before_sequence"] != "9007199254740993" {
						t.Fatal("event cursor changed")
					}
					raw, ok := row["evidence_json"].(string)
					if !ok || !strings.Contains(raw, "9007199254740993") || !strings.Contains(raw, "[REDACTED]") {
						t.Fatalf("exact evidence text missing: %s", payload)
					}
				}
			} else {
				if row[field] != json.Number("9007199254740993") {
					t.Fatalf("numeric compatibility changed: %s", payload)
				}
			}
		}
	}
}
