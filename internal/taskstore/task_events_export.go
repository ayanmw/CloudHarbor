package taskstore

import (
	"context"
	"errors"
)

const TaskEventEvidenceExportLimit = 100

var ErrTaskEventEvidenceExportTooLarge = errors.New("task event evidence export exceeds limit")

// TaskEventEvidenceExport returns all events for one task from a single
// read-only snapshot. It rejects oversized histories instead of truncating.
func (s *Store) TaskEventEvidenceExport(ctx context.Context, id int64) ([]Event, error) {
	events, _, err := s.TaskEventEvidenceExportFiltered(ctx, id, TaskEventFilter{})
	return events, err
}
