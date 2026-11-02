package taskstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

const QueueMonitorScanEvidenceEventLimit = 10000

type QueueMonitorScanRun struct {
	Sequence           int64      `json:"sequence"`
	AccountFingerprint string     `json:"-"`
	RunID              string     `json:"run_id"`
	Status             string     `json:"status"`
	MonitorPath        string     `json:"monitor_path"`
	StartedAtUTC       time.Time  `json:"started_at_utc"`
	FinishedAtUTC      *time.Time `json:"finished_at_utc,omitempty"`
	CloudObservedAtUTC *time.Time `json:"cloud_observed_at_utc,omitempty"`
	RecordedAtUTC      time.Time  `json:"recorded_at_utc"`
	ErrorCode          string     `json:"error_code,omitempty"`
}

type QueueMonitorScanEvent struct {
	Sequence           int64      `json:"sequence"`
	AccountFingerprint string     `json:"-"`
	RunID              string     `json:"run_id"`
	Kind               string     `json:"kind"`
	RecordedAtUTC      time.Time  `json:"recorded_at_utc"`
	CloudObservedAtUTC *time.Time `json:"cloud_observed_at_utc,omitempty"`
	ErrorCode          string     `json:"error_code,omitempty"`
}

type QueueMonitorScanFilter struct{ Status, StartedFromUTC, StartedThroughUTC string }
type QueueMonitorScanBundle struct {
	Run    QueueMonitorScanRun     `json:"run"`
	Events []QueueMonitorScanEvent `json:"events"`
}

type QueueMonitorRetryState struct {
	FailureCount int
	ErrorCode    string
	RetryDelay   time.Duration
	RetryAtUTC   time.Time
	UpdatedAtUTC time.Time
}

