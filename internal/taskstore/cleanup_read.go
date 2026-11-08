package taskstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func cleanupManifestTx(ctx context.Context, tx *sql.Tx, id string, scope CleanupBatchScope) (CleanupBatchManifest, error) {
	var m CleanupBatchManifest
	var owner CleanupBatchScope
	var body []byte
	var digest, created string
	var count int
	err := tx.QueryRowContext(ctx, `SELECT account_fingerprint,application_fingerprint,cache_namespace,manifest_json,manifest_sha256,created_at_utc,item_count FROM cleanup_batches WHERE batch_id=?`, id).Scan(&owner.AccountFingerprint, &owner.ApplicationFingerprint, &owner.CacheNamespace, &body, &digest, &created, &count)
	if err != nil {
		return m, err
	}
	if owner != scope {
		return m, ErrCleanupBatchConflict
	}
	if err = json.Unmarshal(body, &m); err != nil {
		return m, err
	}
	m.CreatedAtUTC, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return m, err
	}
	if m.ID != id || m.AccountFingerprint != owner.AccountFingerprint || m.ApplicationFingerprint != owner.ApplicationFingerprint || m.CacheNamespace != owner.CacheNamespace || len(m.Items) != count {
		return m, ErrCleanupBatchConflict
	}
	canonical, sum, err := encodeCleanupBatchManifest(m)
	if err != nil {
		return m, err
	}
	if sum != digest || !bytes.Equal(body, canonical) {
		return m, fmt.Errorf("cleanup manifest integrity mismatch")
	}
	return m, nil
}

// Returns a verified immutable manifest, not permission to send a mutation.
func (s *Store) CleanupManifest(ctx context.Context, id string, scope CleanupBatchScope) (CleanupBatchManifest, error) {
	if !validCleanupHex(id, 16) {
		return CleanupBatchManifest{}, fmt.Errorf("invalid cleanup batch id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return CleanupBatchManifest{}, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CleanupBatchManifest{}, err
	}
	defer tx.Rollback()
	return cleanupManifestTx(ctx, tx, id, scope)
}

// A preflight refusal has no provider response or operation ID. Only an
// untouched pending item can fail here; sending outcomes use remote evidence.
func (s *Store) FailCleanupPreflight(ctx context.Context, id string, ordinal int, scope CleanupBatchScope, reason string) error {
	switch reason {
	case "observation_changed", "source_identity_changed", "retained_identity_changed", "content_changed", "directory_not_empty", "remote_read_failed", "operation_conflict":
	default:
		return fmt.Errorf("invalid cleanup preflight reason")
	}
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
	if _, err = cleanupManifestTx(ctx, tx, id, scope); err != nil {
		return err
	}
	var itemStatus, batchStatus string
	var op sql.NullString
	var stop bool
	if err = tx.QueryRowContext(ctx, `SELECT i.status,i.operation_id,b.status,b.stop_requested FROM cleanup_batch_items i JOIN cleanup_batches b ON b.batch_id=i.batch_id WHERE i.batch_id=? AND i.ordinal=?`, id, ordinal).Scan(&itemStatus, &op, &batchStatus, &stop); err != nil {
		return err
	}
	if itemStatus == "failed" && !op.Valid {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cleanup_batch_events WHERE batch_id=? AND ordinal=? AND kind='preflight_failed' AND reason=?)`, id, ordinal, reason).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
	}
	if itemStatus != "pending" || op.Valid || stop || (batchStatus != "queued" && batchStatus != "running") {
		return ErrCleanupBatchConflict
	}
	var blockers int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM cleanup_batch_items WHERE batch_id=? AND (status='sending' OR (ordinal<? AND status<>'confirmed'))`, id, ordinal).Scan(&blockers); err != nil {
		return err
	}
	if blockers != 0 {
		return ErrCleanupBatchConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cleanup_batch_items SET status='failed' WHERE batch_id=? AND ordinal=?`, id, ordinal); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cleanup_batches SET status='failed' WHERE batch_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO cleanup_batch_events(batch_id,kind,recorded_at_utc,ordinal,reason) VALUES(?,'preflight_failed',?,?,?)`, id, time.Now().UTC().Format(time.RFC3339Nano), ordinal, reason); err != nil {
		return err
	}
	return tx.Commit()
}
