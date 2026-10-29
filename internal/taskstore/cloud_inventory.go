package taskstore

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"strings"
	"time"
)

const cloudInventoryFileAggregateSQL = `SELECT COALESCE(sum(size_bytes),0),count(*) FROM cloud_inventory WHERE account_fingerprint=? AND is_dir=0 AND path>=? AND path<?`
const cloudInventoryDirectoryAggregateSQL = `SELECT COALESCE(MAX(CASE WHEN path=? AND status='complete' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN path<>? THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status<>'complete' THEN 1 ELSE 0 END),0),MIN(CASE WHEN status='complete' THEN observed_at_utc END) FROM cloud_inventory_directories WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))`
const upsertCloudInventoryEntrySQL = `INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint,fs_id) DO UPDATE SET parent_path=excluded.parent_path,path=excluded.path,name=excluded.name,is_dir=excluded.is_dir,size_bytes=excluded.size_bytes,provider_mtime=excluded.provider_mtime,content_md5=excluded.content_md5,observed_at_utc=excluded.observed_at_utc,last_seen_run_id=excluded.last_seen_run_id`
const upsertCloudInventoryDirectorySQL = `INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,last_run_id) VALUES(?,?,'stale',?) ON CONFLICT(account_fingerprint,path) DO NOTHING`
const loadCloudScanStateSQL = `SELECT account_fingerprint,run_id,status,phase,current_directory,next_offset,pages_processed,items_processed,pending_count,started_at_utc,updated_at_utc,last_error FROM cloud_inventory_scan_state WHERE account_fingerprint=?`

// Keep each multi-row statement below SQLite's 999 bind-variable limit (11 values per row).
const cloudInventoryUpsertBatchSize = 80

func cloudInventoryUpsertBatchCount(entryCount int) int {
	if entryCount <= 0 {
		return 0
	}
	return (entryCount + cloudInventoryUpsertBatchSize - 1) / cloudInventoryUpsertBatchSize
}

