package taskstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

var ErrCleanupBatchConflict = errors.New("cleanup batch content conflict")

const cleanupBatchSchema = `
CREATE TABLE IF NOT EXISTS cleanup_batches(
 batch_id TEXT PRIMARY KEY CHECK(length(batch_id)=32),
 account_fingerprint TEXT NOT NULL, application_fingerprint TEXT NOT NULL, cache_namespace TEXT NOT NULL,
 manifest_json BLOB NOT NULL, manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 created_at_utc TEXT NOT NULL, item_count INTEGER NOT NULL CHECK(item_count BETWEEN 1 AND 1000),
 status TEXT NOT NULL CHECK(status IN ('queued','running','completed','stopped','failed','unknown')),
 stop_requested INTEGER NOT NULL DEFAULT 0 CHECK(stop_requested IN (0,1))
);
CREATE TABLE IF NOT EXISTS cleanup_batch_items(
 batch_id TEXT NOT NULL REFERENCES cleanup_batches(batch_id) ON DELETE RESTRICT,
 ordinal INTEGER NOT NULL CHECK(ordinal>=0),
 status TEXT NOT NULL CHECK(status IN ('pending','sending','confirmed','failed','unknown')),
 operation_id TEXT UNIQUE REFERENCES remote_operations(operation_id) ON DELETE RESTRICT,
 PRIMARY KEY(batch_id,ordinal)
);
CREATE TABLE IF NOT EXISTS cleanup_batch_events(
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 batch_id TEXT NOT NULL REFERENCES cleanup_batches(batch_id) ON DELETE RESTRICT,
 kind TEXT NOT NULL, recorded_at_utc TEXT NOT NULL,
 ordinal INTEGER, operation_id TEXT REFERENCES remote_operations(operation_id) ON DELETE RESTRICT,
 response_observed_at_utc INTEGER, reason TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS cleanup_batch_events_order ON cleanup_batch_events(batch_id,sequence);
CREATE TRIGGER IF NOT EXISTS cleanup_batch_events_no_update BEFORE UPDATE ON cleanup_batch_events BEGIN SELECT RAISE(ABORT,'cleanup events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS cleanup_batch_events_no_delete BEFORE DELETE ON cleanup_batch_events BEGIN SELECT RAISE(ABORT,'cleanup events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS cleanup_batches_manifest_immutable BEFORE UPDATE OF batch_id,account_fingerprint,application_fingerprint,cache_namespace,manifest_json,manifest_sha256,created_at_utc,item_count ON cleanup_batches BEGIN SELECT RAISE(ABORT,'cleanup manifest is immutable'); END;
`

type CleanupBatchRecord struct {
	Pending        int       `json:"pending"`
	Sending        int       `json:"sending"`
	Confirmed      int       `json:"confirmed"`
	Failed         int       `json:"failed"`
	Unknown        int       `json:"unknown"`
	ID             string    `json:"batch_id"`
	ManifestSHA256 string    `json:"manifest_sha256"`
	CreatedAtUTC   time.Time `json:"created_at_utc"`
	ItemCount      int       `json:"item_count"`
	Status         string    `json:"status"`
	StopRequested  bool      `json:"stop_requested"`
}

