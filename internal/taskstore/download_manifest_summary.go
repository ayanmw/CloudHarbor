package taskstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

type DownloadManifestSummary struct {
	Manifest       DownloadManifest      `json:"manifest"`
	Root           DownloadManifestEntry `json:"root"`
	Files          int                   `json:"files"`
	Folders        int                   `json:"folders"`
	Size           int64                 `json:"size"`
	Completed      int                   `json:"completed"`
	CompletedBytes int64                 `json:"completed_bytes"`
}

func (s *Store) CurrentDownloadManifestSummary(ctx context.Context, rootID int64) (*DownloadManifestSummary, error) {
	if rootID <= 0 {
		return nil, fmt.Errorf("invalid download root")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var result DownloadManifestSummary
	var started, published, observed int64
	m, e := &result.Manifest, &result.Root
	err = tx.QueryRowContext(ctx, `SELECT m.manifest_id,m.root_id,m.root_path,m.attempt_id,m.started_at_utc,m.published_at_utc,e.fs_id,e.path,e.name,e.is_dir,e.size_bytes,e.provider_mtime,e.cloud_md5,e.relative_path,e.observed_at_utc,e.observation_source FROM download_manifest_current c JOIN download_manifests m ON m.manifest_id=c.manifest_id JOIN download_manifest_entries e ON e.manifest_id=m.manifest_id AND e.fs_id=m.root_id WHERE c.root_id=?`, rootID).Scan(&m.ID, &m.RootID, &m.RootPath, &m.AttemptID, &started, &published, &e.FsID, &e.Path, &e.Name, &e.IsDir, &e.Size, &e.Mtime, &e.CloudMD5, &e.RelativePath, &observed, &e.ObservationSource)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.StartedAtUTC = time.UnixMicro(started).UTC()
	at := time.UnixMicro(published).UTC()
	m.PublishedAtUTC = &at
	e.ObservedAtUTC = time.UnixMicro(observed).UTC()
	err = tx.QueryRowContext(ctx, `SELECT files,folders,size_bytes,completed,completed_bytes FROM download_manifest_totals WHERE manifest_id=?`, m.ID).Scan(&result.Files, &result.Folders, &result.Size, &result.Completed, &result.CompletedBytes)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &result, nil
}
