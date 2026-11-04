package taskstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

type DownloadManifestFile struct {
	Entry       DownloadManifestEntry   `json:"entry"`
	Status      string                  `json:"status"`
	Error       string                  `json:"error,omitempty"`
	Interrupted bool                    `json:"interrupted"`
	StateAtUTC  *time.Time              `json:"state_at_utc,omitempty"`
	Result      *DownloadManifestResult `json:"result,omitempty"`
}
type DownloadManifestPage struct {
	Items  []DownloadManifestFile
	Total  int
	Counts map[string]int
	Offset int
	Limit  int
}

func (s *Store) RecordDownloadManifestState(ctx context.Context, id string, e DownloadManifestEntry, status, summary string) error {
	if !validDownloadManifestID(id) || e.FsID <= 0 || e.IsDir != 0 || (status != "downloading" && status != "failed" && status != "cancelled" && status != "auth_wait") || len(summary) > 8192 || !utf8.ValidString(summary) {
		return fmt.Errorf("invalid download file state")
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
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM download_manifest_entries e JOIN download_manifests m ON m.manifest_id=e.manifest_id WHERE e.manifest_id=? AND e.fs_id=? AND m.published_at_utc IS NOT NULL AND e.is_dir=0 AND e.path=? AND e.name=? AND e.size_bytes=? AND e.provider_mtime=? AND e.cloud_md5=? AND e.relative_path=?`, id, e.FsID, e.Path, e.Name, e.Size, e.Mtime, e.CloudMD5, e.RelativePath).Scan(&matched)
	if err != nil {
		return err
	}
	if matched != 1 {
		return fmt.Errorf("download state identity does not match published file")
	}
	var done int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM download_manifest_results WHERE manifest_id=? AND fs_id=?`, id, e.FsID).Scan(&done); err != nil {
		return err
	}
	if done != 0 {
		return tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO download_manifest_states(manifest_id,fs_id,status,error_summary,run_id,recorded_at_utc) VALUES(?,?,?,?,?,?) ON CONFLICT(manifest_id,fs_id) DO UPDATE SET status=excluded.status,error_summary=excluded.error_summary,run_id=excluded.run_id,recorded_at_utc=excluded.recorded_at_utc`, id, e.FsID, status, summary, s.runID, time.Now().UTC().UnixMicro())
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Status is resolved inside the read transaction. An old process's downloading
// state is an interrupted observation, never an automatically resumed transfer.
const downloadFileRowsCTE = `WITH file_rows AS (
SELECT e.*,
CASE WHEN r.fs_id IS NOT NULL THEN 'done' WHEN s.status='downloading' THEN CASE WHEN s.run_id=? AND ? THEN 'downloading' ELSE 'pending' END ELSE COALESCE(s.status,'pending') END AS file_status,
CASE WHEN r.fs_id IS NULL AND s.status='downloading' AND (s.run_id<>? OR NOT ?) THEN 1 ELSE 0 END AS interrupted,
COALESCE(s.error_summary,'') AS file_error,s.recorded_at_utc AS state_at,
r.kind AS result_kind,r.sha256 AS result_hash,r.recorded_at_utc AS result_at
FROM download_manifest_entries e LEFT JOIN download_manifest_states s ON s.manifest_id=e.manifest_id AND s.fs_id=e.fs_id LEFT JOIN download_manifest_results r ON r.manifest_id=e.manifest_id AND r.fs_id=e.fs_id
WHERE e.manifest_id=? AND e.is_dir=0)
`

func (s *Store) DownloadManifestFilePage(ctx context.Context, id, status, query string, offset, limit int, activeAllowed bool) (DownloadManifestPage, error) {
	page := DownloadManifestPage{Items: make([]DownloadManifestFile, 0), Counts: map[string]int{}, Offset: offset, Limit: limit}
	validStatus := status == "" || status == "pending" || status == "downloading" || status == "done" || status == "failed" || status == "cancelled" || status == "auth_wait" || status == "paused" || status == "retry_wait" || status == "source_unknown"
	if !validDownloadManifestID(id) || !validStatus || offset < 0 || limit < 1 || limit > 200 || utf8.RuneCountInString(query) > 128 {
		return page, fmt.Errorf("invalid download file page")
	}
	query = strings.ToLower(strings.TrimSpace(query))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return page, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	var sealed int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM download_manifests WHERE manifest_id=? AND published_at_utc IS NOT NULL`, id).Scan(&sealed); err != nil {
		return page, err
	}
	if sealed != 1 {
		return page, fmt.Errorf("download manifest is not published")
	}
	args := []any{s.runID, activeAllowed, s.runID, activeAllowed, id}
	counts, files, err := downloadManifestStatusCounts(ctx, tx, id, s.runID, activeAllowed)
	if err != nil {
		return page, err
	}
	page.Counts = counts
	where := ` WHERE (?='' OR file_status=?) AND (?='' OR instr(search_path,?)>0)`
	filtered := append(append([]any{}, args...), status, status, query, query)
	if query == "" {
		page.Total = files
		if status != "" {
			page.Total = counts[status]
		}
	} else if err = tx.QueryRowContext(ctx, downloadFileRowsCTE+`SELECT count(*) FROM file_rows`+where, filtered...).Scan(&page.Total); err != nil {
		return page, err
	}
	if page.Offset > page.Total {
		page.Offset = page.Total
	}
	if page.Total == 0 {
		return page, tx.Commit()
	}
	paged := append(append([]any{}, filtered...), limit, page.Offset)
	rows, err := tx.QueryContext(ctx, downloadFileRowsCTE+`SELECT fs_id,path,name,is_dir,size_bytes,provider_mtime,cloud_md5,relative_path,observed_at_utc,observation_source,file_status,interrupted,file_error,state_at,result_kind,result_hash,result_at FROM file_rows`+where+` ORDER BY CASE file_status WHEN 'downloading' THEN 0 WHEN 'failed' THEN 1 WHEN 'auth_wait' THEN 1 WHEN 'pending' THEN 2 WHEN 'cancelled' THEN 4 ELSE 5 END,path,fs_id LIMIT ? OFFSET ?`, paged...)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var f DownloadManifestFile
		var observed int64
		var stateAt, resultAt sql.NullInt64
		var kind, hash sql.NullString
		e := &f.Entry
		if err = rows.Scan(&e.FsID, &e.Path, &e.Name, &e.IsDir, &e.Size, &e.Mtime, &e.CloudMD5, &e.RelativePath, &observed, &e.ObservationSource, &f.Status, &f.Interrupted, &f.Error, &stateAt, &kind, &hash, &resultAt); err != nil {
			rows.Close()
			return page, err
		}
		e.ObservedAtUTC = time.UnixMicro(observed).UTC()
		if stateAt.Valid {
			at := time.UnixMicro(stateAt.Int64).UTC()
			f.StateAtUTC = &at
		}
		if resultAt.Valid {
			f.Result = &DownloadManifestResult{Kind: kind.String, SHA256: hash.String, RecordedAtUTC: time.UnixMicro(resultAt.Int64).UTC()}
		}
		page.Items = append(page.Items, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	return page, tx.Commit()
}
