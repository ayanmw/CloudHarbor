package taskstore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

var ErrCloudDuplicateInventoryScanConflict = errors.New("cloud inventory scan overlaps duplicate identity reconciliation")

type CloudDuplicateScanState struct {
	AccountFingerprint     string     `json:"account_fingerprint"`
	RunID                  string     `json:"run_id"`
	Status                 string     `json:"status"`
	Phase                  string     `json:"phase"`
	NextFsID               int64      `json:"next_fs_id"`
	TotalFiles             int64      `json:"total_files"`
	ProcessedFiles         int64      `json:"processed_files"`
	TotalBytes             int64      `json:"total_bytes"`
	ProcessedBytes         int64      `json:"processed_bytes"`
	StartedAtUTC           time.Time  `json:"started_at_utc"`
	UpdatedAtUTC           time.Time  `json:"updated_at_utc"`
	LastError              string     `json:"last_error,omitempty"`
	RetryDelayMillis       int64      `json:"retry_delay_ms,omitempty"`
	RetryBackoffBaseMillis int64      `json:"-"`
	RetryAtUTC             *time.Time `json:"retry_at_utc,omitempty"`
	CurrentFsID            int64      `json:"current_fs_id,omitempty"`
	CurrentPath            string     `json:"current_path,omitempty"`
	CurrentName            string     `json:"current_name,omitempty"`
	CurrentSize            int64      `json:"current_size,omitempty"`
	CurrentProviderMtime   int64      `json:"current_provider_mtime,omitempty"`
	CurrentContentMD5      string     `json:"current_content_md5,omitempty"`
	CurrentBytes           int64      `json:"current_bytes,omitempty"`
	CurrentSHA256State     []byte     `json:"-"`
}
type CloudDuplicateHashCandidateEstimate struct {
	Files          int64 `json:"files"`
	EstimatedBytes int64 `json:"estimated_bytes"`
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

const estimateCloudDuplicateHashCandidatesSQL = `SELECT count(*),coalesce(sum(i.size_bytes),0)
FROM cloud_inventory i
CROSS JOIN cloud_inventory_file_sizes s
JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete'
WHERE i.account_fingerprint=? AND i.is_dir=0 AND i.fs_id>?
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

const countVerifiedCloudDuplicateGroupsSQL = `WITH duplicate_groups AS (
	SELECT h.sha256,h.size_bytes,count(*) AS file_count
	FROM cloud_content_hashes h INDEXED BY cloud_content_hashes_duplicate_group
	JOIN cloud_inventory i ON i.account_fingerprint=h.account_fingerprint AND i.fs_id=h.fs_id AND i.path=h.path AND i.size_bytes=h.size_bytes AND i.provider_mtime=h.provider_mtime AND i.is_dir=0
	JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete'
	WHERE h.account_fingerprint=?
	GROUP BY h.sha256,h.size_bytes HAVING count(*)>1
)
SELECT count(*),coalesce(sum(file_count-1),0) FROM duplicate_groups`

const listVerifiedCloudDuplicateGroupsPageSQL = `WITH duplicate_groups AS (
	SELECT h.account_fingerprint,h.sha256,h.size_bytes
	FROM cloud_content_hashes h INDEXED BY cloud_content_hashes_duplicate_group
	JOIN cloud_inventory i ON i.account_fingerprint=h.account_fingerprint AND i.fs_id=h.fs_id AND i.path=h.path AND i.size_bytes=h.size_bytes AND i.provider_mtime=h.provider_mtime AND i.is_dir=0
	JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete'
	WHERE h.account_fingerprint=?
	GROUP BY h.sha256,h.size_bytes HAVING count(*)>1
	ORDER BY h.sha256,h.size_bytes LIMIT ? OFFSET ?
)
SELECT h.sha256,i.size_bytes,i.fs_id,i.path,i.name,i.provider_mtime,i.content_md5,h.verified_at_utc
FROM duplicate_groups g
JOIN cloud_content_hashes h INDEXED BY cloud_content_hashes_duplicate_group ON h.account_fingerprint=g.account_fingerprint AND h.sha256=g.sha256 AND h.size_bytes=g.size_bytes
JOIN cloud_inventory i ON i.account_fingerprint=h.account_fingerprint AND i.fs_id=h.fs_id AND i.path=h.path AND i.size_bytes=h.size_bytes AND i.provider_mtime=h.provider_mtime AND i.is_dir=0
JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete'
ORDER BY h.sha256,h.size_bytes,i.path,i.fs_id`

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

func (s *Store) EstimateCloudDuplicateHashCandidates(account string, afterFsID int64) (CloudDuplicateHashCandidateEstimate, error) {
	var estimate CloudDuplicateHashCandidateEstimate
	if account == "" || len(account) > 128 || afterFsID < 0 {
		return estimate, fmt.Errorf("invalid account fingerprint")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return estimate, sql.ErrConnDone
	}
	err := s.db.QueryRow(estimateCloudDuplicateHashCandidatesSQL, account, afterFsID).Scan(&estimate.Files, &estimate.EstimatedBytes)
	return estimate, err
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
	res, err := s.db.Exec(`INSERT INTO cloud_content_hashes(account_fingerprint,fs_id,path,size_bytes,provider_mtime,sha256,verified_at_utc) SELECT ?,i.fs_id,i.path,i.size_bytes,i.provider_mtime,?,? FROM cloud_inventory i JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete' WHERE i.account_fingerprint=? AND i.fs_id=? AND i.path=? AND i.is_dir=0 AND i.size_bytes=? AND i.provider_mtime=? AND (i.content_md5='' OR ?='' OR lower(i.content_md5)=lower(?)) ON CONFLICT(account_fingerprint,fs_id) DO UPDATE SET path=excluded.path,size_bytes=excluded.size_bytes,provider_mtime=excluded.provider_mtime,sha256=excluded.sha256,verified_at_utc=excluded.verified_at_utc`, account, digest, verified.UTC().UnixMicro(), account, item.FsID, item.Path, item.Size, item.ProviderMtime, item.ContentMD5, item.ContentMD5)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return fmt.Errorf("cloud inventory identity is stale or incomplete")
	}
	return nil
}

