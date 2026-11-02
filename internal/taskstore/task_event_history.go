package taskstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

const TaskEventHistoryExportLimit = 1000

var ErrTaskEventHistoryExportTooLarge = errors.New("global task event history export exceeds limit")
var ErrTaskEventHistorySnapshotInvalid = errors.New("global task event history snapshot is invalid")
var ErrTaskEventHistoryCursorInvalid = errors.New("global task event history cursor is invalid")

type GlobalTaskEventFilter struct {
	TaskID int64
	TaskEventFilter
}

type TaskEventHistoryPage struct {
	Events             []Event              `json:"events"`
	TotalMatches       int64                `json:"total_matches"`
	AsOfSequence       int64                `json:"as_of_sequence"`
	NextBeforeSequence int64                `json:"next_before_sequence,omitempty"`
	HasMore            bool                 `json:"has_more"`
	KindCounts         []TaskEventKindCount `json:"kind_counts"`
}

// TaskEventHistoryPage reads the page, total, and kind totals from one SQLite
// snapshot. A nil asOfSequence captures the current maximum sequence.
func (s *Store) TaskEventHistoryPage(ctx context.Context, filter GlobalTaskEventFilter, beforeSequence int64, asOfSequence *int64, limit int) (TaskEventHistoryPage, error) {
	if beforeSequence < 0 || (asOfSequence != nil && *asOfSequence < 0) || limit < 1 || limit > 50 {
		return TaskEventHistoryPage{}, fmt.Errorf("invalid global task event page")
	}
	if beforeSequence > 0 && asOfSequence != nil && beforeSequence > *asOfSequence {
		return TaskEventHistoryPage{}, ErrTaskEventHistoryCursorInvalid
	}
	where, args, err := buildGlobalTaskEventFilter(filter)
	if err != nil {
		return TaskEventHistoryPage{}, err
	}
	if err = ctx.Err(); err != nil {
		return TaskEventHistoryPage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return TaskEventHistoryPage{}, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TaskEventHistoryPage{}, err
	}
	defer tx.Rollback()
	asOf, err := resolveTaskEventSnapshot(ctx, tx, asOfSequence)
	if err != nil {
		return TaskEventHistoryPage{}, err
	}
	if beforeSequence > asOf {
		return TaskEventHistoryPage{}, ErrTaskEventHistoryCursorInvalid
	}
	whereAtSnapshot := where + " AND sequence<=?"
	argsAtSnapshot := append(append([]any(nil), args...), asOf)
	var total int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM job_events WHERE `+whereAtSnapshot, argsAtSnapshot...).Scan(&total); err != nil {
		return TaskEventHistoryPage{}, err
	}
	withoutKind := filter
	withoutKind.Kind = ""
	countWhere, countArgs, err := buildGlobalTaskEventFilter(withoutKind)
	if err != nil {
		return TaskEventHistoryPage{}, err
	}
	kindCounts, err := taskEventKindCountsWhere(ctx, tx, countWhere+" AND sequence<=?", append(countArgs, asOf))
	if err != nil {
		return TaskEventHistoryPage{}, err
	}
	pageWhere := whereAtSnapshot
	pageArgs := append([]any(nil), argsAtSnapshot...)
	if beforeSequence > 0 {
		pageWhere += " AND sequence<?"
		pageArgs = append(pageArgs, beforeSequence)
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+eventColumns+` FROM job_events WHERE `+pageWhere+` ORDER BY sequence DESC LIMIT ?`, append(pageArgs, limit+1)...)
	if err != nil {
		return TaskEventHistoryPage{}, err
	}
	events := make([]Event, 0, limit+1)
	for rows.Next() {
		event, scanErr := eventFromRow(rows)
		if scanErr != nil {
			rows.Close()
			return TaskEventHistoryPage{}, scanErr
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return TaskEventHistoryPage{}, err
	}
	if err = rows.Close(); err != nil {
		return TaskEventHistoryPage{}, err
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
		return TaskEventHistoryPage{}, err
	}
	return TaskEventHistoryPage{Events: events, TotalMatches: total, AsOfSequence: asOf, NextBeforeSequence: nextBefore, HasMore: hasMore, KindCounts: kindCounts}, nil
}

// TaskEventHistoryExport returns a complete sequence-ordered snapshot or no rows.
// A nil asOfSequence captures the current maximum sequence.
func (s *Store) TaskEventHistoryExport(ctx context.Context, filter GlobalTaskEventFilter, asOfSequence *int64) ([]Event, int64, int64, error) {
	if asOfSequence != nil && *asOfSequence < 0 {
		return nil, 0, 0, fmt.Errorf("invalid global task event snapshot")
	}
	where, args, err := buildGlobalTaskEventFilter(filter)
	if err != nil {
		return nil, 0, 0, err
	}
	if err = ctx.Err(); err != nil {
		return nil, 0, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, 0, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, 0, err
	}
	defer tx.Rollback()
	asOf, err := resolveTaskEventSnapshot(ctx, tx, asOfSequence)
	if err != nil {
		return nil, 0, 0, err
	}
	where += " AND sequence<=?"
	args = append(args, asOf)
	var total int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM job_events WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, 0, err
	}
	if total > TaskEventHistoryExportLimit {
		return nil, total, asOf, ErrTaskEventHistoryExportTooLarge
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+eventColumns+` FROM job_events WHERE `+where+` ORDER BY sequence ASC LIMIT ?`, append(args, TaskEventHistoryExportLimit+1)...)
	if err != nil {
		return nil, 0, 0, err
	}
	events := make([]Event, 0, total)
	for rows.Next() {
		event, scanErr := eventFromRow(rows)
		if scanErr != nil {
			rows.Close()
			return nil, 0, 0, scanErr
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, 0, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, 0, err
	}
	if int64(len(events)) != total {
		return nil, 0, 0, fmt.Errorf("global task event export snapshot count mismatch")
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, 0, err
	}
	return events, total, asOf, nil
}

func resolveTaskEventSnapshot(ctx context.Context, tx *sql.Tx, requested *int64) (int64, error) {
	var current int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM job_events`).Scan(&current); err != nil {
		return 0, err
	}
	if requested == nil {
		return current, nil
	}
	if *requested > current {
		return 0, ErrTaskEventHistorySnapshotInvalid
	}
	return *requested, nil
}
