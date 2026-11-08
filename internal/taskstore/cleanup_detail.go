package taskstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

type CleanupItemRecord struct {
	CleanupBatchItem
	Ordinal     int    `json:"ordinal"`
	Status      string `json:"status"`
	OperationID string `json:"operation_id,omitempty"`
}
type CleanupBatchEvent struct {
	Sequence              int64      `json:"sequence,string"`
	Kind                  string     `json:"kind"`
	RecordedAtUTC         time.Time  `json:"recorded_at_utc"`
	Ordinal               *int       `json:"ordinal,omitempty"`
	OperationID           string     `json:"operation_id,omitempty"`
	ResponseObservedAtUTC *time.Time `json:"response_observed_at_utc,omitempty"`
	Reason                string     `json:"reason,omitempty"`
}

func (s *Store) CleanupItemPage(ctx context.Context, id string, scope CleanupBatchScope, offset, limit int) ([]CleanupItemRecord, int, error) {
	if !validCleanupHex(id, 16) || offset < 0 || limit < 1 || limit > 100 {
		return nil, 0, fmt.Errorf("invalid cleanup item page")
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
	manifest, err := cleanupManifestTx(ctx, tx, id, scope)
	if err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT ordinal,status,operation_id FROM cleanup_batch_items WHERE batch_id=? ORDER BY ordinal LIMIT ? OFFSET ?`, id, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]CleanupItemRecord, 0, limit)
	for rows.Next() {
		var item CleanupItemRecord
		var op sql.NullString
		if err = rows.Scan(&item.Ordinal, &item.Status, &op); err != nil {
			return nil, 0, err
		}
		if item.Ordinal < 0 || item.Ordinal >= len(manifest.Items) {
			return nil, 0, fmt.Errorf("cleanup item outside manifest")
		}
		item.CleanupBatchItem = manifest.Items[item.Ordinal]
		item.OperationID = op.String
		result = append(result, item)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	return result, len(manifest.Items), nil
}
func (s *Store) CleanupEventPage(ctx context.Context, id string, scope CleanupBatchScope, after int64, limit int) ([]CleanupBatchEvent, bool, error) {
	if !validCleanupHex(id, 16) || after < 0 || limit < 1 || limit > 100 {
		return nil, false, fmt.Errorf("invalid cleanup event page")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, false, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if _, err = cleanupManifestTx(ctx, tx, id, scope); err != nil {
		return nil, false, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,kind,recorded_at_utc,ordinal,operation_id,response_observed_at_utc,reason FROM cleanup_batch_events WHERE batch_id=? AND sequence>? ORDER BY sequence LIMIT ?`, id, after, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := make([]CleanupBatchEvent, 0, limit+1)
	for rows.Next() {
		var event CleanupBatchEvent
		var recorded string
		var ordinal, observed sql.NullInt64
		var op sql.NullString
		if err = rows.Scan(&event.Sequence, &event.Kind, &recorded, &ordinal, &op, &observed, &event.Reason); err != nil {
			return nil, false, err
		}
		event.RecordedAtUTC, err = time.Parse(time.RFC3339Nano, recorded)
		if err != nil {
			return nil, false, err
		}
		event.OperationID = op.String
		if ordinal.Valid {
			v := int(ordinal.Int64)
			event.Ordinal = &v
		}
		if observed.Valid {
			v := time.Unix(0, observed.Int64).UTC()
			event.ResponseObservedAtUTC = &v
		}
		result = append(result, event)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(result) > limit
	if more {
		result = result[:limit]
	}
	return result, more, nil
}
