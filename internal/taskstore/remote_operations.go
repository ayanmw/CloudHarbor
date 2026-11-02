package taskstore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrRemoteOperationNotFound = errors.New("remote operation not found")
var ErrRemoteOperationExportTooLarge = errors.New("remote operation export exceeds limit")
var ErrRemoteOperationEvidenceExportTooLarge = errors.New("remote operation evidence export exceeds limit")

const remoteOperationLocalReconciliationCompleteEvidence = `{"event":"local_reconciliation_complete"}`
const remoteOperationLocalCacheRecoveryCompleteEvidence = `{"event":"local_cache_recovery_complete"}`

const maxRemoteOperationPageSize = 50
const RemoteOperationExportLimit = 5000
const RemoteOperationEvidenceExportLimit = 100

type RemoteOperation struct {
	ID                    string          `json:"operation_id"`
	AccountFingerprint    string          `json:"account_fingerprint"`
	Kind                  string          `json:"kind"`
	ObjectID              int64           `json:"object_id"`
	SourcePath            string          `json:"source_path"`
	DestinationPath       string          `json:"destination_path"`
	Status                string          `json:"status"`
	CreatedAtUTC          time.Time       `json:"created_at_utc"`
	ResponseObservedAtUTC *time.Time      `json:"response_observed_at_utc,omitempty"`
	Evidence              json.RawMessage `json:"evidence"`
}

type ConfirmedRemoteDeleteReceipt struct {
	OperationID           string
	AccountFingerprint    string
	ObjectID              int64
	SourcePath            string
	CreatedAtUTC          time.Time
	ResponseObservedAtUTC time.Time
	PreparedEvidence      json.RawMessage
}

// RemoteOperationSummary omits account-scoped and evidence fields from list responses.
type RemoteOperationSummary struct {
	ID                    string     `json:"operation_id"`
	Kind                  string     `json:"kind"`
	ObjectID              int64      `json:"object_id"`
	SourcePath            string     `json:"source_path"`
	DestinationPath       string     `json:"destination_path"`
	Status                string     `json:"status"`
	CreatedAtUTC          time.Time  `json:"created_at_utc"`
	ResponseObservedAtUTC *time.Time `json:"response_observed_at_utc,omitempty"`
}

type RemoteOperationFilter struct {
	Status  string
	Kind    string
	Query   string
	FromUTC *time.Time
	ToUTC   *time.Time
}

// RemoteOperationCursor is the final row key from a descending history page.
type RemoteOperationCursor struct {
	CreatedAtUTC time.Time `json:"created_at_utc"`
	ID           string    `json:"operation_id"`
}

// RemoteOperationEventFilter selects one operation's evidence events by status and UTC record time.
type RemoteOperationEventFilter struct {
	Status            string
	RecordedFromUTC   *time.Time
	RecordedBeforeUTC *time.Time
}

type RemoteOperationEvent struct {
	Sequence              int64           `json:"sequence"`
	Status                string          `json:"status"`
	RecordedAtUTC         time.Time       `json:"recorded_at_utc"`
	ResponseObservedAtUTC *time.Time      `json:"response_observed_at_utc,omitempty"`
	Evidence              json.RawMessage `json:"evidence"`
}

type RemoteOperationEvidenceBundle struct {
	Operation RemoteOperationSummary `json:"operation"`
	Events    []RemoteOperationEvent `json:"events"`
}

func validRemoteOperationState(state string) bool {
	switch state {
	case "prepared", "confirmed", "unknown", "failed":
		return true
	}
	return false
}

func validateRemoteOperation(op RemoteOperation) error {
	if len(op.ID) != 32 {
		return fmt.Errorf("invalid remote operation id")
	}
	if _, err := hex.DecodeString(op.ID); err != nil {
		return fmt.Errorf("invalid remote operation id")
	}
	if op.AccountFingerprint == "" || len(op.AccountFingerprint) > 128 || op.Kind == "" || len(op.Kind) > 32 || op.ObjectID < 0 || len(op.SourcePath) > 4096 || len(op.DestinationPath) > 4096 || op.CreatedAtUTC.IsZero() || op.Status != "prepared" || len(op.Evidence) == 0 || len(op.Evidence) > 64*1024 || !json.Valid(op.Evidence) {
		return fmt.Errorf("invalid remote operation")
	}
	return nil
}

