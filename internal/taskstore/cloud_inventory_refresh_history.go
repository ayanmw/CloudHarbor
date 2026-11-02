package taskstore

import "database/sql"

// syncCloudInventoryRefreshRun keeps the query projection aligned with the
// durable checkpoint while retaining event sequence/kind as append-only facts.
func syncCloudInventoryRefreshRun(tx *sql.Tx, state CloudInventoryRefreshState) error {
	var sequence int64
	var kind string
	if err := tx.QueryRow(`SELECT sequence,event_kind FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=? ORDER BY sequence DESC LIMIT 1`, state.AccountFingerprint, state.RunID).Scan(&sequence, &kind); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO cloud_inventory_refresh_runs(start_sequence,account_fingerprint,run_id,root_path,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error,latest_sequence,latest_kind) SELECT sequence,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,? FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=? AND event_kind='started' ORDER BY sequence LIMIT 1 ON CONFLICT(account_fingerprint,run_id) DO UPDATE SET root_path=excluded.root_path,status=excluded.status,phase=excluded.phase,current_directory=excluded.current_directory,pages_processed=excluded.pages_processed,items_processed=excluded.items_processed,request_budget=excluded.request_budget,requests_used=excluded.requests_used,pending_count=excluded.pending_count,updated_at_utc=excluded.updated_at_utc,last_error=excluded.last_error,latest_sequence=excluded.latest_sequence,latest_kind=excluded.latest_kind`, state.AccountFingerprint, state.RunID, state.RootPath, state.Status, state.Phase, state.CurrentDirectory, state.PagesProcessed, state.ItemsProcessed, state.RequestBudget, state.RequestsUsed, state.PendingCount, state.StartedAtUTC.UTC().UnixMicro(), state.UpdatedAtUTC.UTC().UnixMicro(), state.LastError, sequence, kind, state.AccountFingerprint, state.RunID)
	return err
}