func bulkUpsertCloudInventoryEntries(tx *sql.Tx, account, directory string, entries []CloudInventoryEntry, observedAt time.Time, runID string) error {
	for batchIndex := 0; batchIndex < cloudInventoryUpsertBatchCount(len(entries)); batchIndex++ {
		start := batchIndex * cloudInventoryUpsertBatchSize
		end := start + cloudInventoryUpsertBatchSize
		if end > len(entries) {
			end = len(entries)
		}
		batch := entries[start:end]
		var query strings.Builder
		query.Grow(250 + len(batch)*24)
		query.WriteString(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES`)
		args := make([]any, 0, len(batch)*11)
		for i, entry := range batch {
			if i != 0 {
				query.WriteByte(',')
			}
			query.WriteString(`(?,?,?,?,?,?,?,?,?,?,?)`)
			isDir := 0
			if entry.Directory {
				isDir = 1
			}
			args = append(args, account, entry.FsID, directory, entry.Path, entry.Name, isDir, entry.Size, entry.ProviderMtime, entry.ContentMD5, observedAt.UTC().UnixMicro(), runID)
		}
		query.WriteString(` ON CONFLICT(account_fingerprint,fs_id) DO UPDATE SET parent_path=excluded.parent_path,path=excluded.path,name=excluded.name,is_dir=excluded.is_dir,size_bytes=excluded.size_bytes,provider_mtime=excluded.provider_mtime,content_md5=excluded.content_md5,observed_at_utc=excluded.observed_at_utc,last_seen_run_id=excluded.last_seen_run_id`)
		if _, err := tx.Exec(query.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

type CloudInventoryEntry struct {
	FsID                 int64      `json:"fs_id"`
	Path                 string     `json:"path"`
	Name                 string     `json:"name"`
	Directory            bool       `json:"directory"`
	Size                 int64      `json:"size"`
	ProviderMtime        int64      `json:"provider_mtime"`
	ContentMD5           string     `json:"content_md5,omitempty"`
	ContentVerifiedAtUTC *time.Time `json:"content_verified_at_utc,omitempty"`
}

type CloudDirectorySize struct {
	Path          string    `json:"path"`
	Bytes         int64     `json:"bytes"`
	Files         int64     `json:"files"`
	Directories   int64     `json:"directories"`
	Complete      bool      `json:"complete"`
	ObservedAtUTC time.Time `json:"observed_at_utc,omitempty"`
	CachedAtUTC   time.Time `json:"cached_at_utc"`
}

type CloudInventoryDirectory struct {
	Path          string     `json:"path"`
	Status        string     `json:"status"`
	ObservedAtUTC *time.Time `json:"observed_at_utc,omitempty"`
}

type CloudScanState struct {
	AccountFingerprint string    `json:"account_fingerprint"`
	RunID              string    `json:"run_id"`
	Status             string    `json:"status"`
	Phase              string    `json:"phase"`
	PendingCount       int64     `json:"pending_count"`
	CurrentDirectory   string    `json:"current_directory"`
	NextOffset         int64     `json:"next_offset"`
	PagesProcessed     int64     `json:"pages_processed"`
	ItemsProcessed     int64     `json:"items_processed"`
	StartedAtUTC       time.Time `json:"started_at_utc"`
	UpdatedAtUTC       time.Time `json:"updated_at_utc"`
	LastError          string    `json:"last_error,omitempty"`
}

func validInventoryPath(value string) bool {
	return value != "" && strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.Contains(value, "\\") && len(value) <= 4096
}

func cloudDirectoryPathRange(directory string) (string, string) {
	prefix := directory + "/"
	if directory == "/" {
		prefix = "/"
	}
	// Every valid child path has this ASCII slash at the end of the prefix.
	// Incrementing it creates an exclusive bound before any sibling prefix.
	upper := prefix[:len(prefix)-1] + "0"
	return prefix, upper
}

func bumpCloudInventoryGeneration(tx *sql.Tx, account string, now time.Time) error {
	_, err := tx.Exec(`INSERT INTO cloud_inventory_generations(account_fingerprint,generation,updated_at_utc) VALUES(?,1,?) ON CONFLICT(account_fingerprint) DO UPDATE SET generation=generation+1,updated_at_utc=excluded.updated_at_utc`, account, now.UTC().UnixMicro())
	return err
}

func pruneCloudInventoryDescendants(tx *sql.Tx, account, directory string, removeDirectoryState bool) error {
	prefix, upper := cloudDirectoryPathRange(directory)
	if _, err := tx.Exec(`DELETE FROM cloud_inventory WHERE account_fingerprint=? AND path>=? AND path<?`, account, prefix, upper); err != nil {
		return err
	}
	if removeDirectoryState {
		_, err := tx.Exec(`DELETE FROM cloud_inventory_directories WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))`, account, directory, prefix, upper)
		return err
	}
	_, err := tx.Exec(`DELETE FROM cloud_inventory_directories WHERE account_fingerprint=? AND path>=? AND path<?`, account, prefix, upper)
	return err
}

// UpsertCloudInventoryPage commits one provider page atomically. A directory
// is marked complete, and unseen direct children are removed, only on its last
// page; descendants of removed directories are pruned in the same transaction.
func (s *Store) UpsertCloudInventoryPage(account, runID, directory string, entries []CloudInventoryEntry, observedAt time.Time, nextOffset *int) error {
	return s.upsertCloudInventoryPage(account, runID, directory, entries, observedAt, nextOffset, nil, nil)
}

func (s *Store) UpsertCloudInventoryScanPage(state *CloudScanState, directory string, entries []CloudInventoryEntry, observedAt time.Time, nextOffset *int, childDirectories []string) error {
	if state == nil || state.Status != "running" || state.CurrentDirectory != directory {
		return fmt.Errorf("invalid cloud scan page state")
	}
	return s.upsertCloudInventoryPage(state.AccountFingerprint, state.RunID, directory, entries, observedAt, nextOffset, state, childDirectories)
}

func (s *Store) upsertCloudInventoryPage(account, runID, directory string, entries []CloudInventoryEntry, observedAt time.Time, nextOffset *int, scanState *CloudScanState, childDirectories []string) error {
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 || !validInventoryPath(directory) || observedAt.IsZero() || (nextOffset != nil && *nextOffset < 1) {
		return fmt.Errorf("invalid cloud inventory page")
	}
	for _, entry := range entries {
		if entry.FsID <= 0 || !validInventoryPath(entry.Path) || path.Dir(entry.Path) != directory || path.Base(entry.Path) != entry.Name || entry.Name == "" || entry.Size < 0 || len(entry.Name) > 1024 || len(entry.ContentMD5) > 256 {
			return fmt.Errorf("invalid cloud inventory entry")
		}
	}
	if scanState != nil {
		if scanState.AccountFingerprint != account || scanState.RunID != runID || scanState.NextOffset < 0 {
			return fmt.Errorf("invalid cloud scan page identity")
		}
		for _, child := range childDirectories {
			if !validInventoryPath(child) || path.Dir(child) != directory {
				return fmt.Errorf("invalid cloud scan child directory")
			}
		}
		if nextOffset != nil && len(childDirectories) != 0 {
			return fmt.Errorf("child directories can only be enqueued on the final page")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err = bumpCloudInventoryGeneration(tx, account, time.Now().UTC()); err != nil {
		_ = tx.Rollback()
		return err
	}
	if scanState != nil {
		var persistedPending int64
		if err = tx.QueryRow(`SELECT pending_count FROM cloud_inventory_scan_state WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&persistedPending); err != nil {
			_ = tx.Rollback()
			return err
		}
		if persistedPending != scanState.PendingCount {
			_ = tx.Rollback()
			return fmt.Errorf("cloud scan pending count changed")
		}
	}
	seenIDs := make(map[int64]struct{}, len(entries))
	uniqueIDs := true
	hasDirectories := false
	for _, entry := range entries {
		if entry.Directory {
			hasDirectories = true
		}
		if _, ok := seenIDs[entry.FsID]; ok {
			uniqueIDs = false
		} else {
			seenIDs[entry.FsID] = struct{}{}
		}
	}
	var entryStmt, directoryStmt *sql.Stmt
	if !uniqueIDs || hasDirectories {
		entryStmt, err = tx.Prepare(upsertCloudInventoryEntrySQL)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if hasDirectories {
		directoryStmt, err = tx.Prepare(upsertCloudInventoryDirectorySQL)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	observed := observedAt.UTC().UnixMicro()
	if _, err = tx.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES(?,?,'scanning',NULL,?) ON CONFLICT(account_fingerprint,path) DO UPDATE SET status='scanning',last_run_id=excluded.last_run_id`, account, directory, runID); err != nil {
		_ = tx.Rollback()
		return err
	}
	prepareDirectoryIdentity := func(entry CloudInventoryEntry) (bool, error) {
		if !entry.Directory {
			return false, nil
		}
		var previousPath string
		identityErr := tx.QueryRow(`SELECT path FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=? AND is_dir=1`, account, entry.FsID).Scan(&previousPath)
		if identityErr != nil && !errorsIsNoRows(identityErr) {
			return false, identityErr
		}
		identityChanged := errorsIsNoRows(identityErr) || previousPath != entry.Path
		if identityChanged {
			if identityErr == nil && previousPath != entry.Path {
				if err := pruneCloudInventoryDescendants(tx, account, previousPath, true); err != nil {
					return false, err
				}
			}
			if err := pruneCloudInventoryDescendants(tx, account, entry.Path, false); err != nil {
				return false, err
			}
		}
		return identityChanged, nil
	}
	if uniqueIDs {
		fileBatchStart := 0
		for index, entry := range entries {
			if !entry.Directory {
				continue
			}
			if fileBatchStart < index {
				if err = bulkUpsertCloudInventoryEntries(tx, account, directory, entries[fileBatchStart:index], observedAt, runID); err != nil {
					_ = tx.Rollback()
					return err
				}
			}
			identityChanged, identityErr := prepareDirectoryIdentity(entry)
			if identityErr != nil {
				_ = tx.Rollback()
				return identityErr
			}
			if _, err = entryStmt.Exec(account, entry.FsID, directory, entry.Path, entry.Name, 1, entry.Size, entry.ProviderMtime, entry.ContentMD5, observed, runID); err != nil {
				_ = tx.Rollback()
				return err
			}
			if _, err = directoryStmt.Exec(account, entry.Path, runID); err != nil {
				_ = tx.Rollback()
				return err
			}
			if identityChanged {
				if _, err = tx.Exec(`UPDATE cloud_inventory_directories SET status='stale',observed_at_utc=NULL,last_run_id=? WHERE account_fingerprint=? AND path=?`, runID, account, entry.Path); err != nil {
					_ = tx.Rollback()
					return err
				}
			}
			fileBatchStart = index + 1
		}
		if fileBatchStart < len(entries) {
			if err = bulkUpsertCloudInventoryEntries(tx, account, directory, entries[fileBatchStart:], observedAt, runID); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	} else {
		for _, entry := range entries {
			identityChanged, identityErr := prepareDirectoryIdentity(entry)
			if identityErr != nil {
				_ = tx.Rollback()
				return identityErr
			}
			isDir := 0
			if entry.Directory {
				isDir = 1
			}
			if _, err = entryStmt.Exec(account, entry.FsID, directory, entry.Path, entry.Name, isDir, entry.Size, entry.ProviderMtime, entry.ContentMD5, observed, runID); err != nil {
				_ = tx.Rollback()
				return err
			}
			if entry.Directory {
				if _, err = directoryStmt.Exec(account, entry.Path, runID); err != nil {
					_ = tx.Rollback()
					return err
				}
				if identityChanged {
					if _, err = tx.Exec(`UPDATE cloud_inventory_directories SET status='stale',observed_at_utc=NULL,last_run_id=? WHERE account_fingerprint=? AND path=?`, runID, account, entry.Path); err != nil {
						_ = tx.Rollback()
						return err
					}
				}
			}
		}
	}
	if entryStmt != nil {
		if err = entryStmt.Close(); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if directoryStmt != nil {
		if err = directoryStmt.Close(); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if nextOffset == nil {
		rows, queryErr := tx.Query(`SELECT fs_id,path,is_dir FROM cloud_inventory WHERE account_fingerprint=? AND parent_path=? AND last_seen_run_id<>?`, account, directory, runID)
		if queryErr != nil {
			_ = tx.Rollback()
			return queryErr
		}
		type stale struct {
			id   int64
			path string
			dir  bool
		}
		var removed []stale
		for rows.Next() {
			var item stale
			var isDir int
			if err = rows.Scan(&item.id, &item.path, &isDir); err != nil {
				rows.Close()
				_ = tx.Rollback()
				return err
			}
			item.dir = isDir == 1
			removed = append(removed, item)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			_ = tx.Rollback()
			return err
		}
		rows.Close()
		for _, item := range removed {
			if item.dir {
				if _, err = tx.Exec(`DELETE FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=? AND is_dir=1`, account, item.id); err != nil {
					_ = tx.Rollback()
					return err
				}
				replacedByCurrentDirectory := false
				for _, current := range entries {
					if current.Directory && current.Path == item.path {
						replacedByCurrentDirectory = true
						break
					}
				}
				if err = pruneCloudInventoryDescendants(tx, account, item.path, !replacedByCurrentDirectory); err != nil {
					_ = tx.Rollback()
					return err
				}
				if replacedByCurrentDirectory {
					if _, err = tx.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,last_run_id) VALUES(?,?,'stale',?) ON CONFLICT(account_fingerprint,path) DO UPDATE SET status='stale',observed_at_utc=NULL,last_run_id=excluded.last_run_id`, account, item.path, runID); err != nil {
						_ = tx.Rollback()
						return err
					}
				}
			} else {
				if _, err = tx.Exec(`DELETE FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=?`, account, item.id); err != nil {
					_ = tx.Rollback()
					return err
				}
			}
		}
		if _, err = tx.Exec(`DELETE FROM cloud_inventory WHERE account_fingerprint=? AND parent_path=? AND last_seen_run_id<>?`, account, directory, runID); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err = tx.Exec(`UPDATE cloud_inventory_directories SET status='complete',observed_at_utc=?,last_run_id=? WHERE account_fingerprint=? AND path=?`, observed, runID, account, directory); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	var updatedScanState CloudScanState
	if scanState != nil {
		updatedScanState = *scanState
		updatedScanState.PagesProcessed++
		updatedScanState.ItemsProcessed += int64(len(entries))
		updatedScanState.UpdatedAtUTC = time.Now().UTC()
		updatedScanState.LastError = ""
		updatedScanState.Status = "running"
		updatedScanState.Phase = "listing"
		if nextOffset != nil {
			updatedScanState.NextOffset = int64(*nextOffset)
		} else {
			if updatedScanState.PendingCount <= 0 {
				_ = tx.Rollback()
				return fmt.Errorf("cloud scan pending count is empty before queue head removal")
			}
			var headSequence int64
			var headPath string
			if err = tx.QueryRow(`SELECT sequence,path FROM cloud_inventory_scan_queue WHERE account_fingerprint=? AND run_id=? ORDER BY sequence LIMIT 1`, account, runID).Scan(&headSequence, &headPath); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("cloud scan queue head missing: %w", err)
			}
			if headPath != directory {
				_ = tx.Rollback()
				return fmt.Errorf("cloud scan queue head changed")
			}
			if _, err = tx.Exec(`DELETE FROM cloud_inventory_scan_queue WHERE account_fingerprint=? AND run_id=? AND sequence=?`, account, runID, headSequence); err != nil {
				_ = tx.Rollback()
				return err
			}
			var nextSequence int64
			if err = tx.QueryRow(`SELECT coalesce(max(sequence),-1)+1 FROM cloud_inventory_scan_queue WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&nextSequence); err != nil {
				_ = tx.Rollback()
				return err
			}
			updatedScanState.PendingCount--
			var scanQueueStmt *sql.Stmt
			if len(childDirectories) > 0 {
				scanQueueStmt, err = tx.Prepare(`INSERT OR IGNORE INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES(?,?,?,?)`)
				if err != nil {
					_ = tx.Rollback()
					return err
				}
			}
			for index, child := range childDirectories {
				inserted, insertErr := scanQueueStmt.Exec(account, runID, nextSequence+int64(index), child)
				if insertErr != nil {
					_ = tx.Rollback()
					return insertErr
				}
				rowsAdded, rowsErr := inserted.RowsAffected()
				if rowsErr != nil {
					_ = tx.Rollback()
					return rowsErr
				}
				updatedScanState.PendingCount += rowsAdded
			}
			if scanQueueStmt != nil {
				if err = scanQueueStmt.Close(); err != nil {
					_ = tx.Rollback()
					return err
				}
			}
			updatedScanState.NextOffset = 0
			if updatedScanState.PendingCount > 0 {
				if err = tx.QueryRow(`SELECT path FROM cloud_inventory_scan_queue WHERE account_fingerprint=? AND run_id=? ORDER BY sequence LIMIT 1`, account, runID).Scan(&updatedScanState.CurrentDirectory); err != nil {
					_ = tx.Rollback()
					return fmt.Errorf("cloud scan pending count exceeds queue rows: %w", err)
				}
			} else {
				var remaining string
				checkErr := tx.QueryRow(`SELECT path FROM cloud_inventory_scan_queue WHERE account_fingerprint=? AND run_id=? ORDER BY sequence LIMIT 1`, account, runID).Scan(&remaining)
				if checkErr == nil {
					_ = tx.Rollback()
					return fmt.Errorf("cloud scan queue has rows after pending count reached zero")
				}
				if checkErr != sql.ErrNoRows {
					_ = tx.Rollback()
					return checkErr
				}
				updatedScanState.Status = "complete"
				updatedScanState.Phase = "complete"
				updatedScanState.CurrentDirectory = directory
			}
		}
		var result sql.Result
		result, err = tx.Exec(`UPDATE cloud_inventory_scan_state SET status=?,phase=?,pending_json='[]',current_directory=?,next_offset=?,pages_processed=?,items_processed=?,pending_count=?,updated_at_utc=?,last_error=? WHERE account_fingerprint=? AND run_id=?`, updatedScanState.Status, updatedScanState.Phase, updatedScanState.CurrentDirectory, updatedScanState.NextOffset, updatedScanState.PagesProcessed, updatedScanState.ItemsProcessed, updatedScanState.PendingCount, updatedScanState.UpdatedAtUTC.UTC().UnixMicro(), updatedScanState.LastError, account, runID)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
			_ = tx.Rollback()
			if rowsErr != nil {
				return rowsErr
			}
			return fmt.Errorf("cloud scan state disappeared during page commit")
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if scanState != nil {
		*scanState = updatedScanState
	}
	return nil
}

func (s *Store) CloudDirectorySize(account, directory string) (CloudDirectorySize, error) {
	result := CloudDirectorySize{Path: directory}
	if account == "" || len(account) > 128 || !validInventoryPath(directory) {
		return result, fmt.Errorf("invalid cloud directory")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return result, os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return result, err
	}
	rollback := func(err error) (CloudDirectorySize, error) {
		_ = tx.Rollback()
		return CloudDirectorySize{Path: directory}, err
	}
	now := time.Now().UTC()
	if _, err = tx.Exec(`INSERT OR IGNORE INTO cloud_inventory_generations(account_fingerprint,generation,updated_at_utc) VALUES(?,0,?)`, account, now.UnixMicro()); err != nil {
		return rollback(err)
	}
	var generation int64
	if err = tx.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint=?`, account).Scan(&generation); err != nil {
		return rollback(err)
	}
	var cachedGeneration, cachedBytes, cachedFiles, cachedDirectories int64
	var cachedComplete int
	var cachedObserved sql.NullInt64
	var cachedAt int64
	err = tx.QueryRow(`SELECT generation,bytes,files,directories,complete,observed_at_utc,cached_at_utc FROM cloud_directory_size_cache WHERE account_fingerprint=? AND path=?`, account, directory).Scan(&cachedGeneration, &cachedBytes, &cachedFiles, &cachedDirectories, &cachedComplete, &cachedObserved, &cachedAt)
	if err == nil && cachedGeneration == generation {
		result.Bytes, result.Files, result.Directories, result.Complete = cachedBytes, cachedFiles, cachedDirectories, cachedComplete == 1
		result.CachedAtUTC = time.UnixMicro(cachedAt).UTC()
		if cachedObserved.Valid {
			result.ObservedAtUTC = time.UnixMicro(cachedObserved.Int64).UTC()
		}
		if err = tx.Commit(); err != nil {
			return CloudDirectorySize{Path: directory}, err
		}
		return result, nil
	}
	if err != nil && !errorsIsNoRows(err) {
		return rollback(err)
	}
	prefix, upper := cloudDirectoryPathRange(directory)
	var complete, directories, incomplete int64
	var observed sql.NullInt64
	if err = tx.QueryRow(cloudInventoryDirectoryAggregateSQL, directory, directory, account, directory, prefix, upper).Scan(&complete, &directories, &incomplete, &observed); err != nil {
		return rollback(err)
	}
	result.Complete = complete == 1 && incomplete == 0
	if err = tx.QueryRow(cloudInventoryFileAggregateSQL, account, prefix, upper).Scan(&result.Bytes, &result.Files); err != nil {
		return rollback(err)
	}
	result.Directories = directories
	if observed.Valid {
		result.ObservedAtUTC = time.UnixMicro(observed.Int64).UTC()
	}
	result.CachedAtUTC = time.UnixMicro(now.UnixMicro()).UTC()
	completeInt := 0
	if result.Complete {
		completeInt = 1
	}
	if _, err = tx.Exec(`INSERT INTO cloud_directory_size_cache(account_fingerprint,path,generation,bytes,files,directories,complete,observed_at_utc,cached_at_utc) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint,path) DO UPDATE SET generation=excluded.generation,bytes=excluded.bytes,files=excluded.files,directories=excluded.directories,complete=excluded.complete,observed_at_utc=excluded.observed_at_utc,cached_at_utc=excluded.cached_at_utc`, account, directory, generation, result.Bytes, result.Files, result.Directories, completeInt, nullableUnixMicro(result.ObservedAtUTC), now.UnixMicro()); err != nil {
		return rollback(err)
	}
	if err = tx.Commit(); err != nil {
		return CloudDirectorySize{Path: directory}, err
	}
	return result, nil
}