func (s *Store) CreateRemoteOperation(op RemoteOperation) error {
	if err := validateRemoteOperation(op); err != nil {
		return err
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
	_, err = tx.Exec(`INSERT INTO remote_operations(operation_id,account_fingerprint,operation_kind,object_id,source_path,destination_path,status,created_at_utc,evidence_json) VALUES(?,?,?,?,?,?,?,?,?)`, op.ID, op.AccountFingerprint, op.Kind, op.ObjectID, op.SourcePath, op.DestinationPath, op.Status, op.CreatedAtUTC.UTC().UnixNano(), []byte(op.Evidence))
	if err == nil {
		_, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json) VALUES(?,?,?,?)`, op.ID, op.Status, time.Now().UTC().UnixNano(), []byte(op.Evidence))
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateRemoteOperation(id, status string, evidence json.RawMessage, responseObservedAt time.Time) error {
	if len(id) != 32 || !validRemoteOperationState(status) || status == "prepared" || len(evidence) == 0 || len(evidence) > 64*1024 || !json.Valid(evidence) {
		return fmt.Errorf("invalid remote operation update")
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
	var current string
	if err = tx.QueryRow(`SELECT status FROM remote_operations WHERE operation_id=?`, id).Scan(&current); err != nil {
		_ = tx.Rollback()
		return err
	}
	if current != "prepared" && current != "unknown" {
		_ = tx.Rollback()
		return fmt.Errorf("remote operation is already final")
	}
	now := time.Now().UTC().UnixNano()
	var observed any
	if !responseObservedAt.IsZero() {
		observed = responseObservedAt.UTC().UnixNano()
	}
	if status == "confirmed" && observed == nil {
		_ = tx.Rollback()
		return fmt.Errorf("confirmed operation requires cloud observation time")
	}
	_, err = tx.Exec(`UPDATE remote_operations SET status=?,response_observed_at_utc=?,evidence_json=? WHERE operation_id=?`, status, observed, []byte(evidence), id)
	if err == nil {
		_, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,response_observed_at_utc,evidence_json) VALUES(?,?,?,?,?)`, id, status, now, observed, []byte(evidence))
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// RecordRemoteCacheRefreshFailure appends a local-cache diagnostic without
// changing the remote operation outcome.
func (s *Store) RecordRemoteCacheRefreshFailure(id, reason string) error {
	if len(id) != 32 {
		return fmt.Errorf("invalid remote operation id")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return fmt.Errorf("invalid remote operation id")
	}
	if reason == "" || len(reason) > 16*1024 {
		return fmt.Errorf("invalid remote cache refresh failure reason")
	}
	evidence, err := json.Marshal(map[string]string{"event": "cache_refresh_failed", "reason": reason})
	if err != nil {
		return err
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
	var status string
	if err = tx.QueryRow(`SELECT status FROM remote_operations WHERE operation_id=?`, id).Scan(&status); err != nil {
		_ = tx.Rollback()
		return err
	}
	if status != "confirmed" && status != "unknown" {
		_ = tx.Rollback()
		return fmt.Errorf("remote cache failure requires a confirmed or unknown operation")
	}
	if _, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json) VALUES(?,?,?,?)`, id, status, time.Now().UTC().UnixNano(), evidence); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// MarkRemoteOperationCacheRecoveryComplete records that the affected cache
// coverage is stale for an unknown/confirmed operation. It does not claim that
// the task queue has been reconciled.
func (s *Store) MarkRemoteOperationCacheRecoveryComplete(id string) error {
	if len(id) != 32 {
		return fmt.Errorf("invalid remote operation id")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return fmt.Errorf("invalid remote operation id")
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
	var status string
	if err = tx.QueryRow(`SELECT status FROM remote_operations WHERE operation_id=?`, id).Scan(&status); err != nil {
		_ = tx.Rollback()
		return err
	}
	if status != "unknown" && status != "confirmed" {
		_ = tx.Rollback()
		return fmt.Errorf("cache recovery completion requires an unknown or confirmed operation")
	}
	if _, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json)
		SELECT ?,?,?,? WHERE NOT EXISTS(SELECT 1 FROM remote_operation_events WHERE operation_id=? AND evidence_json=?)`,
		id, status, time.Now().UTC().UnixNano(), remoteOperationLocalCacheRecoveryCompleteEvidence, id, remoteOperationLocalCacheRecoveryCompleteEvidence); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// RecordRemoteOperationPersistenceFailure preserves local evidence when an
// ambiguous provider result could not transition its prepared journal row.
func (s *Store) RecordRemoteOperationPersistenceFailure(id, reason string, responseObservedAt time.Time) error {
	if len(id) != 32 || reason == "" || len(reason) > 16*1024 {
		return fmt.Errorf("invalid remote operation persistence failure")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return fmt.Errorf("invalid remote operation id")
	}
	evidence, err := json.Marshal(map[string]string{
		"event":           "unknown_result_persistence_failed",
		"intended_status": "unknown",
		"reason":          reason,
	})
	if err != nil {
		return err
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
	var status string
	if err = tx.QueryRow(`SELECT status FROM remote_operations WHERE operation_id=?`, id).Scan(&status); err != nil {
		_ = tx.Rollback()
		return err
	}
	if status != "prepared" {
		_ = tx.Rollback()
		return fmt.Errorf("persistence failure evidence requires a prepared operation")
	}
	var observed any
	if !responseObservedAt.IsZero() {
		observed = responseObservedAt.UTC().UnixNano()
	}
	if _, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,response_observed_at_utc,evidence_json) VALUES(?,'prepared',?,?,?)`, id, time.Now().UTC().UnixNano(), observed, evidence); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// MarkRemoteOperationLocalReconciliationComplete records that the confirmed
// cloud mutation has been reflected in local cache/queue state. It is
// idempotent so restart recovery can safely retry around process termination.
func (s *Store) MarkRemoteOperationLocalReconciliationComplete(id string) error {
	if len(id) != 32 {
		return fmt.Errorf("invalid remote operation id")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return fmt.Errorf("invalid remote operation id")
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
	var status string
	if err = tx.QueryRow(`SELECT status FROM remote_operations WHERE operation_id=?`, id).Scan(&status); err != nil {
		_ = tx.Rollback()
		return err
	}
	if status != "confirmed" {
		_ = tx.Rollback()
		return fmt.Errorf("local reconciliation completion requires a confirmed operation")
	}
	if _, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json)
		SELECT ?,'confirmed',?,? WHERE NOT EXISTS(SELECT 1 FROM remote_operation_events WHERE operation_id=? AND evidence_json=?)`,
		id, time.Now().UTC().UnixNano(), remoteOperationLocalReconciliationCompleteEvidence, id, remoteOperationLocalReconciliationCompleteEvidence); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) LoadRemoteOperations() ([]RemoteOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	rows, err := s.db.Query(`SELECT operation_id,account_fingerprint,operation_kind,object_id,source_path,destination_path,status,created_at_utc,response_observed_at_utc,evidence_json FROM remote_operations ORDER BY created_at_utc,operation_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RemoteOperation, 0)
	for rows.Next() {
		var item RemoteOperation
		var created int64
		var observed sql.NullInt64
		var evidence []byte
		if err = rows.Scan(&item.ID, &item.AccountFingerprint, &item.Kind, &item.ObjectID, &item.SourcePath, &item.DestinationPath, &item.Status, &created, &observed, &evidence); err != nil {
			return nil, err
		}
		item.CreatedAtUTC = time.Unix(0, created).UTC()
		if observed.Valid {
			value := time.Unix(0, observed.Int64).UTC()
			item.ResponseObservedAtUTC = &value
		}
		item.Evidence = append(json.RawMessage(nil), evidence...)
		items = append(items, item)
	}
	return items, rows.Err()
}

// ConfirmedRemoteDeleteForSource returns the newest confirmed delete receipt
// for one exact account/object/path whose queue event has not been committed.
func (s *Store) ConfirmedRemoteDeleteForSource(account string, objectID int64, sourcePath string, jobCreatedAt time.Time) (*ConfirmedRemoteDeleteReceipt, error) {
	if account == "" || len(account) > 128 || objectID <= 0 || sourcePath == "" || !path.IsAbs(sourcePath) || len(sourcePath) > 4096 || jobCreatedAt.IsZero() {
		return nil, fmt.Errorf("invalid confirmed remote delete lookup")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	var item ConfirmedRemoteDeleteReceipt
	var created, observed int64
	var evidence []byte
	err := s.db.QueryRow(`SELECT op.operation_id,op.account_fingerprint,op.object_id,op.source_path,op.created_at_utc,op.response_observed_at_utc,prepared.evidence_json
		FROM remote_operations op
		JOIN remote_operation_events prepared ON prepared.sequence=(SELECT MIN(event.sequence) FROM remote_operation_events event WHERE event.operation_id=op.operation_id AND event.status='prepared')
		WHERE op.account_fingerprint=? AND op.operation_kind='delete' AND op.object_id=? AND op.source_path=? AND op.created_at_utc>=? AND op.status='confirmed' AND op.response_observed_at_utc IS NOT NULL
		AND NOT EXISTS(SELECT 1 FROM job_events event WHERE event.job_id=? AND event.operation_id=op.operation_id AND event.event_type='task_source_deleted_remotely')
		ORDER BY op.response_observed_at_utc DESC,op.operation_id DESC LIMIT 1`, account, objectID, sourcePath, jobCreatedAt.UTC().UnixNano(), objectID).
		Scan(&item.OperationID, &item.AccountFingerprint, &item.ObjectID, &item.SourcePath, &created, &observed, &evidence)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.CreatedAtUTC = time.Unix(0, created).UTC()
	item.ResponseObservedAtUTC = time.Unix(0, observed).UTC()
	item.PreparedEvidence = append(json.RawMessage(nil), evidence...)
	return &item, nil
}

// RemoteOperationPage reads a bounded page from this queue store. The store is
// bound to one Baidu uk; token fingerprints are intentionally not used here
// because access tokens rotate while the bound account remains the same.
func (s *Store) RemoteOperationPage(status string, offset, limit int) ([]RemoteOperationSummary, int, error) {
	return s.RemoteOperationPageFiltered(RemoteOperationFilter{Status: status}, offset, limit)
}

func (s *Store) RemoteOperationPageFiltered(filter RemoteOperationFilter, offset, limit int) ([]RemoteOperationSummary, int, error) {
	filter, err := normalizeRemoteOperationFilter(filter)
	if err != nil || offset < 0 || offset > 1_000_000 || limit < 1 || limit > maxRemoteOperationPageSize {
		return nil, 0, fmt.Errorf("invalid remote operation page")
	}
	where, args := remoteOperationFilterSQL(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, os.ErrClosed
	}
	var total int
	if err := s.db.QueryRow(`SELECT count(*) FROM remote_operations`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any(nil), args...), limit, offset)
	rows, err := s.db.Query(`SELECT operation_id,operation_kind,object_id,source_path,destination_path,status,created_at_utc,response_observed_at_utc FROM remote_operations`+where+` ORDER BY created_at_utc DESC,operation_id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]RemoteOperationSummary, 0, limit)
	for rows.Next() {
		var item RemoteOperationSummary
		var created int64
		var observed sql.NullInt64
		if err = rows.Scan(&item.ID, &item.Kind, &item.ObjectID, &item.SourcePath, &item.DestinationPath, &item.Status, &created, &observed); err != nil {
			return nil, 0, err
		}
		item.CreatedAtUTC = time.Unix(0, created).UTC()
		if observed.Valid {
			value := time.Unix(0, observed.Int64).UTC()
			item.ResponseObservedAtUTC = &value
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// RemoteOperationCursorPageFiltered reads a bounded keyset page and its live
// matching count from one SQLite snapshot. Newer rows cannot shift the cursor.
func (s *Store) RemoteOperationCursorPageFiltered(filter RemoteOperationFilter, before *RemoteOperationCursor, limit int) ([]RemoteOperationSummary, int, *RemoteOperationCursor, error) {
	filter, err := normalizeRemoteOperationFilter(filter)
	if err != nil || limit < 1 || limit > maxRemoteOperationPageSize {
		return nil, 0, nil, fmt.Errorf("invalid remote operation cursor page")
	}
	if before != nil {
		if before.CreatedAtUTC.IsZero() || !time.Unix(0, before.CreatedAtUTC.UnixNano()).Equal(before.CreatedAtUTC.UTC()) || len(before.ID) != 32 {
			return nil, 0, nil, fmt.Errorf("invalid remote operation cursor")
		}
		if _, err := hex.DecodeString(before.ID); err != nil {
			return nil, 0, nil, fmt.Errorf("invalid remote operation cursor")
		}
	}
	where, args := remoteOperationFilterSQL(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, nil, os.ErrClosed
	}
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, nil, err
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRow(`SELECT count(*) FROM remote_operations`+where, args...).Scan(&total); err != nil {
		return nil, 0, nil, err
	}
	queryWhere := where
	queryArgs := append([]any(nil), args...)
	if before != nil {
		cursorCondition := `(created_at_utc < ? OR (created_at_utc = ? AND operation_id < ?))`
		if queryWhere == "" {
			queryWhere = " WHERE " + cursorCondition
		} else {
			queryWhere += " AND " + cursorCondition
		}
		created := before.CreatedAtUTC.UTC().UnixNano()
		queryArgs = append(queryArgs, created, created, strings.ToLower(before.ID))
	}
	queryArgs = append(queryArgs, limit+1)
	rows, err := tx.Query(`SELECT operation_id,operation_kind,object_id,source_path,destination_path,status,created_at_utc,response_observed_at_utc FROM remote_operations`+queryWhere+` ORDER BY created_at_utc DESC,operation_id DESC LIMIT ?`, queryArgs...)
	if err != nil {
		return nil, 0, nil, err
	}
	items := make([]RemoteOperationSummary, 0, limit+1)
	for rows.Next() {
		var item RemoteOperationSummary
		var created int64
		var observed sql.NullInt64
		if err = rows.Scan(&item.ID, &item.Kind, &item.ObjectID, &item.SourcePath, &item.DestinationPath, &item.Status, &created, &observed); err != nil {
			rows.Close()
			return nil, 0, nil, err
		}
		item.CreatedAtUTC = time.Unix(0, created).UTC()
		if observed.Valid {
			value := time.Unix(0, observed.Int64).UTC()
			item.ResponseObservedAtUTC = &value
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, nil, err
	}
	var next *RemoteOperationCursor
	if len(items) > limit {
		last := items[limit-1]
		next = &RemoteOperationCursor{CreatedAtUTC: last.CreatedAtUTC, ID: last.ID}
		items = items[:limit]
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, nil, err
	}
	return items, total, next, nil
}

func (s *Store) RemoteOperationExport(ctx context.Context, filter RemoteOperationFilter) ([]RemoteOperationSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filter, err := normalizeRemoteOperationFilter(filter)
	if err != nil {
		return nil, err
	}
	where, args := remoteOperationFilterSQL(filter)
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
	var total int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM remote_operations`+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	if total > RemoteOperationExportLimit {
		return nil, ErrRemoteOperationExportTooLarge
	}
	query := `SELECT operation_id,operation_kind,object_id,source_path,destination_path,status,created_at_utc,response_observed_at_utc FROM remote_operations` + where + ` ORDER BY created_at_utc DESC,operation_id DESC`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	items := make([]RemoteOperationSummary, 0, total)
	for rows.Next() {
		var item RemoteOperationSummary
		var created int64
		var observed sql.NullInt64
		if err = rows.Scan(&item.ID, &item.Kind, &item.ObjectID, &item.SourcePath, &item.DestinationPath, &item.Status, &created, &observed); err != nil {
			rows.Close()
			return nil, err
		}
		item.CreatedAtUTC = time.Unix(0, created).UTC()
		if observed.Valid {
			value := time.Unix(0, observed.Int64).UTC()
			item.ResponseObservedAtUTC = &value
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

// RemoteOperationEvidenceExport reads one operation and its complete bounded
// event timeline from the same SQLite read snapshot.
func (s *Store) RemoteOperationEvidenceExport(ctx context.Context, operationID string) (RemoteOperationEvidenceBundle, error) {
	return s.RemoteOperationEvidenceExportFiltered(ctx, operationID, RemoteOperationEventFilter{})
}

// RemoteOperationEvidenceExportFiltered reads one operation and its matching
// events from the same SQLite read snapshot, rejecting partial exports.
func (s *Store) RemoteOperationEvidenceExportFiltered(ctx context.Context, operationID string, filter RemoteOperationEventFilter) (RemoteOperationEvidenceBundle, error) {
	if err := ctx.Err(); err != nil {
		return RemoteOperationEvidenceBundle{}, err
	}
	filter, err := normalizeRemoteOperationEventFilter(filter)
	if err != nil {
		return RemoteOperationEvidenceBundle{}, err
	}
	if len(operationID) != 32 {
		return RemoteOperationEvidenceBundle{}, ErrRemoteOperationNotFound
	}
	if _, err := hex.DecodeString(operationID); err != nil {
		return RemoteOperationEvidenceBundle{}, ErrRemoteOperationNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return RemoteOperationEvidenceBundle{}, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RemoteOperationEvidenceBundle{}, err
	}
	defer tx.Rollback()
	var bundle RemoteOperationEvidenceBundle
	var created int64
	var observed sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT operation_id,operation_kind,object_id,source_path,destination_path,status,created_at_utc,response_observed_at_utc FROM remote_operations WHERE operation_id=?`, operationID).Scan(&bundle.Operation.ID, &bundle.Operation.Kind, &bundle.Operation.ObjectID, &bundle.Operation.SourcePath, &bundle.Operation.DestinationPath, &bundle.Operation.Status, &created, &observed)
	if errors.Is(err, sql.ErrNoRows) {
		return RemoteOperationEvidenceBundle{}, ErrRemoteOperationNotFound
	}
	if err != nil {
		return RemoteOperationEvidenceBundle{}, err
	}
	bundle.Operation.CreatedAtUTC = time.Unix(0, created).UTC()
	if observed.Valid {
		value := time.Unix(0, observed.Int64).UTC()
		bundle.Operation.ResponseObservedAtUTC = &value
	}
	where, args := remoteOperationEventFilterSQL(operationID, filter, 0)
	query := `SELECT sequence,status,recorded_at_utc,response_observed_at_utc,evidence_json FROM remote_operation_events` + where + ` ORDER BY sequence ASC LIMIT ?`
	args = append(args, RemoteOperationEvidenceExportLimit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return RemoteOperationEvidenceBundle{}, err
	}
	bundle.Events = make([]RemoteOperationEvent, 0, RemoteOperationEvidenceExportLimit)
	for rows.Next() {
		var event RemoteOperationEvent
		var recorded int64
		var eventObserved sql.NullInt64
		var evidence []byte
		if err = rows.Scan(&event.Sequence, &event.Status, &recorded, &eventObserved, &evidence); err != nil {
			rows.Close()
			return RemoteOperationEvidenceBundle{}, err
		}
		event.RecordedAtUTC = time.Unix(0, recorded).UTC()
		if eventObserved.Valid {
			value := time.Unix(0, eventObserved.Int64).UTC()
			event.ResponseObservedAtUTC = &value
		}
		event.Evidence = append(json.RawMessage(nil), evidence...)
		bundle.Events = append(bundle.Events, event)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return RemoteOperationEvidenceBundle{}, err
	}
	if err = rows.Close(); err != nil {
		return RemoteOperationEvidenceBundle{}, err
	}
	if len(bundle.Events) > RemoteOperationEvidenceExportLimit {
		return RemoteOperationEvidenceBundle{}, ErrRemoteOperationEvidenceExportTooLarge
	}
	if err = tx.Commit(); err != nil {
		return RemoteOperationEvidenceBundle{}, err
	}
	return bundle, nil
}

func normalizeRemoteOperationFilter(filter RemoteOperationFilter) (RemoteOperationFilter, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if (filter.Status != "" && !validRemoteOperationState(filter.Status)) || (filter.Kind != "" && !validRemoteOperationKind(filter.Kind)) || utf8.RuneCountInString(filter.Query) > 128 {
		return RemoteOperationFilter{}, fmt.Errorf("invalid remote operation filter")
	}
	if filter.FromUTC != nil && filter.ToUTC != nil && filter.FromUTC.After(*filter.ToUTC) {
		return RemoteOperationFilter{}, fmt.Errorf("invalid remote operation time range")
	}
	for _, bound := range []*time.Time{filter.FromUTC, filter.ToUTC} {
		if bound != nil && !time.Unix(0, bound.UnixNano()).Equal(bound.UTC()) {
			return RemoteOperationFilter{}, fmt.Errorf("remote operation time outside supported range")
		}
	}
	return filter, nil
}

func validRemoteOperationKind(kind string) bool {
	switch kind {
	case "mkdir", "move", "rename", "delete":
		return true
	}
	return false
}

func remoteOperationFilterSQL(filter RemoteOperationFilter) (string, []any) {
	conditions := make([]string, 0, 5)
	args := make([]any, 0, 8)
	if filter.Status != "" {
		conditions = append(conditions, "status=?")
		args = append(args, filter.Status)
	}
	if filter.Kind != "" {
		conditions = append(conditions, "operation_kind=?")
		args = append(args, filter.Kind)
	}
	if filter.Query != "" {
		pattern := "%" + escapeRemoteOperationSearch(filter.Query) + "%"
		conditions = append(conditions, `(operation_id LIKE ? ESCAPE '\' OR CAST(object_id AS TEXT) LIKE ? ESCAPE '\' OR source_path LIKE ? ESCAPE '\' OR destination_path LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern, pattern, pattern)
	}
	if filter.FromUTC != nil {
		conditions = append(conditions, "created_at_utc>=?")
		args = append(args, filter.FromUTC.UnixNano())
	}
	if filter.ToUTC != nil {
		conditions = append(conditions, "created_at_utc<=?")
		args = append(args, filter.ToUTC.UnixNano())
	}
	if len(conditions) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func escapeRemoteOperationSearch(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

// RemoteOperationEventPage returns the newest events first and scopes lookup
// to the operation rows in this single-account queue store.
func (s *Store) RemoteOperationEventPage(operationID string, filter RemoteOperationEventFilter, beforeSequence int64, limit int) ([]RemoteOperationEvent, int64, bool, error) {
	if len(operationID) != 32 {
		return nil, 0, false, ErrRemoteOperationNotFound
	}
	if _, err := hex.DecodeString(operationID); err != nil {
		return nil, 0, false, ErrRemoteOperationNotFound
	}
	if beforeSequence < 0 || limit < 1 || limit > maxRemoteOperationPageSize {
		return nil, 0, false, fmt.Errorf("invalid remote operation event page")
	}
	filter, err := normalizeRemoteOperationEventFilter(filter)
	if err != nil {
		return nil, 0, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, false, os.ErrClosed
	}
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM remote_operations WHERE operation_id=?`, operationID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return nil, 0, false, ErrRemoteOperationNotFound
	} else if err != nil {
		return nil, 0, false, err
	}
	where, args := remoteOperationEventFilterSQL(operationID, filter, beforeSequence)
	query := `SELECT sequence,status,recorded_at_utc,response_observed_at_utc,evidence_json FROM remote_operation_events` + where + ` ORDER BY sequence DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()
	items := make([]RemoteOperationEvent, 0, limit+1)
	for rows.Next() {
		var item RemoteOperationEvent
		var recorded int64
		var observed sql.NullInt64
		var evidence []byte
		if err = rows.Scan(&item.Sequence, &item.Status, &recorded, &observed, &evidence); err != nil {
			return nil, 0, false, err
		}
		item.RecordedAtUTC = time.Unix(0, recorded).UTC()
		if observed.Valid {
			value := time.Unix(0, observed.Int64).UTC()
			item.ResponseObservedAtUTC = &value
		}
		item.Evidence = append(json.RawMessage(nil), evidence...)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, false, err
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	var nextBefore int64
	if more && len(items) > 0 {
		nextBefore = items[len(items)-1].Sequence
	}
	return items, nextBefore, more, nil
}

func normalizeRemoteOperationEventFilter(filter RemoteOperationEventFilter) (RemoteOperationEventFilter, error) {
	if filter.Status != "" && !validRemoteOperationState(filter.Status) {
		return RemoteOperationEventFilter{}, fmt.Errorf("invalid remote operation event status")
	}
	for _, bound := range []*time.Time{filter.RecordedFromUTC, filter.RecordedBeforeUTC} {
		if bound != nil && !time.Unix(0, bound.UnixNano()).Equal(bound.UTC()) {
			return RemoteOperationEventFilter{}, fmt.Errorf("remote operation event time outside supported range")
		}
	}
	if filter.RecordedFromUTC != nil && filter.RecordedBeforeUTC != nil && !filter.RecordedFromUTC.Before(*filter.RecordedBeforeUTC) {
		return RemoteOperationEventFilter{}, fmt.Errorf("invalid remote operation event time range")
	}
	return filter, nil
}

func remoteOperationEventFilterSQL(operationID string, filter RemoteOperationEventFilter, beforeSequence int64) (string, []any) {
	conditions := []string{"operation_id=?"}
	args := []any{operationID}
	if filter.Status != "" {
		conditions = append(conditions, "status=?")
		args = append(args, filter.Status)
	}
	if filter.RecordedFromUTC != nil {
		conditions = append(conditions, "recorded_at_utc>=?")
		args = append(args, filter.RecordedFromUTC.UnixNano())
	}
	if filter.RecordedBeforeUTC != nil {
		conditions = append(conditions, "recorded_at_utc<?")
		args = append(args, filter.RecordedBeforeUTC.UnixNano())
	}
	if beforeSequence > 0 {
		conditions = append(conditions, "sequence<?")
		args = append(args, beforeSequence)
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func (s *Store) recoverRemoteOperations() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT operation_id,account_fingerprint,source_path,destination_path FROM remote_operations WHERE status='prepared'`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	type pendingOperation struct {
		id, account, source, destination, status string
	}
	operations := make([]pendingOperation, 0)
	for rows.Next() {
		var operation pendingOperation
		if err = rows.Scan(&operation.id, &operation.account, &operation.source, &operation.destination); err != nil {
			rows.Close()
			_ = tx.Rollback()
			return err
		}
		operations = append(operations, operation)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		_ = tx.Rollback()
		return err
	}
	rows.Close()
	for _, operation := range operations {
		cachePaths := remoteOperationCachePaths(operation.source, operation.destination)
		evidence, marshalErr := json.Marshal(map[string]any{
			"reason":                  "process_restart_before_confirmation",
			"invalidated_cache_paths": cachePaths,
		})
		if marshalErr != nil {
			_ = tx.Rollback()
			return marshalErr
		}
		if _, err = tx.Exec(`UPDATE remote_operations SET status='unknown',evidence_json=? WHERE operation_id=? AND status='prepared'`, evidence, operation.id); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json) VALUES(?,'unknown',?,?)`, operation.id, time.Now().UTC().UnixNano(), evidence); err != nil {
			_ = tx.Rollback()
			return err
		}
		if len(cachePaths) > 0 {
			if err = invalidateRemoteOperationCacheTx(tx, operation.account, cachePaths); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if _, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json) VALUES(?,'unknown',?,?)`, operation.id, time.Now().UTC().UnixNano(), remoteOperationLocalCacheRecoveryCompleteEvidence); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	rows, err = tx.Query(`SELECT operation_id,account_fingerprint,source_path,destination_path,status FROM remote_operations r
		WHERE (status='confirmed' AND NOT EXISTS(SELECT 1 FROM remote_operation_events e WHERE e.operation_id=r.operation_id AND e.evidence_json IN (?,?)))
		OR (status='unknown' AND NOT EXISTS(SELECT 1 FROM remote_operation_events e WHERE e.operation_id=r.operation_id AND e.evidence_json=?))`, remoteOperationLocalReconciliationCompleteEvidence, remoteOperationLocalCacheRecoveryCompleteEvidence, remoteOperationLocalCacheRecoveryCompleteEvidence)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	operations = operations[:0]
	for rows.Next() {
		var operation pendingOperation
		if err = rows.Scan(&operation.id, &operation.account, &operation.source, &operation.destination, &operation.status); err != nil {
			rows.Close()
			_ = tx.Rollback()
			return err
		}
		operations = append(operations, operation)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		_ = tx.Rollback()
		return err
	}
	rows.Close()
	for _, operation := range operations {
		cachePaths := remoteOperationCachePaths(operation.source, operation.destination)
		eventName, reason := "confirmed_cache_recovery", "process_restart_before_local_reconciliation"
		if operation.status == "unknown" {
			eventName, reason = "unknown_cache_recovery", "process_restart_after_unknown_invalidation_failure"
		}
		recoveryEvidence, marshalErr := json.Marshal(map[string]any{
			"event":                   eventName,
			"reason":                  reason,
			"invalidated_cache_paths": cachePaths,
		})
		if marshalErr != nil {
			_ = tx.Rollback()
			return marshalErr
		}
		if err = invalidateRemoteOperationCacheTx(tx, operation.account, cachePaths); err != nil {
			_ = tx.Rollback()
			return err
		}
		recordedAt := time.Now().UTC().UnixNano()
		if _, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json) VALUES(?,?,?,?)`, operation.id, operation.status, recordedAt, recoveryEvidence); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json) VALUES(?,?,?,?)`, operation.id, operation.status, recordedAt, remoteOperationLocalCacheRecoveryCompleteEvidence); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func remoteOperationCachePaths(source, destination string) []string {
	seen := make(map[string]struct{}, 4)
	paths := make([]string, 0, 4)
	add := func(value string) {
		if validInventoryPath(value) {
			if _, exists := seen[value]; !exists {
				seen[value] = struct{}{}
				paths = append(paths, value)
			}
		}
	}
	for _, value := range []string{source, destination} {
		if value == "" {
			continue
		}
		add(path.Dir(value))
		add(value)
	}
	return paths
}

func invalidateRemoteOperationCacheTx(tx *sql.Tx, account string, paths []string) error {
	for _, cachePath := range paths {
		prefix, upper := cloudDirectoryPathRange(cachePath)
		if _, err := tx.Exec(`UPDATE cloud_inventory_directories SET status='stale' WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))`, account, cachePath, prefix, upper); err != nil {
			return err
		}
	}
	if len(paths) > 0 {
		return bumpCloudInventoryGeneration(tx, account, time.Now().UTC())
	}
	return nil
}
