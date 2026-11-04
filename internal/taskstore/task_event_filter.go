package taskstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// TaskEventFilter uses a half-open UTC interval on the local event-recording
// time. Provider, cloud-observation, and HTTP-response times remain separate.
type TaskEventFilter struct {
	Kind                   string
	OperationID            string
	AttemptID              string
	RecordedAtFromUTC      *time.Time
	RecordedAtBeforeUTC    *time.Time
	RecordedExactFromUTC   *time.Time
	RecordedExactBeforeUTC *time.Time
}

type TaskEventKindCount struct {
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
}

type TaskEventContext struct {
	Before []Event `json:"before"`
	Anchor Event   `json:"anchor"`
	After  []Event `json:"after"`
}

var taskEventFilterKinds = map[string]struct{}{
	"authentication_wait_entered":        {},
	"cloud_file_observed":                {},
	"download_attempt_finished":          {},
	"download_destination_recorded":      {},
	"download_attempt_started":           {},
	"job_completed":                      {},
	"job_registered":                     {},
	"job_state_saved":                    {},
	"legacy_import":                      {},
	"source_operation_sending":           {},
	"source_identity_changed":            {},
	"source_operation_confirmed":         {},
	"source_operation_retryable_failure": {},
	"source_operation_unknown":           {},
	"startup_recovery":                   {},
	"status_transition":                  {},
	"task_cancelled":                     {},
	"task_cloud_identity_updated":        {},
	"task_paused":                        {},
	"task_recovered_after_scan":          {},
	"task_resumed":                       {},
	"task_retry_requested":               {},
	"task_source_deleted_remotely":       {},
	"task_source_missing":                {},
}

func IsTaskEventFilterKind(kind string) bool {
	_, ok := taskEventFilterKinds[kind]
	return ok
}

func buildTaskEventFilter(id int64, filter TaskEventFilter) (string, []any, error) {
	if id <= 0 {
		return "", nil, fmt.Errorf("invalid task ID")
	}
	conditions, args, err := buildTaskEventConditions(filter)
	if err != nil {
		return "", nil, err
	}
	return "job_id=? AND " + conditions, append([]any{id}, args...), nil
}

func buildGlobalTaskEventFilter(filter GlobalTaskEventFilter) (string, []any, error) {
	if filter.TaskID < 0 {
		return "", nil, fmt.Errorf("invalid task ID")
	}
	conditions, args, err := buildTaskEventConditions(filter.TaskEventFilter)
	if err != nil {
		return "", nil, err
	}
	if filter.TaskID == 0 {
		return conditions, args, nil
	}
	return "job_id=? AND " + conditions, append([]any{filter.TaskID}, args...), nil
}

