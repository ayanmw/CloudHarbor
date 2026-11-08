package taskstore

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// RecordRemoteCopyObservation appends online identities without replacing the
// synchronous completion receipt. The destination is a distinct object; this
// event must never be interpreted as a source task identity change.
func (s *Store) RecordRemoteCopyObservation(id string, source, target CloudInventoryEntry, sourceAt, targetAt time.Time) error {
	if source.FsID <= 0 || target.FsID <= 0 || source.FsID == target.FsID || !validInventoryPath(source.Path) || !validInventoryPath(target.Path) || source.Path == target.Path || source.Directory != target.Directory || sourceAt.IsZero() || targetAt.IsZero() {
		return fmt.Errorf("invalid copy identity observation")
	}
	evidence, err := json.Marshal(map[string]any{"event": "copy_identity_observed", "source": source, "target": target, "source_fs_id": strconv.FormatInt(source.FsID, 10), "target_fs_id": strconv.FormatInt(target.FsID, 10), "source_observed_at_utc": sourceAt.UTC(), "target_observed_at_utc": targetAt.UTC(), "directory_tree_verified": false})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("store closed")
	}
	result, err := s.db.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,response_observed_at_utc,evidence_json) SELECT operation_id,status,?,?,? FROM remote_operations WHERE operation_id=? AND operation_kind='copy' AND status='confirmed' AND object_id=? AND source_path=? AND destination_path=?`, time.Now().UTC().UnixNano(), targetAt.UTC().UnixNano(), evidence, id, source.FsID, source.Path, target.Path)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("copy observation does not match confirmed operation")
	}
	return nil
}
