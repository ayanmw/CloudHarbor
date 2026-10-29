package taskstore

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type CloudDuplicateScanState struct {
	AccountFingerprint string     `json:"account_fingerprint"`
	RunID              string     `json:"run_id"`
	Status             string     `json:"status"`
	Phase              string     `json:"phase"`
	NextFsID           int64      `json:"next_fs_id"`
	TotalFiles         int64      `json:"total_files"`
	ProcessedFiles     int64      `json:"processed_files"`
	StartedAtUTC       time.Time  `json:"started_at_utc"`
	UpdatedAtUTC       time.Time  `json:"updated_at_utc"`
	LastError          string     `json:"last_error,omitempty"`
	RetryDelayMillis   int64      `json:"retry_delay_ms,omitempty"`
	RetryAtUTC         *time.Time `json:"retry_at_utc,omitempty"`
}
type CloudDuplicateScanEvent struct {
	Sequence      int64           `json:"sequence"`
	RunID         string          `json:"run_id"`
	Status        string          `json:"status"`
	Phase         string          `json:"phase"`
	RecordedAtUTC time.Time       `json:"recorded_at_utc"`
	Evidence      json.RawMessage `json:"evidence,omitempty"`
}
type CloudDuplicateGroup struct {
	SHA256 string                `json:"sha256"`
	Size   int64                 `json:"size"`
	Keep   CloudInventoryEntry   `json:"keep"`
	Delete []CloudInventoryEntry `json:"delete"`
}

const countCloudDuplicateHashCandidatesSQL = `SELECT count(*)
FROM cloud_inventory i
CROSS JOIN cloud_inventory_file_sizes s
JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete'
WHERE i.account_fingerprint=? AND i.is_dir=0
AND s.account_fingerprint=i.account_fingerprint AND s.size_bytes=i.size_bytes AND s.file_count>1`

const listCloudDuplicateHashCandidatePageSQL = `SELECT i.fs_id,i.path,i.name,i.size_bytes,i.provider_mtime,i.content_md5
FROM cloud_inventory i
CROSS JOIN cloud_inventory_file_sizes s
JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete'
WHERE i.account_fingerprint=? AND i.is_dir=0 AND i.fs_id>?
AND s.account_fingerprint=i.account_fingerprint AND s.size_bytes=i.size_bytes AND s.file_count>1
ORDER BY i.fs_id LIMIT ?`

const listVerifiedCloudDuplicateGroupsSQL = `WITH duplicate_groups AS (
	SELECT h.account_fingerprint,h.sha256,h.size_bytes
	FROM cloud_content_hashes h INDEXED BY cloud_content_hashes_duplicate_group
	CROSS JOIN cloud_inventory i
	CROSS JOIN cloud_inventory_directories d
	WHERE h.account_fingerprint=?
	AND i.account_fingerprint=h.account_fingerprint AND i.fs_id=h.fs_id AND i.path=h.path AND i.size_bytes=h.size_bytes AND i.provider_mtime=h.provider_mtime AND i.is_dir=0
	AND d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete'
	GROUP BY h.account_fingerprint,h.sha256,h.size_bytes HAVING count(*)>1
)
SELECT h.sha256,i.size_bytes,i.fs_id,i.path,i.name,i.provider_mtime,i.content_md5,h.verified_at_utc
FROM duplicate_groups g
JOIN cloud_content_hashes h INDEXED BY cloud_content_hashes_duplicate_group ON h.account_fingerprint=g.account_fingerprint AND h.sha256=g.sha256 AND h.size_bytes=g.size_bytes
JOIN cloud_inventory i ON i.account_fingerprint=h.account_fingerprint AND i.fs_id=h.fs_id AND i.path=h.path AND i.size_bytes=h.size_bytes AND i.provider_mtime=h.provider_mtime AND i.is_dir=0
JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete'
ORDER BY h.sha256,i.path,i.fs_id`

