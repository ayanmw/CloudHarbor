package intakeapi

import (
	"errors"
	"net/url"
	"strconv"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func operationStringIdentity(query url.Values) (bool, error) {
	values, exists := query["identity_format"]
	if !exists {
		return false, nil
	}
	if len(values) != 1 || (values[0] != "" && values[0] != "string") {
		return false, errors.New("invalid operation identity format")
	}
	return values[0] == "string", nil
}

type operationStringSummary struct {
	taskstore.RemoteOperationSummary
	ObjectID string `json:"object_id"`
}

func operationSummaryResponse(items []taskstore.RemoteOperationSummary, stringIDs bool) any {
	if !stringIDs {
		return items
	}
	out := make([]operationStringSummary, 0, len(items))
	for _, item := range items {
		out = append(out, operationStringSummary{RemoteOperationSummary: item, ObjectID: strconv.FormatInt(item.ObjectID, 10)})
	}
	return out
}

// Evidence stays JSON text for opt-in browser clients. Parsing the arbitrary
// numeric evidence as a JavaScript object would lose int64 identity again.
type operationStringEvent struct {
	Sequence              string     `json:"sequence"`
	Status                string     `json:"status"`
	RecordedAtUTC         time.Time  `json:"recorded_at_utc"`
	ResponseObservedAtUTC *time.Time `json:"response_observed_at_utc,omitempty"`
	EvidenceJSON          string     `json:"evidence_json"`
}

func operationEventStringResponse(events []taskstore.RemoteOperationEvent) []operationStringEvent {
	out := make([]operationStringEvent, 0, len(events))
	for _, event := range events {
		out = append(out, operationStringEvent{Sequence: strconv.FormatInt(event.Sequence, 10), Status: event.Status, RecordedAtUTC: event.RecordedAtUTC, ResponseObservedAtUTC: event.ResponseObservedAtUTC, EvidenceJSON: string(event.Evidence)})
	}
	return out
}
