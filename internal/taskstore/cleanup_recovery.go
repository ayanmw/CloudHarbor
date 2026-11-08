package taskstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

// Projection reads durable provider evidence, never a browser callback or the
// apparent absence of the deleted path. Repeating it cannot re-send a request.
func (s *Store) ReconcileCleanupOperation(ctx context.Context, id string, ordinal int) error {
	if !validCleanupHex(id, 16) || ordinal < 0 || ordinal >= CleanupBatchItemLimit {
		return fmt.Errorf("invalid cleanup item")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = reconcileCleanupOperationTx(ctx, tx, id, ordinal); err != nil {
		return err
	}
	return tx.Commit()
}

func reconcileCleanupOperationTx(ctx context.Context, tx *sql.Tx, id string, ordinal int) error {
	var itemStatus string
	var operation sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT status,operation_id FROM cleanup_batch_items WHERE batch_id=? AND ordinal=?`, id, ordinal).Scan(&itemStatus, &operation); err != nil {
		return err
	}
	if itemStatus != "sending" {
		return nil
	}
	if !operation.Valid {
		return fmt.Errorf("sending cleanup item missing operation")
	}
	var remoteStatus string
	var observed sql.NullInt64
	var reconciled bool
	if err := tx.QueryRowContext(ctx, `SELECT status,response_observed_at_utc,EXISTS(SELECT 1 FROM remote_operation_events e WHERE e.operation_id=r.operation_id AND e.status='confirmed' AND e.evidence_json=?) FROM remote_operations r WHERE operation_id=?`, remoteOperationLocalReconciliationCompleteEvidence, operation.String).Scan(&remoteStatus, &observed, &reconciled); err != nil {
		return err
	}
	if remoteStatus == "prepared" {
		return ErrCleanupBatchConflict
	}
	reason := "remote_" + remoteStatus
	batchStatus := remoteStatus
	switch remoteStatus {
	case "confirmed":
		if !observed.Valid {
			return fmt.Errorf("confirmed cleanup receipt missing observation time")
		}
		if !reconciled {
			batchStatus = "failed"
			reason = "confirmed_before_local_reconciliation"
		} else {
			var pending int
			var stop bool
			if err := tx.QueryRowContext(ctx, `SELECT stop_requested,(SELECT count(*) FROM cleanup_batch_items WHERE batch_id=? AND ordinal<>? AND status<>'confirmed') FROM cleanup_batches WHERE batch_id=?`, id, ordinal, id).Scan(&stop, &pending); err != nil {
				return err
			}
			batchStatus = "queued"
			if pending == 0 {
				batchStatus = "completed"
			} else if stop {
				batchStatus = "stopped"
			}
		}
	case "unknown", "failed":
	default:
		return fmt.Errorf("invalid cleanup remote status")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cleanup_batch_items SET status=? WHERE batch_id=? AND ordinal=? AND status='sending'`, remoteStatus, id, ordinal); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cleanup_batches SET status=? WHERE batch_id=?`, batchStatus, id); err != nil {
		return err
	}
	var response any
	if observed.Valid {
		response = observed.Int64
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO cleanup_batch_events(batch_id,kind,recorded_at_utc,ordinal,operation_id,response_observed_at_utc,reason) VALUES(?,'item_result',?,?,?,?,?)`, id, time.Now().UTC().Format(time.RFC3339Nano), ordinal, operation.String, response, reason)
	return err
}

// The remote journal first converts interrupted prepared intents to unknown.
// Recover only their local projections; never schedule or send from Store.Open.
func (s *Store) recoverCleanupBatches() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT batch_id,ordinal FROM cleanup_batch_items WHERE status='sending' ORDER BY batch_id,ordinal`)
	if err != nil {
		return err
	}
	type item struct {
		id      string
		ordinal int
	}
	var items []item
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.ordinal); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, i := range items {
		if err = reconcileCleanupOperationTx(ctx, tx, i.id, i.ordinal); err != nil {
			return err
		}
	}
	return tx.Commit()
}