// CompleteCloudDuplicateCandidate commits the verified hash and scan cursor
// together so a crash cannot leave a completed hash paired with a stale offset.
func (s *Store) CompleteCloudDuplicateCandidate(account string, state CloudDuplicateScanState, item CloudInventoryEntry, digest string, verified time.Time) error {
	b, err := hex.DecodeString(digest)
	if account == "" || state.AccountFingerprint != account || item.FsID <= state.NextFsID || item.FsID != state.CurrentFsID || item.Path != state.CurrentPath || item.Name != state.CurrentName || item.Size != state.CurrentSize || item.ProviderMtime != state.CurrentProviderMtime || item.ContentMD5 != state.CurrentContentMD5 || state.CurrentBytes != item.Size || len(b) != 32 || len(digest) != 64 || item.Directory || verified.IsZero() {
		return fmt.Errorf("invalid completed duplicate candidate")
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
	res, err := tx.Exec(`INSERT INTO cloud_content_hashes(account_fingerprint,fs_id,path,size_bytes,provider_mtime,sha256,verified_at_utc) SELECT ?,i.fs_id,i.path,i.size_bytes,i.provider_mtime,?,? FROM cloud_inventory i JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete' WHERE i.account_fingerprint=? AND i.fs_id=? AND i.path=? AND i.is_dir=0 AND i.size_bytes=? AND i.provider_mtime=? AND (i.content_md5='' OR ?='' OR lower(i.content_md5)=lower(?)) ON CONFLICT(account_fingerprint,fs_id) DO UPDATE SET path=excluded.path,size_bytes=excluded.size_bytes,provider_mtime=excluded.provider_mtime,sha256=excluded.sha256,verified_at_utc=excluded.verified_at_utc`, account, digest, verified.UTC().UnixMicro(), account, item.FsID, item.Path, item.Size, item.ProviderMtime, item.ContentMD5, item.ContentMD5)
	if err != nil {
		return err
	}
	if n, e := res.RowsAffected(); e != nil || n != 1 {
		if e != nil {
			return e
		}
		return fmt.Errorf("cloud inventory identity is stale or incomplete")
	}
	res, err = tx.Exec(`UPDATE cloud_duplicate_scan_state SET next_fs_id=?,processed_files=processed_files+1,processed_bytes=processed_bytes+?,updated_at_utc=?,current_fs_id=0,current_path='',current_name='',current_size=0,current_provider_mtime=0,current_content_md5='',current_bytes=0,current_sha256_state=x'' WHERE account_fingerprint=? AND run_id=? AND next_fs_id=? AND current_fs_id=? AND current_path=? AND current_name=? AND current_size=? AND current_provider_mtime=? AND current_content_md5=? AND current_bytes=? AND current_sha256_state=?`, item.FsID, item.Size, verified.UTC().UnixMicro(), account, state.RunID, state.NextFsID, item.FsID, item.Path, item.Name, item.Size, item.ProviderMtime, item.ContentMD5, item.Size, state.CurrentSHA256State)
	if err != nil {
		return err
	}
	if n, e := res.RowsAffected(); e != nil || n != 1 {
		if e != nil {
			return e
		}
		return fmt.Errorf("duplicate scan checkpoint changed before completion")
	}
	return tx.Commit()
}

// ReconcileCloudDuplicateCandidateChange applies a live provider identity
// observation and pauses the scan in the same SQLite transaction. A nil
// observation means the metadata response did not uniquely identify the file;
// in that case the cached item and its verified hash are retained until a
// complete directory refresh determines whether the item still exists.
func (s *Store) ReconcileCloudDuplicateCandidateChange(account string, state CloudDuplicateScanState, expected CloudInventoryEntry, observed *CloudInventoryEntry, providerObservedAt, recordedAt time.Time) error {
	if account == "" || len(account) > 128 || state.AccountFingerprint != account || state.RunID == "" || state.Status != "running" || state.NextFsID < 0 || state.NextFsID >= expected.FsID || expected.FsID <= 0 || expected.Directory || !validInventoryPath(expected.Path) || expected.Name == "" || path.Base(expected.Path) != expected.Name || expected.Size < 0 || state.CurrentFsID != expected.FsID || state.CurrentPath != expected.Path || state.CurrentName != expected.Name || state.CurrentSize != expected.Size || state.CurrentProviderMtime != expected.ProviderMtime || state.CurrentContentMD5 != expected.ContentMD5 || recordedAt.IsZero() {
		return fmt.Errorf("invalid duplicate candidate reconciliation")
	}
	if observed != nil {
		if providerObservedAt.IsZero() || observed.FsID != expected.FsID || observed.Directory || !validInventoryPath(observed.Path) || observed.Name == "" || path.Base(observed.Path) != observed.Name || observed.Size < 0 || len(observed.Name) > 1024 || len(observed.ContentMD5) > 256 {
			return fmt.Errorf("invalid observed duplicate candidate identity")
		}
	} else if !providerObservedAt.IsZero() {
		return fmt.Errorf("unavailable duplicate candidate cannot have provider observation time")
	}
	if len(expected.Name) > 1024 || len(expected.ContentMD5) > 256 || state.CurrentBytes < 0 || state.CurrentBytes > expected.Size || len(state.CurrentSHA256State) == 0 {
		return fmt.Errorf("invalid duplicate candidate checkpoint")
	}
	checkpointBytes := state.CurrentBytes
	checkpointHash := append([]byte(nil), state.CurrentSHA256State...)

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

	oldParent := path.Dir(expected.Path)
	parents := []string{oldParent}
	if observed != nil && path.Dir(observed.Path) != oldParent {
		parents = append(parents, path.Dir(observed.Path))
	}
	conflict, err := cloudDuplicateReconciliationScanConflict(tx, account, parents)
	if err != nil {
		return err
	}
	deferred := conflict
	if !deferred {
		var cachedPath, cachedName, cachedMD5 string
		var cachedParent string
		var cachedSize, cachedMtime int64
		if err = tx.QueryRow(`SELECT path,parent_path,name,size_bytes,provider_mtime,content_md5 FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=? AND is_dir=0`, account, expected.FsID).Scan(&cachedPath, &cachedParent, &cachedName, &cachedSize, &cachedMtime, &cachedMD5); err != nil {
			return err
		}
		if cachedPath != expected.Path || cachedParent != oldParent || cachedName != expected.Name || cachedSize != expected.Size || cachedMtime != expected.ProviderMtime || cachedMD5 != expected.ContentMD5 {
			return fmt.Errorf("duplicate candidate inventory identity changed before reconciliation")
		}
		if observed != nil {
			result, updateErr := tx.Exec(`UPDATE cloud_inventory SET parent_path=?,path=?,name=?,size_bytes=?,provider_mtime=?,content_md5=?,observed_at_utc=? WHERE account_fingerprint=? AND fs_id=? AND path=? AND parent_path=? AND name=? AND size_bytes=? AND provider_mtime=? AND content_md5=? AND is_dir=0`, path.Dir(observed.Path), observed.Path, observed.Name, observed.Size, observed.ProviderMtime, observed.ContentMD5, providerObservedAt.UTC().UnixMicro(), account, expected.FsID, expected.Path, oldParent, expected.Name, expected.Size, expected.ProviderMtime, expected.ContentMD5)
			if updateErr != nil {
				return updateErr
			}
			if n, rowsErr := result.RowsAffected(); rowsErr != nil || n != 1 {
				if rowsErr != nil {
					return rowsErr
				}
				return fmt.Errorf("duplicate candidate inventory identity changed before update")
			}
			if _, err = tx.Exec(`DELETE FROM cloud_content_hashes WHERE account_fingerprint=? AND fs_id=?`, account, expected.FsID); err != nil {
				return err
			}
		}
		for _, parent := range parents {
			prefix, upper := cloudDirectoryPathRange(parent)
			if _, err = tx.Exec(`UPDATE cloud_inventory_directories SET status='stale',observed_at_utc=NULL WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))`, account, parent, prefix, upper); err != nil {
				return err
			}
			if _, err = tx.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,last_run_id) VALUES(?,?,'stale',?) ON CONFLICT(account_fingerprint,path) DO UPDATE SET status='stale',observed_at_utc=NULL,last_run_id=excluded.last_run_id`, account, parent, "duplicate-reconcile:"+state.RunID); err != nil {
				return err
			}
		}
		if err = bumpCloudInventoryGeneration(tx, account, recordedAt.UTC()); err != nil {
			return err
		}
	}

	state.Status = "paused"
	state.Phase = "inventory_changed"
	state.UpdatedAtUTC = recordedAt.UTC()
	state.RetryAtUTC = nil
	state.RetryDelayMillis = 0
	state.CurrentFsID = 0
	state.CurrentPath = ""
	state.CurrentName = ""
	state.CurrentSize = 0
	state.CurrentProviderMtime = 0
	state.CurrentContentMD5 = ""
	state.CurrentBytes = 0
	state.CurrentSHA256State = []byte{}
	if deferred {
		state.LastError = "网盘对象信息发生变化，但相关目录正在缓存扫描；等待该扫描结束后确认缓存完整，再恢复重复扫描。"
	} else if observed == nil {
		state.LastError = "百度未返回可唯一确认的文件信息；已保留本地对象记录并将所在目录标记为待刷新。刷新目录后再恢复重复扫描。"
	} else {
		state.LastError = fmt.Sprintf("检测到网盘文件信息变化，已更新本地对象并清除旧哈希；请刷新目录 %s 后恢复重复扫描。", strings.Join(parents, "、"))
	}
	result, err := tx.Exec(`UPDATE cloud_duplicate_scan_state SET status=?,phase=?,updated_at_utc=?,last_error=?,retry_delay_ms=0,retry_backoff_base_millis=0,retry_at_utc=NULL,current_fs_id=0,current_path='',current_name='',current_size=0,current_provider_mtime=0,current_content_md5='',current_bytes=0,current_sha256_state=x'' WHERE account_fingerprint=? AND run_id=? AND status='running' AND next_fs_id=? AND current_fs_id=? AND current_path=? AND current_name=? AND current_size=? AND current_provider_mtime=? AND current_content_md5=? AND current_bytes=? AND current_sha256_state=?`, state.Status, state.Phase, recordedAt.UTC().UnixMicro(), state.LastError, account, state.RunID, state.NextFsID, expected.FsID, expected.Path, expected.Name, expected.Size, expected.ProviderMtime, expected.ContentMD5, checkpointBytes, checkpointHash)
	if err != nil {
		return err
	}
	if n, rowsErr := result.RowsAffected(); rowsErr != nil || n != 1 {
		if rowsErr != nil {
			return rowsErr
		}
		return fmt.Errorf("duplicate scan checkpoint changed before reconciliation")
	}
	evidence := map[string]any{
		"change_kind":               "cloud_identity_changed",
		"old_identity":              expected,
		"observed_identity":         observed,
		"affected_directories":      parents,
		"provider_observed_at_utc":  nullableTimePtr(providerObservedAt, observed != nil),
		"local_recorded_at_utc":     recordedAt.UTC(),
		"reconciliation_deferred":   deferred,
		"next_fs_id_preserved":      state.NextFsID,
		"completed_files_preserved": state.ProcessedFiles,
		"completed_bytes_preserved": state.ProcessedBytes,
	}
	if observed == nil {
		evidence["change_kind"] = "identity_unavailable"
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO cloud_duplicate_scan_events(account_fingerprint,run_id,status,phase,recorded_at_utc,evidence_json) VALUES(?,?,?,?,?,?)`, account, state.RunID, state.Status, state.Phase, recordedAt.UTC().UnixMicro(), evidenceJSON); err != nil {
		return err
	}
	return tx.Commit()
}

func cloudDuplicateReconciliationScanConflict(tx *sql.Tx, account string, parents []string) (bool, error) {
	for _, query := range []string{
		`SELECT path FROM cloud_inventory_scan_queue WHERE account_fingerprint=?`,
		`SELECT current_directory FROM cloud_inventory_scan_state WHERE account_fingerprint=? AND status IN ('running','paused')`,
		`SELECT path FROM cloud_inventory_refresh_queue WHERE account_fingerprint=?`,
		`SELECT root_path FROM cloud_inventory_refresh_state WHERE account_fingerprint=? AND status IN ('running','paused')`,
		`SELECT current_directory FROM cloud_inventory_refresh_state WHERE account_fingerprint=? AND status IN ('running','paused')`,
	} {
		rows, err := tx.Query(query, account)
		if err != nil {
			return false, err
		}
		for rows.Next() {
			var scanPath string
			if err = rows.Scan(&scanPath); err != nil {
				rows.Close()
				return false, err
			}
			if !validInventoryPath(scanPath) {
				continue
			}
			for _, parent := range parents {
				if cloudInventoryPathWithin(scanPath, parent) || cloudInventoryPathWithin(parent, scanPath) {
					rows.Close()
					return true, nil
				}
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return false, err
		}
		if err = rows.Close(); err != nil {
			return false, err
		}
	}
	return false, nil
}

func nullableTimePtr(value time.Time, present bool) *time.Time {
	if !present || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

// RefreshCloudInventoryMD5 records a provider-observed MD5 change and invalidates
// the SHA-256 derived from the old cloud identity in the same transaction.
func (s *Store) RefreshCloudInventoryMD5(account string, item CloudInventoryEntry, md5 string, observed time.Time) error {
	if account == "" || len(account) > 128 || item.FsID <= 0 || item.Directory || item.Path == "" || md5 == "" || observed.IsZero() {
		return fmt.Errorf("invalid cloud inventory MD5 refresh")
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
	res, err := tx.Exec(`UPDATE cloud_inventory SET content_md5=?,observed_at_utc=? WHERE account_fingerprint=? AND fs_id=? AND path=? AND parent_path=? AND is_dir=0 AND size_bytes=? AND provider_mtime=? AND lower(content_md5)=lower(?) AND lower(content_md5)<>lower(?) AND EXISTS(SELECT 1 FROM cloud_inventory_directories d WHERE d.account_fingerprint=? AND d.path=cloud_inventory.parent_path AND d.status='complete')`, md5, observed.UTC().UnixMicro(), account, item.FsID, item.Path, path.Dir(item.Path), item.Size, item.ProviderMtime, item.ContentMD5, md5, account)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("cloud inventory identity changed before MD5 refresh")
	}
	if _, err = tx.Exec(`DELETE FROM cloud_content_hashes WHERE account_fingerprint=? AND fs_id=?`, account, item.FsID); err != nil {
		return err
	}
	if err = bumpCloudInventoryGeneration(tx, account, observed.UTC()); err != nil {
		return err
	}
	return tx.Commit()
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

func (s *Store) ListVerifiedCloudDuplicateGroupsPage(account string, offset, limit int) ([]CloudDuplicateGroup, int, int, error) {
	if account == "" || offset < 0 || offset > 1_000_000 || limit < 1 || limit > 50 {
		return nil, 0, 0, fmt.Errorf("invalid duplicate preview page")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, 0, sql.ErrConnDone
	}
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, 0, err
	}
	defer tx.Rollback()
	var totalGroups, totalDelete int
	if err = tx.QueryRow(countVerifiedCloudDuplicateGroupsSQL, account).Scan(&totalGroups, &totalDelete); err != nil {
		return nil, 0, 0, err
	}
	rows, err := tx.Query(listVerifiedCloudDuplicateGroupsPageSQL, account, limit, offset)
	if err != nil {
		return nil, 0, 0, err
	}
	groups, err := scanVerifiedCloudDuplicateGroups(rows)
	if err != nil {
		return nil, 0, 0, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, 0, err
	}
	return groups, totalGroups, totalDelete, nil
}

func scanVerifiedCloudDuplicateGroups(rows *sql.Rows) ([]CloudDuplicateGroup, error) {
	defer rows.Close()
	groups := []CloudDuplicateGroup{}
	idx := map[string]int{}
	for rows.Next() {
		var sha string
		var size, fsid, mtime, verified int64
		var path, name, md5 string
		if err := rows.Scan(&sha, &size, &fsid, &path, &name, &mtime, &md5, &verified); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s:%d", sha, size)
		n, ok := idx[key]
		if !ok {
			n = len(groups)
			idx[key] = n
			groups = append(groups, CloudDuplicateGroup{SHA256: sha, Size: size})
		}
		entry := CloudInventoryEntry{FsID: fsid, Path: path, Name: name, Size: size, ProviderMtime: mtime, ContentMD5: md5}
		verifiedAt := time.UnixMicro(verified).UTC()
		entry.ContentVerifiedAtUTC = &verifiedAt
		if groups[n].Keep.FsID == 0 {
			groups[n].Keep = entry
		} else {
			groups[n].Delete = append(groups[n].Delete, entry)
		}
	}
	return groups, rows.Err()
}
func (s *Store) SaveCloudDuplicateScanState(v CloudDuplicateScanState, transition bool) error {
	if v.AccountFingerprint == "" || v.RunID == "" || !validRemoteScanStatus(v.Status) || v.NextFsID < 0 || v.TotalFiles < 0 || v.ProcessedFiles < 0 || v.ProcessedFiles > v.TotalFiles || v.TotalBytes < 0 || v.ProcessedBytes < 0 || v.ProcessedBytes > v.TotalBytes || v.RetryDelayMillis < 0 || v.RetryDelayMillis > 3_600_000 || v.RetryBackoffBaseMillis < 0 || v.RetryBackoffBaseMillis > 3_600_000 || v.StartedAtUTC.IsZero() || v.UpdatedAtUTC.IsZero() {
		return fmt.Errorf("invalid duplicate scan state")
	}
	if v.CurrentFsID == 0 {
		if v.CurrentPath != "" || v.CurrentName != "" || v.CurrentSize != 0 || v.CurrentProviderMtime != 0 || v.CurrentContentMD5 != "" || v.CurrentBytes != 0 || len(v.CurrentSHA256State) != 0 {
			return fmt.Errorf("duplicate scan has incomplete active-file identity")
		}
	} else if v.CurrentFsID <= v.NextFsID || !validInventoryPath(v.CurrentPath) || path.Base(v.CurrentPath) != v.CurrentName || v.CurrentName == "" || v.CurrentSize < 0 || v.CurrentBytes < 0 || v.CurrentBytes > v.CurrentSize || v.CurrentBytes > v.TotalBytes-v.ProcessedBytes || len(v.CurrentContentMD5) > 256 || len(v.CurrentSHA256State) == 0 || len(v.CurrentSHA256State) > 1024 {
		return fmt.Errorf("invalid duplicate scan active-file checkpoint")
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
	_, err = tx.Exec(`INSERT INTO cloud_duplicate_scan_state(account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,total_bytes,processed_bytes,started_at_utc,updated_at_utc,last_error,retry_delay_ms,retry_backoff_base_millis,retry_at_utc,current_fs_id,current_path,current_name,current_size,current_provider_mtime,current_content_md5,current_bytes,current_sha256_state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint) DO UPDATE SET run_id=excluded.run_id,status=excluded.status,phase=excluded.phase,next_fs_id=excluded.next_fs_id,total_files=excluded.total_files,processed_files=excluded.processed_files,total_bytes=excluded.total_bytes,processed_bytes=excluded.processed_bytes,started_at_utc=excluded.started_at_utc,updated_at_utc=excluded.updated_at_utc,last_error=excluded.last_error,retry_delay_ms=excluded.retry_delay_ms,retry_backoff_base_millis=excluded.retry_backoff_base_millis,retry_at_utc=excluded.retry_at_utc,current_fs_id=excluded.current_fs_id,current_path=excluded.current_path,current_name=excluded.current_name,current_size=excluded.current_size,current_provider_mtime=excluded.current_provider_mtime,current_content_md5=excluded.current_content_md5,current_bytes=excluded.current_bytes,current_sha256_state=excluded.current_sha256_state`, v.AccountFingerprint, v.RunID, v.Status, v.Phase, v.NextFsID, v.TotalFiles, v.ProcessedFiles, v.TotalBytes, v.ProcessedBytes, v.StartedAtUTC.UTC().UnixMicro(), v.UpdatedAtUTC.UTC().UnixMicro(), v.LastError, v.RetryDelayMillis, v.RetryBackoffBaseMillis, retryAt, v.CurrentFsID, v.CurrentPath, v.CurrentName, v.CurrentSize, v.CurrentProviderMtime, v.CurrentContentMD5, v.CurrentBytes, nonNilBytes(v.CurrentSHA256State))
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if transition {
		evidence := map[string]any{"processed_files": v.ProcessedFiles, "total_files": v.TotalFiles, "processed_bytes": v.ProcessedBytes, "total_bytes": v.TotalBytes, "next_fs_id": v.NextFsID, "current_fs_id": v.CurrentFsID, "current_path": v.CurrentPath, "current_bytes": v.CurrentBytes, "updated_at_utc": v.UpdatedAtUTC.UTC(), "last_error": v.LastError, "retry_delay_ms": v.RetryDelayMillis}
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
	if err = syncCloudDuplicateScanRun(tx, v); err != nil {
		_ = tx.Rollback()
		return err
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
	err := s.db.QueryRow(`SELECT account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,total_bytes,processed_bytes,started_at_utc,updated_at_utc,last_error,retry_delay_ms,retry_backoff_base_millis,retry_at_utc,current_fs_id,current_path,current_name,current_size,current_provider_mtime,current_content_md5,current_bytes,current_sha256_state FROM cloud_duplicate_scan_state WHERE account_fingerprint=?`, account).Scan(&v.AccountFingerprint, &v.RunID, &v.Status, &v.Phase, &v.NextFsID, &v.TotalFiles, &v.ProcessedFiles, &v.TotalBytes, &v.ProcessedBytes, &a, &b, &v.LastError, &v.RetryDelayMillis, &v.RetryBackoffBaseMillis, &retryAt, &v.CurrentFsID, &v.CurrentPath, &v.CurrentName, &v.CurrentSize, &v.CurrentProviderMtime, &v.CurrentContentMD5, &v.CurrentBytes, &v.CurrentSHA256State)
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

func (s *Store) CloudDuplicateScanEventPage(account string, before int64, limit int) ([]CloudDuplicateScanEvent, int64, bool, error) {
	if account == "" || len(account) > 128 || before < 0 || limit < 1 || limit > 50 {
		return nil, 0, false, fmt.Errorf("invalid duplicate event page")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, false, sql.ErrConnDone
	}
	rows, err := s.db.Query(`SELECT sequence,run_id,status,phase,recorded_at_utc,evidence_json FROM cloud_duplicate_scan_events WHERE account_fingerprint=? AND (?=0 OR sequence<?) ORDER BY sequence DESC LIMIT ?`, account, before, before, limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()
	out := []CloudDuplicateScanEvent{}
	for rows.Next() {
		var event CloudDuplicateScanEvent
		var recorded int64
		if err = rows.Scan(&event.Sequence, &event.RunID, &event.Status, &event.Phase, &recorded, &event.Evidence); err != nil {
			return nil, 0, false, err
		}
		event.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		out = append(out, event)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, false, err
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	var next int64
	if more && len(out) > 0 {
		next = out[len(out)-1].Sequence
	}
	return out, next, more, nil
}

func nonNilBytes(value []byte) []byte {
	if value == nil {
		return []byte{}
	}
	return value
}