func (s *Store) QueueMonitorRetryState(account string) (*QueueMonitorRetryState, error) {
	if account == "" || len(account) > 128 {
		return nil, fmt.Errorf("invalid queue monitor retry identity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	var state QueueMonitorRetryState
	var retryAt, updated int64
	var delayMillis int64
	err := s.db.QueryRow(`SELECT failure_count,error_code,retry_delay_millis,retry_at_utc,updated_at_utc FROM queue_monitor_retry_state WHERE account_fingerprint=?`, account).Scan(&state.FailureCount, &state.ErrorCode, &delayMillis, &retryAt, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	state.RetryDelay = time.Duration(delayMillis) * time.Millisecond
	state.RetryAtUTC = time.UnixMicro(retryAt).UTC()
	state.UpdatedAtUTC = time.UnixMicro(updated).UTC()
	return &state, nil
}

func (s *Store) ScheduleQueueMonitorRetry(account, errorCode string, failureCount int, delay time.Duration, now time.Time) (QueueMonitorRetryState, error) {
	if account == "" || len(account) > 128 || errorCode == "" || len(errorCode) > 64 || failureCount < 1 || delay <= 0 || now.IsZero() {
		return QueueMonitorRetryState{}, fmt.Errorf("invalid queue monitor retry state")
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	updated := now.UTC()
	state := QueueMonitorRetryState{FailureCount: failureCount, ErrorCode: errorCode, RetryDelay: delay, RetryAtUTC: updated.Add(delay), UpdatedAtUTC: updated}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return QueueMonitorRetryState{}, os.ErrClosed
	}
	_, err := s.db.Exec(`INSERT INTO queue_monitor_retry_state(account_fingerprint,failure_count,error_code,retry_delay_millis,retry_at_utc,updated_at_utc) VALUES(?,?,?,?,?,?) ON CONFLICT(account_fingerprint) DO UPDATE SET failure_count=excluded.failure_count,error_code=excluded.error_code,retry_delay_millis=excluded.retry_delay_millis,retry_at_utc=excluded.retry_at_utc,updated_at_utc=excluded.updated_at_utc`, account, failureCount, errorCode, delay.Milliseconds(), state.RetryAtUTC.UnixMicro(), updated.UnixMicro())
	return state, err
}

func (s *Store) ClearQueueMonitorRetryState(account string) error {
	if account == "" || len(account) > 128 {
		return fmt.Errorf("invalid queue monitor retry identity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	_, err := s.db.Exec(`DELETE FROM queue_monitor_retry_state WHERE account_fingerprint=?`, account)
	return err
}

type queueMonitorScanRow interface{ Scan(...any) error }

func scanQueueMonitorScanRun(row queueMonitorScanRow) (*QueueMonitorScanRun, error) {
	var r QueueMonitorScanRun
	var start, record int64
	var finish, cloud sql.NullInt64
	if err := row.Scan(&r.Sequence, &r.AccountFingerprint, &r.RunID, &r.Status, &r.MonitorPath, &start, &finish, &cloud, &record, &r.ErrorCode); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.StartedAtUTC = time.UnixMicro(start).UTC()
	r.RecordedAtUTC = time.UnixMicro(record).UTC()
	if finish.Valid {
		t := time.UnixMicro(finish.Int64).UTC()
		r.FinishedAtUTC = &t
	}
	if cloud.Valid {
		t := time.UnixMicro(cloud.Int64).UTC()
		r.CloudObservedAtUTC = &t
	}
	return &r, nil
}

// LatestQueueMonitorScanRuns reads the latest run and latest successful run
// from one SQLite snapshot so health cannot combine different scan moments.
func (s *Store) LatestQueueMonitorScanRuns(ctx context.Context, account string) (latest, latestSuccess *QueueMonitorScanRun, err error) {
	if account == "" || len(account) > 128 {
		return nil, nil, fmt.Errorf("invalid queue monitor scan identity")
	}
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	const columns = `sequence,account_fingerprint,run_id,status,monitor_path,started_at_utc,finished_at_utc,cloud_observed_at_utc,recorded_at_utc,error_code`
	latest, err = scanQueueMonitorScanRun(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM queue_monitor_scan_runs WHERE account_fingerprint=? ORDER BY sequence DESC LIMIT 1`, account))
	if err != nil {
		return nil, nil, err
	}
	latestSuccess, err = scanQueueMonitorScanRun(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM queue_monitor_scan_runs WHERE account_fingerprint=? AND status='success' ORDER BY sequence DESC LIMIT 1`, account))
	if err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return latest, latestSuccess, nil
}

func (s *Store) StartQueueMonitorScan(account, runID, monitorPath string, started time.Time) error {
	if account == "" || len(account) > 128 || len(runID) != 32 || monitorPath == "" || len(monitorPath) > 4096 || started.IsZero() {
		return fmt.Errorf("invalid queue monitor scan")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	startedMicros := started.UTC().UnixMicro()
	rows, err := tx.Query(`SELECT run_id FROM queue_monitor_scan_runs WHERE account_fingerprint=? AND status='running'`, account)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		stale = append(stale, id)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range stale {
		if _, err = tx.Exec(`UPDATE queue_monitor_scan_runs SET status='interrupted',finished_at_utc=?,recorded_at_utc=?,error_code='scan_interrupted' WHERE account_fingerprint=? AND run_id=? AND status='running'`, startedMicros, startedMicros, account, id); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO queue_monitor_scan_events(account_fingerprint,run_id,event_kind,recorded_at_utc,error_code) VALUES(?,?,'interrupted',?,'scan_interrupted')`, account, id, startedMicros); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT INTO queue_monitor_scan_runs(account_fingerprint,run_id,status,monitor_path,started_at_utc,recorded_at_utc) VALUES(?,?,'running',?,?,?)`, account, runID, monitorPath, startedMicros, startedMicros); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO queue_monitor_scan_events(account_fingerprint,run_id,event_kind,recorded_at_utc) VALUES(?,?,'started',?)`, account, runID, startedMicros); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecordQueueMonitorCloudListed(account, runID string, observed time.Time) error {
	if account == "" || len(account) > 128 || len(runID) != 32 || observed.IsZero() {
		return fmt.Errorf("invalid queue monitor cloud observation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := observed.UTC().UnixMicro()
	res, err := tx.Exec(`UPDATE queue_monitor_scan_runs SET cloud_observed_at_utc=?,recorded_at_utc=? WHERE account_fingerprint=? AND run_id=? AND status='running'`, at, at, account, runID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("queue monitor scan is not running")
	}
	if _, err = tx.Exec(`INSERT INTO queue_monitor_scan_events(account_fingerprint,run_id,event_kind,recorded_at_utc,cloud_observed_at_utc) VALUES(?,?,'cloud_listed',?,?)`, account, runID, at, at); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FinishQueueMonitorScan(account, runID, status, errorCode string, finished time.Time) error {
	if account == "" || len(account) > 128 || len(runID) != 32 || (status != "success" && status != "failed") || finished.IsZero() || len(errorCode) > 64 || (status == "success" && errorCode != "") || (status == "failed" && errorCode == "") {
		return fmt.Errorf("invalid queue monitor scan result")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := finished.UTC().UnixMicro()
	res, err := tx.Exec(`UPDATE queue_monitor_scan_runs SET status=?,finished_at_utc=?,recorded_at_utc=?,error_code=? WHERE account_fingerprint=? AND run_id=? AND status='running'`, status, at, at, errorCode, account, runID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("queue monitor scan is not running")
	}
	kind := "completed"
	if status == "failed" {
		kind = "failed"
	}
	if _, err = tx.Exec(`INSERT INTO queue_monitor_scan_events(account_fingerprint,run_id,event_kind,recorded_at_utc,error_code) VALUES(?,?,?,?,?)`, account, runID, kind, at, errorCode); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListQueueMonitorScanRuns(account string, before int64, filter QueueMonitorScanFilter, limit int) ([]QueueMonitorScanRun, error) {
	if account == "" || len(account) > 128 || before < 0 || limit < 1 || limit > 101 {
		return nil, fmt.Errorf("invalid queue monitor scan query")
	}
	if filter.Status != "" && filter.Status != "running" && filter.Status != "success" && filter.Status != "failed" && filter.Status != "interrupted" {
		return nil, fmt.Errorf("invalid queue monitor scan status")
	}
	var from, through *time.Time
	parse := func(v string) (*time.Time, error) {
		if v == "" {
			return nil, nil
		}
		t, e := time.Parse("2006-01-02", v)
		if e != nil || t.Format("2006-01-02") != v {
			return nil, fmt.Errorf("invalid UTC date")
		}
		return &t, nil
	}
	var err error
	if from, err = parse(filter.StartedFromUTC); err != nil {
		return nil, err
	}
	if through, err = parse(filter.StartedThroughUTC); err != nil {
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
	q := `SELECT sequence,account_fingerprint,run_id,status,monitor_path,started_at_utc,finished_at_utc,cloud_observed_at_utc,recorded_at_utc,error_code FROM queue_monitor_scan_runs WHERE account_fingerprint=?`
	args := []any{account}
	if filter.Status != "" {
		q += ` AND status=?`
		args = append(args, filter.Status)
	}
	if from != nil {
		q += ` AND started_at_utc>=?`
		args = append(args, from.UTC().UnixMicro())
	}
	if through != nil {
		q += ` AND started_at_utc<?`
		args = append(args, through.Add(24*time.Hour).UTC().UnixMicro())
	}
	if before > 0 {
		q += ` AND sequence<?`
		args = append(args, before)
	}
	q += ` ORDER BY sequence DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueMonitorScanRun{}
	for rows.Next() {
		var r QueueMonitorScanRun
		var start, record int64
		var finish, cloud sql.NullInt64
		if err = rows.Scan(&r.Sequence, &r.AccountFingerprint, &r.RunID, &r.Status, &r.MonitorPath, &start, &finish, &cloud, &record, &r.ErrorCode); err != nil {
			return nil, err
		}
		r.StartedAtUTC = time.UnixMicro(start).UTC()
		r.RecordedAtUTC = time.UnixMicro(record).UTC()
		if finish.Valid {
			t := time.UnixMicro(finish.Int64).UTC()
			r.FinishedAtUTC = &t
		}
		if cloud.Valid {
			t := time.UnixMicro(cloud.Int64).UTC()
			r.CloudObservedAtUTC = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListQueueMonitorScanEvents(account, runID string, before int64, limit int) ([]QueueMonitorScanEvent, error) {
	if account == "" || len(account) > 128 || len(runID) != 32 || before < 0 || limit < 1 || limit > 101 {
		return nil, fmt.Errorf("invalid queue monitor scan event query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	q := `SELECT sequence,account_fingerprint,run_id,event_kind,recorded_at_utc,cloud_observed_at_utc,error_code FROM queue_monitor_scan_events WHERE account_fingerprint=? AND run_id=?`
	args := []any{account, runID}
	if before > 0 {
		q += ` AND sequence<?`
		args = append(args, before)
	}
	q += ` ORDER BY sequence DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueMonitorScanEvent{}
	for rows.Next() {
		var e QueueMonitorScanEvent
		var recorded int64
		var cloud sql.NullInt64
		if err = rows.Scan(&e.Sequence, &e.AccountFingerprint, &e.RunID, &e.Kind, &recorded, &cloud, &e.ErrorCode); err != nil {
			return nil, err
		}
		e.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		if cloud.Valid {
			t := time.UnixMicro(cloud.Int64).UTC()
			e.CloudObservedAtUTC = &t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) ExportQueueMonitorScanEvidence(ctx context.Context, account, runID string) (QueueMonitorScanBundle, error) {
	b := QueueMonitorScanBundle{Events: []QueueMonitorScanEvent{}}
	if account == "" || len(account) > 128 || len(runID) != 32 {
		return b, fmt.Errorf("invalid queue monitor scan evidence identity")
	}
	if err := ctx.Err(); err != nil {
		return b, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return b, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	var start, record int64
	var finish, cloud sql.NullInt64
	r := &b.Run
	if err = tx.QueryRowContext(ctx, `SELECT sequence,account_fingerprint,run_id,status,monitor_path,started_at_utc,finished_at_utc,cloud_observed_at_utc,recorded_at_utc,error_code FROM queue_monitor_scan_runs WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&r.Sequence, &r.AccountFingerprint, &r.RunID, &r.Status, &r.MonitorPath, &start, &finish, &cloud, &record, &r.ErrorCode); err != nil {
		return b, err
	}
	r.StartedAtUTC = time.UnixMicro(start).UTC()
	r.RecordedAtUTC = time.UnixMicro(record).UTC()
	if finish.Valid {
		t := time.UnixMicro(finish.Int64).UTC()
		r.FinishedAtUTC = &t
	}
	if cloud.Valid {
		t := time.UnixMicro(cloud.Int64).UTC()
		r.CloudObservedAtUTC = &t
	}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,account_fingerprint,run_id,event_kind,recorded_at_utc,cloud_observed_at_utc,error_code FROM queue_monitor_scan_events WHERE account_fingerprint=? AND run_id=? ORDER BY sequence LIMIT ?`, account, runID, QueueMonitorScanEvidenceEventLimit+1)
	if err != nil {
		return b, err
	}
	defer rows.Close()
	for rows.Next() {
		var e QueueMonitorScanEvent
		var at int64
		var c sql.NullInt64
		if err = rows.Scan(&e.Sequence, &e.AccountFingerprint, &e.RunID, &e.Kind, &at, &c, &e.ErrorCode); err != nil {
			return b, err
		}
		e.RecordedAtUTC = time.UnixMicro(at).UTC()
		if c.Valid {
			t := time.UnixMicro(c.Int64).UTC()
			e.CloudObservedAtUTC = &t
		}
		b.Events = append(b.Events, e)
	}
	if err = rows.Err(); err != nil {
		return b, err
	}
	if len(b.Events) > QueueMonitorScanEvidenceEventLimit {
		return QueueMonitorScanBundle{}, ErrCloudScanEvidenceTooLarge
	}
	if err = tx.Commit(); err != nil {
		return QueueMonitorScanBundle{}, err
	}
	return b, nil
}
