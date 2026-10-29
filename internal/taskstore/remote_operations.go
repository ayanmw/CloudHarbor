package taskstore

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

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
	rows, err := tx.Query(`SELECT operation_id FROM remote_operations WHERE status='prepared'`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			_ = tx.Rollback()
			return err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		_ = tx.Rollback()
		return err
	}
	rows.Close()
	for _, id := range ids {
		evidence := []byte(`{"reason":"process_restart_before_confirmation"}`)
		if _, err = tx.Exec(`UPDATE remote_operations SET status='unknown',evidence_json=? WHERE operation_id=? AND status='prepared'`, evidence, id); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,evidence_json) VALUES(?,'unknown',?,?)`, id, time.Now().UTC().UnixNano(), evidence); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
