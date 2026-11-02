package taskstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

var ErrCloudDuplicateScanSummaryExportTooLarge = errors.New("duplicate scan summary export exceeds limit")
var ErrCloudDuplicateScanEvidenceExportTooLarge = errors.New("duplicate scan evidence export exceeds limit")
var ErrCloudDuplicateScanRunNotFound = errors.New("duplicate scan run not found")

const (
	CloudDuplicateScanSummaryExportLimit  = 10000
	CloudDuplicateScanEvidenceExportLimit = 10000
)

type CloudDuplicateScanRun struct {
	StartSequence      int64     `json:"start_sequence"`
	AccountFingerprint string    `json:"-"`
	RunID              string    `json:"run_id"`
	Status             string    `json:"status"`
	Phase              string    `json:"phase"`
	NextFsID           int64     `json:"next_fs_id"`
	TotalFiles         int64     `json:"total_files"`
	ProcessedFiles     int64     `json:"processed_files"`
	TotalBytes         int64     `json:"total_bytes"`
	ProcessedBytes     int64     `json:"processed_bytes"`
	StartedAtUTC       time.Time `json:"started_at_utc"`
	UpdatedAtUTC       time.Time `json:"updated_at_utc"`
	LastError          string    `json:"last_error,omitempty"`
	LatestSequence     int64     `json:"latest_sequence"`
}

type CloudDuplicateScanRunFilter struct {
	Status            string
	StartedFromUTC    string
	StartedThroughUTC string
}

type CloudDuplicateScanEvidenceBundle struct {
	Run    CloudDuplicateScanRun     `json:"run"`
	Events []CloudDuplicateScanEvent `json:"events"`
}

func syncCloudDuplicateScanRun(tx *sql.Tx, state CloudDuplicateScanState) error {
	var startSequence, latestSequence sql.NullInt64
	if err := tx.QueryRow(`SELECT min(sequence),max(sequence) FROM cloud_duplicate_scan_events WHERE account_fingerprint=? AND run_id=?`, state.AccountFingerprint, state.RunID).Scan(&startSequence, &latestSequence); err != nil {
		return err
	}
	started := state.StartedAtUTC.UTC().UnixMicro()
	if startSequence.Valid {
		if err := tx.QueryRow(`SELECT recorded_at_utc FROM cloud_duplicate_scan_events WHERE sequence=?`, startSequence.Int64).Scan(&started); err != nil {
			return err
		}
	}
	latest := int64(0)
	if latestSequence.Valid {
		latest = latestSequence.Int64
	}
	_, err := tx.Exec(`INSERT INTO cloud_duplicate_scan_runs(start_sequence,account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,total_bytes,processed_bytes,started_at_utc,updated_at_utc,last_error,latest_sequence) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint,run_id) DO UPDATE SET status=excluded.status,phase=excluded.phase,next_fs_id=excluded.next_fs_id,total_files=excluded.total_files,processed_files=excluded.processed_files,total_bytes=excluded.total_bytes,processed_bytes=excluded.processed_bytes,updated_at_utc=excluded.updated_at_utc,last_error=excluded.last_error,latest_sequence=excluded.latest_sequence`, nullableInt64Value(startSequence), state.AccountFingerprint, state.RunID, state.Status, state.Phase, state.NextFsID, state.TotalFiles, state.ProcessedFiles, state.TotalBytes, state.ProcessedBytes, started, state.UpdatedAtUTC.UTC().UnixMicro(), state.LastError, latest)
	return err
}

