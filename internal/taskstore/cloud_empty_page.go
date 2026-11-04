package taskstore

import (
	"database/sql"
	"fmt"
	"os"
	"time"
)

type CloudEmptyDirectoryPage struct {
	Entries      []CloudInventoryEntry
	Total        int64
	Generation   int64
	Coverage     string
	RunID        string
	UpdatedAtUTC time.Time
}

// The workbench lists immediately empty directories. Empty ancestors become
// candidates after their children are removed and the cache is refreshed.
const emptyLeafDirectoryFromSQL = `FROM cloud_inventory e
 JOIN cloud_inventory_directories d ON d.account_fingerprint=e.account_fingerprint AND d.path=e.path
 WHERE e.account_fingerprint=? AND e.is_dir=1 AND d.status='complete' AND e.path<>'/'
 AND NOT EXISTS(SELECT 1 FROM cloud_inventory child WHERE child.account_fingerprint=e.account_fingerprint AND child.parent_path=e.path)
 AND ?<>'/' AND e.path<>? AND NOT(e.path>=?||'/' AND e.path<?||'0')
 AND substr(?,1,length(e.path)+1)<>e.path||'/'`

func (s *Store) ListEmptyCloudDirectoryPage(account, protected string, offset int64, limit int, expected *int64) (CloudEmptyDirectoryPage, error) {
	page := CloudEmptyDirectoryPage{Entries: []CloudInventoryEntry{}, Coverage: "unknown"}
	if account == "" || len(account) > 128 || !IsValidCloudInventoryPath(protected) || offset < 0 || offset > 1000000 || limit < 1 || limit > 100 || (offset > 0 && expected == nil) {
		return page, fmt.Errorf("invalid empty-directory page")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return page, os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err = cloudInventoryGeneration(tx, account, &page.Generation); err != nil {
		return page, err
	}
	if expected != nil && *expected != page.Generation {
		return page, ErrCloudInventoryGenerationChanged
	}
	var status string
	var updated int64
	err = tx.QueryRow(`SELECT run_id,status,updated_at_utc FROM cloud_inventory_scan_state WHERE account_fingerprint=?`, account).Scan(&page.RunID, &status, &updated)
	if err == sql.ErrNoRows {
		return page, nil
	}
	if err != nil {
		return page, err
	}
	page.UpdatedAtUTC = time.UnixMicro(updated).UTC()
	coverage, err := cloudInventoryCoverageInTx(tx, account, status, &page.UpdatedAtUTC)
	if err != nil {
		return page, err
	}
	page.Coverage = coverage.Status
	if coverage.Status != "complete" {
		return page, nil
	}
	args := []any{account, protected, protected, protected, protected, protected}
	if err = tx.QueryRow(`SELECT count(*) `+emptyLeafDirectoryFromSQL, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := tx.Query(`SELECT e.fs_id,e.path,e.name,e.size_bytes,e.provider_mtime,e.content_md5 `+emptyLeafDirectoryFromSQL+` ORDER BY e.path,e.fs_id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var item CloudInventoryEntry
		if err = rows.Scan(&item.FsID, &item.Path, &item.Name, &item.Size, &item.ProviderMtime, &item.ContentMD5); err != nil {
			rows.Close()
			return page, err
		}
		item.Directory = true
		page.Entries = append(page.Entries, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if err = tx.Commit(); err != nil {
		return page, err
	}
	return page, nil
}
