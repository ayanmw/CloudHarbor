package taskstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

var ErrDownloadRecordChanged = errors.New("download record changed or is not clearable")

// ClearDownloadRecord is the explicit user-authorized history reset. Transactional
// DDL restores append-only guards before commit; failure rolls back data and guards.
func (s *Store) ClearDownloadRecord(ctx context.Context, id int64, expected []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	if id <= 0 || !json.Valid(expected) {
		return ErrDownloadRecordChanged
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var body []byte
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT payload_json,status FROM jobs WHERE id=?`, id).Scan(&body, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return os.ErrNotExist
		}
		return err
	}
	var record struct {
		SourceState string `json:"source_state"`
	}
	if json.Unmarshal(body, &record) != nil || !bytes.Equal(body, expected) || record.SourceState == "sending" || (status != "done" && status != "failed" && status != "cancelled" && status != "source_unknown") {
		return ErrDownloadRecordChanged
	}
	guards := []string{"job_events_no_delete", "download_manifest_results_no_delete"}
	definitions := make([]string, len(guards))
	for i, name := range guards {
		if err = tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?`, name).Scan(&definitions[i]); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DROP TRIGGER `+name); err != nil {
			return err
		}
	}
	for _, table := range []string{"download_manifest_results", "download_manifest_states", "download_manifest_entries", "download_manifest_totals"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE manifest_id IN (SELECT manifest_id FROM download_manifests WHERE root_id=?)`, id); err != nil {
			return err
		}
	}
	for _, query := range []string{`DELETE FROM download_manifest_current WHERE root_id=?`, `DELETE FROM download_manifests WHERE root_id=?`, `DELETE FROM job_events WHERE job_id=?`, `DELETE FROM jobs WHERE id=?`} {
		if _, err = tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
	}
	for _, definition := range definitions {
		if definition == "" {
			return fmt.Errorf("missing cleanup guard definition")
		}
		if _, err = tx.ExecContext(ctx, definition); err != nil {
			return err
		}
	}
	return tx.Commit()
}