func buildTaskEventConditions(filter TaskEventFilter) (string, []any, error) {
	if filter.Kind != "" {
		if !IsTaskEventFilterKind(filter.Kind) {
			return "", nil, fmt.Errorf("invalid task event kind")
		}
	}
	if filter.RecordedAtFromUTC != nil && filter.RecordedAtBeforeUTC != nil && !filter.RecordedAtBeforeUTC.After(*filter.RecordedAtFromUTC) {
		return "", nil, fmt.Errorf("invalid task event UTC range")
	}
	if filter.RecordedExactFromUTC != nil && filter.RecordedExactBeforeUTC != nil && !filter.RecordedExactBeforeUTC.After(*filter.RecordedExactFromUTC) {
		return "", nil, fmt.Errorf("invalid exact task event UTC range")
	}
	if (filter.RecordedAtFromUTC != nil || filter.RecordedAtBeforeUTC != nil) && (filter.RecordedExactFromUTC != nil || filter.RecordedExactBeforeUTC != nil) {
		return "", nil, fmt.Errorf("date and exact UTC ranges cannot be combined")
	}
	conditions := make([]string, 0, 6)
	args := make([]any, 0, 6)
	if filter.Kind != "" {
		conditions = append(conditions, "event_type=?")
		args = append(args, filter.Kind)
	}
	if filter.OperationID != "" {
		conditions = append(conditions, "operation_id=?")
		args = append(args, filter.OperationID)
	}
	if filter.AttemptID != "" {
		conditions = append(conditions, "attempt_id=?")
		args = append(args, filter.AttemptID)
	}
	if filter.RecordedAtFromUTC != nil {
		conditions = append(conditions, "recorded_at_utc>=?")
		args = append(args, filter.RecordedAtFromUTC.UTC().UnixMicro())
	}
	if filter.RecordedAtBeforeUTC != nil {
		conditions = append(conditions, "recorded_at_utc<?")
		args = append(args, filter.RecordedAtBeforeUTC.UTC().UnixMicro())
	}
	if filter.RecordedExactFromUTC != nil {
		conditions = append(conditions, "recorded_at_utc>=?")
		args = append(args, filter.RecordedExactFromUTC.UTC().UnixMicro())
	}
	if filter.RecordedExactBeforeUTC != nil {
		conditions = append(conditions, "recorded_at_utc<?")
		args = append(args, filter.RecordedExactBeforeUTC.UTC().UnixMicro())
	}
	if len(conditions) == 0 {
		return "1=1", args, nil
	}
	return strings.Join(conditions, " AND "), args, nil
}

func (s *Store) EventPageFiltered(ctx context.Context, id, beforeSequence int64, limit int, filter TaskEventFilter) ([]Event, int64, int64, bool, error) {
	events, total, next, more, _, err := s.EventPageFilteredWithCounts(ctx, id, beforeSequence, limit, filter)
	return events, total, next, more, err
}

