package taskstore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

// RecordedAtUTC observes this local success; it never reconstructs a previous
// process's completion time. A reused file is a new verification observation.
type DownloadManifestResult struct {
	Kind          string    `json:"kind"`
	SHA256        string    `json:"sha256"`
	RecordedAtUTC time.Time `json:"recorded_at_utc"`
}

func (s *Store) RecordDownloadManifestResult(ctx context.Context, id string, expected DownloadManifestEntry, result DownloadManifestResult) error {
	digest, err := hex.DecodeString(result.SHA256)
	if !validDownloadManifestID(id) || expected.FsID <= 0 || expected.IsDir != 0 || (result.Kind != "published" && result.Kind != "local_verified") || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != result.SHA256 || result.RecordedAtUTC.IsZero() {
		return fmt.Errorf("invalid download manifest result")
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
	var matched int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM download_manifest_entries e JOIN download_manifests m ON m.manifest_id=e.manifest_id JOIN download_manifest_totals t ON t.manifest_id=m.manifest_id WHERE e.manifest_id=? AND e.fs_id=? AND m.published_at_utc IS NOT NULL AND e.is_dir=0 AND e.path=? AND e.name=? AND e.size_bytes=? AND e.provider_mtime=? AND e.cloud_md5=? AND e.relative_path=?`, id, expected.FsID, expected.Path, expected.Name, expected.Size, expected.Mtime, expected.CloudMD5, expected.RelativePath).Scan(&matched); err != nil {
		return err
	}
	if matched != 1 {
		return fmt.Errorf("download result identity does not match a published file manifest")
	}
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT sha256 FROM download_manifest_results WHERE manifest_id=? AND fs_id=?`, id, expected.FsID).Scan(&previous)
	if err == nil {
		if previous != result.SHA256 {
			return fmt.Errorf("download result digest conflicts with recorded evidence")
		}
		return tx.Commit()
	}
	if err != sql.ErrNoRows {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO download_manifest_results(manifest_id,fs_id,kind,sha256,recorded_at_utc) VALUES(?,?,?,?,?)`, id, expected.FsID, result.Kind, result.SHA256, result.RecordedAtUTC.UTC().UnixMicro()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DownloadManifestFileResult(ctx context.Context, id string, fsID int64) (*DownloadManifestResult, error) {
	if !validDownloadManifestID(id) || fsID <= 0 {
		return nil, fmt.Errorf("invalid download result identity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	var r DownloadManifestResult
	var recorded int64
	err := s.db.QueryRowContext(ctx, `SELECT kind,sha256,recorded_at_utc FROM download_manifest_results WHERE manifest_id=? AND fs_id=?`, id, fsID).Scan(&r.Kind, &r.SHA256, &recorded)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.RecordedAtUTC = time.UnixMicro(recorded).UTC()
	return &r, nil
}
