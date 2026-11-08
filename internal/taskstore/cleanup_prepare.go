package taskstore

import (
	"context"
	"fmt"
	"os"
	"time"
)

// These are the backend-verified bound account, application and cache namespace,
// not the expiring credential fingerprint recorded on the remote operation.
type CleanupBatchScope struct{ AccountFingerprint, ApplicationFingerprint, CacheNamespace string }

// PrepareCleanupOperation is the final durable admission before the network
// request. The remote intent, item association and batch event commit together.
// A failure here forbids sending; an existing association is never re-admitted.
func (s *Store) PrepareCleanupOperation(ctx context.Context, id string, ordinal int, scope CleanupBatchScope, op RemoteOperation) error {
	if !validCleanupHex(id, 16) || ordinal < 0 || ordinal >= CleanupBatchItemLimit || op.Kind != "delete" || op.DestinationPath != "" {
		return fmt.Errorf("invalid cleanup operation")
	}
	if err := validateRemoteOperation(op); err != nil {
		return err
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
	manifest, err := cleanupManifestTx(ctx, tx, id, scope)
	if err != nil {
		return err
	}
	var status string
	var stopped bool
	if err = tx.QueryRowContext(ctx, `SELECT status,stop_requested FROM cleanup_batches WHERE batch_id=?`, id).Scan(&status, &stopped); err != nil {
		return err
	}
	if stopped || (status != "queued" && status != "running") || ordinal >= len(manifest.Items) {
		return ErrCleanupBatchConflict
	}
	expected := manifest.Items[ordinal].Expected
	if op.ObjectID != expected.FsID || op.SourcePath != expected.Path {
		return ErrCleanupBatchConflict
	}
	var itemStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM cleanup_batch_items WHERE batch_id=? AND ordinal=?`, id, ordinal).Scan(&itemStatus); err != nil {
		return err
	}
	if itemStatus != "pending" {
		return ErrCleanupBatchConflict
	}
	var conflicts int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM cleanup_batch_items WHERE status='sending')+(SELECT count(*) FROM cleanup_batch_items WHERE batch_id=? AND ordinal<? AND status<>'confirmed')`, id, ordinal).Scan(&conflicts); err != nil {
		return err
	}
	if conflicts != 0 {
		return ErrCleanupBatchConflict
	}
	if err = insertRemoteOperationTx(ctx, tx, op); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cleanup_batch_items SET status='sending',operation_id=? WHERE batch_id=? AND ordinal=?`, op.ID, id, ordinal); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cleanup_batches SET status='running' WHERE batch_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO cleanup_batch_events(batch_id,kind,recorded_at_utc,ordinal,operation_id) VALUES(?,'item_prepared',?,?,?)`, id, time.Now().UTC().Format(time.RFC3339Nano), ordinal, op.ID); err != nil {
		return err
	}
	return tx.Commit()
}
