package taskstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// InvalidateIdleDuplicateCheckpoint requires the caller to hold the downloader's
// scan/write admission reservation: a live hash worker must never coexist here.
// Persisted running/paused/failed rows can outlive that worker. Preserve their
// counts and identity as history; only an explicit future start creates a new run.
func (s *Store) InvalidateIdleDuplicateCheckpoint(account string) error {
	if account == "" || len(account) > 128 {
		return fmt.Errorf("invalid account fingerprint")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, err := scanCloudDuplicateState(tx.QueryRow(cloudDuplicateStateQuery, account))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if state.Status == "complete" || state.Phase == "restart_required" {
		return nil
	}
	state.Status = "failed"
	state.Phase = "restart_required"
	state.UpdatedAtUTC = time.Now().UTC()
	state.LastError = "网盘写入已准入，旧哈希检查点已作废；请刷新缓存后重新开始扫描"
	state.RetryAtUTC = nil
	state.RetryDelayMillis = 0
	state.RetryBackoffBaseMillis = 0
	if _, err = tx.Exec(`UPDATE cloud_duplicate_scan_state SET status=?,phase=?,updated_at_utc=?,last_error=?,retry_at_utc=NULL,retry_delay_ms=0,retry_backoff_base_millis=0 WHERE account_fingerprint=? AND run_id=?`, state.Status, state.Phase, state.UpdatedAtUTC.UnixMicro(), state.LastError, account, state.RunID); err != nil {
		return err
	}
	evidence, _ := json.Marshal(map[string]any{"reason": "namespace_write_admitted", "processed_files": state.ProcessedFiles, "total_files": state.TotalFiles, "processed_bytes": state.ProcessedBytes, "total_bytes": state.TotalBytes, "next_fs_id": state.NextFsID, "current_fs_id": state.CurrentFsID, "current_bytes": state.CurrentBytes, "last_error": state.LastError})
	if _, err = tx.Exec(`INSERT INTO cloud_duplicate_scan_events(account_fingerprint,run_id,status,phase,recorded_at_utc,evidence_json) VALUES(?,?,?,?,?,?)`, account, state.RunID, state.Status, state.Phase, state.UpdatedAtUTC.UnixMicro(), evidence); err != nil {
		return err
	}
	if err = syncCloudDuplicateScanRun(tx, state); err != nil {
		return err
	}
	return tx.Commit()
}
