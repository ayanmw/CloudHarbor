package intakeapi

import (
	"errors"
	"net/url"
	"strconv"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

type migrationStringEvent struct {
	Sequence      string    `json:"sequence"`
	Status        string    `json:"status"`
	Phase         string    `json:"phase"`
	RecordedAtUTC time.Time `json:"recorded_at_utc"`
	EvidenceJSON  string    `json:"evidence_json"`
}

func migrationEventResponse(events []taskstore.LocalDirectoryMigrationEvidenceEvent, exact bool) any {
	if !exact {
		return events
	}
	out := make([]migrationStringEvent, 0, len(events))
	for _, event := range events {
		out = append(out, migrationStringEvent{strconv.FormatInt(event.Sequence, 10), event.Status, event.Phase, event.RecordedAtUTC, string(event.Evidence)})
	}
	return out
}

func migrationStringCounters(query url.Values) (bool, error) {
	values, exists := query["counter_format"]
	if !exists {
		return false, nil
	}
	if len(values) != 1 || (values[0] != "" && values[0] != "string") {
		return false, errors.New("invalid migration counter format")
	}
	return values[0] == "string", nil
}

type migrationStringSummary struct {
	taskstore.LocalDirectoryMigration
	TotalFiles     string `json:"total_files"`
	ProcessedFiles string `json:"processed_files"`
	TotalBytes     string `json:"total_bytes"`
	ProcessedBytes string `json:"processed_bytes"`
}

func migrationHistoryResponse(items []taskstore.LocalDirectoryMigration, exact bool) any {
	if !exact {
		return items
	}
	out := make([]migrationStringSummary, 0, len(items))
	for _, m := range items {
		out = append(out, migrationStringSummary{m, strconv.FormatInt(m.TotalFiles, 10), strconv.FormatInt(m.ProcessedFiles, 10), strconv.FormatInt(m.TotalBytes, 10), strconv.FormatInt(m.ProcessedBytes, 10)})
	}
	return out
}