func (s *Store) EventPageFilteredWithCounts(ctx context.Context, id, beforeSequence int64, limit int, filter TaskEventFilter) ([]Event, int64, int64, bool, []TaskEventKindCount, error) {
	if beforeSequence < 0 || limit < 1 || limit > 50 {
		return nil, 0, 0, false, nil, fmt.Errorf("invalid task event page")
	}
	where, args, err := buildTaskEventFilter(id, filter)
	if err != nil {
		return nil, 0, 0, false, nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, 0, 0, false, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, 0, false, nil, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, 0, false, nil, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM jobs WHERE id=?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, 0, false, nil, os.ErrNotExist
		}
		return nil, 0, 0, false, nil, err
	}
	var total int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM job_events WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, 0, false, nil, err
	}
	counts, err := taskEventKindCounts(ctx, tx, id, filter)
	if err != nil {
		return nil, 0, 0, false, nil, err
	}
	pageWhere := where
	pageArgs := append([]any(nil), args...)
	if beforeSequence > 0 {
		pageWhere += " AND sequence<?"
		pageArgs = append(pageArgs, beforeSequence)
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+eventColumns+` FROM job_events WHERE `+pageWhere+` ORDER BY sequence DESC LIMIT ?`, append(pageArgs, limit+1)...)
	if err != nil {
		return nil, 0, 0, false, nil, err
	}
	events := make([]Event, 0, limit+1)
	for rows.Next() {
		event, scanErr := eventFromRow(rows)
		if scanErr != nil {
			rows.Close()
			return nil, 0, 0, false, nil, scanErr
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, 0, false, nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, 0, false, nil, err
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	var nextBefore int64
	if hasMore {
		nextBefore = events[0].Sequence
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, 0, false, nil, err
	}
	return events, total, nextBefore, hasMore, counts, nil
}

func taskEventKindCounts(ctx context.Context, tx *sql.Tx, id int64, filter TaskEventFilter) ([]TaskEventKindCount, error) {
	filter.Kind = ""
	where, args, err := buildTaskEventFilter(id, filter)
	if err != nil {
		return nil, err
	}
	return taskEventKindCountsWhere(ctx, tx, where, args)
}

func taskEventKindCountsWhere(ctx context.Context, tx *sql.Tx, where string, args []any) ([]TaskEventKindCount, error) {
	counts := make(map[string]int64, len(taskEventFilterKinds)+1)
	for kind := range taskEventFilterKinds {
		counts[kind] = 0
	}
	rows, err := tx.QueryContext(ctx, `SELECT event_type,count(*) FROM job_events WHERE `+where+` GROUP BY event_type`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var count int64
		if err = rows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		if _, ok := counts[kind]; !ok {
			counts["other"] += count
		} else {
			counts[kind] = count
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := make([]TaskEventKindCount, 0, len(counts))
	for kind, count := range counts {
		result = append(result, TaskEventKindCount{Kind: kind, Count: count})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Kind < result[j].Kind })
	return result, nil
}

const taskEventContextWindowSize = 5

func (s *Store) TaskEventContext(ctx context.Context, id, sequence int64) (TaskEventContext, error) {
	if id <= 0 || sequence <= 0 {
		return TaskEventContext{}, fmt.Errorf("invalid task event context")
	}
	if err := ctx.Err(); err != nil {
		return TaskEventContext{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return TaskEventContext{}, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TaskEventContext{}, err
	}
	defer tx.Rollback()
	var anchor Event
	if err = scanTaskEvent(ctx, tx, id, sequence, &anchor); err != nil {
		return TaskEventContext{}, err
	}
	before, err := scanTaskEventRows(ctx, tx, `SELECT `+eventColumns+` FROM job_events WHERE job_id=? AND sequence<? ORDER BY sequence DESC LIMIT ?`, id, sequence, taskEventContextWindowSize)
	if err != nil {
		return TaskEventContext{}, err
	}
	for left, right := 0, len(before)-1; left < right; left, right = left+1, right-1 {
		before[left], before[right] = before[right], before[left]
	}
	after, err := scanTaskEventRows(ctx, tx, `SELECT `+eventColumns+` FROM job_events WHERE job_id=? AND sequence>? ORDER BY sequence ASC LIMIT ?`, id, sequence, taskEventContextWindowSize)
	if err != nil {
		return TaskEventContext{}, err
	}
	if err = tx.Commit(); err != nil {
		return TaskEventContext{}, err
	}
	return TaskEventContext{Before: before, Anchor: anchor, After: after}, nil
}

func scanTaskEvent(ctx context.Context, tx *sql.Tx, id, sequence int64, destination *Event) error {
	rows, err := tx.QueryContext(ctx, `SELECT `+eventColumns+` FROM job_events WHERE job_id=? AND sequence=?`, id, sequence)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return err
		}
		return os.ErrNotExist
	}
	*destination, err = eventFromRow(rows)
	if err != nil {
		return err
	}
	return rows.Err()
}

func scanTaskEventRows(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]Event, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]Event, 0, taskEventContextWindowSize)
	for rows.Next() {
		event, scanErr := eventFromRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *Store) TaskEventEvidenceExportFiltered(ctx context.Context, id int64, filter TaskEventFilter) ([]Event, int64, error) {
	where, args, err := buildTaskEventFilter(id, filter)
	if err != nil {
		return nil, 0, err
	}
	if err = ctx.Err(); err != nil {
		return nil, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM jobs WHERE id=?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, os.ErrNotExist
		}
		return nil, 0, err
	}
	var total int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM job_events WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total > TaskEventEvidenceExportLimit {
		return nil, total, ErrTaskEventEvidenceExportTooLarge
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+eventColumns+` FROM job_events WHERE `+where+` ORDER BY sequence ASC`, args...)
	if err != nil {
		return nil, 0, err
	}
	events := make([]Event, 0, total)
	for rows.Next() {
		event, scanErr := eventFromRow(rows)
		if scanErr != nil {
			rows.Close()
			return nil, 0, scanErr
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return events, total, nil
}
