package taskstore

import (
	"context"
	"fmt"
	"os"
	"time"
)

type CleanupBatchSummary struct{ CleanupBatchRecord }

func validCleanupScope(s CleanupBatchScope) bool {
	return validCleanupHex(s.AccountFingerprint, 32) && validCleanupHex(s.ApplicationFingerprint, 32) && validCleanupHex(s.CacheNamespace, 32)
}

// Arrival order uses SQLite insertion order, not wall-clock timestamps which
// can move backwards. This is selection only; Prepare is the durable admission.
func (s *Store) NextCleanupItem(ctx context.Context, scope CleanupBatchScope) (string, int, error) {
	if !validCleanupScope(scope) {
		return "", 0, fmt.Errorf("invalid cleanup scope")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", 0, os.ErrClosed
	}
	var id string
	var ordinal int
	err := s.db.QueryRowContext(ctx, `SELECT b.batch_id,i.ordinal FROM cleanup_batches b JOIN cleanup_batch_items i ON i.batch_id=b.batch_id WHERE b.account_fingerprint=? AND b.application_fingerprint=? AND b.cache_namespace=? AND b.status='queued' AND b.stop_requested=0 AND i.status='pending' AND NOT EXISTS(SELECT 1 FROM cleanup_batch_items a WHERE a.status='sending') AND NOT EXISTS(SELECT 1 FROM cleanup_batch_items p WHERE p.batch_id=b.batch_id AND p.ordinal<i.ordinal AND p.status<>'confirmed') ORDER BY b.rowid,i.ordinal LIMIT 1`, scope.AccountFingerprint, scope.ApplicationFingerprint, scope.CacheNamespace).Scan(&id, &ordinal)
	return id, ordinal, err
}

func (s *Store) CleanupBatchPage(ctx context.Context, scope CleanupBatchScope, offset, limit int) ([]CleanupBatchSummary, int, error) {
	if !validCleanupScope(scope) || offset < 0 || limit < 1 || limit > 50 {
		return nil, 0, fmt.Errorf("invalid cleanup batch page")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM cleanup_batches WHERE account_fingerprint=? AND application_fingerprint=? AND cache_namespace=?`, scope.AccountFingerprint, scope.ApplicationFingerprint, scope.CacheNamespace).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT b.batch_id,b.manifest_sha256,b.created_at_utc,b.item_count,b.status,b.stop_requested,
 (SELECT count(*) FROM cleanup_batch_items i WHERE i.batch_id=b.batch_id AND i.status='pending'),
 (SELECT count(*) FROM cleanup_batch_items i WHERE i.batch_id=b.batch_id AND i.status='sending'),
 (SELECT count(*) FROM cleanup_batch_items i WHERE i.batch_id=b.batch_id AND i.status='confirmed'),
 (SELECT count(*) FROM cleanup_batch_items i WHERE i.batch_id=b.batch_id AND i.status='failed'),
 (SELECT count(*) FROM cleanup_batch_items i WHERE i.batch_id=b.batch_id AND i.status='unknown')
 FROM cleanup_batches b WHERE account_fingerprint=? AND application_fingerprint=? AND cache_namespace=? ORDER BY b.rowid DESC LIMIT ? OFFSET ?`, scope.AccountFingerprint, scope.ApplicationFingerprint, scope.CacheNamespace, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]CleanupBatchSummary, 0, limit)
	for rows.Next() {
		var v CleanupBatchSummary
		var created string
		if err = rows.Scan(&v.ID, &v.ManifestSHA256, &created, &v.ItemCount, &v.Status, &v.StopRequested, &v.Pending, &v.Sending, &v.Confirmed, &v.Failed, &v.Unknown); err != nil {
			return nil, 0, err
		}
		v.CreatedAtUTC, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, v)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	return result, total, nil
}