func (s *Store) ListCloudDuplicateHashCandidates(account string, afterFsID int64, limit int) ([]CloudInventoryEntry, int, error) {
	if account == "" || afterFsID < 0 || limit < 1 || limit > 1000 {
		return nil, 0, fmt.Errorf("invalid duplicate candidate query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, sql.ErrConnDone
	}
	var total int
	if err := s.db.QueryRow(countCloudDuplicateHashCandidatesSQL, account).Scan(&total); err != nil {
		return nil, 0, err
	}
	items, err := listCloudDuplicateHashCandidatePage(s.db, account, afterFsID, limit)
	return items, total, err
}

func (s *Store) ListCloudDuplicateHashCandidatePage(account string, afterFsID int64, limit int) ([]CloudInventoryEntry, error) {
	if account == "" || afterFsID < 0 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid duplicate candidate query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, sql.ErrConnDone
	}
	return listCloudDuplicateHashCandidatePage(s.db, account, afterFsID, limit)
}

func listCloudDuplicateHashCandidatePage(db *sql.DB, account string, afterFsID int64, limit int) ([]CloudInventoryEntry, error) {
	rows, err := db.Query(listCloudDuplicateHashCandidatePageSQL, account, afterFsID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CloudInventoryEntry{}
	for rows.Next() {
		var item CloudInventoryEntry
		if err = rows.Scan(&item.FsID, &item.Path, &item.Name, &item.Size, &item.ProviderMtime, &item.ContentMD5); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SaveVerifiedCloudContentHash(account string, item CloudInventoryEntry, digest string, verified time.Time) error {
	b, err := hex.DecodeString(digest)
	if account == "" || len(b) != 32 || len(digest) != 64 || item.FsID <= 0 || item.Directory || verified.IsZero() {
		return fmt.Errorf("invalid verified cloud content hash")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	res, err := s.db.Exec(`INSERT INTO cloud_content_hashes(account_fingerprint,fs_id,path,size_bytes,provider_mtime,sha256,verified_at_utc) SELECT ?,i.fs_id,i.path,i.size_bytes,i.provider_mtime,?,? FROM cloud_inventory i JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete' WHERE i.account_fingerprint=? AND i.fs_id=? AND i.path=? AND i.is_dir=0 AND i.size_bytes=? AND i.provider_mtime=? ON CONFLICT(account_fingerprint,fs_id) DO UPDATE SET path=excluded.path,size_bytes=excluded.size_bytes,provider_mtime=excluded.provider_mtime,sha256=excluded.sha256,verified_at_utc=excluded.verified_at_utc`, account, digest, verified.UTC().UnixMicro(), account, item.FsID, item.Path, item.Size, item.ProviderMtime)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return fmt.Errorf("cloud inventory identity is stale or incomplete")
	}
	return nil
}
func (s *Store) ListVerifiedCloudDuplicateGroups(account string) ([]CloudDuplicateGroup, error) {
	if account == "" {
		return nil, fmt.Errorf("invalid account fingerprint")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, sql.ErrConnDone
	}
	rows, err := s.db.Query(listVerifiedCloudDuplicateGroupsSQL, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []CloudDuplicateGroup{}
	idx := map[string]int{}
	for rows.Next() {
		var sha string
		var size, fsid, mtime, verified int64
		var path, name, md5 string
		if err = rows.Scan(&sha, &size, &fsid, &path, &name, &mtime, &md5, &verified); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s:%d", sha, size)
		n, ok := idx[key]
		if !ok {
			n = len(groups)
			idx[key] = n
			groups = append(groups, CloudDuplicateGroup{SHA256: sha, Size: size})
		}
		g := &groups[n]
		e := CloudInventoryEntry{FsID: fsid, Path: path, Name: name, Size: size, ProviderMtime: mtime, ContentMD5: md5}
		verifiedAt := time.UnixMicro(verified).UTC()
		e.ContentVerifiedAtUTC = &verifiedAt
		if g.Keep.FsID == 0 {
			g.Keep = e
		} else {
			g.Delete = append(g.Delete, e)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}
func (s *Store) SaveCloudDuplicateScanState(v CloudDuplicateScanState, transition bool) error {
	if v.AccountFingerprint == "" || v.RunID == "" || !validRemoteScanStatus(v.Status) || v.NextFsID < 0 || v.TotalFiles < 0 || v.ProcessedFiles < 0 || v.RetryDelayMillis < 0 || v.StartedAtUTC.IsZero() || v.UpdatedAtUTC.IsZero() {
		return fmt.Errorf("invalid duplicate scan state")
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
	var retryAt any
	if v.RetryAtUTC != nil {
		retryAt = v.RetryAtUTC.UTC().UnixMicro()
	}
	_, err = tx.Exec(`INSERT INTO cloud_duplicate_scan_state(account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,started_at_utc,updated_at_utc,last_error,retry_delay_ms,retry_at_utc) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint) DO UPDATE SET run_id=excluded.run_id,status=excluded.status,phase=excluded.phase,next_fs_id=excluded.next_fs_id,total_files=excluded.total_files,processed_files=excluded.processed_files,started_at_utc=excluded.started_at_utc,updated_at_utc=excluded.updated_at_utc,last_error=excluded.last_error,retry_delay_ms=excluded.retry_delay_ms,retry_at_utc=excluded.retry_at_utc`, v.AccountFingerprint, v.RunID, v.Status, v.Phase, v.NextFsID, v.TotalFiles, v.ProcessedFiles, v.StartedAtUTC.UTC().UnixMicro(), v.UpdatedAtUTC.UTC().UnixMicro(), v.LastError, v.RetryDelayMillis, retryAt)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if transition {
		evidence := map[string]any{"processed_files": v.ProcessedFiles, "next_fs_id": v.NextFsID, "updated_at_utc": v.UpdatedAtUTC.UTC(), "last_error": v.LastError, "retry_delay_ms": v.RetryDelayMillis}
		if v.RetryAtUTC != nil {
			evidence["retry_at_utc"] = v.RetryAtUTC.UTC()
		}
		ev, _ := json.Marshal(evidence)
		_, err = tx.Exec(`INSERT INTO cloud_duplicate_scan_events(account_fingerprint,run_id,status,phase,recorded_at_utc,evidence_json) VALUES(?,?,?,?,?,?)`, v.AccountFingerprint, v.RunID, v.Status, v.Phase, v.UpdatedAtUTC.UTC().UnixMicro(), ev)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) LoadCloudDuplicateScanState(account string) (CloudDuplicateScanState, error) {
	var v CloudDuplicateScanState
	if account == "" {
		return v, fmt.Errorf("invalid account fingerprint")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return v, sql.ErrConnDone
	}
	var a, b int64
	var retryAt sql.NullInt64
	err := s.db.QueryRow(`SELECT account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,started_at_utc,updated_at_utc,last_error,retry_delay_ms,retry_at_utc FROM cloud_duplicate_scan_state WHERE account_fingerprint=?`, account).Scan(&v.AccountFingerprint, &v.RunID, &v.Status, &v.Phase, &v.NextFsID, &v.TotalFiles, &v.ProcessedFiles, &a, &b, &v.LastError, &v.RetryDelayMillis, &retryAt)
	if err != nil {
		return v, err
	}
	v.StartedAtUTC = time.UnixMicro(a).UTC()
	v.UpdatedAtUTC = time.UnixMicro(b).UTC()
	if retryAt.Valid {
		value := time.UnixMicro(retryAt.Int64).UTC()
		v.RetryAtUTC = &value
	}
	return v, nil
}
func (s *Store) CloudDuplicateScanEvents(account string, after int64, limit int) ([]CloudDuplicateScanEvent, error) {
	if account == "" || after < 0 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid duplicate event query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, sql.ErrConnDone
	}
	rows, err := s.db.Query(`SELECT sequence,run_id,status,phase,recorded_at_utc,evidence_json FROM cloud_duplicate_scan_events WHERE account_fingerprint=? AND sequence>? ORDER BY sequence LIMIT ?`, account, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CloudDuplicateScanEvent{}
	for rows.Next() {
		var e CloudDuplicateScanEvent
		var ts int64
		if err = rows.Scan(&e.Sequence, &e.RunID, &e.Status, &e.Phase, &ts, &e.Evidence); err != nil {
			return nil, err
		}
		e.RecordedAtUTC = time.UnixMicro(ts).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}