func nullableInt64Value(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func (s *Store) CloudDuplicateScanRunPage(account string, beforeStartSequence int64, filter CloudDuplicateScanRunFilter, limit int) ([]CloudDuplicateScanRun, int64, bool, error) {
	if account == "" || len(account) > 128 || beforeStartSequence < 0 || limit < 1 || limit > 100 || (filter.Status != "" && filter.Status != "running" && filter.Status != "paused" && filter.Status != "complete" && filter.Status != "failed") {
		return nil, 0, false, fmt.Errorf("invalid duplicate scan run query")
	}
	from, err := parseCloudDuplicateRunDate(filter.StartedFromUTC)
	if err != nil {
		return nil, 0, false, err
	}
	through, err := parseCloudDuplicateRunDate(filter.StartedThroughUTC)
	if err != nil {
		return nil, 0, false, err
	}
	if from != nil && through != nil && filter.StartedFromUTC > filter.StartedThroughUTC {
		return nil, 0, false, fmt.Errorf("invalid UTC start date range")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, false, os.ErrClosed
	}
	query := `SELECT start_sequence,account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,total_bytes,processed_bytes,started_at_utc,updated_at_utc,last_error,latest_sequence FROM cloud_duplicate_scan_runs WHERE account_fingerprint=?`
	args := []any{account}
	if filter.Status != "" {
		query += ` AND status=?`
		args = append(args, filter.Status)
	}
	if from != nil {
		query += ` AND started_at_utc>=?`
		args = append(args, from.UTC().UnixMicro())
	}
	if through != nil {
		query += ` AND started_at_utc<?`
		args = append(args, through.Add(24*time.Hour).UTC().UnixMicro())
	}
	if beforeStartSequence > 0 {
		query += ` AND start_sequence<?`
		args = append(args, beforeStartSequence)
	}
	query += ` ORDER BY start_sequence DESC,run_id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()
	items := make([]CloudDuplicateScanRun, 0, limit+1)
	for rows.Next() {
		var item CloudDuplicateScanRun
		var started, updated int64
		if err = rows.Scan(&item.StartSequence, &item.AccountFingerprint, &item.RunID, &item.Status, &item.Phase, &item.NextFsID, &item.TotalFiles, &item.ProcessedFiles, &item.TotalBytes, &item.ProcessedBytes, &started, &updated, &item.LastError, &item.LatestSequence); err != nil {
			return nil, 0, false, err
		}
		item.StartedAtUTC, item.UpdatedAtUTC = time.UnixMicro(started).UTC(), time.UnixMicro(updated).UTC()
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, false, err
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	var next int64
	if more && len(items) > 0 {
		next = items[len(items)-1].StartSequence
	}
	return items, next, more, nil
}

func parseCloudDuplicateRunDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return nil, fmt.Errorf("invalid UTC start date")
	}
	return &parsed, nil
}

func (s *Store) ExportCloudDuplicateScanRuns(ctx context.Context, account string, filter CloudDuplicateScanRunFilter) ([]CloudDuplicateScanRun, error) {
	if account == "" || len(account) > 128 || (filter.Status != "" && filter.Status != "running" && filter.Status != "paused" && filter.Status != "complete" && filter.Status != "failed") {
		return nil, fmt.Errorf("invalid duplicate scan run export")
	}
	from, err := parseCloudDuplicateRunDate(filter.StartedFromUTC)
	if err != nil {
		return nil, err
	}
	through, err := parseCloudDuplicateRunDate(filter.StartedThroughUTC)
	if err != nil {
		return nil, err
	}
	if from != nil && through != nil && filter.StartedFromUTC > filter.StartedThroughUTC {
		return nil, fmt.Errorf("invalid UTC start date range")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	query := `SELECT start_sequence,account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,total_bytes,processed_bytes,started_at_utc,updated_at_utc,last_error,latest_sequence FROM cloud_duplicate_scan_runs WHERE account_fingerprint=?`
	args := []any{account}
	if filter.Status != "" {
		query += ` AND status=?`
		args = append(args, filter.Status)
	}
	if from != nil {
		query += ` AND started_at_utc>=?`
		args = append(args, from.UTC().UnixMicro())
	}
	if through != nil {
		query += ` AND started_at_utc<?`
		args = append(args, through.Add(24*time.Hour).UTC().UnixMicro())
	}
	query += ` ORDER BY start_sequence DESC,run_id DESC LIMIT ?`
	args = append(args, CloudDuplicateScanSummaryExportLimit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	items := make([]CloudDuplicateScanRun, 0, CloudDuplicateScanSummaryExportLimit)
	for rows.Next() {
		var item CloudDuplicateScanRun
		var started, updated int64
		if err = rows.Scan(&item.StartSequence, &item.AccountFingerprint, &item.RunID, &item.Status, &item.Phase, &item.NextFsID, &item.TotalFiles, &item.ProcessedFiles, &item.TotalBytes, &item.ProcessedBytes, &started, &updated, &item.LastError, &item.LatestSequence); err != nil {
			rows.Close()
			return nil, err
		}
		item.StartedAtUTC, item.UpdatedAtUTC = time.UnixMicro(started).UTC(), time.UnixMicro(updated).UTC()
		items = append(items, item)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) > CloudDuplicateScanSummaryExportLimit {
		return nil, ErrCloudDuplicateScanSummaryExportTooLarge
	}
	return items, nil
}

func (s *Store) CloudDuplicateScanRunEvents(account, runID string, beforeSequence int64, limit int) ([]CloudDuplicateScanEvent, int64, bool, error) {
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 || beforeSequence < 0 || limit < 1 || limit > 50 {
		return nil, 0, false, fmt.Errorf("invalid duplicate scan run event query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, false, os.ErrClosed
	}
	var exists int
	if err := s.db.QueryRow(`SELECT count(*) FROM cloud_duplicate_scan_runs WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&exists); err != nil {
		return nil, 0, false, err
	}
	if exists == 0 {
		return nil, 0, false, ErrCloudDuplicateScanRunNotFound
	}
	rows, err := s.db.Query(`SELECT sequence,run_id,status,phase,recorded_at_utc,evidence_json FROM cloud_duplicate_scan_events WHERE account_fingerprint=? AND run_id=? AND (?=0 OR sequence<?) ORDER BY sequence DESC LIMIT ?`, account, runID, beforeSequence, beforeSequence, limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()
	events := make([]CloudDuplicateScanEvent, 0, limit+1)
	for rows.Next() {
		var event CloudDuplicateScanEvent
		var recorded int64
		if err = rows.Scan(&event.Sequence, &event.RunID, &event.Status, &event.Phase, &recorded, &event.Evidence); err != nil {
			return nil, 0, false, err
		}
		event.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, false, err
	}
	more := len(events) > limit
	if more {
		events = events[:limit]
	}
	var next int64
	if more && len(events) > 0 {
		next = events[len(events)-1].Sequence
	}
	return events, next, more, nil
}

func (s *Store) CloudDuplicateScanEvidenceExport(ctx context.Context, account, runID string) (CloudDuplicateScanEvidenceBundle, error) {
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 {
		return CloudDuplicateScanEvidenceBundle{}, fmt.Errorf("invalid duplicate scan evidence export")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return CloudDuplicateScanEvidenceBundle{}, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CloudDuplicateScanEvidenceBundle{}, err
	}
	defer tx.Rollback()
	var started, updated int64
	bundle := CloudDuplicateScanEvidenceBundle{}
	err = tx.QueryRowContext(ctx, `SELECT start_sequence,account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,total_bytes,processed_bytes,started_at_utc,updated_at_utc,last_error,latest_sequence FROM cloud_duplicate_scan_runs WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&bundle.Run.StartSequence, &bundle.Run.AccountFingerprint, &bundle.Run.RunID, &bundle.Run.Status, &bundle.Run.Phase, &bundle.Run.NextFsID, &bundle.Run.TotalFiles, &bundle.Run.ProcessedFiles, &bundle.Run.TotalBytes, &bundle.Run.ProcessedBytes, &started, &updated, &bundle.Run.LastError, &bundle.Run.LatestSequence)
	if errors.Is(err, sql.ErrNoRows) {
		return CloudDuplicateScanEvidenceBundle{}, ErrCloudDuplicateScanRunNotFound
	}
	if err != nil {
		return CloudDuplicateScanEvidenceBundle{}, err
	}
	bundle.Run.StartedAtUTC, bundle.Run.UpdatedAtUTC = time.UnixMicro(started).UTC(), time.UnixMicro(updated).UTC()
	rows, err := tx.QueryContext(ctx, `SELECT sequence,run_id,status,phase,recorded_at_utc,evidence_json FROM cloud_duplicate_scan_events WHERE account_fingerprint=? AND run_id=? ORDER BY sequence ASC LIMIT ?`, account, runID, CloudDuplicateScanEvidenceExportLimit+1)
	if err != nil {
		return CloudDuplicateScanEvidenceBundle{}, err
	}
	bundle.Events = make([]CloudDuplicateScanEvent, 0, CloudDuplicateScanEvidenceExportLimit)
	for rows.Next() {
		var event CloudDuplicateScanEvent
		var recorded int64
		if err = rows.Scan(&event.Sequence, &event.RunID, &event.Status, &event.Phase, &recorded, &event.Evidence); err != nil {
			rows.Close()
			return CloudDuplicateScanEvidenceBundle{}, err
		}
		event.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		bundle.Events = append(bundle.Events, event)
	}
	if err = rows.Close(); err != nil {
		return CloudDuplicateScanEvidenceBundle{}, err
	}
	if err = rows.Err(); err != nil {
		return CloudDuplicateScanEvidenceBundle{}, err
	}
	if len(bundle.Events) > CloudDuplicateScanEvidenceExportLimit {
		return CloudDuplicateScanEvidenceBundle{}, ErrCloudDuplicateScanEvidenceExportTooLarge
	}
	return bundle, nil
}
