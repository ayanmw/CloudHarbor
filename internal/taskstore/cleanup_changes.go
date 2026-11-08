package taskstore

import (
	"context"
	"fmt"
	"os"
)

// CleanupChangeSequence observes committed successes across every batch in this
// scope. Event sequence, rather than batch order or UTC, survives clock changes.
// A confirmed remote deletion still changes the list when local reconciliation
// failed; failures and unknown outcomes do not assert a successful deletion.
func (s *Store) CleanupChangeSequence(ctx context.Context, scope CleanupBatchScope) (int64, error) {
	if !validCleanupScope(scope) {
		return 0, fmt.Errorf("invalid cleanup scope")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	var sequence int64
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(max(e.sequence),0) FROM cleanup_batch_events e JOIN cleanup_batches b ON b.batch_id=e.batch_id WHERE b.account_fingerprint=? AND b.application_fingerprint=? AND b.cache_namespace=? AND e.kind='item_result' AND e.reason IN ('remote_confirmed','confirmed_before_local_reconciliation')`, scope.AccountFingerprint, scope.ApplicationFingerprint, scope.CacheNamespace).Scan(&sequence)
	return sequence, err
}