func readCleanupBatch(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (CleanupBatchRecord, error) {
	var record CleanupBatchRecord
	var created string
	err := q.QueryRowContext(ctx, `SELECT batch_id,manifest_sha256,created_at_utc,item_count,status,stop_requested,(SELECT count(*) FROM cleanup_batch_items WHERE batch_id=b.batch_id AND status='pending'),(SELECT count(*) FROM cleanup_batch_items WHERE batch_id=b.batch_id AND status='sending'),(SELECT count(*) FROM cleanup_batch_items WHERE batch_id=b.batch_id AND status='confirmed'),(SELECT count(*) FROM cleanup_batch_items WHERE batch_id=b.batch_id AND status='failed'),(SELECT count(*) FROM cleanup_batch_items WHERE batch_id=b.batch_id AND status='unknown') FROM cleanup_batches b WHERE batch_id=?`, id).Scan(&record.ID, &record.ManifestSHA256, &created, &record.ItemCount, &record.Status, &record.StopRequested, &record.Pending, &record.Sending, &record.Confirmed, &record.Failed, &record.Unknown)
	if err != nil {
		return record, err
	}
	record.CreatedAtUTC, err = time.Parse(time.RFC3339Nano, created)
	return record, err
}

func (s *Store) CreateCleanupBatch(ctx context.Context, m CleanupBatchManifest) (CleanupBatchRecord, error) {
	body, digest, err := encodeCleanupBatchManifest(m)
	if err != nil {
		return CleanupBatchRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return CleanupBatchRecord{}, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CleanupBatchRecord{}, err
	}
	defer tx.Rollback()
	record, err := readCleanupBatch(ctx, tx, m.ID)
	if err == nil {
		if record.ManifestSHA256 != digest {
			return CleanupBatchRecord{}, ErrCleanupBatchConflict
		}
		return record, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CleanupBatchRecord{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO cleanup_batches(batch_id,account_fingerprint,application_fingerprint,cache_namespace,manifest_json,manifest_sha256,created_at_utc,item_count,status) VALUES(?,?,?,?,?,?,?,?,'queued')`, m.ID, m.AccountFingerprint, m.ApplicationFingerprint, m.CacheNamespace, body, digest, m.CreatedAtUTC.UTC().Format(time.RFC3339Nano), len(m.Items))
	if err != nil {
		return CleanupBatchRecord{}, err
	}
	for ordinal := range m.Items {
		if _, err = tx.ExecContext(ctx, `INSERT INTO cleanup_batch_items(batch_id,ordinal,status) VALUES(?,?,'pending')`, m.ID, ordinal); err != nil {
			return CleanupBatchRecord{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO cleanup_batch_events(batch_id,kind,recorded_at_utc) VALUES(?,'created',?)`, m.ID, m.CreatedAtUTC.UTC().Format(time.RFC3339Nano)); err != nil {
		return CleanupBatchRecord{}, err
	}
	record, err = readCleanupBatch(ctx, tx, m.ID)
	if err != nil {
		return record, err
	}
	return record, tx.Commit()
}

func (s *Store) CleanupBatch(ctx context.Context, id string) (CleanupBatchRecord, error) {
	if !validCleanupHex(id, 16) {
		return CleanupBatchRecord{}, fmt.Errorf("invalid cleanup batch id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return CleanupBatchRecord{}, os.ErrClosed
	}
	return readCleanupBatch(ctx, s.db, id)
}

// Stop blocks subsequent admission. An already sending request must still record
// its actual result; changing the batch flag does not cancel a remote write.
func (s *Store) StopCleanupBatch(ctx context.Context, id string) error {
	if !validCleanupHex(id, 16) {
		return fmt.Errorf("invalid cleanup batch id")
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
	record, err := readCleanupBatch(ctx, tx, id)
	if err != nil {
		return err
	}
	if record.StopRequested || record.Status == "completed" || record.Status == "failed" || record.Status == "unknown" {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cleanup_batches SET stop_requested=1,status=CASE WHEN status='queued' THEN 'stopped' ELSE status END WHERE batch_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO cleanup_batch_events(batch_id,kind,recorded_at_utc) VALUES(?,'stop_requested',?)`, id, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func validateCleanupBatchSchema(ctx context.Context, db *sql.DB) error {
	for _, query := range []string{`SELECT batch_id,account_fingerprint,application_fingerprint,cache_namespace,manifest_json,manifest_sha256,created_at_utc,item_count,status,stop_requested FROM cleanup_batches LIMIT 0`, `SELECT batch_id,ordinal,status,operation_id FROM cleanup_batch_items LIMIT 0`, `SELECT sequence,batch_id,kind,recorded_at_utc,ordinal,operation_id,response_observed_at_utc,reason FROM cleanup_batch_events LIMIT 0`} {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE (type='trigger' AND name IN ('cleanup_batch_events_no_update','cleanup_batch_events_no_delete','cleanup_batches_manifest_immutable')) OR (type='index' AND name='cleanup_batch_events_order')`).Scan(&count); err != nil {
		return err
	}
	if count != 4 {
		return fmt.Errorf("cleanup batch schema incomplete")
	}
	return nil
}
