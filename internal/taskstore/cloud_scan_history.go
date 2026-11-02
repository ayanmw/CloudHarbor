package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

var ErrCloudScanEvidenceTooLarge = errors.New("cloud scan evidence exceeds export limit")

const CloudScanEvidenceEventLimit = 10000

type CloudScanEvidenceBundle struct {
	Run    CloudScanRun     `json:"run"`
	Events []CloudScanEvent `json:"events"`
}

type CloudScanRunFilter struct {
	Status                string
	StartedFromUTC        string
	StartedThroughUTC     string
	CoverageStatus        string
	CoverageIncompleteMin *int64
	CoverageIncompleteMax *int64
	CoverageReason        string
}

type CloudScanRun struct {
	Sequence                      int64                          `json:"sequence"`
	AccountFingerprint            string                         `json:"-"`
	RunID                         string                         `json:"run_id"`
	Status                        string                         `json:"status"`
	Phase                         string                         `json:"phase"`
	CurrentDirectory              string                         `json:"current_directory"`
	PagesProcessed                int64                          `json:"pages_processed"`
	ItemsProcessed                int64                          `json:"items_processed"`
	RequestBudget                 int64                          `json:"request_budget"`
	RequestsUsed                  int64                          `json:"requests_used"`
	PendingCount                  int64                          `json:"pending_count"`
	StartedAtUTC                  time.Time                      `json:"started_at_utc"`
	UpdatedAtUTC                  time.Time                      `json:"updated_at_utc"`
	LastError                     string                         `json:"last_error,omitempty"`
	CoverageStatus                *string                        `json:"coverage_status"`
	CoverageIncompleteDirectories *int64                         `json:"coverage_incomplete_directories"`
	CoverageGeneration            *int64                         `json:"coverage_generation"`
	CoverageObservedAtUTC         *time.Time                     `json:"coverage_observed_at_utc"`
	CoverageReasons               *CloudInventoryCoverageReasons `json:"coverage_reasons,omitempty"`
	CoverageComparison            *CloudScanCoverageComparison   `json:"coverage_comparison,omitempty"`
}

type CloudScanCoverageComparison struct {
	Status                        string                         `json:"status"`
	ReasonStatus                  string                         `json:"reason_status,omitempty"`
	PreviousRunID                 string                         `json:"previous_run_id,omitempty"`
	PreviousSequence              int64                          `json:"previous_sequence,omitempty"`
	PreviousObservedAtUTC         *time.Time                     `json:"previous_observed_at_utc,omitempty"`
	PreviousCoverageStatus        *string                        `json:"previous_coverage_status,omitempty"`
	PreviousIncompleteDirectories *int64                         `json:"previous_incomplete_directories,omitempty"`
	PreviousReasons               *CloudInventoryCoverageReasons `json:"previous_reasons,omitempty"`
	IncompleteDirectoriesDelta    *int64                         `json:"incomplete_directories_delta,omitempty"`
	ReasonDeltas                  *CloudInventoryCoverageReasons `json:"reason_deltas,omitempty"`
}