func nullableUnixMicro(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().UnixMicro()
}

func errorsIsNoRows(err error) bool { return err == sql.ErrNoRows }

func (s *Store) InvalidateCloudInventory(account string, paths []string) error {
	if account == "" || len(account) > 128 {
		return fmt.Errorf("invalid account fingerprint")
	}
	for _, p := range paths {
		if !validInventoryPath(p) {
			return fmt.Errorf("invalid cloud directory path")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, p := range paths {
		prefix, upper := cloudDirectoryPathRange(p)
		if _, err = tx.Exec(`UPDATE cloud_inventory_directories SET status='stale' WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))`, account, p, prefix, upper); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if len(paths) > 0 {
		if err = bumpCloudInventoryGeneration(tx, account, time.Now().UTC()); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) StartCloudScan(state *CloudScanState) error {
	if state == nil || state.AccountFingerprint == "" || state.RunID == "" || len(state.AccountFingerprint) > 128 || len(state.RunID) > 128 || !validRemoteScanStatus(state.Status) || state.StartedAtUTC.IsZero() || state.UpdatedAtUTC.IsZero() || !validInventoryPath(state.CurrentDirectory) {
		return fmt.Errorf("invalid cloud scan state")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM cloud_inventory_scan_queue WHERE account_fingerprint=?`, state.AccountFingerprint); err == nil {
		_, err = tx.Exec(`INSERT INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES(?,?,0,?)`, state.AccountFingerprint, state.RunID, state.CurrentDirectory)
	}
	if err == nil {
		_, err = tx.Exec(`INSERT INTO cloud_inventory_scan_state(account_fingerprint,run_id,status,phase,pending_json,current_directory,next_offset,pages_processed,items_processed,pending_count,started_at_utc,updated_at_utc,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint) DO UPDATE SET run_id=excluded.run_id,status=excluded.status,phase=excluded.phase,pending_json='[]',current_directory=excluded.current_directory,next_offset=excluded.next_offset,pages_processed=excluded.pages_processed,items_processed=excluded.items_processed,pending_count=excluded.pending_count,started_at_utc=excluded.started_at_utc,updated_at_utc=excluded.updated_at_utc,last_error=excluded.last_error`, state.AccountFingerprint, state.RunID, state.Status, state.Phase, []byte("[]"), state.CurrentDirectory, state.NextOffset, state.PagesProcessed, state.ItemsProcessed, 1, state.StartedAtUTC.UTC().UnixMicro(), state.UpdatedAtUTC.UTC().UnixMicro(), state.LastError)
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	state.PendingCount = 1
	return nil
}

func (s *Store) SaveCloudScanState(account string, runID, status, phase, current string, offset, pages, items int64, started, updated time.Time, lastError string) error {
	if account == "" || runID == "" || len(account) > 128 || len(runID) > 128 || !validRemoteScanStatus(status) || !validInventoryPath(current) || offset < 0 || pages < 0 || items < 0 || started.IsZero() || updated.IsZero() {
		return fmt.Errorf("invalid cloud scan state")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	_, err := s.db.Exec(`INSERT INTO cloud_inventory_scan_state(account_fingerprint,run_id,status,phase,pending_json,current_directory,next_offset,pages_processed,items_processed,started_at_utc,updated_at_utc,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint) DO UPDATE SET run_id=excluded.run_id,status=excluded.status,phase=excluded.phase,pending_json='[]',current_directory=excluded.current_directory,next_offset=excluded.next_offset,pages_processed=excluded.pages_processed,items_processed=excluded.items_processed,started_at_utc=excluded.started_at_utc,updated_at_utc=excluded.updated_at_utc,last_error=excluded.last_error`, account, runID, status, phase, []byte("[]"), current, offset, pages, items, started.UTC().UnixMicro(), updated.UTC().UnixMicro(), lastError)
	return err
}

func validRemoteScanStatus(status string) bool {
	return status == "running" || status == "paused" || status == "complete" || status == "failed"
}

func (s *Store) LoadCloudScanState(account string) (CloudScanState, error) {
	state := CloudScanState{}
	if account == "" || len(account) > 128 {
		return state, fmt.Errorf("invalid account fingerprint")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return state, os.ErrClosed
	}
	var started, updated int64
	err := s.db.QueryRow(loadCloudScanStateSQL, account).Scan(&state.AccountFingerprint, &state.RunID, &state.Status, &state.Phase, &state.CurrentDirectory, &state.NextOffset, &state.PagesProcessed, &state.ItemsProcessed, &state.PendingCount, &started, &updated, &state.LastError)
	if err != nil {
		return state, err
	}
	state.StartedAtUTC = time.UnixMicro(started).UTC()
	state.UpdatedAtUTC = time.UnixMicro(updated).UTC()
	return state, nil
}

func (s *Store) CloudScanQueueHead(account, runID string) (string, error) {
	if account == "" || runID == "" {
		return "", fmt.Errorf("invalid cloud scan queue identity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", os.ErrClosed
	}
	var directory string
	err := s.db.QueryRow(`SELECT path FROM cloud_inventory_scan_queue WHERE account_fingerprint=? AND run_id=? ORDER BY sequence LIMIT 1`, account, runID).Scan(&directory)
	return directory, err
}

func (s *Store) ListCloudInventory(account, directory string, offset, limit int) ([]CloudInventoryEntry, CloudInventoryDirectory, int, error) {
	items := []CloudInventoryEntry{}
	meta := CloudInventoryDirectory{Path: directory}
	if account == "" || !validInventoryPath(directory) || offset < 0 || limit < 1 || limit > 1000 {
		return nil, meta, 0, fmt.Errorf("invalid cloud inventory query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, meta, 0, os.ErrClosed
	}
	var observed sql.NullInt64
	err := s.db.QueryRow(`SELECT status,observed_at_utc FROM cloud_inventory_directories WHERE account_fingerprint=? AND path=?`, account, directory).Scan(&meta.Status, &observed)
	if err != nil && !errorsIsNoRows(err) {
		return nil, meta, 0, err
	}
	if observed.Valid {
		v := time.UnixMicro(observed.Int64).UTC()
		meta.ObservedAtUTC = &v
	}
	var total int
	if err = s.db.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint=? AND parent_path=?`, account, directory).Scan(&total); err != nil {
		return nil, meta, 0, err
	}
	rows, err := s.db.Query(`SELECT fs_id,path,name,is_dir,size_bytes,provider_mtime,content_md5 FROM cloud_inventory WHERE account_fingerprint=? AND parent_path=? ORDER BY name,fs_id LIMIT ? OFFSET ?`, account, directory, limit, offset)
	if err != nil {
		return nil, meta, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var item CloudInventoryEntry
		var isDir int
		if err = rows.Scan(&item.FsID, &item.Path, &item.Name, &isDir, &item.Size, &item.ProviderMtime, &item.ContentMD5); err != nil {
			return nil, meta, 0, err
		}
		item.Directory = isDir == 1
		items = append(items, item)
	}
	return items, meta, total, rows.Err()
}

// ListCompleteEmptyCloudDirectories only returns folders covered by a complete
// directory listing and having no cached children. Callers must still recheck
// emptiness against the live provider before deleting each candidate.
const listCompleteEmptyCloudDirectoriesSQL = `SELECT e.fs_id,e.path,e.name,e.size_bytes,e.provider_mtime,e.content_md5
FROM cloud_inventory e
JOIN cloud_inventory_directories d ON d.account_fingerprint=e.account_fingerprint AND d.path=e.path
WHERE e.account_fingerprint=? AND e.is_dir=1 AND d.status='complete'
AND NOT EXISTS (
	SELECT 1 FROM cloud_inventory file
	WHERE file.account_fingerprint=e.account_fingerprint AND file.is_dir=0
	AND file.path>=CASE WHEN e.path='/' THEN '/' ELSE e.path||'/' END
	AND file.path<CASE WHEN e.path='/' THEN '0' ELSE e.path||'0' END
)
AND NOT EXISTS (
	SELECT 1 FROM cloud_inventory sub
	LEFT JOIN cloud_inventory_directories sd ON sd.account_fingerprint=sub.account_fingerprint AND sd.path=sub.path
	WHERE sub.account_fingerprint=e.account_fingerprint AND sub.is_dir=1
	AND sub.path>=CASE WHEN e.path='/' THEN '/' ELSE e.path||'/' END
	AND sub.path<CASE WHEN e.path='/' THEN '0' ELSE e.path||'0' END
	AND (sd.path IS NULL OR sd.status<>'complete')
)
ORDER BY (length(e.path)-length(replace(e.path,'/',''))) DESC,e.path`

func (s *Store) ListCompleteEmptyCloudDirectories(account string) ([]CloudInventoryEntry, error) {
	if account == "" || len(account) > 128 {
		return nil, fmt.Errorf("invalid account fingerprint")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	rows, err := s.db.Query(listCompleteEmptyCloudDirectoriesSQL, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CloudInventoryEntry, 0)
	for rows.Next() {
		var item CloudInventoryEntry
		if err = rows.Scan(&item.FsID, &item.Path, &item.Name, &item.Size, &item.ProviderMtime, &item.ContentMD5); err != nil {
			return nil, err
		}
		item.Directory = true
		items = append(items, item)
	}
	return items, rows.Err()
}