func BuildCloudScanCoverageComparison(current CloudScanRun, previous *CloudScanRun) *CloudScanCoverageComparison {
	comparison := &CloudScanCoverageComparison{}
	if previous == nil {
		comparison.Status = "no_previous_run"
		return comparison
	}
	comparison.PreviousRunID = previous.RunID
	comparison.PreviousSequence = previous.Sequence
	comparison.PreviousObservedAtUTC = previous.CoverageObservedAtUTC
	comparison.PreviousCoverageStatus = previous.CoverageStatus
	comparison.PreviousIncompleteDirectories = previous.CoverageIncompleteDirectories
	comparison.PreviousReasons = previous.CoverageReasons
	if current.CoverageStatus == nil || current.CoverageIncompleteDirectories == nil || current.CoverageObservedAtUTC == nil {
		comparison.Status = "current_snapshot_unrecorded"
		return comparison
	}
	if previous.CoverageStatus == nil || previous.CoverageIncompleteDirectories == nil || previous.CoverageObservedAtUTC == nil {
		comparison.Status = "previous_snapshot_unrecorded"
		return comparison
	}
	comparison.Status = "comparable"
	incompleteDelta := *current.CoverageIncompleteDirectories - *previous.CoverageIncompleteDirectories
	comparison.IncompleteDirectoriesDelta = &incompleteDelta
	if current.CoverageReasons == nil || previous.CoverageReasons == nil {
		comparison.ReasonStatus = "reasons_unrecorded"
		return comparison
	}
	comparison.ReasonStatus = "comparable"
	comparison.ReasonDeltas = &CloudInventoryCoverageReasons{
		ScanningDirectories:    current.CoverageReasons.ScanningDirectories - previous.CoverageReasons.ScanningDirectories,
		StaleDirectories:       current.CoverageReasons.StaleDirectories - previous.CoverageReasons.StaleDirectories,
		UnknownDirectories:     current.CoverageReasons.UnknownDirectories - previous.CoverageReasons.UnknownDirectories,
		FailedDirectories:      current.CoverageReasons.FailedDirectories - previous.CoverageReasons.FailedDirectories,
		MissingDirectoryStatus: current.CoverageReasons.MissingDirectoryStatus - previous.CoverageReasons.MissingDirectoryStatus,
		OrphanDirectoryStatus:  current.CoverageReasons.OrphanDirectoryStatus - previous.CoverageReasons.OrphanDirectoryStatus,
		ParentNotComplete:      current.CoverageReasons.ParentNotComplete - previous.CoverageReasons.ParentNotComplete,
		PathMismatch:           current.CoverageReasons.PathMismatch - previous.CoverageReasons.PathMismatch,
		MissingRoot:            current.CoverageReasons.MissingRoot - previous.CoverageReasons.MissingRoot,
	}
	return comparison
}

type CloudScanEvent struct {
	Sequence           int64     `json:"sequence"`
	AccountFingerprint string    `json:"-"`
	RunID              string    `json:"run_id"`
	Kind               string    `json:"kind"`
	Status             string    `json:"status"`
	Phase              string    `json:"phase"`
	CurrentDirectory   string    `json:"current_directory"`
	PagesProcessed     int64     `json:"pages_processed"`
	ItemsProcessed     int64     `json:"items_processed"`
	RequestBudget      int64     `json:"request_budget"`
	RequestsUsed       int64     `json:"requests_used"`
	PendingCount       int64     `json:"pending_count"`
	RecordedAtUTC      time.Time `json:"recorded_at_utc"`
	LastError          string    `json:"last_error,omitempty"`
	RestartReason      string    `json:"restart_reason,omitempty"`
}

type cloudScanRunRowScanner interface {
	Scan(dest ...any) error
}

func scanCloudScanRun(row cloudScanRunRowScanner) (CloudScanRun, error) {
	var item CloudScanRun
	var started, updated int64
	var coverageObserved sql.NullInt64
	var coverageReasons sql.NullString
	err := row.Scan(&item.Sequence, &item.AccountFingerprint, &item.RunID, &item.Status, &item.Phase, &item.CurrentDirectory, &item.PagesProcessed, &item.ItemsProcessed, &item.RequestBudget, &item.RequestsUsed, &item.PendingCount, &started, &updated, &item.LastError, &item.CoverageStatus, &item.CoverageIncompleteDirectories, &item.CoverageGeneration, &coverageObserved, &coverageReasons)
	if err != nil {
		return CloudScanRun{}, err
	}
	item.StartedAtUTC = time.UnixMicro(started).UTC()
	item.UpdatedAtUTC = time.UnixMicro(updated).UTC()
	if coverageObserved.Valid {
		observed := time.UnixMicro(coverageObserved.Int64).UTC()
		item.CoverageObservedAtUTC = &observed
	}
	if coverageReasons.Valid {
		var reasons CloudInventoryCoverageReasons
		if err = json.Unmarshal([]byte(coverageReasons.String), &reasons); err != nil {
			return CloudScanRun{}, fmt.Errorf("decode cloud scan coverage reasons: %w", err)
		}
		item.CoverageReasons = &reasons
	}
	return item, nil
}

func previousCompletedCloudScanRun(queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, ctx context.Context, account string, beforeSequence int64) (*CloudScanRun, error) {
	previous, err := scanCloudScanRun(queryer.QueryRowContext(ctx, `SELECT sequence,account_fingerprint,run_id,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error,coverage_status,coverage_incomplete_directories,coverage_generation,coverage_observed_at_utc,coverage_reasons_json FROM cloud_scan_runs WHERE account_fingerprint=? AND status='complete' AND sequence<? ORDER BY sequence DESC LIMIT 1`, account, beforeSequence))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &previous, nil
}

func createCloudScanHistory(tx *sql.Tx, state CloudScanState) error {
	_, err := tx.Exec(`INSERT INTO cloud_scan_runs(account_fingerprint,run_id,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, state.AccountFingerprint, state.RunID, state.Status, state.Phase, state.CurrentDirectory, state.PagesProcessed, state.ItemsProcessed, state.RequestBudget, state.RequestsUsed, state.PendingCount, state.StartedAtUTC.UTC().UnixMicro(), state.UpdatedAtUTC.UTC().UnixMicro(), state.LastError)
	if err != nil {
		return err
	}
	return appendCloudScanEvent(tx, state, "started", state.UpdatedAtUTC)
}

func appendCloudScanEvent(tx *sql.Tx, state CloudScanState, kind string, recordedAt time.Time, restartReason ...string) error {
	if recordedAt.IsZero() {
		return fmt.Errorf("invalid cloud scan event time")
	}
	reason := ""
	if len(restartReason) > 0 {
		reason = restartReason[0]
	}
	if len(restartReason) > 1 || (kind == "boundary_restart") != (reason != "") || (reason != "" && reason != "page_boundary_mismatch" && reason != "provider_duplicate_page_id") {
		return fmt.Errorf("invalid cloud scan restart reason")
	}
	_, err := tx.Exec(`INSERT INTO cloud_inventory_scan_events(account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc,last_error,restart_reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, state.AccountFingerprint, state.RunID, kind, state.Status, state.Phase, state.CurrentDirectory, state.PagesProcessed, state.ItemsProcessed, state.RequestBudget, state.RequestsUsed, state.PendingCount, recordedAt.UTC().UnixMicro(), state.LastError, reason)
	return err
}

func updateCloudScanRun(tx *sql.Tx, state CloudScanState) (bool, error) {
	result, err := tx.Exec(`UPDATE cloud_scan_runs SET status=?,phase=?,current_directory=?,pages_processed=?,items_processed=?,request_budget=?,requests_used=?,pending_count=?,updated_at_utc=?,last_error=? WHERE account_fingerprint=? AND run_id=?`, state.Status, state.Phase, state.CurrentDirectory, state.PagesProcessed, state.ItemsProcessed, state.RequestBudget, state.RequestsUsed, state.PendingCount, state.UpdatedAtUTC.UTC().UnixMicro(), state.LastError, state.AccountFingerprint, state.RunID)
	if err != nil {
		return false, err
	}
	n, e := result.RowsAffected()
	return n == 1, e
}

func ensureCloudScanRun(tx *sql.Tx, state CloudScanState) (bool, error) {
	result, err := tx.Exec(`INSERT OR IGNORE INTO cloud_scan_runs(account_fingerprint,run_id,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, state.AccountFingerprint, state.RunID, state.Status, state.Phase, state.CurrentDirectory, state.PagesProcessed, state.ItemsProcessed, state.RequestBudget, state.RequestsUsed, state.PendingCount, state.StartedAtUTC.UTC().UnixMicro(), state.UpdatedAtUTC.UTC().UnixMicro(), state.LastError)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func cloudScanTransitionEvent(previousStatus, previousPhase string, state CloudScanState) string {
	if state.Phase == "request_budget_reached" && previousPhase != "request_budget_reached" {
		return "request_budget_reached"
	}
	if state.Status == "complete" && previousStatus != "complete" {
		return "completed"
	}
	if state.Status == "failed" && previousStatus != "failed" {
		return "failed"
	}
	if state.Status == "paused" && previousStatus != "paused" {
		return "paused"
	}
	if state.Status == "running" && (previousStatus == "paused" || previousStatus == "failed") {
		return "resumed"
	}
	if state.Phase == "backoff" && previousPhase != "backoff" {
		return "retry_scheduled"
	}
	return ""
}

func (s *Store) ListCloudScanRuns(account string, beforeSequence int64, limit int) ([]CloudScanRun, error) {
	return s.ListCloudScanRunsFiltered(account, beforeSequence, CloudScanRunFilter{}, limit)
}

func (s *Store) ListCloudScanRunsFiltered(account string, beforeSequence int64, filter CloudScanRunFilter, limit int) ([]CloudScanRun, error) {
	if account == "" || len(account) > 128 || beforeSequence < 0 || limit < 1 || limit > 101 || (filter.Status != "" && filter.Status != "running" && filter.Status != "paused" && filter.Status != "complete" && filter.Status != "failed") || ValidateCloudScanRunCoverageFilter(filter) != nil {
		return nil, fmt.Errorf("invalid cloud scan run query")
	}
	var from, through *time.Time
	parseDate := func(value string) (*time.Time, error) {
		if value == "" {
			return nil, nil
		}
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil || parsed.Format("2006-01-02") != value {
			return nil, fmt.Errorf("invalid UTC date filter")
		}
		return &parsed, nil
	}
	var err error
	if from, err = parseDate(filter.StartedFromUTC); err != nil {
		return nil, err
	}
	if through, err = parseDate(filter.StartedThroughUTC); err != nil {
		return nil, err
	}
	if from != nil && through != nil && filter.StartedFromUTC > filter.StartedThroughUTC {
		return nil, fmt.Errorf("invalid UTC date range")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	query := `SELECT sequence,account_fingerprint,run_id,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error,coverage_status,coverage_incomplete_directories,coverage_generation,coverage_observed_at_utc,coverage_reasons_json FROM cloud_scan_runs WHERE account_fingerprint=?`
	args := []any{account}
	if filter.CoverageStatus == "unrecorded" {
		query += ` AND coverage_status IS NULL`
	} else if filter.CoverageStatus != "" {
		query += ` AND coverage_status=?`
		args = append(args, filter.CoverageStatus)
	}
	if filter.CoverageIncompleteMin != nil {
		query += ` AND coverage_incomplete_directories IS NOT NULL AND coverage_incomplete_directories>=?`
		args = append(args, *filter.CoverageIncompleteMin)
	}
	if filter.CoverageIncompleteMax != nil {
		query += ` AND coverage_incomplete_directories IS NOT NULL AND coverage_incomplete_directories<=?`
		args = append(args, *filter.CoverageIncompleteMax)
	}
	if filter.CoverageReason == "unrecorded" {
		query += ` AND coverage_reasons_json IS NULL`
	} else if filter.CoverageReason != "" {
		query += ` AND coverage_reasons_json IS NOT NULL AND COALESCE(json_extract(coverage_reasons_json,?),0)>0`
		args = append(args, cloudScanCoverageReasonJSONPath(filter.CoverageReason))
	}
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
	if beforeSequence > 0 {
		query += ` AND sequence<?`
		args = append(args, beforeSequence)
	}
	query += ` ORDER BY sequence DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []CloudScanRun{}
	for rows.Next() {
		var item CloudScanRun
		var started, updated int64
		var coverageObserved sql.NullInt64
		var coverageReasons sql.NullString
		if err = rows.Scan(&item.Sequence, &item.AccountFingerprint, &item.RunID, &item.Status, &item.Phase, &item.CurrentDirectory, &item.PagesProcessed, &item.ItemsProcessed, &item.RequestBudget, &item.RequestsUsed, &item.PendingCount, &started, &updated, &item.LastError, &item.CoverageStatus, &item.CoverageIncompleteDirectories, &item.CoverageGeneration, &coverageObserved, &coverageReasons); err != nil {
			return nil, err
		}
		item.StartedAtUTC = time.UnixMicro(started).UTC()
		item.UpdatedAtUTC = time.UnixMicro(updated).UTC()
		if coverageObserved.Valid {
			observed := time.UnixMicro(coverageObserved.Int64).UTC()
			item.CoverageObservedAtUTC = &observed
		}
		if coverageReasons.Valid {
			var reasons CloudInventoryCoverageReasons
			if err = json.Unmarshal([]byte(coverageReasons.String), &reasons); err != nil {
				return nil, fmt.Errorf("decode cloud scan coverage reasons: %w", err)
			}
			item.CoverageReasons = &reasons
		}
		runs = append(runs, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for i := range runs {
		if runs[i].Status != "complete" {
			continue
		}
		previous, previousErr := previousCompletedCloudScanRun(s.db, context.Background(), account, runs[i].Sequence)
		if previousErr != nil {
			return nil, previousErr
		}
		runs[i].CoverageComparison = BuildCloudScanCoverageComparison(runs[i], previous)
	}
	return runs, nil
}

func ValidateCloudScanRunCoverageFilter(filter CloudScanRunFilter) error {
	if !validCloudScanCoverageStatusFilter(filter.CoverageStatus) || !validCloudScanCoverageReasonFilter(filter.CoverageReason) {
		return fmt.Errorf("invalid cloud scan coverage filter")
	}
	for _, value := range []*int64{filter.CoverageIncompleteMin, filter.CoverageIncompleteMax} {
		if value != nil && (*value < 0 || *value > 1000000) {
			return fmt.Errorf("invalid cloud scan coverage count filter")
		}
	}
	if filter.CoverageIncompleteMin != nil && filter.CoverageIncompleteMax != nil && *filter.CoverageIncompleteMin > *filter.CoverageIncompleteMax {
		return fmt.Errorf("invalid cloud scan coverage count range")
	}
	return nil
}

func validCloudScanCoverageStatusFilter(value string) bool {
	switch value {
	case "", "unrecorded", "unknown", "scanning", "incomplete", "complete":
		return true
	default:
		return false
	}
}

func validCloudScanCoverageReasonFilter(value string) bool {
	_, ok := cloudScanCoverageReasonJSONPaths[value]
	return value == "" || value == "unrecorded" || ok
}

var cloudScanCoverageReasonJSONPaths = map[string]string{
	"scanning_directories":     "$.scanning_directories",
	"stale_directories":        "$.stale_directories",
	"unknown_directories":      "$.unknown_directories",
	"failed_directories":       "$.failed_directories",
	"missing_directory_status": "$.missing_directory_status",
	"orphan_directory_status":  "$.orphan_directory_status",
	"parent_not_complete":      "$.parent_not_complete",
	"path_mismatch":            "$.path_mismatch",
	"missing_root":             "$.missing_root",
}

func cloudScanCoverageReasonJSONPath(reason string) string {
	return cloudScanCoverageReasonJSONPaths[reason]
}

func (s *Store) ListCloudScanEvents(account, runID string, beforeSequence int64, limit int) ([]CloudScanEvent, error) {
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 || beforeSequence < 0 || limit < 1 || limit > 101 {
		return nil, fmt.Errorf("invalid cloud scan event query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	query := `SELECT sequence,account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc,last_error,restart_reason FROM cloud_inventory_scan_events WHERE account_fingerprint=? AND run_id=?`
	args := []any{account, runID}
	if beforeSequence > 0 {
		query += ` AND sequence<?`
		args = append(args, beforeSequence)
	}
	query += ` ORDER BY sequence DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []CloudScanEvent{}
	for rows.Next() {
		var item CloudScanEvent
		var recorded int64
		if err = rows.Scan(&item.Sequence, &item.AccountFingerprint, &item.RunID, &item.Kind, &item.Status, &item.Phase, &item.CurrentDirectory, &item.PagesProcessed, &item.ItemsProcessed, &item.RequestBudget, &item.RequestsUsed, &item.PendingCount, &recorded, &item.LastError, &item.RestartReason); err != nil {
			return nil, err
		}
		item.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		events = append(events, item)
	}
	return events, rows.Err()
}

func (s *Store) ExportCloudScanEvidence(ctx context.Context, account, runID string) (CloudScanEvidenceBundle, error) {
	bundle := CloudScanEvidenceBundle{Events: []CloudScanEvent{}}
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 {
		return bundle, fmt.Errorf("invalid cloud scan evidence identity")
	}
	if err := ctx.Err(); err != nil {
		return bundle, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return bundle, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return bundle, err
	}
	defer tx.Rollback()
	var started, updated int64
	var coverageObserved sql.NullInt64
	var coverageReasons sql.NullString
	r := &bundle.Run
	if err = tx.QueryRowContext(ctx, `SELECT sequence,account_fingerprint,run_id,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error,coverage_status,coverage_incomplete_directories,coverage_generation,coverage_observed_at_utc,coverage_reasons_json FROM cloud_scan_runs WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&r.Sequence, &r.AccountFingerprint, &r.RunID, &r.Status, &r.Phase, &r.CurrentDirectory, &r.PagesProcessed, &r.ItemsProcessed, &r.RequestBudget, &r.RequestsUsed, &r.PendingCount, &started, &updated, &r.LastError, &r.CoverageStatus, &r.CoverageIncompleteDirectories, &r.CoverageGeneration, &coverageObserved, &coverageReasons); err != nil {
		return bundle, err
	}
	r.StartedAtUTC = time.UnixMicro(started).UTC()
	r.UpdatedAtUTC = time.UnixMicro(updated).UTC()
	if coverageObserved.Valid {
		observed := time.UnixMicro(coverageObserved.Int64).UTC()
		r.CoverageObservedAtUTC = &observed
	}
	if coverageReasons.Valid {
		var reasons CloudInventoryCoverageReasons
		if err = json.Unmarshal([]byte(coverageReasons.String), &reasons); err != nil {
			return bundle, fmt.Errorf("decode cloud scan coverage reasons: %w", err)
		}
		r.CoverageReasons = &reasons
	}
	if r.Status == "complete" {
		previous, previousErr := previousCompletedCloudScanRun(tx, ctx, account, r.Sequence)
		if previousErr != nil {
			return bundle, previousErr
		}
		r.CoverageComparison = BuildCloudScanCoverageComparison(*r, previous)
	}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc,last_error FROM cloud_inventory_scan_events WHERE account_fingerprint=? AND run_id=? ORDER BY sequence LIMIT ?`, account, runID, CloudScanEvidenceEventLimit+1)
	if err != nil {
		return bundle, err
	}
	defer rows.Close()
	for rows.Next() {
		var e CloudScanEvent
		var recorded int64
		if err = rows.Scan(&e.Sequence, &e.AccountFingerprint, &e.RunID, &e.Kind, &e.Status, &e.Phase, &e.CurrentDirectory, &e.PagesProcessed, &e.ItemsProcessed, &e.RequestBudget, &e.RequestsUsed, &e.PendingCount, &recorded, &e.LastError); err != nil {
			return bundle, err
		}
		e.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		bundle.Events = append(bundle.Events, e)
	}
	if err = rows.Err(); err != nil {
		return bundle, err
	}
	if len(bundle.Events) > CloudScanEvidenceEventLimit {
		return CloudScanEvidenceBundle{}, ErrCloudScanEvidenceTooLarge
	}
	if err = tx.Commit(); err != nil {
		return CloudScanEvidenceBundle{}, err
	}
	return bundle, nil
}
