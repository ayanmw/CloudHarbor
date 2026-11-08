package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrCloudInventoryRefreshNotFound = errors.New("cloud inventory refresh run not found")
var ErrCloudInventoryRefreshRequestBudgetReached = errors.New("目录刷新请求额度已耗尽")
var ErrCloudInventoryScanRequestBudgetReached = errors.New("全盘扫描请求额度已耗尽")
var ErrCloudInventoryBoundaryRestartLimitReached = errors.New("云端分页边界漂移后自动重扫次数已达上限")
var ErrCloudInventoryDuplicateAcrossPages = errors.New("duplicate fs_id across cloud inventory scan pages")

type CloudInventoryDuplicateAcrossPagesError struct{ FsID int64 }

func (e *CloudInventoryDuplicateAcrossPagesError) Error() string {
	return fmt.Sprintf("%s: %d", ErrCloudInventoryDuplicateAcrossPages, e.FsID)
}

func (e *CloudInventoryDuplicateAcrossPagesError) Unwrap() error {
	return ErrCloudInventoryDuplicateAcrossPages
}

var ErrCloudInventoryRefreshEvidenceExportTooLarge = errors.New("cloud inventory refresh evidence export exceeds limit")
var ErrCloudInventoryRefreshRunSummaryExportTooLarge = errors.New("cloud inventory refresh run summary export exceeds limit")
var ErrCloudInventoryRefreshChangesExportTooLarge = errors.New("cloud inventory refresh changes export exceeds limit")
var ErrCloudInventoryGenerationChanged = errors.New("本地网盘缓存已更新，请重新读取")

const cloudInventoryIncompletePathsSQL = `SELECT path FROM cloud_inventory_directories WHERE account_fingerprint=? AND status<>'complete'
	UNION
	SELECT e.path FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.is_dir=1 AND NOT EXISTS(SELECT 1 FROM cloud_inventory_directories d WHERE d.account_fingerprint=e.account_fingerprint AND d.path=e.path AND d.status='complete')
	UNION
	SELECT d.path FROM cloud_inventory_directories d WHERE d.account_fingerprint=? AND d.path<>'/' AND NOT EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=d.account_fingerprint AND e.path=d.path AND e.is_dir=1)
	UNION
	SELECT e.parent_path FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.parent_path<>'/' AND NOT EXISTS(SELECT 1 FROM cloud_inventory p JOIN cloud_inventory_directories d ON d.account_fingerprint=p.account_fingerprint AND d.path=p.path AND d.status='complete' WHERE p.account_fingerprint=e.account_fingerprint AND p.path=e.parent_path AND p.is_dir=1)
	UNION
	SELECT e.path FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.path<>CASE WHEN e.parent_path='/' THEN '/'||e.name ELSE e.parent_path||'/'||e.name END
	UNION
	SELECT '/' WHERE NOT EXISTS(SELECT 1 FROM cloud_inventory_directories WHERE account_fingerprint=? AND path='/' AND status='complete')`

const cloudInventoryIncompleteDirectoriesSQL = `SELECT count(*) FROM (` + cloudInventoryIncompletePathsSQL + `)`

const CloudInventoryRefreshEvidenceExportLimit = 100
const CloudInventoryRefreshRunSummaryExportLimit = 5000
const CloudInventoryRefreshChangesExportLimit = 5000
const CloudInventoryRefreshRequestBudgetLimit int64 = 100000
const CloudInventoryBoundaryRestartLimit int64 = 2

func IsCloudInventoryRefreshEventKind(kind string) bool {
	switch kind {
	case "started", "resumed", "paused", "request_budget_reached", "retry_scheduled", "boundary_restart", "failed", "completed", "directory_published":
		return true
	default:
		return false
	}
}

type CloudInventoryRefreshEvidenceBundle struct {
	Run    CloudInventoryRefreshRun     `json:"run"`
	Events []CloudInventoryRefreshEvent `json:"events"`
}

const cloudInventoryFileAggregateSQL = `SELECT COALESCE(sum(size_bytes),0),count(*) FROM cloud_inventory WHERE account_fingerprint=? AND is_dir=0 AND path>=? AND path<?`
const cloudInventoryPathParentMismatchSQL = `e.path<>CASE WHEN e.parent_path='/' THEN '/'||e.name ELSE e.parent_path||'/'||e.name END`
const cloudInventoryDirectoryAggregateSQL = `SELECT
	COALESCE(MAX(CASE WHEN path=? AND status='complete' THEN 1 ELSE 0 END),0),
	COALESCE(SUM(CASE WHEN path<>? THEN 1 ELSE 0 END),0),
	COALESCE(SUM(CASE WHEN status<>'complete' THEN 1 ELSE 0 END),0),
	MIN(CASE WHEN status='complete' THEN observed_at_utc END),
	EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.is_dir=1 AND (e.path=? OR (e.path>=? AND e.path<?)) AND NOT EXISTS(SELECT 1 FROM cloud_inventory_directories d WHERE d.account_fingerprint=e.account_fingerprint AND d.path=e.path AND d.status='complete')) OR
	EXISTS(SELECT 1 FROM cloud_inventory_directories d WHERE d.account_fingerprint=? AND d.path<>'/' AND (d.path=? OR (d.path>=? AND d.path<?)) AND NOT EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=d.account_fingerprint AND e.path=d.path AND e.is_dir=1)) OR
	EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.parent_path<>'/' AND (e.path=? OR (e.path>=? AND e.path<?)) AND NOT EXISTS(SELECT 1 FROM cloud_inventory p JOIN cloud_inventory_directories d ON d.account_fingerprint=p.account_fingerprint AND d.path=p.path AND d.status='complete' WHERE p.account_fingerprint=e.account_fingerprint AND p.path=e.parent_path AND p.is_dir=1))
	OR EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=? AND (e.path=? OR (e.path>=? AND e.path<?)) AND ` + cloudInventoryPathParentMismatchSQL + `)
	FROM cloud_inventory_directories WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))`
const upsertCloudInventoryEntrySQL = `INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint,fs_id) DO UPDATE SET parent_path=excluded.parent_path,path=excluded.path,name=excluded.name,is_dir=excluded.is_dir,size_bytes=excluded.size_bytes,provider_mtime=excluded.provider_mtime,content_md5=excluded.content_md5,observed_at_utc=excluded.observed_at_utc,last_seen_run_id=excluded.last_seen_run_id`
const upsertCloudInventoryDirectorySQL = `INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,last_run_id) VALUES(?,?,'stale',?) ON CONFLICT(account_fingerprint,path) DO NOTHING`
const loadCloudScanStateSQL = `SELECT s.account_fingerprint,s.run_id,s.status,s.phase,s.current_directory,s.next_offset,s.pages_processed,s.items_processed,s.pending_count,s.started_at_utc,s.updated_at_utc,s.last_error,s.retry_delay_millis,s.retry_backoff_base_millis,s.retry_at_utc,s.next_page_boundary_json,s.request_budget,s.requests_used,(SELECT count(*) FROM cloud_inventory_scan_events e WHERE e.account_fingerprint=s.account_fingerprint AND e.run_id=s.run_id AND e.current_directory=s.current_directory AND e.event_kind='boundary_restart') FROM cloud_inventory_scan_state s WHERE s.account_fingerprint=?`

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
			entryObservedAt := observedAt
			if entry.observedAtKnown {
				entryObservedAt = entry.observedAtUTC
			}
			entryObservedMicros := int64(0)
			if !entryObservedAt.IsZero() {
				entryObservedMicros = entryObservedAt.UTC().UnixMicro()
			}
			args = append(args, account, entry.FsID, directory, entry.Path, entry.Name, isDir, entry.Size, entry.ProviderMtime, entry.ContentMD5, entryObservedMicros, runID)
		}
		query.WriteString(` ON CONFLICT(account_fingerprint,fs_id) DO UPDATE SET parent_path=excluded.parent_path,path=excluded.path,name=excluded.name,is_dir=excluded.is_dir,size_bytes=excluded.size_bytes,provider_mtime=excluded.provider_mtime,content_md5=excluded.content_md5,observed_at_utc=excluded.observed_at_utc,last_seen_run_id=excluded.last_seen_run_id`)
		if _, err := tx.Exec(query.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

type CloudInventoryEntry struct {
	FsID                  int64      `json:"fs_id"`
	Path                  string     `json:"path"`
	Name                  string     `json:"name"`
	Directory             bool       `json:"directory"`
	Size                  int64      `json:"size"`
	DirectorySizeBytes    *int64     `json:"directory_size_bytes,omitempty"`
	DirectorySizeComplete *bool      `json:"directory_size_complete,omitempty"`
	ProviderMtime         int64      `json:"provider_mtime"`
	ContentMD5            string     `json:"content_md5,omitempty"`
	ContentVerifiedAtUTC  *time.Time `json:"content_verified_at_utc,omitempty"`
	observedAtUTC         time.Time
	observedAtKnown       bool
}

type CloudInventoryMutation struct {
	OperationID           string
	Operation             string
	Before                CloudInventoryEntry
	After                 CloudInventoryEntry
	ResponseObservedAtUTC time.Time
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
	Generation    int64      `json:"cache_generation"`
}

type CloudInventoryIncompleteDirectory struct {
	Path            string     `json:"path"`
	Status          string     `json:"status"`
	Reason          string     `json:"reason"`
	CachedDirectory bool       `json:"cached_directory"`
	ObservedAtUTC   *time.Time `json:"observed_at_utc,omitempty"`
	LastRunID       string     `json:"last_run_id,omitempty"`
}

type CloudInventoryIncompleteDirectoryPage struct {
	Entries        []CloudInventoryIncompleteDirectory `json:"entries"`
	Total          int                                 `json:"total"`
	NextBeforePath string                              `json:"next_before_path,omitempty"`
	HasMore        bool                                `json:"has_more"`
	Generation     int64                               `json:"cache_generation"`
}

type CloudInventorySearchCoverage struct {
	Status                string                         `json:"status"`
	IncompleteDirectories int64                          `json:"incomplete_directories"`
	ScanUpdatedAtUTC      *time.Time                     `json:"scan_updated_at_utc,omitempty"`
	Generation            int64                          `json:"cache_generation"`
	Reasons               *CloudInventoryCoverageReasons `json:"incomplete_reasons,omitempty"`
}

// Structural reason counts are independent diagnostics and may overlap.
type CloudInventoryCoverageReasons struct {
	ScanningDirectories    int64 `json:"scanning_directories"`
	StaleDirectories       int64 `json:"stale_directories"`
	UnknownDirectories     int64 `json:"unknown_directories"`
	FailedDirectories      int64 `json:"failed_directories"`
	MissingDirectoryStatus int64 `json:"missing_directory_status"`
	OrphanDirectoryStatus  int64 `json:"orphan_directory_status"`
	ParentNotComplete      int64 `json:"parent_not_complete"`
	PathMismatch           int64 `json:"path_mismatch"`
	MissingRoot            int64 `json:"missing_root"`
}

type CloudScanState struct {
	AccountFingerprint     string                        `json:"account_fingerprint"`
	RunID                  string                        `json:"run_id"`
	Status                 string                        `json:"status"`
	WorkerActive           bool                          `json:"worker_active"`
	Phase                  string                        `json:"phase"`
	PendingCount           int64                         `json:"pending_count"`
	CurrentDirectory       string                        `json:"current_directory"`
	NextOffset             int64                         `json:"next_offset"`
	PagesProcessed         int64                         `json:"pages_processed"`
	ItemsProcessed         int64                         `json:"items_processed"`
	RequestBudget          int64                         `json:"request_budget"`
	RequestsUsed           int64                         `json:"requests_used"`
	BoundaryRestartCount   int64                         `json:"boundary_restart_count"`
	InventoryCoverage      *CloudInventorySearchCoverage `json:"inventory_coverage,omitempty"`
	StartedAtUTC           time.Time                     `json:"started_at_utc"`
	UpdatedAtUTC           time.Time                     `json:"updated_at_utc"`
	LastError              string                        `json:"last_error,omitempty"`
	RetryDelayMillis       int64                         `json:"retry_delay_millis,omitempty"`
	RetryBackoffBaseMillis int64                         `json:"-"`
	RetryAtUTC             *time.Time                    `json:"retry_at_utc,omitempty"`
	NextPageBoundary       *CloudInventoryPageBoundary   `json:"-"`
}

type CloudInventoryRefreshState struct {
	AccountFingerprint     string                      `json:"account_fingerprint"`
	RunID                  string                      `json:"run_id"`
	RootPath               string                      `json:"root_path"`
	Status                 string                      `json:"status"`
	WorkerActive           bool                        `json:"worker_active"`
	Phase                  string                      `json:"phase"`
	PendingCount           int64                       `json:"pending_count"`
	CurrentDirectory       string                      `json:"current_directory"`
	NextOffset             int64                       `json:"next_offset"`
	PagesProcessed         int64                       `json:"pages_processed"`
	ItemsProcessed         int64                       `json:"items_processed"`
	RequestBudget          int64                       `json:"request_budget"`
	RequestsUsed           int64                       `json:"requests_used"`
	RequestBudgetAdded     int64                       `json:"-"`
	BoundaryRestartCount   int64                       `json:"boundary_restart_count"`
	StartedAtUTC           time.Time                   `json:"started_at_utc"`
	UpdatedAtUTC           time.Time                   `json:"updated_at_utc"`
	LastError              string                      `json:"last_error,omitempty"`
	RetryDelayMillis       int64                       `json:"retry_delay_millis,omitempty"`
	RetryBackoffBaseMillis int64                       `json:"-"`
	RetryAtUTC             *time.Time                  `json:"retry_at_utc,omitempty"`
	NextPageBoundary       *CloudInventoryPageBoundary `json:"-"`
}

// CloudInventoryPageBoundary is local checkpoint evidence from the provider's
// lookahead item. It is intentionally excluded from public status responses.
type CloudInventoryPageBoundary struct {
	FsID          int64  `json:"fs_id"`
	Path          string `json:"path"`
	Name          string `json:"name"`
	Directory     bool   `json:"directory"`
	Size          int64  `json:"size"`
	ProviderMtime int64  `json:"provider_mtime"`
	ContentMD5    string `json:"content_md5"`
}

func marshalCloudInventoryPageBoundary(boundary *CloudInventoryPageBoundary) (string, error) {
	if boundary == nil {
		return "", nil
	}
	if boundary.FsID <= 0 || !validInventoryPath(boundary.Path) || path.Base(boundary.Path) != boundary.Name || boundary.Name == "" || boundary.Size < 0 || len(boundary.ContentMD5) > 256 {
		return "", fmt.Errorf("invalid cloud inventory page boundary")
	}
	body, err := json.Marshal(boundary)
	if err != nil {
		return "", err
	}
	if len(body) > 8192 {
		return "", fmt.Errorf("cloud inventory page boundary exceeds size limit")
	}
	return string(body), nil
}

func unmarshalCloudInventoryPageBoundary(body string) (*CloudInventoryPageBoundary, error) {
	if body == "" {
		return nil, nil
	}
	var boundary CloudInventoryPageBoundary
	if len(body) > 8192 || json.Unmarshal([]byte(body), &boundary) != nil {
		return nil, fmt.Errorf("invalid persisted cloud inventory page boundary")
	}
	if _, err := marshalCloudInventoryPageBoundary(&boundary); err != nil {
		return nil, fmt.Errorf("invalid persisted cloud inventory page boundary: %w", err)
	}
	return &boundary, nil
}

type CloudInventoryRefreshEvent struct {
	Sequence           int64      `json:"sequence"`
	AccountFingerprint string     `json:"-"`
	RunID              string     `json:"run_id"`
	Kind               string     `json:"kind"`
	Status             string     `json:"status"`
	Phase              string     `json:"phase"`
	CurrentDirectory   string     `json:"current_directory,omitempty"`
	PagesProcessed     int64      `json:"pages_processed"`
	ItemsProcessed     int64      `json:"items_processed"`
	RequestBudget      int64      `json:"request_budget"`
	RequestsUsed       int64      `json:"requests_used"`
	RequestBudgetAdded int64      `json:"request_budget_added,omitempty"`
	RetryAtUTC         *time.Time `json:"retry_at_utc,omitempty"`
	LastError          string     `json:"last_error,omitempty"`
	RestartReason      string     `json:"restart_reason,omitempty"`
	RestartOffset      *int64     `json:"restart_offset,omitempty"`
	RestartFSID        *int64     `json:"restart_fs_id,omitempty"`
	PendingCount       int64      `json:"pending_count"`
	RecordedAtUTC      time.Time  `json:"recorded_at_utc"`
	CloudObservedAtUTC *time.Time `json:"cloud_observed_at_utc,omitempty"`
	DirectoryChanged   *bool      `json:"directory_changed,omitempty"`
	AddedCount         *int64     `json:"added_count,omitempty"`
	RemovedCount       *int64     `json:"removed_count,omitempty"`
	ChangedCount       *int64     `json:"changed_count,omitempty"`
}

type CloudInventoryRefreshDelta struct {
	AddedCount   *int64
	RemovedCount *int64
	ChangedCount *int64
	Changes      []CloudInventoryRefreshChangeSnapshot
}

type CloudInventoryRefreshChangeItem struct {
	FsID                       int64      `json:"fs_id"`
	Path                       string     `json:"path"`
	Name                       string     `json:"name"`
	Directory                  bool       `json:"directory"`
	Size                       int64      `json:"size"`
	ProviderMtime              int64      `json:"provider_mtime"`
	ContentMD5                 string     `json:"content_md5,omitempty"`
	ObservedAtUTC              *time.Time `json:"observed_at_utc"`
	ContentSHA256              string     `json:"content_sha256,omitempty"`
	ContentSHA256VerifiedAtUTC *time.Time `json:"content_sha256_verified_at_utc,omitempty"`
}

type CloudInventoryRefreshChangeSnapshot struct {
	Kind string                           `json:"kind"`
	Old  *CloudInventoryRefreshChangeItem `json:"old,omitempty"`
	New  *CloudInventoryRefreshChangeItem `json:"new,omitempty"`
}

type CloudInventoryRefreshChange struct {
	Sequence           int64                            `json:"sequence"`
	Ordinal            int64                            `json:"ordinal"`
	Kind               string                           `json:"kind"`
	Old                *CloudInventoryRefreshChangeItem `json:"old,omitempty"`
	New                *CloudInventoryRefreshChangeItem `json:"new,omitempty"`
	RecordedAtUTC      time.Time                        `json:"recorded_at_utc"`
	CloudObservedAtUTC time.Time                        `json:"cloud_observed_at_utc"`
}

type CloudInventoryRefreshChangeCounts struct {
	Total           int64 `json:"total"`
	Added           int64 `json:"added"`
	Removed         int64 `json:"removed"`
	MetadataChanged int64 `json:"metadata_changed"`
	IdentityChanged int64 `json:"identity_changed"`
}

type CloudInventoryRefreshChangePage struct {
	Changes            []CloudInventoryRefreshChange     `json:"changes"`
	Counts             CloudInventoryRefreshChangeCounts `json:"counts"`
	MatchedCount       int64                             `json:"matched_count"`
	NextBeforeSequence int64                             `json:"next_before_sequence"`
	HasMore            bool                              `json:"has_more"`
}

type CloudInventoryRefreshChangeExport struct {
	RunID              string                            `json:"run_id"`
	EventSequence      int64                             `json:"event_sequence"`
	RecordedAtUTC      time.Time                         `json:"recorded_at_utc"`
	CloudObservedAtUTC time.Time                         `json:"cloud_observed_at_utc"`
	Kind               string                            `json:"kind,omitempty"`
	Query              string                            `json:"query,omitempty"`
	Counts             CloudInventoryRefreshChangeCounts `json:"counts"`
	MatchedCount       int64                             `json:"matched_count"`
	Changes            []CloudInventoryRefreshChange     `json:"changes"`
}

type CloudInventoryRefreshRun struct {
	StartSequence     int64     `json:"start_sequence"`
	RunID             string    `json:"run_id"`
	StartedAtUTC      time.Time `json:"started_at_utc"`
	LatestSequence    int64     `json:"latest_sequence"`
	LatestKind        string    `json:"latest_kind"`
	Status            string    `json:"status"`
	Phase             string    `json:"phase"`
	RootPath          string    `json:"root_path"`
	CurrentDirectory  string    `json:"current_directory,omitempty"`
	PagesProcessed    int64     `json:"pages_processed"`
	ItemsProcessed    int64     `json:"items_processed"`
	RequestBudget     int64     `json:"request_budget"`
	RequestsUsed      int64     `json:"requests_used"`
	PendingCount      int64     `json:"pending_count"`
	LastRecordedAtUTC time.Time `json:"last_recorded_at_utc"`
	LastError         string    `json:"last_error,omitempty"`
}

type CloudInventoryRefreshRunFilter struct {
	Status            string
	StartedFromUTC    string
	StartedThroughUTC string
}

func appendCloudInventoryRefreshEvent(tx *sql.Tx, event CloudInventoryRefreshEvent) error {
	if event.AccountFingerprint == "" || len(event.AccountFingerprint) > 128 || event.RunID == "" || len(event.RunID) > 128 || !validCloudInventoryRefreshEventKind(event.Kind) || !validRemoteScanStatus(event.Status) || event.PagesProcessed < 0 || event.ItemsProcessed < 0 || event.PendingCount < 0 || event.RequestBudgetAdded < 0 || event.RecordedAtUTC.IsZero() || (event.CurrentDirectory != "" && !validInventoryPath(event.CurrentDirectory)) || (event.RetryAtUTC != nil && event.RetryAtUTC.IsZero()) {
		return fmt.Errorf("invalid cloud inventory refresh event")
	}
	if (event.Kind == "retry_scheduled") != (event.RetryAtUTC != nil) || (event.RequestBudgetAdded > 0 && event.Kind != "resumed") || (event.Kind == "boundary_restart") != (event.RestartReason != "") || (event.RestartReason != "" && event.RestartReason != "page_boundary_mismatch" && event.RestartReason != "provider_duplicate_page_id" && event.RestartReason != "staged_page_duplicate_id") || (event.Kind != "boundary_restart" && (event.RestartOffset != nil || event.RestartFSID != nil)) || (event.RestartOffset != nil && *event.RestartOffset <= 0) || (event.RestartFSID != nil && *event.RestartFSID <= 0) || ((event.RestartReason == "provider_duplicate_page_id" || event.RestartReason == "staged_page_duplicate_id") && event.RestartFSID == nil && event.RestartOffset != nil) {
		return fmt.Errorf("invalid cloud inventory refresh event evidence")
	}
	counts := []*int64{event.AddedCount, event.RemovedCount, event.ChangedCount}
	knownCounts := 0
	for _, count := range counts {
		if count != nil {
			knownCounts++
			if *count < 0 {
				return fmt.Errorf("invalid cloud inventory refresh delta count")
			}
		}
	}
	if (knownCounts != 0 && knownCounts != len(counts)) || (event.Kind != "directory_published" && knownCounts != 0) {
		return fmt.Errorf("invalid cloud inventory refresh delta counts")
	}
	var observed any
	if event.CloudObservedAtUTC != nil {
		observed = event.CloudObservedAtUTC.UTC().UnixMicro()
	}
	var retryAt any
	if event.RetryAtUTC != nil {
		retryAt = event.RetryAtUTC.UTC().UnixMicro()
	}
	var changed any
	if event.DirectoryChanged != nil {
		changed = boolToSQLiteInt(*event.DirectoryChanged)
	}
	var added, removed, modified any
	if event.AddedCount != nil {
		added, removed, modified = *event.AddedCount, *event.RemovedCount, *event.ChangedCount
	}
	var restartOffset, restartFSID any
	if event.RestartOffset != nil {
		restartOffset = *event.RestartOffset
	}
	if event.RestartFSID != nil {
		restartFSID = *event.RestartFSID
	}
	result, err := tx.Exec(`INSERT INTO cloud_inventory_refresh_events(account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,request_budget_added,retry_at_utc,pending_count,recorded_at_utc,cloud_observed_at_utc,directory_changed,added_count,removed_count,changed_count,last_error,restart_reason,restart_offset,restart_fs_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, event.AccountFingerprint, event.RunID, event.Kind, event.Status, event.Phase, event.CurrentDirectory, event.PagesProcessed, event.ItemsProcessed, event.RequestBudget, event.RequestsUsed, event.RequestBudgetAdded, retryAt, event.PendingCount, event.RecordedAtUTC.UTC().UnixMicro(), observed, changed, added, removed, modified, event.LastError, event.RestartReason, restartOffset, restartFSID)
	if err != nil {
		return err
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO cloud_inventory_refresh_runs(start_sequence,account_fingerprint,run_id,root_path,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error,latest_sequence,latest_kind) SELECT started.sequence,?,? ,started.current_directory,?,?,?,?,?,?,?,?,started.recorded_at_utc,?,?,?,? FROM cloud_inventory_refresh_events started WHERE started.account_fingerprint=? AND started.run_id=? AND started.event_kind='started' ORDER BY started.sequence LIMIT 1 ON CONFLICT(account_fingerprint,run_id) DO UPDATE SET status=excluded.status,phase=excluded.phase,current_directory=excluded.current_directory,pages_processed=excluded.pages_processed,items_processed=excluded.items_processed,request_budget=excluded.request_budget,requests_used=excluded.requests_used,pending_count=excluded.pending_count,updated_at_utc=excluded.updated_at_utc,last_error=excluded.last_error,latest_sequence=excluded.latest_sequence,latest_kind=excluded.latest_kind`, event.AccountFingerprint, event.RunID, event.Status, event.Phase, event.CurrentDirectory, event.PagesProcessed, event.ItemsProcessed, event.RequestBudget, event.RequestsUsed, event.PendingCount, event.RecordedAtUTC.UTC().UnixMicro(), event.LastError, sequence, event.Kind, event.AccountFingerprint, event.RunID)
	return err
}

func appendCloudInventoryRefreshChanges(tx *sql.Tx, event CloudInventoryRefreshEvent, changes []CloudInventoryRefreshChangeSnapshot) error {
	if len(changes) == 0 {
		return nil
	}
	if event.Kind != "directory_published" || event.CloudObservedAtUTC == nil {
		return fmt.Errorf("refresh change details require a published directory event")
	}
	var eventSequence int64
	if err := tx.QueryRow(`SELECT sequence FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=? AND event_kind='directory_published' AND recorded_at_utc=? ORDER BY sequence DESC LIMIT 1`, event.AccountFingerprint, event.RunID, event.RecordedAtUTC.UTC().UnixMicro()).Scan(&eventSequence); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO cloud_inventory_refresh_changes(account_fingerprint,run_id,event_sequence,ordinal,change_kind,old_snapshot,new_snapshot,recorded_at_utc,cloud_observed_at_utc) VALUES(?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for index, change := range changes {
		if !validCloudInventoryRefreshChange(change) {
			return fmt.Errorf("invalid cloud inventory refresh change detail")
		}
		var oldSnapshot, newSnapshot any
		if change.Old != nil {
			encoded, marshalErr := json.Marshal(change.Old)
			if marshalErr != nil {
				return marshalErr
			}
			oldSnapshot = string(encoded)
		}
		if change.New != nil {
			encoded, marshalErr := json.Marshal(change.New)
			if marshalErr != nil {
				return marshalErr
			}
			newSnapshot = string(encoded)
		}
		if _, err = stmt.Exec(event.AccountFingerprint, event.RunID, eventSequence, index+1, change.Kind, oldSnapshot, newSnapshot, event.RecordedAtUTC.UTC().UnixMicro(), event.CloudObservedAtUTC.UTC().UnixMicro()); err != nil {
			return err
		}
	}
	return nil
}

func validCloudInventoryRefreshChange(change CloudInventoryRefreshChangeSnapshot) bool {
	validItem := func(item *CloudInventoryRefreshChangeItem) bool {
		return item != nil && item.FsID > 0 && validInventoryPath(item.Path) && path.Base(item.Path) == item.Name && item.Name != "" && item.Size >= 0
	}
	switch change.Kind {
	case "added":
		return change.Old == nil && validItem(change.New)
	case "removed":
		return validItem(change.Old) && change.New == nil
	case "metadata_changed":
		return validItem(change.Old) && validItem(change.New) && change.Old.FsID == change.New.FsID
	case "identity_changed":
		return validItem(change.Old) && validItem(change.New) && change.Old.FsID != change.New.FsID && change.Old.Path == change.New.Path
	default:
		return false
	}
}

func refreshEventFromState(state CloudInventoryRefreshState, kind string) CloudInventoryRefreshEvent {
	event := CloudInventoryRefreshEvent{
		AccountFingerprint: state.AccountFingerprint,
		RunID:              state.RunID,
		Kind:               kind,
		Status:             state.Status,
		Phase:              state.Phase,
		CurrentDirectory:   state.CurrentDirectory,
		PagesProcessed:     state.PagesProcessed,
		ItemsProcessed:     state.ItemsProcessed,
		RequestBudget:      state.RequestBudget,
		RequestsUsed:       state.RequestsUsed,
		LastError:          state.LastError,
		PendingCount:       state.PendingCount,
		RecordedAtUTC:      state.UpdatedAtUTC,
	}
	if kind == "retry_scheduled" {
		event.RetryAtUTC = state.RetryAtUTC
	}
	return event
}

func validCloudInventoryRefreshEventKind(kind string) bool {
	switch kind {
	case "started", "resumed", "paused", "request_budget_reached", "retry_scheduled", "boundary_restart", "failed", "completed", "directory_published":
		return true
	default:
		return false
	}
}

func (s *Store) ListCloudInventoryRefreshRuns(account string, beforeStartSequence int64, limit int) ([]CloudInventoryRefreshRun, error) {
	return s.ListCloudInventoryRefreshRunsFiltered(account, beforeStartSequence, CloudInventoryRefreshRunFilter{}, limit)
}

func (s *Store) ListCloudInventoryRefreshRunsFiltered(account string, beforeStartSequence int64, filter CloudInventoryRefreshRunFilter, limit int) ([]CloudInventoryRefreshRun, error) {
	if account == "" || len(account) > 128 || beforeStartSequence < 0 || limit < 1 || limit > 101 {
		return nil, fmt.Errorf("invalid cloud inventory refresh run query")
	}
	query, args, err := cloudInventoryRefreshRunsQuery(account, beforeStartSequence, filter, limit)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCloudInventoryRefreshRuns(rows)
}

func cloudInventoryRefreshRunsQuery(account string, beforeStartSequence int64, filter CloudInventoryRefreshRunFilter, limit int) (string, []any, error) {
	if account == "" || len(account) > 128 || beforeStartSequence < 0 || limit < 1 {
		return "", nil, fmt.Errorf("invalid cloud inventory refresh run query")
	}
	if filter.Status != "" && !validRemoteScanStatus(filter.Status) {
		return "", nil, fmt.Errorf("invalid cloud inventory refresh run status")
	}
	for _, date := range []string{filter.StartedFromUTC, filter.StartedThroughUTC} {
		if date == "" {
			continue
		}
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil || parsed.Format("2006-01-02") != date || !time.UnixMicro(parsed.UnixMicro()).Equal(parsed.UTC()) {
			return "", nil, fmt.Errorf("invalid cloud inventory refresh run UTC date")
		}
	}
	if filter.StartedFromUTC != "" && filter.StartedThroughUTC != "" && filter.StartedFromUTC > filter.StartedThroughUTC {
		return "", nil, fmt.Errorf("invalid cloud inventory refresh run date range")
	}
	query := `SELECT start_sequence,run_id,started_at_utc,latest_sequence,latest_kind,status,phase,root_path,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,updated_at_utc,last_error FROM cloud_inventory_refresh_runs WHERE account_fingerprint=?`
	args := []any{account}
	if filter.Status != "" {
		query += ` AND status=?`
		args = append(args, filter.Status)
	}
	if filter.StartedFromUTC != "" {
		query += ` AND started_at_utc>=?`
		from, _ := time.Parse("2006-01-02", filter.StartedFromUTC)
		args = append(args, from.UTC().UnixMicro())
	}
	if filter.StartedThroughUTC != "" {
		query += ` AND started_at_utc<?`
		through, _ := time.Parse("2006-01-02", filter.StartedThroughUTC)
		args = append(args, through.AddDate(0, 0, 1).UTC().UnixMicro())
	}
	if beforeStartSequence > 0 {
		query += ` AND start_sequence<?`
		args = append(args, beforeStartSequence)
	}
	query += ` ORDER BY start_sequence DESC LIMIT ?`
	args = append(args, limit)
	return query, args, nil
}

func scanCloudInventoryRefreshRuns(rows *sql.Rows) ([]CloudInventoryRefreshRun, error) {
	runs := make([]CloudInventoryRefreshRun, 0)
	for rows.Next() {
		var run CloudInventoryRefreshRun
		var startedAt, lastRecorded int64
		if err := rows.Scan(&run.StartSequence, &run.RunID, &startedAt, &run.LatestSequence, &run.LatestKind, &run.Status, &run.Phase, &run.RootPath, &run.CurrentDirectory, &run.PagesProcessed, &run.ItemsProcessed, &run.RequestBudget, &run.RequestsUsed, &run.PendingCount, &lastRecorded, &run.LastError); err != nil {
			return nil, err
		}
		run.StartedAtUTC = time.UnixMicro(startedAt).UTC()
		run.LastRecordedAtUTC = time.UnixMicro(lastRecorded).UTC()
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) ExportCloudInventoryRefreshRunSummaries(ctx context.Context, account string, filter CloudInventoryRefreshRunFilter) ([]CloudInventoryRefreshRun, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query, args, err := cloudInventoryRefreshRunsQuery(account, 0, filter, CloudInventoryRefreshRunSummaryExportLimit+1)
	if err != nil {
		return nil, err
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
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	items, err := scanCloudInventoryRefreshRuns(rows)
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if len(items) > CloudInventoryRefreshRunSummaryExportLimit {
		return nil, ErrCloudInventoryRefreshRunSummaryExportTooLarge
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func IsCloudInventoryRefreshRestartReason(reason string) bool {
	return reason == "page_boundary_mismatch" || reason == "provider_duplicate_page_id" || reason == "staged_page_duplicate_id"
}

func (s *Store) ListCloudInventoryRefreshEvents(account, runID, kind string, beforeSequence int64, limit int) ([]CloudInventoryRefreshEvent, error) {
	return s.ListCloudInventoryRefreshEventsFiltered(account, runID, kind, "", beforeSequence, limit)
}

func (s *Store) ListCloudInventoryRefreshEventsFiltered(account, runID, kind, restartReason string, beforeSequence int64, limit int) ([]CloudInventoryRefreshEvent, error) {
	events := []CloudInventoryRefreshEvent{}
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 || (kind != "" && !IsCloudInventoryRefreshEventKind(kind)) || (restartReason != "" && !IsCloudInventoryRefreshRestartReason(restartReason)) || beforeSequence < 0 || limit < 1 || limit > 101 {
		return nil, fmt.Errorf("invalid cloud inventory refresh event query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	query := `SELECT sequence,account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,request_budget_added,retry_at_utc,pending_count,recorded_at_utc,cloud_observed_at_utc,directory_changed,added_count,removed_count,changed_count,last_error,restart_reason,restart_offset,restart_fs_id FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=?`
	args := []any{account, runID}
	if beforeSequence > 0 {
		query += ` AND sequence<?`
		args = append(args, beforeSequence)
	}
	if kind != "" {
		query += ` AND event_kind=?`
		args = append(args, kind)
	}
	if restartReason != "" {
		query += ` AND restart_reason=?`
		args = append(args, restartReason)
	}
	query += ` ORDER BY sequence DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var event CloudInventoryRefreshEvent
		var recorded int64
		var observed sql.NullInt64
		var retryAt sql.NullInt64
		var changed sql.NullInt64
		var added, removed, modified sql.NullInt64
		var restartOffset, restartFSID sql.NullInt64
		if err = rows.Scan(&event.Sequence, &event.AccountFingerprint, &event.RunID, &event.Kind, &event.Status, &event.Phase, &event.CurrentDirectory, &event.PagesProcessed, &event.ItemsProcessed, &event.RequestBudget, &event.RequestsUsed, &event.RequestBudgetAdded, &retryAt, &event.PendingCount, &recorded, &observed, &changed, &added, &removed, &modified, &event.LastError, &event.RestartReason, &restartOffset, &restartFSID); err != nil {
			return nil, err
		}
		event.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		if retryAt.Valid {
			value := time.UnixMicro(retryAt.Int64).UTC()
			event.RetryAtUTC = &value
		}
		if observed.Valid {
			value := time.UnixMicro(observed.Int64).UTC()
			event.CloudObservedAtUTC = &value
		}
		if changed.Valid {
			value := changed.Int64 == 1
			event.DirectoryChanged = &value
		}
		setRefreshDeltaCounts(&event, added, removed, modified)
		if restartOffset.Valid {
			value := restartOffset.Int64
			event.RestartOffset = &value
		}
		if restartFSID.Valid {
			value := restartFSID.Int64
			event.RestartFSID = &value
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) ListCloudInventoryRefreshChanges(account, runID string, eventSequence, beforeSequence int64, limit int) ([]CloudInventoryRefreshChange, error) {
	changes := []CloudInventoryRefreshChange{}
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 || eventSequence <= 0 || beforeSequence < 0 || limit < 1 || limit > 51 {
		return nil, fmt.Errorf("invalid cloud inventory refresh change query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	var kind string
	if err := s.db.QueryRow(`SELECT event_kind FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=? AND sequence=?`, account, runID, eventSequence).Scan(&kind); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCloudInventoryRefreshNotFound
	} else if err != nil {
		return nil, err
	}
	if kind != "directory_published" {
		return nil, ErrCloudInventoryRefreshNotFound
	}
	var recorded, observed int64
	if err := s.db.QueryRow(`SELECT recorded_at_utc,cloud_observed_at_utc FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=? AND sequence=?`, account, runID, eventSequence).Scan(&recorded, &observed); err != nil {
		return nil, err
	}
	query := `SELECT change_sequence,ordinal,change_kind,old_snapshot,new_snapshot FROM cloud_inventory_refresh_changes WHERE account_fingerprint=? AND run_id=? AND event_sequence=?`
	args := []any{account, runID, eventSequence}
	if beforeSequence > 0 {
		query += ` AND change_sequence<?`
		args = append(args, beforeSequence)
	}
	query += ` ORDER BY change_sequence DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var change CloudInventoryRefreshChange
		var oldSnapshot, newSnapshot sql.NullString
		if err = rows.Scan(&change.Sequence, &change.Ordinal, &change.Kind, &oldSnapshot, &newSnapshot); err != nil {
			return nil, err
		}
		if oldSnapshot.Valid {
			var item CloudInventoryRefreshChangeItem
			if err = json.Unmarshal([]byte(oldSnapshot.String), &item); err != nil {
				return nil, fmt.Errorf("decode stored cloud refresh old snapshot: %w", err)
			}
			change.Old = &item
		}
		if newSnapshot.Valid {
			var item CloudInventoryRefreshChangeItem
			if err = json.Unmarshal([]byte(newSnapshot.String), &item); err != nil {
				return nil, fmt.Errorf("decode stored cloud refresh new snapshot: %w", err)
			}
			change.New = &item
		}
		change.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		change.CloudObservedAtUTC = time.UnixMicro(observed).UTC()
		changes = append(changes, change)
	}
	return changes, rows.Err()
}

// QueryCloudInventoryRefreshChanges returns one stable filtered page while keeping event totals unfiltered.
func (s *Store) QueryCloudInventoryRefreshChanges(account, runID string, eventSequence, beforeSequence int64, limit int, kind, query string) (CloudInventoryRefreshChangePage, error) {
	page := CloudInventoryRefreshChangePage{Changes: []CloudInventoryRefreshChange{}}
	query = strings.TrimSpace(query)
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 || eventSequence <= 0 || beforeSequence < 0 || limit < 1 || limit > 50 || (kind != "" && !validCloudInventoryRefreshChangeKind(kind)) || utf8.RuneCountInString(query) > 128 {
		return page, fmt.Errorf("invalid cloud inventory refresh change query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return page, os.ErrClosed
	}
	var eventKind string
	var recorded, observed int64
	if err := s.db.QueryRow(`SELECT event_kind,recorded_at_utc,cloud_observed_at_utc FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=? AND sequence=?`, account, runID, eventSequence).Scan(&eventKind, &recorded, &observed); errors.Is(err, sql.ErrNoRows) {
		return page, ErrCloudInventoryRefreshNotFound
	} else if err != nil {
		return page, err
	}
	if eventKind != "directory_published" {
		return page, ErrCloudInventoryRefreshNotFound
	}
	base := `account_fingerprint=? AND run_id=? AND event_sequence=?`
	baseArgs := []any{account, runID, eventSequence}
	rows, err := s.db.Query(`SELECT change_kind,count(*) FROM cloud_inventory_refresh_changes WHERE `+base+` GROUP BY change_kind`, baseArgs...)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var changeKind string
		var count int64
		if err = rows.Scan(&changeKind, &count); err != nil {
			rows.Close()
			return page, err
		}
		page.Counts.Total += count
		switch changeKind {
		case "added":
			page.Counts.Added = count
		case "removed":
			page.Counts.Removed = count
		case "metadata_changed":
			page.Counts.MetadataChanged = count
		case "identity_changed":
			page.Counts.IdentityChanged = count
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return page, err
	}
	rows.Close()
	where, args := cloudInventoryRefreshChangeWhere(base, baseArgs, kind, query)
	if err = s.db.QueryRow(`SELECT count(*) FROM cloud_inventory_refresh_changes WHERE `+where, args...).Scan(&page.MatchedCount); err != nil {
		return page, err
	}
	listQuery := `SELECT change_sequence,ordinal,change_kind,old_snapshot,new_snapshot FROM cloud_inventory_refresh_changes WHERE ` + where
	listArgs := append([]any(nil), args...)
	if beforeSequence > 0 {
		listQuery += ` AND change_sequence<?`
		listArgs = append(listArgs, beforeSequence)
	}
	listQuery += ` ORDER BY change_sequence DESC LIMIT ?`
	// Put the limit last after the optional cursor predicate.
	if beforeSequence > 0 {
	}
	listArgs = append(listArgs, limit+1)
	changeRows, err := s.db.Query(listQuery, listArgs...)
	if err != nil {
		return page, err
	}
	defer changeRows.Close()
	for changeRows.Next() {
		var change CloudInventoryRefreshChange
		var oldSnapshot, newSnapshot sql.NullString
		if err = changeRows.Scan(&change.Sequence, &change.Ordinal, &change.Kind, &oldSnapshot, &newSnapshot); err != nil {
			return page, err
		}
		if oldSnapshot.Valid {
			var item CloudInventoryRefreshChangeItem
			if err = json.Unmarshal([]byte(oldSnapshot.String), &item); err != nil {
				return page, fmt.Errorf("decode stored cloud refresh old snapshot: %w", err)
			}
			change.Old = &item
		}
		if newSnapshot.Valid {
			var item CloudInventoryRefreshChangeItem
			if err = json.Unmarshal([]byte(newSnapshot.String), &item); err != nil {
				return page, fmt.Errorf("decode stored cloud refresh new snapshot: %w", err)
			}
			change.New = &item
		}
		change.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		change.CloudObservedAtUTC = time.UnixMicro(observed).UTC()
		page.Changes = append(page.Changes, change)
	}
	if err = changeRows.Err(); err != nil {
		return page, err
	}
	if len(page.Changes) > limit {
		page.Changes = page.Changes[:limit]
		page.HasMore = true
		page.NextBeforeSequence = page.Changes[len(page.Changes)-1].Sequence
	}
	return page, nil
}

func validCloudInventoryRefreshChangeKind(kind string) bool {
	switch kind {
	case "added", "removed", "metadata_changed", "identity_changed":
		return true
	default:
		return false
	}
}

func IsCloudInventoryRefreshChangeKind(kind string) bool {
	return validCloudInventoryRefreshChangeKind(kind)
}

func cloudInventoryRefreshChangeWhere(base string, args []any, kind, query string) (string, []any) {
	where := base
	filteredArgs := append([]any(nil), args...)
	if kind != "" {
		where += ` AND change_kind=?`
		filteredArgs = append(filteredArgs, kind)
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query != "" {
		literal := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
		pattern := `%` + literal + `%`
		where += ` AND (
			lower(coalesce(json_extract(old_snapshot,'$.path'),'')) LIKE ? ESCAPE '\' OR
			lower(coalesce(json_extract(old_snapshot,'$.name'),'')) LIKE ? ESCAPE '\' OR
			lower(coalesce(CAST(json_extract(old_snapshot,'$.fs_id') AS TEXT),'')) LIKE ? ESCAPE '\' OR
			lower(coalesce(json_extract(new_snapshot,'$.path'),'')) LIKE ? ESCAPE '\' OR
			lower(coalesce(json_extract(new_snapshot,'$.name'),'')) LIKE ? ESCAPE '\' OR
			lower(coalesce(CAST(json_extract(new_snapshot,'$.fs_id') AS TEXT),'')) LIKE ? ESCAPE '\')`
		filteredArgs = append(filteredArgs, pattern, pattern, pattern, pattern, pattern, pattern)
	}
	return where, filteredArgs
}

// CloudInventoryRefreshChangesExport returns one bounded event-local detail export from a single SQLite snapshot.
func (s *Store) CloudInventoryRefreshChangesExport(ctx context.Context, account, runID string, eventSequence int64, kind, query string) (CloudInventoryRefreshChangeExport, error) {
	result := CloudInventoryRefreshChangeExport{RunID: runID, EventSequence: eventSequence, Kind: kind, Query: strings.TrimSpace(query), Changes: []CloudInventoryRefreshChange{}}
	query = strings.TrimSpace(query)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 || eventSequence <= 0 || (kind != "" && !validCloudInventoryRefreshChangeKind(kind)) || utf8.RuneCountInString(query) > 128 {
		return result, fmt.Errorf("invalid cloud inventory refresh changes export query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return result, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var eventKind string
	var recorded, observed int64
	if err = tx.QueryRowContext(ctx, `SELECT event_kind,recorded_at_utc,cloud_observed_at_utc FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=? AND sequence=?`, account, runID, eventSequence).Scan(&eventKind, &recorded, &observed); errors.Is(err, sql.ErrNoRows) {
		return result, ErrCloudInventoryRefreshNotFound
	} else if err != nil {
		return result, err
	}
	if eventKind != "directory_published" {
		return result, ErrCloudInventoryRefreshNotFound
	}
	result.RecordedAtUTC = time.UnixMicro(recorded).UTC()
	result.CloudObservedAtUTC = time.UnixMicro(observed).UTC()
	base := `account_fingerprint=? AND run_id=? AND event_sequence=?`
	baseArgs := []any{account, runID, eventSequence}
	countRows, err := tx.QueryContext(ctx, `SELECT change_kind,count(*) FROM cloud_inventory_refresh_changes WHERE `+base+` GROUP BY change_kind`, baseArgs...)
	if err != nil {
		return result, err
	}
	for countRows.Next() {
		var changeKind string
		var count int64
		if err = countRows.Scan(&changeKind, &count); err != nil {
			countRows.Close()
			return result, err
		}
		result.Counts.Total += count
		switch changeKind {
		case "added":
			result.Counts.Added = count
		case "removed":
			result.Counts.Removed = count
		case "metadata_changed":
			result.Counts.MetadataChanged = count
		case "identity_changed":
			result.Counts.IdentityChanged = count
		}
	}
	if err = countRows.Err(); err != nil {
		countRows.Close()
		return result, err
	}
	countRows.Close()
	where, args := cloudInventoryRefreshChangeWhere(base, baseArgs, kind, query)
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM cloud_inventory_refresh_changes WHERE `+where, args...).Scan(&result.MatchedCount); err != nil {
		return result, err
	}
	if result.MatchedCount > CloudInventoryRefreshChangesExportLimit {
		return CloudInventoryRefreshChangeExport{}, ErrCloudInventoryRefreshChangesExportTooLarge
	}
	rows, err := tx.QueryContext(ctx, `SELECT change_sequence,ordinal,change_kind,old_snapshot,new_snapshot FROM cloud_inventory_refresh_changes WHERE `+where+` ORDER BY change_sequence DESC LIMIT ?`, append(args, CloudInventoryRefreshChangesExportLimit+1)...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var change CloudInventoryRefreshChange
		var oldSnapshot, newSnapshot sql.NullString
		if err = rows.Scan(&change.Sequence, &change.Ordinal, &change.Kind, &oldSnapshot, &newSnapshot); err != nil {
			return result, err
		}
		if oldSnapshot.Valid {
			var item CloudInventoryRefreshChangeItem
			if err = json.Unmarshal([]byte(oldSnapshot.String), &item); err != nil {
				return result, fmt.Errorf("decode stored cloud refresh old snapshot: %w", err)
			}
			change.Old = &item
		}
		if newSnapshot.Valid {
			var item CloudInventoryRefreshChangeItem
			if err = json.Unmarshal([]byte(newSnapshot.String), &item); err != nil {
				return result, fmt.Errorf("decode stored cloud refresh new snapshot: %w", err)
			}
			change.New = &item
		}
		change.RecordedAtUTC = result.RecordedAtUTC
		change.CloudObservedAtUTC = result.CloudObservedAtUTC
		result.Changes = append(result.Changes, change)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Changes) != int(result.MatchedCount) {
		return CloudInventoryRefreshChangeExport{}, fmt.Errorf("cloud refresh change export count changed within snapshot")
	}
	if err = tx.Commit(); err != nil {
		return CloudInventoryRefreshChangeExport{}, err
	}
	return result, nil
}

// CloudInventoryRefreshEvidenceExport reads one run summary and its complete bounded event history from one SQLite snapshot.
func (s *Store) CloudInventoryRefreshEvidenceExport(ctx context.Context, account, runID string) (CloudInventoryRefreshEvidenceBundle, error) {
	if err := ctx.Err(); err != nil {
		return CloudInventoryRefreshEvidenceBundle{}, err
	}
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 {
		return CloudInventoryRefreshEvidenceBundle{}, fmt.Errorf("invalid cloud inventory refresh evidence export query")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return CloudInventoryRefreshEvidenceBundle{}, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CloudInventoryRefreshEvidenceBundle{}, err
	}
	defer tx.Rollback()
	var bundle CloudInventoryRefreshEvidenceBundle
	var startedAt int64
	var updatedAt int64
	err = tx.QueryRowContext(ctx, `SELECT start_sequence,run_id,started_at_utc,latest_sequence,latest_kind,status,phase,root_path,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,updated_at_utc,last_error FROM cloud_inventory_refresh_runs WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&bundle.Run.StartSequence, &bundle.Run.RunID, &startedAt, &bundle.Run.LatestSequence, &bundle.Run.LatestKind, &bundle.Run.Status, &bundle.Run.Phase, &bundle.Run.RootPath, &bundle.Run.CurrentDirectory, &bundle.Run.PagesProcessed, &bundle.Run.ItemsProcessed, &bundle.Run.RequestBudget, &bundle.Run.RequestsUsed, &bundle.Run.PendingCount, &updatedAt, &bundle.Run.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return CloudInventoryRefreshEvidenceBundle{}, ErrCloudInventoryRefreshNotFound
	}
	if err != nil {
		return CloudInventoryRefreshEvidenceBundle{}, err
	}
	bundle.Run.StartedAtUTC = time.UnixMicro(startedAt).UTC()
	bundle.Run.LastRecordedAtUTC = time.UnixMicro(updatedAt).UTC()
	rows, err := tx.QueryContext(ctx, `SELECT sequence,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,request_budget_added,retry_at_utc,pending_count,recorded_at_utc,cloud_observed_at_utc,directory_changed,added_count,removed_count,changed_count,last_error,restart_reason,restart_offset,restart_fs_id FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=? ORDER BY sequence ASC LIMIT ?`, account, runID, CloudInventoryRefreshEvidenceExportLimit+1)
	if err != nil {
		return CloudInventoryRefreshEvidenceBundle{}, err
	}
	bundle.Events = make([]CloudInventoryRefreshEvent, 0, CloudInventoryRefreshEvidenceExportLimit)
	for rows.Next() {
		var event CloudInventoryRefreshEvent
		var recorded int64
		var observed, changed, added, removed, modified, retryAt, restartOffset, restartFSID sql.NullInt64
		if err = rows.Scan(&event.Sequence, &event.RunID, &event.Kind, &event.Status, &event.Phase, &event.CurrentDirectory, &event.PagesProcessed, &event.ItemsProcessed, &event.RequestBudget, &event.RequestsUsed, &event.RequestBudgetAdded, &retryAt, &event.PendingCount, &recorded, &observed, &changed, &added, &removed, &modified, &event.LastError, &event.RestartReason, &restartOffset, &restartFSID); err != nil {
			rows.Close()
			return CloudInventoryRefreshEvidenceBundle{}, err
		}
		event.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		if retryAt.Valid {
			value := time.UnixMicro(retryAt.Int64).UTC()
			event.RetryAtUTC = &value
		}
		if observed.Valid {
			value := time.UnixMicro(observed.Int64).UTC()
			event.CloudObservedAtUTC = &value
		}
		if changed.Valid {
			value := changed.Int64 == 1
			event.DirectoryChanged = &value
		}
		setRefreshDeltaCounts(&event, added, removed, modified)
		if restartOffset.Valid {
			value := restartOffset.Int64
			event.RestartOffset = &value
		}
		if restartFSID.Valid {
			value := restartFSID.Int64
			event.RestartFSID = &value
		}
		bundle.Events = append(bundle.Events, event)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return CloudInventoryRefreshEvidenceBundle{}, err
	}
	if err = rows.Close(); err != nil {
		return CloudInventoryRefreshEvidenceBundle{}, err
	}
	if len(bundle.Events) > CloudInventoryRefreshEvidenceExportLimit {
		return CloudInventoryRefreshEvidenceBundle{}, ErrCloudInventoryRefreshEvidenceExportTooLarge
	}
	if err = tx.Commit(); err != nil {
		return CloudInventoryRefreshEvidenceBundle{}, err
	}
	return bundle, nil
}

func setRefreshDeltaCounts(event *CloudInventoryRefreshEvent, added, removed, changed sql.NullInt64) {
	if added.Valid && removed.Valid && changed.Valid {
		event.AddedCount = &added.Int64
		event.RemovedCount = &removed.Int64
		event.ChangedCount = &changed.Int64
	}
}

func cloudInventoryPathWithin(root, candidate string) bool {
	if root == candidate {
		return true
	}
	if root == "/" {
		return strings.HasPrefix(candidate, "/")
	}
	return strings.HasPrefix(candidate, root+"/")
}

func validInventoryPath(value string) bool {
	return value != "" && strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.Contains(value, "\\") && len(value) <= 4096
}

func IsValidCloudInventoryPath(value string) bool { return validInventoryPath(value) }

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
	if _, err := tx.Exec(`DELETE FROM cloud_content_hashes WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))`, account, directory, prefix, upper); err != nil {
		return err
	}
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

func preserveMissingCloudInventoryMD5(tx *sql.Tx, account, directory string, entries []CloudInventoryEntry) error {
	for index := range entries {
		item := &entries[index]
		var oldPath, oldMD5 string
		var oldSize, oldMtime int64
		var oldDirectory int
		err := tx.QueryRow(`SELECT path,size_bytes,provider_mtime,content_md5,is_dir FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=?`, account, item.FsID).Scan(&oldPath, &oldSize, &oldMtime, &oldMD5, &oldDirectory)
		if errorsIsNoRows(err) {
			continue
		}
		if err != nil {
			return err
		}
		if item.ContentMD5 == "" && oldPath == item.Path && oldSize == item.Size && oldMtime == item.ProviderMtime && oldDirectory == boolToSQLiteInt(item.Directory) {
			item.ContentMD5 = oldMD5
		}
		if !item.Directory && (oldPath != item.Path || oldSize != item.Size || oldMtime != item.ProviderMtime || (item.ContentMD5 != "" && oldMD5 != "" && !strings.EqualFold(oldMD5, item.ContentMD5))) {
			if _, err = tx.Exec(`DELETE FROM cloud_content_hashes WHERE account_fingerprint=? AND fs_id=?`, account, item.FsID); err != nil {
				return err
			}
		}
	}
	return nil
}

func boolToSQLiteInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func cloudInventoryDirectoryDelta(tx *sql.Tx, account, directory string, entries []CloudInventoryEntry, observedAt time.Time) (CloudInventoryRefreshDelta, bool, error) {
	unknown := CloudInventoryRefreshDelta{}
	var status string
	err := tx.QueryRow(`SELECT status FROM cloud_inventory_directories WHERE account_fingerprint=? AND path=?`, account, directory).Scan(&status)
	if errorsIsNoRows(err) {
		return unknown, false, nil
	}
	if err != nil {
		return unknown, false, err
	}
	if status != "complete" {
		return unknown, false, nil
	}
	previous := make(map[int64]CloudInventoryRefreshChangeItem)
	rows, err := tx.Query(`SELECT i.fs_id,i.path,i.name,i.is_dir,i.size_bytes,i.provider_mtime,i.content_md5,i.observed_at_utc,h.sha256,h.verified_at_utc FROM cloud_inventory AS i LEFT JOIN cloud_content_hashes AS h ON h.account_fingerprint=i.account_fingerprint AND h.fs_id=i.fs_id AND h.path=i.path AND h.size_bytes=i.size_bytes AND h.provider_mtime=i.provider_mtime WHERE i.account_fingerprint=? AND i.parent_path=?`, account, directory)
	if err != nil {
		return unknown, false, err
	}
	for rows.Next() {
		var item CloudInventoryRefreshChangeItem
		var isDir int
		var observed, verified sql.NullInt64
		var sha sql.NullString
		if err = rows.Scan(&item.FsID, &item.Path, &item.Name, &isDir, &item.Size, &item.ProviderMtime, &item.ContentMD5, &observed, &sha, &verified); err != nil {
			_ = rows.Close()
			return unknown, false, err
		}
		item.Directory = isDir == 1
		if observed.Valid && observed.Int64 > 0 {
			value := time.UnixMicro(observed.Int64).UTC()
			item.ObservedAtUTC = &value
		}
		if sha.Valid && verified.Valid {
			item.ContentSHA256 = sha.String
			value := time.UnixMicro(verified.Int64).UTC()
			item.ContentSHA256VerifiedAtUTC = &value
		}
		previous[item.FsID] = item
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return unknown, false, err
	}
	if err = rows.Close(); err != nil {
		return unknown, false, err
	}
	current := make(map[int64]CloudInventoryRefreshChangeItem, len(entries))
	for index := range entries {
		item := entries[index]
		if _, duplicate := current[item.FsID]; duplicate {
			return unknown, false, nil
		}
		if old, exists := previous[item.FsID]; exists && item.ContentMD5 == "" && old.Path == item.Path && old.Size == item.Size && old.ProviderMtime == item.ProviderMtime && old.Directory == item.Directory {
			item.ContentMD5 = old.ContentMD5
			entries[index].ContentMD5 = old.ContentMD5
		}
		item.observedAtUTC = observedAt.UTC()
		if entries[index].observedAtKnown {
			item.observedAtUTC = entries[index].observedAtUTC
			if !item.observedAtUTC.IsZero() {
				item.observedAtUTC = item.observedAtUTC.UTC()
			}
		}
		observed := item.observedAtUTC
		currentItem := CloudInventoryRefreshChangeItem{FsID: item.FsID, Path: item.Path, Name: item.Name, Directory: item.Directory, Size: item.Size, ProviderMtime: item.ProviderMtime, ContentMD5: item.ContentMD5}
		if !observed.IsZero() {
			currentItem.ObservedAtUTC = &observed
		}
		current[item.FsID] = currentItem
	}
	added, removed, modified := int64(0), int64(0), int64(0)
	var changes []CloudInventoryRefreshChangeSnapshot
	var addedItems, removedItems []CloudInventoryRefreshChangeItem
	for id, item := range current {
		old, exists := previous[id]
		if !exists {
			// If the destination is refreshed before the source, the global
			// fs_id upsert relocates its row. Capture that historical path now;
			// otherwise the later source page can no longer record the move.
			global, e := loadCloudRefreshGlobalItem(tx, account, id)
			if e != nil {
				return unknown, false, e
			}
			if global != nil && path.Dir(global.Path) != directory {
				modified++
				newCopy := item
				changes = append(changes, CloudInventoryRefreshChangeSnapshot{Kind: "metadata_changed", Old: global, New: &newCopy})
				continue
			}
			added++
			addedItems = append(addedItems, item)
			continue
		}
		if old.Path != item.Path || old.Name != item.Name || old.Directory != item.Directory || old.Size != item.Size || old.ProviderMtime != item.ProviderMtime || old.ContentMD5 != item.ContentMD5 {
			modified++
			oldCopy, newCopy := old, item
			changes = append(changes, CloudInventoryRefreshChangeSnapshot{Kind: "metadata_changed", Old: &oldCopy, New: &newCopy})
		}
	}
	for id, item := range previous {
		if _, exists := current[id]; !exists {
			removed++
			removedItems = append(removedItems, item)
		}
	}
	addedByPath := make(map[string][]int)
	removedByPath := make(map[string][]int)
	for index := range addedItems {
		addedByPath[addedItems[index].Path] = append(addedByPath[addedItems[index].Path], index)
	}
	for index := range removedItems {
		removedByPath[removedItems[index].Path] = append(removedByPath[removedItems[index].Path], index)
	}
	pairedAdded, pairedRemoved := make([]bool, len(addedItems)), make([]bool, len(removedItems))
	for fullPath, oldIndexes := range removedByPath {
		newIndexes := addedByPath[fullPath]
		if len(oldIndexes) != 1 || len(newIndexes) != 1 {
			continue
		}
		oldCopy, newCopy := removedItems[oldIndexes[0]], addedItems[newIndexes[0]]
		changes = append(changes, CloudInventoryRefreshChangeSnapshot{Kind: "identity_changed", Old: &oldCopy, New: &newCopy})
		pairedAdded[newIndexes[0]], pairedRemoved[oldIndexes[0]] = true, true
	}
	for index, item := range addedItems {
		if pairedAdded[index] {
			continue
		}
		newCopy := item
		changes = append(changes, CloudInventoryRefreshChangeSnapshot{Kind: "added", New: &newCopy})
	}
	for index, item := range removedItems {
		if pairedRemoved[index] {
			continue
		}
		oldCopy := item
		changes = append(changes, CloudInventoryRefreshChangeSnapshot{Kind: "removed", Old: &oldCopy})
	}
	sort.Slice(changes, func(i, j int) bool {
		pathOf := func(item CloudInventoryRefreshChangeSnapshot) string {
			if item.New != nil {
				return item.New.Path
			}
			return item.Old.Path
		}
		left, right := pathOf(changes[i]), pathOf(changes[j])
		if left != right {
			return left < right
		}
		if changes[i].Kind != changes[j].Kind {
			return changes[i].Kind < changes[j].Kind
		}
		leftID, rightID := int64(0), int64(0)
		if changes[i].New != nil {
			leftID = changes[i].New.FsID
		} else if changes[i].Old != nil {
			leftID = changes[i].Old.FsID
		}
		if changes[j].New != nil {
			rightID = changes[j].New.FsID
		} else if changes[j].Old != nil {
			rightID = changes[j].Old.FsID
		}
		return leftID < rightID
	})
	return CloudInventoryRefreshDelta{AddedCount: &added, RemovedCount: &removed, ChangedCount: &modified, Changes: changes}, true, nil
}

// UpsertCloudInventoryPage commits one provider page atomically. A directory
// is marked complete, and unseen direct children are removed, only on its last
// page; descendants of removed directories are pruned in the same transaction.
func (s *Store) UpsertCloudInventoryPage(account, runID, directory string, entries []CloudInventoryEntry, observedAt time.Time, nextOffset *int) error {
	return s.upsertCloudInventoryPage(account, runID, directory, entries, observedAt, nextOffset, nil, nil, nil, false)
}

func rejectCloudInventoryScanPageOverlap(tx *sql.Tx, account, runID, directory string, entries []CloudInventoryEntry) error {
	pageIDs := make(map[int64]struct{}, len(entries))
	for _, entry := range entries {
		if _, exists := pageIDs[entry.FsID]; exists {
			return fmt.Errorf("duplicate fs_id within cloud inventory scan page: %d", entry.FsID)
		}
		pageIDs[entry.FsID] = struct{}{}
	}
	for start := 0; start < len(entries); start += cloudInventoryUpsertBatchSize {
		end := start + cloudInventoryUpsertBatchSize
		if end > len(entries) {
			end = len(entries)
		}
		var query strings.Builder
		query.WriteString(`SELECT fs_id FROM cloud_inventory WHERE account_fingerprint=? AND parent_path=? AND last_seen_run_id=? AND fs_id IN (`)
		args := make([]any, 0, 3+end-start)
		args = append(args, account, directory, runID)
		for index := start; index < end; index++ {
			if index != start {
				query.WriteByte(',')
			}
			query.WriteByte('?')
			args = append(args, entries[index].FsID)
		}
		query.WriteByte(')')
		rows, err := tx.Query(query.String(), args...)
		if err != nil {
			return err
		}
		if rows.Next() {
			var fsID int64
			if err = rows.Scan(&fsID); err != nil {
				_ = rows.Close()
				return err
			}
			_ = rows.Close()
			return &CloudInventoryDuplicateAcrossPagesError{FsID: fsID}
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func rejectCloudInventoryRefreshPageOverlap(tx *sql.Tx, account, runID, directory string, entries []CloudInventoryEntry) error {
	pageIDs := make(map[int64]struct{}, len(entries))
	for _, entry := range entries {
		if _, exists := pageIDs[entry.FsID]; exists {
			return fmt.Errorf("duplicate fs_id within cloud inventory refresh page: %d", entry.FsID)
		}
		pageIDs[entry.FsID] = struct{}{}
	}
	for start := 0; start < len(entries); start += cloudInventoryUpsertBatchSize {
		end := start + cloudInventoryUpsertBatchSize
		if end > len(entries) {
			end = len(entries)
		}
		var query strings.Builder
		query.WriteString(`SELECT fs_id FROM cloud_inventory_refresh_entries WHERE account_fingerprint=? AND run_id=? AND directory=? AND fs_id IN (`)
		args := make([]any, 0, 4+end-start)
		args = append(args, account, runID, directory)
		for index := start; index < end; index++ {
			if index != start {
				query.WriteByte(',')
			}
			query.WriteByte('?')
			args = append(args, entries[index].FsID)
		}
		query.WriteByte(')')
		rows, err := tx.Query(query.String(), args...)
		if err != nil {
			return err
		}
		if rows.Next() {
			var fsID int64
			if err = rows.Scan(&fsID); err != nil {
				_ = rows.Close()
				return err
			}
			_ = rows.Close()
			return &CloudInventoryDuplicateAcrossPagesError{FsID: fsID}
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) UpsertCloudInventoryScanPage(state *CloudScanState, directory string, entries []CloudInventoryEntry, observedAt time.Time, nextOffset *int, childDirectories []string) error {
	if state == nil || state.Status != "running" || state.CurrentDirectory != directory {
		return fmt.Errorf("invalid cloud scan page state")
	}
	if nextOffset != nil && state.NextPageBoundary == nil {
		return fmt.Errorf("cloud scan page boundary is missing")
	}
	return s.upsertCloudInventoryPage(state.AccountFingerprint, state.RunID, directory, entries, observedAt, nextOffset, state, nil, childDirectories, false)
}

func (s *Store) UpsertCloudInventoryRefreshPage(state *CloudInventoryRefreshState, directory string, entries []CloudInventoryEntry, observedAt time.Time, nextOffset *int, childDirectories []string) error {
	if state == nil || state.Status != "running" || state.CurrentDirectory != directory {
		return fmt.Errorf("invalid cloud inventory refresh page state")
	}
	if nextOffset != nil && state.NextPageBoundary == nil {
		return fmt.Errorf("cloud inventory refresh page boundary is missing")
	}
	return s.upsertCloudInventoryPage(state.AccountFingerprint, state.RunID, directory, entries, observedAt, nextOffset, nil, state, childDirectories, false)
}

func (s *Store) upsertCloudInventoryRefreshStagedPage(state *CloudInventoryRefreshState, directory string, entries []CloudInventoryEntry, observedAt time.Time, nextOffset *int) error {
	account, runID := state.AccountFingerprint, state.RunID
	boundaryJSON, err := marshalCloudInventoryPageBoundary(state.NextPageBoundary)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	var persistedRun, persistedDirectory string
	var persistedOffset int64
	if err = tx.QueryRow(`SELECT run_id,current_directory,next_offset FROM cloud_inventory_refresh_state WHERE account_fingerprint=?`, account).Scan(&persistedRun, &persistedDirectory, &persistedOffset); err != nil {
		_ = tx.Rollback()
		s.mu.Unlock()
		return err
	}
	if persistedRun != runID || persistedDirectory != directory || persistedOffset != state.NextOffset {
		_ = tx.Rollback()
		s.mu.Unlock()
		return fmt.Errorf("cloud inventory refresh checkpoint changed")
	}
	if state.NextOffset == 0 {
		if _, err = tx.Exec(`DELETE FROM cloud_inventory_refresh_entries WHERE account_fingerprint=? AND (run_id<>? OR (run_id=? AND directory=?))`, account, runID, runID, directory); err != nil {
			_ = tx.Rollback()
			s.mu.Unlock()
			return err
		}
	}
	if err = rejectCloudInventoryRefreshPageOverlap(tx, account, runID, directory, entries); err != nil {
		_ = tx.Rollback()
		s.mu.Unlock()
		return err
	}
	stage, err := tx.Prepare(`INSERT INTO cloud_inventory_refresh_entries(account_fingerprint,run_id,directory,fs_id,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc) VALUES(?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		s.mu.Unlock()
		return err
	}
	for _, item := range entries {
		isDir := 0
		if item.Directory {
			isDir = 1
		}
		if _, err = stage.Exec(account, runID, directory, item.FsID, item.Path, item.Name, isDir, item.Size, item.ProviderMtime, item.ContentMD5, observedAt.UTC().UnixMicro()); err != nil {
			_ = stage.Close()
			_ = tx.Rollback()
			s.mu.Unlock()
			return err
		}
	}
	if err = stage.Close(); err != nil {
		_ = tx.Rollback()
		s.mu.Unlock()
		return err
	}
	updated := *state
	updated.PagesProcessed++
	updated.ItemsProcessed += int64(len(entries))
	updated.UpdatedAtUTC = time.Now().UTC()
	updated.LastError = ""
	updated.RetryDelayMillis = 0
	updated.RetryBackoffBaseMillis = 0
	updated.Status, updated.Phase = "running", "listing"
	if nextOffset != nil {
		updated.NextOffset = int64(*nextOffset)
		if _, err = tx.Exec(`UPDATE cloud_inventory_refresh_state SET next_offset=?,pages_processed=?,items_processed=?,updated_at_utc=?,last_error='',status='running',phase='listing',retry_delay_millis=0,retry_backoff_base_millis=0,retry_at_utc=NULL,next_page_boundary_json=? WHERE account_fingerprint=? AND run_id=?`, updated.NextOffset, updated.PagesProcessed, updated.ItemsProcessed, updated.UpdatedAtUTC.UnixMicro(), boundaryJSON, account, runID); err != nil {
			_ = tx.Rollback()
			s.mu.Unlock()
			return err
		}
	}
	allEntries := make([]CloudInventoryEntry, 0)
	if nextOffset == nil {
		rows, queryErr := tx.Query(`SELECT fs_id,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc FROM cloud_inventory_refresh_entries WHERE account_fingerprint=? AND run_id=? AND directory=? ORDER BY fs_id`, account, runID, directory)
		if queryErr != nil {
			_ = tx.Rollback()
			s.mu.Unlock()
			return queryErr
		}
		for rows.Next() {
			var item CloudInventoryEntry
			var isDir int
			var pageObservedAt int64
			if queryErr = rows.Scan(&item.FsID, &item.Path, &item.Name, &isDir, &item.Size, &item.ProviderMtime, &item.ContentMD5, &pageObservedAt); queryErr != nil {
				_ = rows.Close()
				_ = tx.Rollback()
				s.mu.Unlock()
				return queryErr
			}
			item.Directory = isDir == 1
			if pageObservedAt > 0 {
				item.observedAtUTC = time.UnixMicro(pageObservedAt).UTC()
			}
			item.observedAtKnown = true
			allEntries = append(allEntries, item)
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			_ = tx.Rollback()
			s.mu.Unlock()
			return err
		}
		if err = rows.Close(); err != nil {
			_ = tx.Rollback()
			s.mu.Unlock()
			return err
		}
		updated.NextOffset = 0
		updated.NextPageBoundary = nil
	}
	if err = tx.Commit(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	if nextOffset != nil {
		*state = updated
		return nil
	}
	if err = s.upsertCloudInventoryPage(account, runID, directory, allEntries, observedAt, nil, nil, &updated, nil, true); err != nil {
		return err
	}
	*state = updated
	return nil
}

func (s *Store) upsertCloudInventoryPage(account, runID, directory string, entries []CloudInventoryEntry, observedAt time.Time, nextOffset *int, scanState *CloudScanState, refreshState *CloudInventoryRefreshState, childDirectories []string, stagedRefreshPublish bool) error {
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 || !validInventoryPath(directory) || observedAt.IsZero() || (nextOffset != nil && *nextOffset < 1) {
		return fmt.Errorf("invalid cloud inventory page")
	}
	for _, entry := range entries {
		if entry.FsID <= 0 || !validInventoryPath(entry.Path) || path.Dir(entry.Path) != directory || path.Base(entry.Path) != entry.Name || entry.Name == "" || entry.Size < 0 || len(entry.Name) > 1024 || len(entry.ContentMD5) > 256 {
			return fmt.Errorf("invalid cloud inventory entry")
		}
	}
	if scanState != nil && refreshState != nil {
		return fmt.Errorf("cloud inventory page cannot advance two scan states")
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
	if refreshState != nil {
		if refreshState.AccountFingerprint != account || refreshState.RunID != runID || refreshState.NextOffset < 0 || !validInventoryPath(refreshState.RootPath) || !cloudInventoryPathWithin(refreshState.RootPath, directory) {
			return fmt.Errorf("invalid cloud inventory refresh page identity")
		}
		for _, child := range childDirectories {
			if !validInventoryPath(child) || path.Dir(child) != directory || !cloudInventoryPathWithin(refreshState.RootPath, child) {
				return fmt.Errorf("invalid cloud inventory refresh child directory")
			}
		}
		if nextOffset != nil && len(childDirectories) != 0 {
			return fmt.Errorf("child directories can only be enqueued on the final page")
		}
	}
	if refreshState != nil && !stagedRefreshPublish {
		return s.upsertCloudInventoryRefreshStagedPage(refreshState, directory, entries, observedAt, nextOffset)
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
	directoryUnchanged := false
	refreshDelta := CloudInventoryRefreshDelta{}
	if refreshState != nil {
		var known bool
		refreshDelta, known, err = cloudInventoryDirectoryDelta(tx, account, directory, entries, observedAt)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if known {
			directoryUnchanged = *refreshDelta.AddedCount == 0 && *refreshDelta.RemovedCount == 0 && *refreshDelta.ChangedCount == 0
		}
	}
	if err = preserveMissingCloudInventoryMD5(tx, account, directory, entries); err != nil {
		_ = tx.Rollback()
		return err
	}
	if !directoryUnchanged {
		if err = bumpCloudInventoryGeneration(tx, account, time.Now().UTC()); err != nil {
			_ = tx.Rollback()
			return err
		}
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
		if err = rejectCloudInventoryScanPageOverlap(tx, account, runID, directory, entries); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if refreshState != nil {
		var persistedPending int64
		if err = tx.QueryRow(`SELECT pending_count FROM cloud_inventory_refresh_state WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&persistedPending); err != nil {
			_ = tx.Rollback()
			return err
		}
		if persistedPending != refreshState.PendingCount {
			_ = tx.Rollback()
			return fmt.Errorf("cloud inventory refresh pending count changed")
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
				if _, err = tx.Exec(`DELETE FROM cloud_content_hashes WHERE account_fingerprint=? AND fs_id=?`, account, item.id); err != nil {
					_ = tx.Rollback()
					return err
				}
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
		if refreshState != nil {
			if directoryUnchanged {
				// The published file/directory aggregates remain valid, but the
				// cloud observation time changed. Refresh cached ancestor evidence
				// without invalidating unrelated account-wide size aggregates.
				if _, err = tx.Exec(`UPDATE cloud_directory_size_cache
				SET observed_at_utc=(SELECT MIN(d.observed_at_utc) FROM cloud_inventory_directories d
					WHERE d.account_fingerprint=cloud_directory_size_cache.account_fingerprint AND d.status='complete'
					AND (d.path=cloud_directory_size_cache.path OR
						(d.path>=CASE WHEN cloud_directory_size_cache.path='/' THEN '/' ELSE cloud_directory_size_cache.path||'/' END
						AND d.path<CASE WHEN cloud_directory_size_cache.path='/' THEN '0' ELSE cloud_directory_size_cache.path||'0' END)))
				WHERE account_fingerprint=? AND (path='/' OR path=? OR
					(path<>'/' AND ? >= path||'/' AND ? < path||'0'))`, account, directory, directory, directory); err != nil {
					_ = tx.Rollback()
					return err
				}
			}
			if _, err = tx.Exec(`DELETE FROM cloud_inventory_refresh_entries WHERE account_fingerprint=? AND run_id=? AND directory=?`, account, runID, directory); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	var updatedScanState CloudScanState
	if scanState != nil {
		queuedChildren := childDirectories
		if nextOffset == nil {
			// A directory listing can span several pages. Read all children
			// persisted for this run so directories from earlier pages are not
			// omitted when the parent leaves the scan queue.
			rows, queryErr := tx.Query(`SELECT path FROM cloud_inventory WHERE account_fingerprint=? AND parent_path=? AND last_seen_run_id=? AND is_dir=1 ORDER BY path`, account, directory, runID)
			if queryErr != nil {
				_ = tx.Rollback()
				return queryErr
			}
			queuedChildren = make([]string, 0)
			for rows.Next() {
				var child string
				if queryErr = rows.Scan(&child); queryErr != nil {
					rows.Close()
					_ = tx.Rollback()
					return queryErr
				}
				queuedChildren = append(queuedChildren, child)
			}
			if queryErr = rows.Err(); queryErr != nil {
				rows.Close()
				_ = tx.Rollback()
				return queryErr
			}
			if queryErr = rows.Close(); queryErr != nil {
				_ = tx.Rollback()
				return queryErr
			}
		}
		updatedScanState = *scanState
		updatedScanState.LastError = ""
		updatedScanState.RetryDelayMillis = 0
		updatedScanState.RetryBackoffBaseMillis = 0
		updatedScanState.RetryAtUTC = nil
		updatedScanState.PagesProcessed++
		updatedScanState.ItemsProcessed += int64(len(entries))
		updatedScanState.UpdatedAtUTC = time.Now().UTC()
		updatedScanState.LastError = ""
		updatedScanState.Status = "running"
		updatedScanState.Phase = "listing"
		if nextOffset != nil {
			updatedScanState.NextOffset = int64(*nextOffset)
		} else {
			updatedScanState.NextPageBoundary = nil
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
			if len(queuedChildren) > 0 {
				scanQueueStmt, err = tx.Prepare(`INSERT OR IGNORE INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES(?,?,?,?)`)
				if err != nil {
					_ = tx.Rollback()
					return err
				}
			}
			for index, child := range queuedChildren {
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
				complete, completeErr := cloudInventorySubtreeComplete(tx, account, "/")
				if completeErr != nil {
					_ = tx.Rollback()
					return completeErr
				}
				if !complete {
					_ = tx.Rollback()
					return fmt.Errorf("cloud scan cannot complete while directory coverage is inconsistent")
				}
				updatedScanState.Status = "complete"
				updatedScanState.Phase = "complete"
				updatedScanState.CurrentDirectory = directory
			}
		}
		var result sql.Result
		boundaryJSON, boundaryErr := marshalCloudInventoryPageBoundary(updatedScanState.NextPageBoundary)
		if boundaryErr != nil {
			_ = tx.Rollback()
			return boundaryErr
		}
		var previousStatus, previousPhase string
		if err = tx.QueryRow(`SELECT status,phase FROM cloud_inventory_scan_state WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&previousStatus, &previousPhase); err != nil {
			_ = tx.Rollback()
			return err
		}
		result, err = tx.Exec(`UPDATE cloud_inventory_scan_state SET status=?,phase=?,pending_json='[]',current_directory=?,next_offset=?,pages_processed=?,items_processed=?,pending_count=?,updated_at_utc=?,last_error=?,retry_delay_millis=?,retry_backoff_base_millis=?,retry_at_utc=?,next_page_boundary_json=? WHERE account_fingerprint=? AND run_id=?`, updatedScanState.Status, updatedScanState.Phase, updatedScanState.CurrentDirectory, updatedScanState.NextOffset, updatedScanState.PagesProcessed, updatedScanState.ItemsProcessed, updatedScanState.PendingCount, updatedScanState.UpdatedAtUTC.UTC().UnixMicro(), updatedScanState.LastError, updatedScanState.RetryDelayMillis, updatedScanState.RetryBackoffBaseMillis, nullableTimePtrUnixMicro(updatedScanState.RetryAtUTC), boundaryJSON, account, runID)
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
		runExists, historyErr := updateCloudScanRun(tx, updatedScanState)
		if historyErr != nil {
			_ = tx.Rollback()
			return historyErr
		}
		if kind := cloudScanTransitionEvent(previousStatus, previousPhase, updatedScanState); runExists && kind != "" {
			if err = appendCloudScanEvent(tx, updatedScanState, kind, updatedScanState.UpdatedAtUTC); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	var updatedRefreshState CloudInventoryRefreshState
	if refreshState != nil {
		updatedRefreshState = *refreshState
		updatedRefreshState.LastError = ""
		updatedRefreshState.RetryDelayMillis = 0
		updatedRefreshState.RetryBackoffBaseMillis = 0
		updatedRefreshState.RetryAtUTC = nil
		if !stagedRefreshPublish {
			updatedRefreshState.PagesProcessed++
			updatedRefreshState.ItemsProcessed += int64(len(entries))
		}
		updatedRefreshState.UpdatedAtUTC = time.Now().UTC()
		updatedRefreshState.LastError = ""
		updatedRefreshState.Status = "running"
		updatedRefreshState.Phase = "listing"
		if nextOffset != nil {
			updatedRefreshState.NextOffset = int64(*nextOffset)
		} else {
			updatedRefreshState.NextPageBoundary = nil
			rows, queryErr := tx.Query(`SELECT path FROM cloud_inventory WHERE account_fingerprint=? AND parent_path=? AND last_seen_run_id=? AND is_dir=1 ORDER BY path`, account, directory, runID)
			if queryErr != nil {
				_ = tx.Rollback()
				return queryErr
			}
			var queuedChildren []string
			for rows.Next() {
				var child string
				if queryErr = rows.Scan(&child); queryErr != nil {
					rows.Close()
					_ = tx.Rollback()
					return queryErr
				}
				queuedChildren = append(queuedChildren, child)
			}
			if queryErr = rows.Err(); queryErr != nil {
				rows.Close()
				_ = tx.Rollback()
				return queryErr
			}
			if queryErr = rows.Close(); queryErr != nil {
				_ = tx.Rollback()
				return queryErr
			}
			if updatedRefreshState.PendingCount <= 0 {
				_ = tx.Rollback()
				return fmt.Errorf("cloud inventory refresh pending count is empty before queue head removal")
			}
			var headSequence int64
			var headPath string
			if err = tx.QueryRow(`SELECT sequence,path FROM cloud_inventory_refresh_queue WHERE account_fingerprint=? AND run_id=? ORDER BY sequence LIMIT 1`, account, runID).Scan(&headSequence, &headPath); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("cloud inventory refresh queue head missing: %w", err)
			}
			if headPath != directory {
				_ = tx.Rollback()
				return fmt.Errorf("cloud inventory refresh queue head changed")
			}
			if _, err = tx.Exec(`DELETE FROM cloud_inventory_refresh_queue WHERE account_fingerprint=? AND run_id=? AND sequence=?`, account, runID, headSequence); err != nil {
				_ = tx.Rollback()
				return err
			}
			var nextSequence int64
			if err = tx.QueryRow(`SELECT coalesce(max(sequence),-1)+1 FROM cloud_inventory_refresh_queue WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&nextSequence); err != nil {
				_ = tx.Rollback()
				return err
			}
			updatedRefreshState.PendingCount--
			var queueStmt *sql.Stmt
			if len(queuedChildren) > 0 {
				queueStmt, err = tx.Prepare(`INSERT OR IGNORE INTO cloud_inventory_refresh_queue(account_fingerprint,run_id,sequence,path) VALUES(?,?,?,?)`)
				if err != nil {
					_ = tx.Rollback()
					return err
				}
			}
			for index, child := range queuedChildren {
				inserted, insertErr := queueStmt.Exec(account, runID, nextSequence+int64(index), child)
				if insertErr != nil {
					_ = tx.Rollback()
					return insertErr
				}
				rowsAdded, rowsErr := inserted.RowsAffected()
				if rowsErr != nil {
					_ = tx.Rollback()
					return rowsErr
				}
				updatedRefreshState.PendingCount += rowsAdded
			}
			if queueStmt != nil {
				if err = queueStmt.Close(); err != nil {
					_ = tx.Rollback()
					return err
				}
			}
			updatedRefreshState.NextOffset = 0
			if updatedRefreshState.PendingCount > 0 {
				if err = tx.QueryRow(`SELECT path FROM cloud_inventory_refresh_queue WHERE account_fingerprint=? AND run_id=? ORDER BY sequence LIMIT 1`, account, runID).Scan(&updatedRefreshState.CurrentDirectory); err != nil {
					_ = tx.Rollback()
					return fmt.Errorf("cloud inventory refresh pending count exceeds queue rows: %w", err)
				}
			} else {
				var remaining string
				checkErr := tx.QueryRow(`SELECT path FROM cloud_inventory_refresh_queue WHERE account_fingerprint=? AND run_id=? ORDER BY sequence LIMIT 1`, account, runID).Scan(&remaining)
				if checkErr == nil {
					_ = tx.Rollback()
					return fmt.Errorf("cloud inventory refresh queue has rows after pending count reached zero")
				}
				if checkErr != sql.ErrNoRows {
					_ = tx.Rollback()
					return checkErr
				}
				updatedRefreshState.Status = "complete"
				updatedRefreshState.Phase = "complete"
				updatedRefreshState.CurrentDirectory = directory
			}
		}
		boundaryJSON, boundaryErr := marshalCloudInventoryPageBoundary(updatedRefreshState.NextPageBoundary)
		if boundaryErr != nil {
			_ = tx.Rollback()
			return boundaryErr
		}
		result, updateErr := tx.Exec(`UPDATE cloud_inventory_refresh_state SET status=?,phase=?,current_directory=?,next_offset=?,pages_processed=?,items_processed=?,pending_count=?,updated_at_utc=?,last_error=?,retry_delay_millis=?,retry_backoff_base_millis=?,retry_at_utc=?,next_page_boundary_json=? WHERE account_fingerprint=? AND run_id=?`, updatedRefreshState.Status, updatedRefreshState.Phase, updatedRefreshState.CurrentDirectory, updatedRefreshState.NextOffset, updatedRefreshState.PagesProcessed, updatedRefreshState.ItemsProcessed, updatedRefreshState.PendingCount, updatedRefreshState.UpdatedAtUTC.UTC().UnixMicro(), updatedRefreshState.LastError, updatedRefreshState.RetryDelayMillis, updatedRefreshState.RetryBackoffBaseMillis, nullableTimePtrUnixMicro(updatedRefreshState.RetryAtUTC), boundaryJSON, account, runID)
		if updateErr != nil {
			_ = tx.Rollback()
			return updateErr
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
			_ = tx.Rollback()
			if rowsErr != nil {
				return rowsErr
			}
			return fmt.Errorf("cloud inventory refresh state disappeared during page commit")
		}
		changed := !directoryUnchanged
		directoryEvent := refreshEventFromState(updatedRefreshState, "directory_published")
		directoryEvent.CurrentDirectory = directory
		directoryEvent.CloudObservedAtUTC = &observedAt
		directoryEvent.DirectoryChanged = &changed
		directoryEvent.AddedCount = refreshDelta.AddedCount
		directoryEvent.RemovedCount = refreshDelta.RemovedCount
		directoryEvent.ChangedCount = refreshDelta.ChangedCount
		if err = appendCloudInventoryRefreshEvent(tx, directoryEvent); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err = appendCloudInventoryRefreshChanges(tx, directoryEvent, refreshDelta.Changes); err != nil {
			_ = tx.Rollback()
			return err
		}
		if updatedRefreshState.Status == "complete" {
			if err = appendCloudInventoryRefreshEvent(tx, refreshEventFromState(updatedRefreshState, "completed")); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if scanState != nil {
		*scanState = updatedScanState
	}
	if refreshState != nil {
		*refreshState = updatedRefreshState
	}
	return nil
}

func (s *Store) CloudDirectorySize(account, directory string) (CloudDirectorySize, error) {
	if account == "" || len(account) > 128 || !validInventoryPath(directory) {
		return CloudDirectorySize{Path: directory}, fmt.Errorf("invalid cloud directory")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cloudDirectorySizeLocked(account, directory)
}

// cloudDirectorySizeLocked reads or refreshes the generation-scoped local size cache.
// The caller must hold s.mu so a listing can enrich its rows without releasing
// the store lock between reading entries and their aggregate sizes.
func (s *Store) cloudDirectorySizeLocked(account, directory string) (CloudDirectorySize, error) {
	result := CloudDirectorySize{Path: directory}
	if s.closed {
		return result, os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return result, err
	}
	result, err = cloudDirectorySizeInTx(tx, account, directory, time.Now().UTC())
	if err != nil {
		_ = tx.Rollback()
		return CloudDirectorySize{Path: directory}, err
	}
	if err = tx.Commit(); err != nil {
		return CloudDirectorySize{Path: directory}, err
	}
	return result, nil
}

func cloudDirectorySizeInTx(tx *sql.Tx, account, directory string, now time.Time) (CloudDirectorySize, error) {
	result := CloudDirectorySize{Path: directory}
	fail := func(err error) (CloudDirectorySize, error) { return CloudDirectorySize{Path: directory}, err }
	var err error
	if _, err = tx.Exec(`INSERT OR IGNORE INTO cloud_inventory_generations(account_fingerprint,generation,updated_at_utc) VALUES(?,0,?)`, account, now.UnixMicro()); err != nil {
		return fail(err)
	}
	var generation int64
	if err = tx.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint=?`, account).Scan(&generation); err != nil {
		return fail(err)
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
		return result, nil
	}
	if err != nil && !errorsIsNoRows(err) {
		return fail(err)
	}
	prefix, upper := cloudDirectoryPathRange(directory)
	var complete, directories, incomplete, missingCoverage int64
	var observed sql.NullInt64
	if err = tx.QueryRow(cloudInventoryDirectoryAggregateSQL, directory, directory, account, directory, prefix, upper, account, directory, prefix, upper, account, directory, prefix, upper, account, directory, prefix, upper, account, directory, prefix, upper).Scan(&complete, &directories, &incomplete, &observed, &missingCoverage); err != nil {
		return fail(err)
	}
	result.Complete = complete == 1 && incomplete == 0 && missingCoverage == 0
	if err = tx.QueryRow(cloudInventoryFileAggregateSQL, account, prefix, upper).Scan(&result.Bytes, &result.Files); err != nil {
		return fail(err)
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
		return fail(err)
	}
	return result, nil
}

func nullableUnixMicro(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().UnixMicro()
}

func nullableTimePtrUnixMicro(value *time.Time) any {
	if value == nil {
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

// CloudMutationScanConflict reports whether any queued scan path overlaps an
// affected directory subtree. A persisted paused scan queue also counts: it
// may be resumed later with the same path and offset cursor.
func (s *Store) CloudMutationScanConflict(account string, paths []string) (bool, error) {
	if account == "" || len(account) > 128 {
		return false, fmt.Errorf("invalid account fingerprint")
	}
	for _, p := range paths {
		if !validInventoryPath(p) {
			return false, fmt.Errorf("invalid cloud directory path")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, os.ErrClosed
	}
	return cloudMutationScanConflict(s.db, account, paths)
}

type cloudMutationQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func cloudMutationScanConflict(queryer cloudMutationQueryer, account string, paths []string) (bool, error) {
	var duplicateScan int
	if err := queryer.QueryRow(`SELECT EXISTS(SELECT 1 FROM cloud_duplicate_scan_state WHERE account_fingerprint=? AND status IN ('running','paused','failed') AND phase<>'restart_required')`, account).Scan(&duplicateScan); err != nil {
		return false, err
	}
	if duplicateScan != 0 {
		return true, nil
	}
	for _, p := range paths {
		prefix, upper := cloudDirectoryPathRange(p)
		var found int
		err := queryer.QueryRow("SELECT EXISTS(SELECT 1 FROM cloud_inventory_scan_queue WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))) OR EXISTS(SELECT 1 FROM cloud_inventory_refresh_queue WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?)))", account, p, prefix, upper, account, p, prefix, upper).Scan(&found)
		if err != nil {
			return false, err
		}
		if found != 0 {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) ApplyConfirmedCloudMutation(account string, mutation CloudInventoryMutation) (bool, error) {
	before, after := mutation.Before, mutation.After
	if account == "" || len(account) > 128 || mutation.ResponseObservedAtUTC.IsZero() {
		return false, fmt.Errorf("invalid confirmed cloud mutation")
	}
	var affectedParents []string
	var scanPaths []string
	switch mutation.Operation {
	case "mkdir":
		if before.FsID != 0 || before.Path != "" || !after.Directory || after.FsID <= 0 ||
			!validInventoryPath(after.Path) || after.Name == "" || path.Base(after.Path) != after.Name || after.Size < 0 {
			return false, fmt.Errorf("invalid confirmed cloud mkdir")
		}
		affectedParents = []string{path.Dir(after.Path)}
		scanPaths = append(append([]string(nil), affectedParents...), after.Path)
	case "rename", "move":
		if before.FsID <= 0 || before.Directory != after.Directory || !validInventoryPath(before.Path) ||
			before.Name == "" || before.Size < 0 || after.FsID != before.FsID || after.FsID <= 0 ||
			!validInventoryPath(after.Path) || after.Name == "" || path.Base(after.Path) != after.Name || after.Size < 0 || before.Path == "/" {
			return false, fmt.Errorf("invalid confirmed cloud rename or move")
		}
		affectedParents = []string{path.Dir(before.Path), path.Dir(after.Path)}
		scanPaths = append([]string(nil), affectedParents...)
		if before.Directory {
			scanPaths = append(scanPaths, before.Path, after.Path)
		}
	case "delete":
		if before.FsID <= 0 || !validInventoryPath(before.Path) || before.Name == "" || before.Size < 0 ||
			after.FsID != 0 || after.Path != "" {
			return false, fmt.Errorf("invalid confirmed cloud delete")
		}
		affectedParents = []string{path.Dir(before.Path)}
		scanPaths = append([]string(nil), affectedParents...)
		if before.Directory {
			scanPaths = append(scanPaths, before.Path)
		}
	default:
		return false, fmt.Errorf("invalid confirmed cloud mutation")
	}
	for _, parent := range affectedParents {
		if !validInventoryPath(parent) {
			return false, fmt.Errorf("invalid cloud mutation parent")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	rollback := func() {
		_ = tx.Rollback()
	}
	conflict, err := cloudMutationScanConflict(tx, account, scanPaths)
	if err != nil {
		rollback()
		return false, err
	}
	if conflict {
		rollback()
		return false, nil
	}
	reconcile := func() (bool, error) {
		if mutation.OperationID == "" {
			rollback()
			return false, nil
		}
		return reconcileConfirmedCloudMutation(tx, account, mutation, affectedParents)
	}
	for _, parent := range affectedParents {
		var status string
		if err = tx.QueryRow("SELECT status FROM cloud_inventory_directories WHERE account_fingerprint=? AND path=?", account, parent).Scan(&status); err != nil {
			if errorsIsNoRows(err) {
				return reconcile()
			}
			rollback()
			return false, err
		}
		if status != "complete" {
			return reconcile()
		}
	}
	now := mutation.ResponseObservedAtUTC.UTC().UnixMicro()
	if mutation.Operation == "mkdir" {
		var collision int
		if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM cloud_inventory WHERE account_fingerprint=? AND (fs_id=? OR path=?))", account, after.FsID, after.Path).Scan(&collision); err != nil {
			rollback()
			return false, err
		}
		if collision != 0 {
			return reconcile()
		}
		if _, err = tx.Exec("INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES(?,?,?,?,?,1,?,?,?,?,?)", account, after.FsID, path.Dir(after.Path), after.Path, after.Name, after.Size, after.ProviderMtime, after.ContentMD5, now, "confirmed-cloud-mutation"); err != nil {
			rollback()
			return false, err
		}
		if _, err = tx.Exec("INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES(?,?,'complete',?,?)", account, after.Path, now, "confirmed-cloud-mutation"); err != nil {
			rollback()
			return false, err
		}
		if _, err = tx.Exec("UPDATE cloud_inventory_directories SET observed_at_utc=? WHERE account_fingerprint=? AND path=? AND status='complete'", now, account, affectedParents[0]); err != nil {
			rollback()
			return false, err
		}
		if err = bumpCloudInventoryGeneration(tx, account, mutation.ResponseObservedAtUTC); err != nil {
			rollback()
			return false, err
		}
		if err = tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}
	if mutation.Operation == "delete" && before.Directory {
		complete, checkErr := cloudInventorySubtreeComplete(tx, account, before.Path)
		if checkErr != nil {
			rollback()
			return false, checkErr
		}
		if !complete {
			return reconcile()
		}
	}
	if (mutation.Operation == "rename" || mutation.Operation == "move") && before.Directory {
		complete, checkErr := cloudInventorySubtreeComplete(tx, account, before.Path)
		if checkErr != nil {
			rollback()
			return false, checkErr
		}
		if !complete {
			return reconcile()
		}
	}
	var currentPath, currentName, currentMD5 string
	var currentDirectory int
	var currentSize, currentMtime int64
	err = tx.QueryRow("SELECT path,name,is_dir,size_bytes,provider_mtime,content_md5 FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=?", account, before.FsID).Scan(&currentPath, &currentName, &currentDirectory, &currentSize, &currentMtime, &currentMD5)
	if err != nil {
		if errorsIsNoRows(err) {
			return reconcile()
		}
		rollback()
		return false, err
	}
	expectedDirectory := 0
	if before.Directory {
		expectedDirectory = 1
	}
	// Directory size and mtime can change when children are created, moved, or
	// deleted. The provider object was just revalidated before this cache update;
	// keep strict size/mtime/content checks for files, where they identify content.
	if currentPath != before.Path || currentName != before.Name || currentDirectory != expectedDirectory ||
		(!before.Directory && (currentSize != before.Size || currentMtime != before.ProviderMtime ||
			(before.ContentMD5 != "" && !strings.EqualFold(currentMD5, before.ContentMD5)))) {
		return reconcile()
	}
	if after.ContentMD5 == "" && currentSize == after.Size && currentMtime == after.ProviderMtime {
		// File metadata responses can omit MD5 on a path-only mutation; retain
		// the cached value only while size and mtime still identify that content.
		after.ContentMD5 = currentMD5
	}
	if (mutation.Operation == "rename" || mutation.Operation == "move") && before.Directory {
		applied, applyErr := rebaseCloudInventorySubtree(tx, account, before, after, mutation.ResponseObservedAtUTC)
		if applyErr != nil {
			rollback()
			return false, applyErr
		}
		if !applied {
			rollback()
			return false, nil
		}
		now := mutation.ResponseObservedAtUTC.UTC().UnixMicro()
		if err = updateCloudInventoryParentObservations(tx, account, affectedParents, now); err != nil {
			rollback()
			return false, err
		}
		if err = bumpCloudInventoryGeneration(tx, account, mutation.ResponseObservedAtUTC); err != nil {
			rollback()
			return false, err
		}
		if err = tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}
	if mutation.Operation == "delete" {
		if before.Directory {
			prefix, upper := cloudDirectoryPathRange(before.Path)
			if _, err = tx.Exec("DELETE FROM cloud_content_hashes WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))", account, before.Path, prefix, upper); err != nil {
				rollback()
				return false, err
			}
			if _, err = tx.Exec("DELETE FROM cloud_inventory WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))", account, before.Path, prefix, upper); err != nil {
				rollback()
				return false, err
			}
			if _, err = tx.Exec("DELETE FROM cloud_inventory_directories WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))", account, before.Path, prefix, upper); err != nil {
				rollback()
				return false, err
			}
		} else {
			if _, err = tx.Exec("DELETE FROM cloud_content_hashes WHERE account_fingerprint=? AND fs_id=?", account, before.FsID); err != nil {
				rollback()
				return false, err
			}
			if _, err = tx.Exec("DELETE FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=?", account, before.FsID); err != nil {
				rollback()
				return false, err
			}
		}
		now := mutation.ResponseObservedAtUTC.UTC().UnixMicro()
		if _, err = tx.Exec("UPDATE cloud_inventory_directories SET observed_at_utc=? WHERE account_fingerprint=? AND path=? AND status='complete'", now, account, affectedParents[0]); err != nil {
			rollback()
			return false, err
		}
		if err = bumpCloudInventoryGeneration(tx, account, mutation.ResponseObservedAtUTC); err != nil {
			rollback()
			return false, err
		}
		if err = tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}
	if after.Path != before.Path {
		var collision int
		if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM cloud_inventory WHERE account_fingerprint=? AND path=? AND fs_id<>?)", account, after.Path, before.FsID).Scan(&collision); err != nil {
			rollback()
			return false, err
		}
		if collision != 0 {
			return reconcile()
		}
	}
	if _, err = tx.Exec("UPDATE cloud_inventory SET parent_path=?,path=?,name=?,size_bytes=?,provider_mtime=?,content_md5=?,observed_at_utc=?,last_seen_run_id='confirmed-cloud-mutation' WHERE account_fingerprint=? AND fs_id=?", path.Dir(after.Path), after.Path, after.Name, after.Size, after.ProviderMtime, after.ContentMD5, now, account, before.FsID); err != nil {
		rollback()
		return false, err
	}
	if currentSize == after.Size && currentMtime == after.ProviderMtime && (after.ContentMD5 == "" || currentMD5 == "" || strings.EqualFold(currentMD5, after.ContentMD5)) {
		if _, err = tx.Exec("UPDATE cloud_content_hashes SET path=? WHERE account_fingerprint=? AND fs_id=?", after.Path, account, before.FsID); err != nil {
			rollback()
			return false, err
		}
	} else {
		if _, err = tx.Exec("DELETE FROM cloud_content_hashes WHERE account_fingerprint=? AND fs_id=?", account, before.FsID); err != nil {
			rollback()
			return false, err
		}
	}
	seenParents := make(map[string]struct{}, len(affectedParents))
	for _, parent := range affectedParents {
		if _, seen := seenParents[parent]; seen {
			continue
		}
		seenParents[parent] = struct{}{}
		if _, err = tx.Exec("UPDATE cloud_inventory_directories SET observed_at_utc=? WHERE account_fingerprint=? AND path=? AND status='complete'", now, account, parent); err != nil {
			rollback()
			return false, err
		}
	}
	if err = bumpCloudInventoryGeneration(tx, account, mutation.ResponseObservedAtUTC); err != nil {
		rollback()
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func reconcileConfirmedCloudMutation(tx *sql.Tx, account string, mutation CloudInventoryMutation, parents []string) (bool, error) {
	fail := func(err error) (bool, error) { _ = tx.Rollback(); return false, err }
	var operationStatus, operationKind, sourcePath, destinationPath string
	var objectID int64
	if err := tx.QueryRow(`SELECT status,operation_kind,object_id,source_path,destination_path FROM remote_operations WHERE operation_id=? AND account_fingerprint=?`, mutation.OperationID, account).Scan(&operationStatus, &operationKind, &objectID, &sourcePath, &destinationPath); err != nil {
		return fail(err)
	}
	wantObjectID, wantSourcePath, wantDestinationPath := mutation.Before.FsID, mutation.Before.Path, mutation.After.Path
	if mutation.Operation == "mkdir" {
		wantObjectID, wantSourcePath = 0, ""
	}
	if operationStatus != "confirmed" || operationKind != mutation.Operation || objectID != wantObjectID || sourcePath != wantSourcePath || destinationPath != wantDestinationPath {
		_ = tx.Rollback()
		return false, fmt.Errorf("cloud cache reconciliation evidence does not match a confirmed remote operation")
	}
	var cachedPath, cachedName, cachedMD5 string
	var cachedSize, cachedMtime int64
	var cachedDirectory int
	cacheErr := tx.QueryRow(`SELECT path,name,size_bytes,provider_mtime,content_md5,is_dir FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=?`, account, mutation.Before.FsID).Scan(&cachedPath, &cachedName, &cachedSize, &cachedMtime, &cachedMD5, &cachedDirectory)
	if cacheErr != nil && !errorsIsNoRows(cacheErr) {
		return fail(cacheErr)
	}
	hadCached := cacheErr == nil
	if hadCached {
		parents = append(parents, path.Dir(cachedPath))
	}
	if mutation.Operation == "delete" {
		if hadCached && cachedDirectory == 1 {
			if err := pruneCloudInventoryDescendants(tx, account, cachedPath, true); err != nil {
				return fail(err)
			}
			if _, err := tx.Exec(`DELETE FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=?`, account, mutation.Before.FsID); err != nil {
				return fail(err)
			}
			if _, err := tx.Exec(`DELETE FROM cloud_inventory_directories WHERE account_fingerprint=? AND path=?`, account, cachedPath); err != nil {
				return fail(err)
			}
		} else {
			if _, err := tx.Exec(`DELETE FROM cloud_content_hashes WHERE account_fingerprint=? AND fs_id=?`, account, mutation.Before.FsID); err != nil {
				return fail(err)
			}
			if _, err := tx.Exec(`DELETE FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=?`, account, mutation.Before.FsID); err != nil {
				return fail(err)
			}
		}
	} else {
		after := mutation.After
		if hadCached && cachedDirectory == 1 && cachedPath != after.Path {
			if err := pruneCloudInventoryDescendants(tx, account, cachedPath, true); err != nil {
				return fail(err)
			}
			if _, err := tx.Exec(`DELETE FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=?`, account, mutation.Before.FsID); err != nil {
				return fail(err)
			}
			if _, err := tx.Exec(`DELETE FROM cloud_inventory_directories WHERE account_fingerprint=? AND path=?`, account, cachedPath); err != nil {
				return fail(err)
			}
		}
		var conflicts []struct {
			fsID int64
			path string
			dir  int
		}
		rows, err := tx.Query(`SELECT fs_id,path,is_dir FROM cloud_inventory WHERE account_fingerprint=? AND path=? AND fs_id<>?`, account, after.Path, after.FsID)
		if err != nil {
			return fail(err)
		}
		for rows.Next() {
			var item struct {
				fsID int64
				path string
				dir  int
			}
			if err = rows.Scan(&item.fsID, &item.path, &item.dir); err != nil {
				_ = rows.Close()
				return fail(err)
			}
			conflicts = append(conflicts, item)
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return fail(err)
		}
		if err = rows.Close(); err != nil {
			return fail(err)
		}
		for _, conflict := range conflicts {
			if conflict.dir == 1 {
				if err = pruneCloudInventoryDescendants(tx, account, conflict.path, true); err != nil {
					return fail(err)
				}
			}
			if _, err = tx.Exec(`DELETE FROM cloud_content_hashes WHERE account_fingerprint=? AND fs_id=?`, account, conflict.fsID); err != nil {
				return fail(err)
			}
			if _, err = tx.Exec(`DELETE FROM cloud_inventory WHERE account_fingerprint=? AND fs_id=?`, account, conflict.fsID); err != nil {
				return fail(err)
			}
		}
		if hadCached && cachedDirectory == 0 && (cachedSize != after.Size || cachedMtime != after.ProviderMtime || (after.ContentMD5 != "" && cachedMD5 != "" && !strings.EqualFold(cachedMD5, after.ContentMD5))) {
			if _, err = tx.Exec(`DELETE FROM cloud_content_hashes WHERE account_fingerprint=? AND fs_id=?`, account, after.FsID); err != nil {
				return fail(err)
			}
		} else if hadCached && cachedDirectory == 0 && cachedPath != after.Path {
			if _, err = tx.Exec(`UPDATE cloud_content_hashes SET path=? WHERE account_fingerprint=? AND fs_id=?`, after.Path, account, after.FsID); err != nil {
				return fail(err)
			}
		}
		if after.ContentMD5 == "" && hadCached && cachedPath == after.Path && cachedSize == after.Size && cachedMtime == after.ProviderMtime {
			after.ContentMD5 = cachedMD5
		}
		if _, err = tx.Exec(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint,fs_id) DO UPDATE SET parent_path=excluded.parent_path,path=excluded.path,name=excluded.name,is_dir=excluded.is_dir,size_bytes=excluded.size_bytes,provider_mtime=excluded.provider_mtime,content_md5=excluded.content_md5,observed_at_utc=excluded.observed_at_utc,last_seen_run_id=excluded.last_seen_run_id`, account, after.FsID, path.Dir(after.Path), after.Path, after.Name, boolToSQLiteInt(after.Directory), after.Size, after.ProviderMtime, after.ContentMD5, mutation.ResponseObservedAtUTC.UTC().UnixMicro(), "confirmed-cloud-cache-repair"); err != nil {
			return fail(err)
		}
		if after.Directory {
			status := "stale"
			if mutation.Operation == "mkdir" {
				status = "complete"
			}
			if _, err = tx.Exec(`INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES(?,?,?,?,?) ON CONFLICT(account_fingerprint,path) DO UPDATE SET status=excluded.status,observed_at_utc=excluded.observed_at_utc,last_run_id=excluded.last_run_id`, account, after.Path, status, mutation.ResponseObservedAtUTC.UTC().UnixMicro(), "confirmed-cloud-cache-repair"); err != nil {
				return fail(err)
			}
		}
	}
	for _, parent := range parents {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO cloud_inventory_directories(account_fingerprint,path,status,last_run_id) VALUES(?,?,'stale','confirmed-cloud-cache-repair')`, account, parent); err != nil {
			return fail(err)
		}
		if _, err := tx.Exec(`UPDATE cloud_inventory_directories SET status='stale',observed_at_utc=NULL,last_run_id='confirmed-cloud-cache-repair' WHERE account_fingerprint=? AND path=?`, account, parent); err != nil {
			return fail(err)
		}
	}
	if err := bumpCloudInventoryGeneration(tx, account, mutation.ResponseObservedAtUTC); err != nil {
		return fail(err)
	}
	evidence, err := json.Marshal(map[string]any{"event": "cache_refresh_reconciled", "operation": mutation.Operation, "fs_id": mutation.Before.FsID, "cached_path_before": cachedPath, "cached_entry_found": hadCached, "provider_after": mutation.After, "recorded_at_utc": time.Now().UTC(), "response_observed_at_utc": mutation.ResponseObservedAtUTC.UTC()})
	if err != nil {
		return fail(err)
	}
	if _, err = tx.Exec(`INSERT INTO remote_operation_events(operation_id,status,recorded_at_utc,response_observed_at_utc,evidence_json) VALUES(?,'confirmed',?,?,?)`, mutation.OperationID, time.Now().UTC().UnixNano(), mutation.ResponseObservedAtUTC.UTC().UnixNano(), evidence); err != nil {
		return fail(err)
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func cloudInventorySubtreeComplete(tx *sql.Tx, account, root string) (bool, error) {
	prefix, upper := cloudDirectoryPathRange(root)
	var rootComplete, incomplete, missingCoverage int
	err := tx.QueryRow(`SELECT
		COALESCE(MAX(CASE WHEN path=? AND status='complete' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status<>'complete' THEN 1 ELSE 0 END),0),
		EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.is_dir=1 AND (e.path=? OR (e.path>=? AND e.path<?)) AND NOT EXISTS(SELECT 1 FROM cloud_inventory_directories d WHERE d.account_fingerprint=e.account_fingerprint AND d.path=e.path AND d.status='complete')) OR
		EXISTS(SELECT 1 FROM cloud_inventory_directories d WHERE d.account_fingerprint=? AND d.path<>'/' AND (d.path=? OR (d.path>=? AND d.path<?)) AND NOT EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=d.account_fingerprint AND e.path=d.path AND e.is_dir=1)) OR
		EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.parent_path<>'/' AND (e.path=? OR (e.path>=? AND e.path<?)) AND NOT EXISTS(SELECT 1 FROM cloud_inventory p JOIN cloud_inventory_directories d ON d.account_fingerprint=p.account_fingerprint AND d.path=p.path AND d.status='complete' WHERE p.account_fingerprint=e.account_fingerprint AND p.path=e.parent_path AND p.is_dir=1))
		OR EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=? AND (e.path=? OR (e.path>=? AND e.path<?)) AND `+cloudInventoryPathParentMismatchSQL+`)
		FROM cloud_inventory_directories WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))`,
		root, account, root, prefix, upper, account, root, prefix, upper, account, root, prefix, upper, account, root, prefix, upper, account, root, prefix, upper).Scan(&rootComplete, &incomplete, &missingCoverage)
	if err != nil {
		return false, err
	}
	return rootComplete == 1 && incomplete == 0 && missingCoverage == 0, nil
}

func updateCloudInventoryParentObservations(tx *sql.Tx, account string, parents []string, observedAt int64) error {
	seen := make(map[string]struct{}, len(parents))
	for _, parent := range parents {
		if _, ok := seen[parent]; ok {
			continue
		}
		seen[parent] = struct{}{}
		if _, err := tx.Exec("UPDATE cloud_inventory_directories SET observed_at_utc=? WHERE account_fingerprint=? AND path=? AND status='complete'", observedAt, account, parent); err != nil {
			return err
		}
	}
	return nil
}

type cloudInventoryRebaseEntry struct {
	entry         CloudInventoryEntry
	observedAt    int64
	lastSeenRunID string
}

type cloudInventoryRebaseDirectory struct {
	path       string
	status     string
	observedAt sql.NullInt64
	lastRunID  string
}

func rebaseCloudInventorySubtree(tx *sql.Tx, account string, before, after CloudInventoryEntry, observedAt time.Time) (bool, error) {
	if before.Path != after.Path && strings.HasPrefix(after.Path, before.Path+"/") {
		return false, nil
	}
	oldPrefix, oldUpper := cloudDirectoryPathRange(before.Path)
	newPrefix, newUpper := cloudDirectoryPathRange(after.Path)
	for _, query := range []string{
		"SELECT path FROM cloud_inventory WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))",
		"SELECT path FROM cloud_inventory_directories WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))",
	} {
		rows, err := tx.Query(query, account, after.Path, newPrefix, newUpper)
		if err != nil {
			return false, err
		}
		for rows.Next() {
			var candidate string
			if err = rows.Scan(&candidate); err != nil {
				rows.Close()
				return false, err
			}
			if candidate != before.Path && candidate != after.Path &&
				!(candidate >= oldPrefix && candidate < oldUpper) {
				rows.Close()
				return false, nil
			}
			if candidate == after.Path && after.Path != before.Path {
				rows.Close()
				return false, nil
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
	entries := make([]cloudInventoryRebaseEntry, 0)
	rows, err := tx.Query("SELECT fs_id,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id FROM cloud_inventory WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))", account, before.Path, oldPrefix, oldUpper)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var item cloudInventoryRebaseEntry
		var isDir int
		if err = rows.Scan(&item.entry.FsID, &item.entry.Path, &item.entry.Name, &isDir, &item.entry.Size, &item.entry.ProviderMtime, &item.entry.ContentMD5, &item.observedAt, &item.lastSeenRunID); err != nil {
			rows.Close()
			return false, err
		}
		item.entry.Directory = isDir == 1
		entries = append(entries, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	if err = rows.Close(); err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	directories := make([]cloudInventoryRebaseDirectory, 0)
	rows, err = tx.Query("SELECT path,status,observed_at_utc,last_run_id FROM cloud_inventory_directories WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))", account, before.Path, oldPrefix, oldUpper)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var item cloudInventoryRebaseDirectory
		if err = rows.Scan(&item.path, &item.status, &item.observedAt, &item.lastRunID); err != nil {
			rows.Close()
			return false, err
		}
		directories = append(directories, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	if err = rows.Close(); err != nil {
		return false, err
	}
	if len(directories) == 0 {
		return false, nil
	}
	observed := observedAt.UTC().UnixMicro()
	for _, item := range entries {
		oldPath := item.entry.Path
		newPath := after.Path + strings.TrimPrefix(oldPath, before.Path)
		newName := item.entry.Name
		size, mtime, md5 := item.entry.Size, item.entry.ProviderMtime, item.entry.ContentMD5
		entryObserved, lastSeen := item.observedAt, item.lastSeenRunID
		if oldPath == before.Path {
			newName, size, mtime, md5 = after.Name, after.Size, after.ProviderMtime, after.ContentMD5
			entryObserved, lastSeen = observed, "confirmed-cloud-mutation"
		}
		if _, err = tx.Exec("UPDATE cloud_inventory SET parent_path=?,path=?,name=?,size_bytes=?,provider_mtime=?,content_md5=?,observed_at_utc=?,last_seen_run_id=? WHERE account_fingerprint=? AND fs_id=?", path.Dir(newPath), newPath, newName, size, mtime, md5, entryObserved, lastSeen, account, item.entry.FsID); err != nil {
			return false, err
		}
		if _, err = tx.Exec("UPDATE cloud_content_hashes SET path=? WHERE account_fingerprint=? AND fs_id=?", newPath, account, item.entry.FsID); err != nil {
			return false, err
		}
	}
	if _, err = tx.Exec("DELETE FROM cloud_inventory_directories WHERE account_fingerprint=? AND (path=? OR (path>=? AND path<?))", account, before.Path, oldPrefix, oldUpper); err != nil {
		return false, err
	}
	for _, item := range directories {
		newPath := after.Path + strings.TrimPrefix(item.path, before.Path)
		observedAtValue, lastRunID := any(nil), item.lastRunID
		if item.path == before.Path {
			observedAtValue, lastRunID = observed, "confirmed-cloud-mutation"
		} else if item.observedAt.Valid {
			observedAtValue = item.observedAt.Int64
		}
		if _, err = tx.Exec("INSERT INTO cloud_inventory_directories(account_fingerprint,path,status,observed_at_utc,last_run_id) VALUES(?,?,?,?,?)", account, newPath, item.status, observedAtValue, lastRunID); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Store) StartCloudScan(state *CloudScanState) error {
	if state == nil || state.AccountFingerprint == "" || state.RunID == "" || len(state.AccountFingerprint) > 128 || len(state.RunID) > 128 || !validRemoteScanStatus(state.Status) || state.StartedAtUTC.IsZero() || state.UpdatedAtUTC.IsZero() || !validInventoryPath(state.CurrentDirectory) || state.RequestBudget < 0 || state.RequestBudget > CloudInventoryRefreshRequestBudgetLimit || state.RequestsUsed != 0 {
		return fmt.Errorf("invalid cloud scan state")
	}
	boundaryJSON, err := marshalCloudInventoryPageBoundary(state.NextPageBoundary)
	if err != nil {
		return fmt.Errorf("invalid cloud scan page boundary: %w", err)
	}
	if state.NextOffset > 0 && state.NextPageBoundary == nil {
		return fmt.Errorf("cloud scan offset has no page boundary")
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
		_, err = tx.Exec(`INSERT INTO cloud_inventory_scan_state(account_fingerprint,run_id,status,phase,pending_json,current_directory,next_offset,pages_processed,items_processed,pending_count,started_at_utc,updated_at_utc,last_error,next_page_boundary_json,request_budget,requests_used) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint) DO UPDATE SET run_id=excluded.run_id,status=excluded.status,phase=excluded.phase,pending_json='[]',current_directory=excluded.current_directory,next_offset=excluded.next_offset,pages_processed=excluded.pages_processed,items_processed=excluded.items_processed,pending_count=excluded.pending_count,started_at_utc=excluded.started_at_utc,updated_at_utc=excluded.updated_at_utc,last_error=excluded.last_error,next_page_boundary_json=excluded.next_page_boundary_json,request_budget=excluded.request_budget,requests_used=excluded.requests_used`, state.AccountFingerprint, state.RunID, state.Status, state.Phase, []byte("[]"), state.CurrentDirectory, state.NextOffset, state.PagesProcessed, state.ItemsProcessed, 1, state.StartedAtUTC.UTC().UnixMicro(), state.UpdatedAtUTC.UTC().UnixMicro(), state.LastError, boundaryJSON, state.RequestBudget, state.RequestsUsed)
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	state.PendingCount = 1
	if err = createCloudScanHistory(tx, *state); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Store) SaveCloudScanState(account string, runID, status, phase, current string, offset, pages, items int64, started, updated time.Time, lastError string) error {
	return s.SaveCloudScanCheckpoint(CloudScanState{AccountFingerprint: account, RunID: runID, Status: status, Phase: phase, CurrentDirectory: current, NextOffset: offset, PagesProcessed: pages, ItemsProcessed: items, StartedAtUTC: started, UpdatedAtUTC: updated, LastError: lastError})
}

func (s *Store) SaveCloudScanCheckpoint(state CloudScanState) error {
	if state.AccountFingerprint == "" || state.RunID == "" || len(state.AccountFingerprint) > 128 || len(state.RunID) > 128 || !validRemoteScanStatus(state.Status) || !validInventoryPath(state.CurrentDirectory) || state.NextOffset < 0 || state.PagesProcessed < 0 || state.ItemsProcessed < 0 || state.RequestBudget < 0 || state.RequestsUsed < 0 || (state.RequestBudget > 0 && state.RequestsUsed > state.RequestBudget) || state.StartedAtUTC.IsZero() || state.UpdatedAtUTC.IsZero() || state.RetryDelayMillis < 0 || state.RetryDelayMillis > 3_600_000 || state.RetryBackoffBaseMillis < 0 || state.RetryBackoffBaseMillis > 3_600_000 || (state.RetryAtUTC != nil && state.RetryAtUTC.IsZero()) {
		return fmt.Errorf("invalid cloud scan state")
	}
	boundaryJSON, err := marshalCloudInventoryPageBoundary(state.NextPageBoundary)
	if err != nil {
		return err
	}
	if state.NextOffset > 0 && state.NextPageBoundary == nil {
		return fmt.Errorf("cloud scan offset has no page boundary")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	var retryAt any
	if state.RetryAtUTC != nil {
		retryAt = state.RetryAtUTC.UTC().UnixMicro()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	var oldRunID, oldDirectory, oldStatus, oldPhase string
	var oldOffset, oldBudget int64
	lookupErr := tx.QueryRow(`SELECT run_id,current_directory,next_offset,status,phase,request_budget FROM cloud_inventory_scan_state WHERE account_fingerprint=?`, state.AccountFingerprint).Scan(&oldRunID, &oldDirectory, &oldOffset, &oldStatus, &oldPhase, &oldBudget)
	if lookupErr != nil && lookupErr != sql.ErrNoRows {
		_ = tx.Rollback()
		return lookupErr
	}
	if lookupErr == nil && oldRunID == state.RunID && oldDirectory == state.CurrentDirectory && oldOffset > 0 && state.NextOffset == 0 && state.NextPageBoundary == nil {
		if _, err = tx.Exec(`UPDATE cloud_inventory SET last_seen_run_id='' WHERE account_fingerprint=? AND parent_path=? AND last_seen_run_id=?`, state.AccountFingerprint, state.CurrentDirectory, state.RunID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	_, err = tx.Exec(`INSERT INTO cloud_inventory_scan_state(account_fingerprint,run_id,status,phase,pending_json,current_directory,next_offset,pages_processed,items_processed,started_at_utc,updated_at_utc,last_error,retry_delay_millis,retry_backoff_base_millis,retry_at_utc,next_page_boundary_json,request_budget,requests_used) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint) DO UPDATE SET run_id=excluded.run_id,status=excluded.status,phase=excluded.phase,pending_json='[]',current_directory=excluded.current_directory,next_offset=excluded.next_offset,pages_processed=excluded.pages_processed,items_processed=excluded.items_processed,started_at_utc=excluded.started_at_utc,updated_at_utc=excluded.updated_at_utc,last_error=excluded.last_error,retry_delay_millis=excluded.retry_delay_millis,retry_backoff_base_millis=excluded.retry_backoff_base_millis,retry_at_utc=excluded.retry_at_utc,next_page_boundary_json=excluded.next_page_boundary_json,request_budget=excluded.request_budget,requests_used=excluded.requests_used`, state.AccountFingerprint, state.RunID, state.Status, state.Phase, []byte("[]"), state.CurrentDirectory, state.NextOffset, state.PagesProcessed, state.ItemsProcessed, state.StartedAtUTC.UTC().UnixMicro(), state.UpdatedAtUTC.UTC().UnixMicro(), state.LastError, state.RetryDelayMillis, state.RetryBackoffBaseMillis, retryAt, boundaryJSON, state.RequestBudget, state.RequestsUsed)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if state.Status == "failed" {
		if _, err = tx.Exec(`UPDATE cloud_inventory_directories SET status='failed',observed_at_utc=NULL WHERE account_fingerprint=? AND path=? AND status='scanning' AND last_run_id=?`, state.AccountFingerprint, state.CurrentDirectory, state.RunID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if oldRunID == state.RunID {
		inserted, ensureErr := ensureCloudScanRun(tx, state)
		if ensureErr != nil {
			_ = tx.Rollback()
			return ensureErr
		}
		if _, err = updateCloudScanRun(tx, state); err != nil {
			_ = tx.Rollback()
			return err
		}
		kind := cloudScanTransitionEvent(oldStatus, oldPhase, state)
		// A resumed worker can retain running/backoff after a restart. Its explicit
		// limit change still needs evidence in the same transaction as the new cap.
		if kind == "" && oldBudget != state.RequestBudget {
			kind = "resumed"
		}
		if inserted && state.Status == "running" {
			kind = "resumed"
		}
		if kind == "completed" {
			observedAt := state.UpdatedAtUTC.UTC()
			coverage, coverageErr := cloudInventoryCoverageInTx(tx, state.AccountFingerprint, state.Status, &observedAt)
			if coverageErr != nil {
				_ = tx.Rollback()
				return coverageErr
			}
			reasonsJSON, marshalErr := json.Marshal(coverage.Reasons)
			if marshalErr != nil {
				_ = tx.Rollback()
				return marshalErr
			}
			if _, err = tx.Exec(`UPDATE cloud_scan_runs SET coverage_status=?,coverage_incomplete_directories=?,coverage_generation=?,coverage_observed_at_utc=?,coverage_reasons_json=? WHERE account_fingerprint=? AND run_id=?`, coverage.Status, coverage.IncompleteDirectories, coverage.Generation, observedAt.UnixMicro(), string(reasonsJSON), state.AccountFingerprint, state.RunID); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if kind != "" {
			if err = appendCloudScanEvent(tx, state, kind, state.UpdatedAtUTC); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

// RestartCloudScanBoundary atomically resets the current directory and records
// the restart evidence before the worker sends its next provider request.
func (s *Store) RestartCloudScanBoundary(state CloudScanState, at time.Time, restartReason ...string) (CloudScanState, error) {
	reason := "page_boundary_mismatch"
	if len(restartReason) > 0 {
		reason = restartReason[0]
	}
	if len(restartReason) > 1 || (reason != "page_boundary_mismatch" && reason != "provider_duplicate_page_id") {
		return state, fmt.Errorf("invalid cloud scan restart reason")
	}
	if state.Status != "running" || state.NextOffset <= 0 || state.NextPageBoundary == nil || at.IsZero() || !validInventoryPath(state.CurrentDirectory) {
		return state, fmt.Errorf("invalid cloud scan boundary restart")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return state, os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return state, err
	}
	defer tx.Rollback()
	var runID, directory, status, boundaryJSON string
	var offset int64
	if err = tx.QueryRow(`SELECT run_id,current_directory,status,next_offset,next_page_boundary_json FROM cloud_inventory_scan_state WHERE account_fingerprint=?`, state.AccountFingerprint).Scan(&runID, &directory, &status, &offset, &boundaryJSON); err != nil {
		return state, err
	}
	wantBoundary, _ := marshalCloudInventoryPageBoundary(state.NextPageBoundary)
	if runID != state.RunID || directory != state.CurrentDirectory || status != "running" || offset != state.NextOffset || boundaryJSON != wantBoundary {
		return state, fmt.Errorf("cloud scan boundary checkpoint changed before restart")
	}
	if err = tx.QueryRow(`SELECT count(*) FROM cloud_inventory_scan_events WHERE account_fingerprint=? AND run_id=? AND current_directory=? AND event_kind='boundary_restart'`, state.AccountFingerprint, state.RunID, state.CurrentDirectory).Scan(&state.BoundaryRestartCount); err != nil {
		return state, err
	}
	if state.BoundaryRestartCount >= CloudInventoryBoundaryRestartLimit {
		return state, ErrCloudInventoryBoundaryRestartLimitReached
	}
	state.BoundaryRestartCount++
	state.NextOffset, state.NextPageBoundary, state.UpdatedAtUTC = 0, nil, at.UTC()
	state.LastError, state.RetryDelayMillis, state.RetryBackoffBaseMillis, state.RetryAtUTC = "", 0, 0, nil
	if _, err = tx.Exec(`UPDATE cloud_inventory SET last_seen_run_id='' WHERE account_fingerprint=? AND parent_path=? AND last_seen_run_id=?`, state.AccountFingerprint, state.CurrentDirectory, state.RunID); err != nil {
		return state, err
	}
	result, err := tx.Exec(`UPDATE cloud_inventory_scan_state SET next_offset=0,next_page_boundary_json='',updated_at_utc=? WHERE account_fingerprint=? AND run_id=? AND current_directory=? AND status='running' AND next_offset=?`, at.UTC().UnixMicro(), state.AccountFingerprint, state.RunID, state.CurrentDirectory, offset)
	if err != nil {
		return state, err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
		if rowsErr != nil {
			return state, rowsErr
		}
		return state, fmt.Errorf("cloud scan boundary checkpoint changed concurrently")
	}
	if updated, updateErr := updateCloudScanRun(tx, state); updateErr != nil {
		return state, updateErr
	} else if !updated {
		return state, fmt.Errorf("cloud scan history run missing during boundary restart")
	}
	if err = appendCloudScanEvent(tx, state, "boundary_restart", at, reason); err != nil {
		return state, err
	}
	if err = tx.Commit(); err != nil {
		return state, err
	}
	return state, nil
}

// ReserveCloudScanRequest durably consumes one provider list request slot before GET.
func (s *Store) ReserveCloudScanRequest(account, runID string, reservedAt time.Time) (int64, error) {
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 || reservedAt.IsZero() {
		return 0, fmt.Errorf("invalid cloud scan request reservation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	var status string
	var budget, used, pending int64
	if err = tx.QueryRow(`SELECT status,request_budget,requests_used,pending_count FROM cloud_inventory_scan_state WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&status, &budget, &used, &pending); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if status != "running" || pending <= 0 {
		_ = tx.Rollback()
		return 0, fmt.Errorf("cloud scan request is not active")
	}
	if budget > 0 && used >= budget {
		_ = tx.Rollback()
		return used, ErrCloudInventoryScanRequestBudgetReached
	}
	if used == int64(^uint64(0)>>1) {
		_ = tx.Rollback()
		return 0, fmt.Errorf("cloud scan request count overflow")
	}
	used++
	result, err := tx.Exec(`UPDATE cloud_inventory_scan_state SET requests_used=?,updated_at_utc=? WHERE account_fingerprint=? AND run_id=? AND status='running' AND requests_used=? AND request_budget=?`, used, reservedAt.UTC().UnixMicro(), account, runID, used-1, budget)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		_ = tx.Rollback()
		if e != nil {
			return 0, e
		}
		return 0, fmt.Errorf("cloud scan request reservation changed concurrently")
	}
	if _, err = tx.Exec(`UPDATE cloud_scan_runs SET requests_used=?,updated_at_utc=? WHERE account_fingerprint=? AND run_id=?`, used, reservedAt.UTC().UnixMicro(), account, runID); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return used, nil
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
	var retryAt sql.NullInt64
	var boundaryJSON string
	err := s.db.QueryRow(loadCloudScanStateSQL, account).Scan(&state.AccountFingerprint, &state.RunID, &state.Status, &state.Phase, &state.CurrentDirectory, &state.NextOffset, &state.PagesProcessed, &state.ItemsProcessed, &state.PendingCount, &started, &updated, &state.LastError, &state.RetryDelayMillis, &state.RetryBackoffBaseMillis, &retryAt, &boundaryJSON, &state.RequestBudget, &state.RequestsUsed, &state.BoundaryRestartCount)
	if err != nil {
		return state, err
	}
	state.StartedAtUTC = time.UnixMicro(started).UTC()
	state.UpdatedAtUTC = time.UnixMicro(updated).UTC()
	if retryAt.Valid {
		retry := time.UnixMicro(retryAt.Int64).UTC()
		state.RetryAtUTC = &retry
	}
	state.NextPageBoundary, err = unmarshalCloudInventoryPageBoundary(boundaryJSON)
	if err != nil {
		return state, err
	}
	return state, nil
}

// LoadCloudInventoryScanOverview keeps the persisted run checkpoint and the
// current cache coverage in one SQLite snapshot; a completed run may coexist
// with directories invalidated by later confirmed mutations.
func (s *Store) LoadCloudInventoryScanOverview(account string) (CloudScanState, CloudInventorySearchCoverage, error) {
	state := CloudScanState{}
	coverage := CloudInventorySearchCoverage{Status: "unknown"}
	if account == "" || len(account) > 128 {
		return state, coverage, fmt.Errorf("invalid account fingerprint")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return state, coverage, os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return state, coverage, err
	}
	defer tx.Rollback()
	var started, updated int64
	var retryAt sql.NullInt64
	var boundaryJSON string
	err = tx.QueryRow(loadCloudScanStateSQL, account).Scan(&state.AccountFingerprint, &state.RunID, &state.Status, &state.Phase, &state.CurrentDirectory, &state.NextOffset, &state.PagesProcessed, &state.ItemsProcessed, &state.PendingCount, &started, &updated, &state.LastError, &state.RetryDelayMillis, &state.RetryBackoffBaseMillis, &retryAt, &boundaryJSON, &state.RequestBudget, &state.RequestsUsed, &state.BoundaryRestartCount)
	if err != nil && !errorsIsNoRows(err) {
		return state, coverage, err
	}
	if err == nil {
		state.StartedAtUTC = time.UnixMicro(started).UTC()
		state.UpdatedAtUTC = time.UnixMicro(updated).UTC()
		if retryAt.Valid {
			retry := time.UnixMicro(retryAt.Int64).UTC()
			state.RetryAtUTC = &retry
		}
		state.NextPageBoundary, err = unmarshalCloudInventoryPageBoundary(boundaryJSON)
		if err != nil {
			return state, coverage, err
		}
		updatedAt := state.UpdatedAtUTC
		coverage.ScanUpdatedAtUTC = &updatedAt
	}
	var updatedAt *time.Time
	if coverage.ScanUpdatedAtUTC != nil {
		updatedAt = coverage.ScanUpdatedAtUTC
	}
	coverage, err = cloudInventoryCoverageInTx(tx, account, state.Status, updatedAt)
	if err != nil {
		return state, coverage, err
	}
	if err = tx.Commit(); err != nil {
		return state, coverage, err
	}
	return state, coverage, nil
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

func (s *Store) StartCloudInventoryRefresh(state *CloudInventoryRefreshState) error {
	if state == nil || state.AccountFingerprint == "" || len(state.AccountFingerprint) > 128 || state.RunID == "" || len(state.RunID) > 128 || !validInventoryPath(state.RootPath) || state.CurrentDirectory != state.RootPath || !validRemoteScanStatus(state.Status) || state.StartedAtUTC.IsZero() || state.UpdatedAtUTC.IsZero() || state.RequestBudget < 0 || state.RequestBudget > CloudInventoryRefreshRequestBudgetLimit || state.RequestsUsed != 0 || state.RetryDelayMillis < 0 || state.RetryDelayMillis > 3_600_000 || state.RetryBackoffBaseMillis < 0 || state.RetryBackoffBaseMillis > 3_600_000 || (state.RetryAtUTC != nil && state.RetryAtUTC.IsZero()) {
		return fmt.Errorf("invalid cloud inventory refresh state")
	}
	boundaryJSON, err := marshalCloudInventoryPageBoundary(state.NextPageBoundary)
	if err != nil {
		return err
	}
	if state.NextOffset > 0 && state.NextPageBoundary == nil {
		return fmt.Errorf("cloud inventory refresh offset has no page boundary")
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
	rollback := func(err error) error { _ = tx.Rollback(); return err }
	var status string
	var pending int64
	err = tx.QueryRow(`SELECT status,pending_count FROM cloud_inventory_scan_state WHERE account_fingerprint=?`, state.AccountFingerprint).Scan(&status, &pending)
	if err == nil && (status == "running" || status == "paused") && pending > 0 {
		return rollback(fmt.Errorf("全盘扫描仍有待处理目录，请先完成或恢复全盘扫描"))
	}
	if err != nil && !errorsIsNoRows(err) {
		return rollback(err)
	}
	var oldRunID, oldStatus string
	var oldPending int64
	err = tx.QueryRow(`SELECT run_id,status,pending_count FROM cloud_inventory_refresh_state WHERE account_fingerprint=?`, state.AccountFingerprint).Scan(&oldRunID, &oldStatus, &oldPending)
	if err == nil && (oldStatus == "running" || oldStatus == "paused") && oldPending > 0 {
		return rollback(fmt.Errorf("已有目录刷新待恢复；请先恢复原目录刷新"))
	}
	if err != nil && !errorsIsNoRows(err) {
		return rollback(err)
	}
	if _, err = tx.Exec(`DELETE FROM cloud_inventory_refresh_queue WHERE account_fingerprint=?`, state.AccountFingerprint); err != nil {
		return rollback(err)
	}
	if _, err = tx.Exec(`INSERT INTO cloud_inventory_refresh_queue(account_fingerprint,run_id,sequence,path) VALUES(?,?,0,?)`, state.AccountFingerprint, state.RunID, state.RootPath); err != nil {
		return rollback(err)
	}
	var retryAt any
	if state.RetryAtUTC != nil {
		retryAt = state.RetryAtUTC.UTC().UnixMicro()
	}
	_, err = tx.Exec(`INSERT INTO cloud_inventory_refresh_state(account_fingerprint,run_id,root_path,status,phase,current_directory,next_offset,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error,retry_delay_millis,retry_backoff_base_millis,retry_at_utc,next_page_boundary_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(account_fingerprint) DO UPDATE SET run_id=excluded.run_id,root_path=excluded.root_path,status=excluded.status,phase=excluded.phase,current_directory=excluded.current_directory,next_offset=excluded.next_offset,pages_processed=excluded.pages_processed,items_processed=excluded.items_processed,request_budget=excluded.request_budget,requests_used=excluded.requests_used,pending_count=excluded.pending_count,started_at_utc=excluded.started_at_utc,updated_at_utc=excluded.updated_at_utc,last_error=excluded.last_error,retry_delay_millis=excluded.retry_delay_millis,retry_backoff_base_millis=excluded.retry_backoff_base_millis,retry_at_utc=excluded.retry_at_utc,next_page_boundary_json=excluded.next_page_boundary_json`, state.AccountFingerprint, state.RunID, state.RootPath, state.Status, state.Phase, state.CurrentDirectory, state.NextOffset, state.PagesProcessed, state.ItemsProcessed, state.RequestBudget, state.RequestsUsed, 1, state.StartedAtUTC.UTC().UnixMicro(), state.UpdatedAtUTC.UTC().UnixMicro(), state.LastError, state.RetryDelayMillis, state.RetryBackoffBaseMillis, retryAt, boundaryJSON)
	if err != nil {
		return rollback(err)
	}
	startedEvent := *state
	startedEvent.PendingCount = 1
	if err = appendCloudInventoryRefreshEvent(tx, refreshEventFromState(startedEvent, "started")); err != nil {
		return rollback(err)
	}
	if err = syncCloudInventoryRefreshRun(tx, startedEvent); err != nil {
		return rollback(err)
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	state.PendingCount = 1
	return nil
}

func (s *Store) SaveCloudInventoryRefreshState(state CloudInventoryRefreshState) error {
	return s.saveCloudInventoryRefreshState(state, false)
}

// ResumeCloudInventoryRefreshState records an explicit reactivation of a
// persisted checkpoint, including one that still said running after a crash.
func (s *Store) ResumeCloudInventoryRefreshState(state CloudInventoryRefreshState) error {
	if state.Status != "running" {
		return fmt.Errorf("invalid resumed cloud inventory refresh state")
	}
	return s.saveCloudInventoryRefreshState(state, true)
}

// ReserveCloudInventoryRefreshRequest durably consumes one list request slot
// before the caller sends the provider GET. A crash may waste a slot, never reuse it.
func (s *Store) ReserveCloudInventoryRefreshRequest(account, runID string, reservedAt time.Time) (int64, error) {
	if account == "" || len(account) > 128 || runID == "" || len(runID) > 128 || reservedAt.IsZero() {
		return 0, fmt.Errorf("invalid cloud inventory refresh request reservation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	var status string
	var budget, used, pending int64
	if err = tx.QueryRow(`SELECT status,request_budget,requests_used,pending_count FROM cloud_inventory_refresh_state WHERE account_fingerprint=? AND run_id=?`, account, runID).Scan(&status, &budget, &used, &pending); err != nil {
		_ = tx.Rollback()
		if errorsIsNoRows(err) {
			return 0, ErrCloudInventoryRefreshNotFound
		}
		return 0, err
	}
	if status != "running" || pending <= 0 {
		_ = tx.Rollback()
		return 0, fmt.Errorf("cloud inventory refresh request is not active")
	}
	if budget > 0 && used >= budget {
		_ = tx.Rollback()
		return used, ErrCloudInventoryRefreshRequestBudgetReached
	}
	if used == int64(^uint64(0)>>1) {
		_ = tx.Rollback()
		return 0, fmt.Errorf("cloud inventory refresh request count overflow")
	}
	used++
	result, err := tx.Exec(`UPDATE cloud_inventory_refresh_state SET requests_used=?,updated_at_utc=? WHERE account_fingerprint=? AND run_id=? AND status='running' AND requests_used=? AND request_budget=?`, used, reservedAt.UTC().UnixMicro(), account, runID, used-1, budget)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
		_ = tx.Rollback()
		if rowsErr != nil {
			return 0, rowsErr
		}
		return 0, fmt.Errorf("cloud inventory refresh request reservation changed concurrently")
	}
	if _, err = tx.Exec(`UPDATE cloud_inventory_refresh_runs SET requests_used=?,updated_at_utc=? WHERE account_fingerprint=? AND run_id=?`, used, reservedAt.UTC().UnixMicro(), account, runID); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return used, nil
}

func (s *Store) saveCloudInventoryRefreshState(state CloudInventoryRefreshState, resumed bool) error {
	if state.AccountFingerprint == "" || len(state.AccountFingerprint) > 128 || state.RunID == "" || len(state.RunID) > 128 || !validInventoryPath(state.RootPath) || !validInventoryPath(state.CurrentDirectory) || !cloudInventoryPathWithin(state.RootPath, state.CurrentDirectory) || !validRemoteScanStatus(state.Status) || state.NextOffset < 0 || state.PagesProcessed < 0 || state.ItemsProcessed < 0 || state.RequestBudget < 0 || state.RequestsUsed < 0 || (state.RequestBudget > 0 && state.RequestsUsed > state.RequestBudget) || state.PendingCount < 0 || state.StartedAtUTC.IsZero() || state.UpdatedAtUTC.IsZero() || state.RetryDelayMillis < 0 || state.RetryDelayMillis > 3_600_000 || state.RetryBackoffBaseMillis < 0 || state.RetryBackoffBaseMillis > 3_600_000 || (state.RetryAtUTC != nil && state.RetryAtUTC.IsZero()) {
		return fmt.Errorf("invalid cloud inventory refresh state")
	}
	boundaryJSON, err := marshalCloudInventoryPageBoundary(state.NextPageBoundary)
	if err != nil {
		return err
	}
	if state.NextOffset > 0 && state.NextPageBoundary == nil {
		return fmt.Errorf("cloud inventory refresh offset has no page boundary")
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
	var previousStatus, previousPhase string
	err = tx.QueryRow(`SELECT status,phase FROM cloud_inventory_refresh_state WHERE account_fingerprint=? AND run_id=?`, state.AccountFingerprint, state.RunID).Scan(&previousStatus, &previousPhase)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if resumed && previousStatus != "running" && previousStatus != "paused" {
		_ = tx.Rollback()
		return fmt.Errorf("cloud inventory refresh checkpoint is not resumable")
	}
	if state.Status == "failed" {
		if _, err = tx.Exec(`DELETE FROM cloud_inventory_refresh_queue WHERE account_fingerprint=? AND run_id=?`, state.AccountFingerprint, state.RunID); err != nil {
			_ = tx.Rollback()
			return err
		}
		// Failed refreshes cannot publish or resume their staged pages; discard
		// account-scoped staging data while leaving the last published inventory intact.
		if _, err = tx.Exec(`DELETE FROM cloud_inventory_refresh_entries WHERE account_fingerprint=?`, state.AccountFingerprint); err != nil {
			_ = tx.Rollback()
			return err
		}
		state.PendingCount = 0
	}
	var retryAt any
	if state.RetryAtUTC != nil {
		retryAt = state.RetryAtUTC.UTC().UnixMicro()
	}
	result, err := tx.Exec(`UPDATE cloud_inventory_refresh_state SET status=?,phase=?,current_directory=?,next_offset=?,pages_processed=?,items_processed=?,request_budget=?,requests_used=?,pending_count=?,updated_at_utc=?,last_error=?,retry_delay_millis=?,retry_backoff_base_millis=?,retry_at_utc=?,next_page_boundary_json=? WHERE account_fingerprint=? AND run_id=?`, state.Status, state.Phase, state.CurrentDirectory, state.NextOffset, state.PagesProcessed, state.ItemsProcessed, state.RequestBudget, state.RequestsUsed, state.PendingCount, state.UpdatedAtUTC.UTC().UnixMicro(), state.LastError, state.RetryDelayMillis, state.RetryBackoffBaseMillis, retryAt, boundaryJSON, state.AccountFingerprint, state.RunID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
		_ = tx.Rollback()
		if rowsErr != nil {
			return rowsErr
		}
		return fmt.Errorf("cloud inventory refresh state disappeared")
	}
	eventKind := ""
	if resumed {
		eventKind = "resumed"
	} else if state.Phase == "request_budget_reached" && previousPhase != "request_budget_reached" {
		eventKind = "request_budget_reached"
	} else if state.Phase == "backoff" && previousPhase != "backoff" {
		eventKind = "retry_scheduled"
	} else if state.Status == "paused" && previousStatus != "paused" {
		eventKind = "paused"
	} else if state.Status == "failed" && previousStatus != "failed" {
		eventKind = "failed"
	} else if state.Status == "running" && previousStatus == "paused" {
		eventKind = "resumed"
	}
	if eventKind != "" {
		event := refreshEventFromState(state, eventKind)
		if eventKind == "resumed" {
			event.RequestBudgetAdded = state.RequestBudgetAdded
		}
		if err = appendCloudInventoryRefreshEvent(tx, event); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err = syncCloudInventoryRefreshRun(tx, state); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// RestartCloudInventoryRefreshBoundary discards only the current directory's
// staged observation and records a durable restart before its next list call.
func (s *Store) RestartCloudInventoryRefreshBoundary(state CloudInventoryRefreshState, at time.Time, restartReason ...string) (CloudInventoryRefreshState, error) {
	reason := "page_boundary_mismatch"
	if len(restartReason) > 0 {
		reason = restartReason[0]
	}
	if len(restartReason) > 1 || (reason != "page_boundary_mismatch" && reason != "provider_duplicate_page_id" && reason != "staged_page_duplicate_id") {
		return state, fmt.Errorf("invalid cloud inventory refresh restart reason")
	}
	return s.RestartCloudInventoryRefreshBoundaryWithEvidence(state, at, reason, nil)
}

func (s *Store) RestartCloudInventoryRefreshBoundaryWithEvidence(state CloudInventoryRefreshState, at time.Time, reason string, fsID *int64) (CloudInventoryRefreshState, error) {
	if !IsCloudInventoryRefreshRestartReason(reason) || ((reason == "provider_duplicate_page_id" || reason == "staged_page_duplicate_id") && (fsID == nil || *fsID <= 0)) || (fsID != nil && *fsID <= 0) {
		return state, fmt.Errorf("invalid cloud inventory refresh restart evidence")
	}
	if state.Status != "running" || state.NextOffset <= 0 || state.NextPageBoundary == nil || at.IsZero() || !validInventoryPath(state.CurrentDirectory) {
		return state, fmt.Errorf("invalid cloud inventory refresh boundary restart")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return state, os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return state, err
	}
	defer tx.Rollback()
	var runID, directory, status, boundaryJSON string
	var offset int64
	if err = tx.QueryRow(`SELECT run_id,current_directory,status,next_offset,next_page_boundary_json FROM cloud_inventory_refresh_state WHERE account_fingerprint=?`, state.AccountFingerprint).Scan(&runID, &directory, &status, &offset, &boundaryJSON); err != nil {
		return state, err
	}
	wantBoundary, _ := marshalCloudInventoryPageBoundary(state.NextPageBoundary)
	if runID != state.RunID || directory != state.CurrentDirectory || status != "running" || offset != state.NextOffset || boundaryJSON != wantBoundary {
		return state, fmt.Errorf("cloud inventory refresh boundary checkpoint changed before restart")
	}
	if err = tx.QueryRow(`SELECT count(*) FROM cloud_inventory_refresh_events WHERE account_fingerprint=? AND run_id=? AND current_directory=? AND event_kind='boundary_restart'`, state.AccountFingerprint, state.RunID, state.CurrentDirectory).Scan(&state.BoundaryRestartCount); err != nil {
		return state, err
	}
	if state.BoundaryRestartCount >= CloudInventoryBoundaryRestartLimit {
		return state, ErrCloudInventoryBoundaryRestartLimitReached
	}
	state.BoundaryRestartCount++
	state.NextOffset, state.NextPageBoundary, state.UpdatedAtUTC = 0, nil, at.UTC()
	state.LastError, state.RetryDelayMillis, state.RetryBackoffBaseMillis, state.RetryAtUTC = "", 0, 0, nil
	if _, err = tx.Exec(`DELETE FROM cloud_inventory_refresh_entries WHERE account_fingerprint=? AND run_id=? AND directory=?`, state.AccountFingerprint, state.RunID, state.CurrentDirectory); err != nil {
		return state, err
	}
	result, err := tx.Exec(`UPDATE cloud_inventory_refresh_state SET next_offset=0,next_page_boundary_json='',updated_at_utc=? WHERE account_fingerprint=? AND run_id=? AND current_directory=? AND status='running' AND next_offset=?`, at.UTC().UnixMicro(), state.AccountFingerprint, state.RunID, state.CurrentDirectory, offset)
	if err != nil {
		return state, err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
		if rowsErr != nil {
			return state, rowsErr
		}
		return state, fmt.Errorf("cloud inventory refresh boundary checkpoint changed concurrently")
	}
	event := refreshEventFromState(state, "boundary_restart")
	event.RestartReason = reason
	restartOffset := offset
	event.RestartOffset = &restartOffset
	event.RestartFSID = fsID
	if err = appendCloudInventoryRefreshEvent(tx, event); err != nil {
		return state, err
	}
	if err = tx.Commit(); err != nil {
		return state, err
	}
	return state, nil
}

func (s *Store) LoadCloudInventoryRefreshState(account string) (CloudInventoryRefreshState, error) {
	state := CloudInventoryRefreshState{}
	if account == "" || len(account) > 128 {
		return state, fmt.Errorf("invalid account fingerprint")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return state, os.ErrClosed
	}
	var started, updated int64
	var retryAt sql.NullInt64
	var boundaryJSON string
	err := s.db.QueryRow(`SELECT s.account_fingerprint,s.run_id,s.root_path,s.status,s.phase,s.current_directory,s.next_offset,s.pages_processed,s.items_processed,s.request_budget,s.requests_used,s.pending_count,s.started_at_utc,s.updated_at_utc,s.last_error,s.retry_delay_millis,s.retry_backoff_base_millis,s.retry_at_utc,s.next_page_boundary_json,(SELECT count(*) FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=s.account_fingerprint AND e.run_id=s.run_id AND e.current_directory=s.current_directory AND e.event_kind='boundary_restart') FROM cloud_inventory_refresh_state s WHERE s.account_fingerprint=?`, account).Scan(&state.AccountFingerprint, &state.RunID, &state.RootPath, &state.Status, &state.Phase, &state.CurrentDirectory, &state.NextOffset, &state.PagesProcessed, &state.ItemsProcessed, &state.RequestBudget, &state.RequestsUsed, &state.PendingCount, &started, &updated, &state.LastError, &state.RetryDelayMillis, &state.RetryBackoffBaseMillis, &retryAt, &boundaryJSON, &state.BoundaryRestartCount)
	if err != nil {
		return state, err
	}
	state.StartedAtUTC = time.UnixMicro(started).UTC()
	state.UpdatedAtUTC = time.UnixMicro(updated).UTC()
	if retryAt.Valid {
		retry := time.UnixMicro(retryAt.Int64).UTC()
		state.RetryAtUTC = &retry
	}
	state.NextPageBoundary, err = unmarshalCloudInventoryPageBoundary(boundaryJSON)
	if err != nil {
		return state, err
	}
	return state, nil
}

func (s *Store) CloudInventoryRefreshQueueHead(account, runID string) (string, error) {
	if account == "" || runID == "" {
		return "", fmt.Errorf("invalid cloud inventory refresh queue identity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", os.ErrClosed
	}
	var directory string
	err := s.db.QueryRow(`SELECT path FROM cloud_inventory_refresh_queue WHERE account_fingerprint=? AND run_id=? ORDER BY sequence LIMIT 1`, account, runID).Scan(&directory)
	return directory, err
}

func (s *Store) ListCloudInventory(account, directory string, offset, limit int) ([]CloudInventoryEntry, CloudInventoryDirectory, int, error) {
	return s.ListCloudInventoryAtGeneration(account, directory, offset, limit, nil)
}

func (s *Store) ListCloudInventoryAtGeneration(account, directory string, offset, limit int, expected *int64) ([]CloudInventoryEntry, CloudInventoryDirectory, int, error) {
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
	tx, err := s.db.Begin()
	if err != nil {
		return nil, meta, 0, err
	}
	defer tx.Rollback()
	if err = cloudInventoryGeneration(tx, account, &meta.Generation); err != nil {
		return nil, meta, 0, err
	}
	if expected != nil && *expected != meta.Generation {
		return nil, meta, 0, ErrCloudInventoryGenerationChanged
	}
	var observed sql.NullInt64
	err = tx.QueryRow(`SELECT status,observed_at_utc FROM cloud_inventory_directories WHERE account_fingerprint=? AND path=?`, account, directory).Scan(&meta.Status, &observed)
	if err != nil && !errorsIsNoRows(err) {
		return nil, meta, 0, err
	}
	if observed.Valid {
		v := time.UnixMicro(observed.Int64).UTC()
		meta.ObservedAtUTC = &v
	}
	var total int
	if err = tx.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint=? AND parent_path=?`, account, directory).Scan(&total); err != nil {
		return nil, meta, 0, err
	}
	rows, err := tx.Query(`SELECT fs_id,path,name,is_dir,size_bytes,provider_mtime,content_md5 FROM cloud_inventory WHERE account_fingerprint=? AND parent_path=? ORDER BY name,fs_id LIMIT ? OFFSET ?`, account, directory, limit, offset)
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
	if err = rows.Err(); err != nil {
		return nil, meta, 0, err
	}
	if err = rows.Close(); err != nil {
		return nil, meta, 0, err
	}
	for i := range items {
		if !items[i].Directory {
			continue
		}
		size, sizeErr := cloudDirectorySizeInTx(tx, account, items[i].Path, time.Now().UTC())
		if sizeErr != nil {
			return nil, meta, 0, sizeErr
		}
		items[i].DirectorySizeBytes = &size.Bytes
		items[i].DirectorySizeComplete = &size.Complete
	}
	if err = tx.Commit(); err != nil {
		return nil, meta, 0, err
	}
	return items, meta, total, nil
}

const cloudInventoryIncompleteReasonExpr = `CASE
	WHEN d.status IN ('scanning','stale','unknown','failed') THEN d.status
	WHEN d.path IS NULL AND EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.path=i.path AND e.is_dir=1) THEN 'missing_status'
	WHEN d.path IS NOT NULL AND NOT EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.path=i.path AND e.is_dir=1) THEN 'orphan_status'
	WHEN i.path='/' AND NOT EXISTS(SELECT 1 FROM cloud_inventory_directories root WHERE root.account_fingerprint=? AND root.path='/' AND root.status='complete') THEN 'root_incomplete'
	WHEN EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.path=i.path AND e.path<>CASE WHEN e.parent_path='/' THEN '/'||e.name ELSE e.parent_path||'/'||e.name END) THEN 'path_mismatch'
	WHEN EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.parent_path=i.path AND e.parent_path<>'/' AND NOT EXISTS(SELECT 1 FROM cloud_inventory p JOIN cloud_inventory_directories pd ON pd.account_fingerprint=p.account_fingerprint AND pd.path=p.path AND pd.status='complete' WHERE p.account_fingerprint=e.account_fingerprint AND p.path=e.parent_path AND p.is_dir=1)) THEN 'parent_incomplete'
	ELSE 'structural'
END`

func IsValidCloudInventoryIncompleteReason(reason string) bool {
	switch reason {
	case "", "scanning", "stale", "unknown", "failed", "missing_status", "orphan_status", "root_incomplete", "path_mismatch", "parent_incomplete", "structural":
		return true
	default:
		return false
	}
}

// ListCloudInventoryIncompleteDirectories reads only the local cache. Its path cursor and
// generation check keep subsequent pages tied to the same cache version.
func (s *Store) ListCloudInventoryIncompleteDirectories(account, beforePath, reason, query string, limit int, expected *int64) (CloudInventoryIncompleteDirectoryPage, error) {
	page := CloudInventoryIncompleteDirectoryPage{Entries: []CloudInventoryIncompleteDirectory{}}
	query = strings.TrimSpace(query)
	if account == "" || len(account) > 128 || (beforePath != "" && !validInventoryPath(beforePath)) || !IsValidCloudInventoryIncompleteReason(reason) || utf8.RuneCountInString(query) > 128 || limit < 1 || limit > 100 {
		return page, fmt.Errorf("invalid incomplete cloud inventory query")
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
	base := `WITH incomplete_paths(path) AS (` + cloudInventoryIncompletePathsSQL + `), details AS (
		SELECT i.path,coalesce(d.status,'') AS status,
			CASE WHEN EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.path=i.path AND e.is_dir=1) THEN 1 ELSE 0 END AS cached_directory,
			d.observed_at_utc,coalesce(d.last_run_id,'') AS last_run_id,` + cloudInventoryIncompleteReasonExpr + ` AS reason
		FROM incomplete_paths i LEFT JOIN cloud_inventory_directories d ON d.account_fingerprint=? AND d.path=i.path
	)`
	args := []any{account, account, account, account, account, account, account, account, account, account, account, account, account}
	where := ` WHERE (?='' OR reason=?) AND (?='' OR instr(lower(path),lower(?))>0)`
	countArgs := append(append([]any(nil), args...), reason, reason, query, query)
	if err = tx.QueryRow(base+` SELECT count(*) FROM details`+where, countArgs...).Scan(&page.Total); err != nil {
		return page, err
	}
	pageArgs := append(append([]any(nil), args...), reason, reason, query, query, beforePath, limit+1)
	rows, err := tx.Query(base+` SELECT path,status,reason,cached_directory,observed_at_utc,last_run_id FROM details`+where+` AND path>? ORDER BY path LIMIT ?`, pageArgs...)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var entry CloudInventoryIncompleteDirectory
		var cached int
		var observed sql.NullInt64
		if err = rows.Scan(&entry.Path, &entry.Status, &entry.Reason, &cached, &observed, &entry.LastRunID); err != nil {
			_ = rows.Close()
			return page, err
		}
		entry.CachedDirectory = cached == 1
		if observed.Valid {
			v := time.UnixMicro(observed.Int64).UTC()
			entry.ObservedAtUTC = &v
		}
		page.Entries = append(page.Entries, entry)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return page, err
	}
	if err = rows.Close(); err != nil {
		return page, err
	}
	if len(page.Entries) > limit {
		page.HasMore = true
		page.Entries = page.Entries[:limit]
		page.NextBeforePath = page.Entries[len(page.Entries)-1].Path
	}
	if err = tx.Commit(); err != nil {
		return page, err
	}
	return page, nil
}

func cloudInventoryGeneration(tx *sql.Tx, account string, generation *int64) error {
	err := tx.QueryRow(`SELECT generation FROM cloud_inventory_generations WHERE account_fingerprint=?`, account).Scan(generation)
	if errorsIsNoRows(err) {
		*generation = 0
		return nil
	}
	return err
}

func cloudInventoryCoverageInTx(tx *sql.Tx, account, scanStatus string, scanUpdatedAt *time.Time) (CloudInventorySearchCoverage, error) {
	coverage := CloudInventorySearchCoverage{Status: "unknown"}
	reasons := &CloudInventoryCoverageReasons{}
	coverage.Reasons = reasons
	if scanUpdatedAt != nil {
		updated := scanUpdatedAt.UTC()
		coverage.ScanUpdatedAtUTC = &updated
	}
	if err := tx.QueryRow(cloudInventoryIncompleteDirectoriesSQL, account, account, account, account, account, account).Scan(&coverage.IncompleteDirectories); err != nil {
		return coverage, err
	}
	if err := cloudInventoryGeneration(tx, account, &coverage.Generation); err != nil {
		return coverage, err
	}
	if err := tx.QueryRow(`SELECT
		(SELECT count(*) FROM cloud_inventory_directories WHERE account_fingerprint=? AND status='scanning'),
		(SELECT count(*) FROM cloud_inventory_directories WHERE account_fingerprint=? AND status='stale'),
		(SELECT count(*) FROM cloud_inventory_directories WHERE account_fingerprint=? AND status='unknown'),
		(SELECT count(*) FROM cloud_inventory_directories WHERE account_fingerprint=? AND status='failed'),
		(SELECT count(*) FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.is_dir=1 AND NOT EXISTS(SELECT 1 FROM cloud_inventory_directories d WHERE d.account_fingerprint=e.account_fingerprint AND d.path=e.path)),
		(SELECT count(*) FROM cloud_inventory_directories d WHERE d.account_fingerprint=? AND d.path<>'/' AND NOT EXISTS(SELECT 1 FROM cloud_inventory e WHERE e.account_fingerprint=d.account_fingerprint AND e.path=d.path AND e.is_dir=1)),
		(SELECT count(*) FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.parent_path<>'/' AND NOT EXISTS(SELECT 1 FROM cloud_inventory p JOIN cloud_inventory_directories d ON d.account_fingerprint=p.account_fingerprint AND d.path=p.path AND d.status='complete' WHERE p.account_fingerprint=e.account_fingerprint AND p.path=e.parent_path AND p.is_dir=1)),
		(SELECT count(*) FROM cloud_inventory e WHERE e.account_fingerprint=? AND e.path<>CASE WHEN e.parent_path='/' THEN '/'||e.name ELSE e.parent_path||'/'||e.name END),
		(SELECT count(*) FROM (SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM cloud_inventory_directories WHERE account_fingerprint=? AND path='/' AND status='complete')))`,
		account, account, account, account, account, account, account, account, account).Scan(
		&reasons.ScanningDirectories, &reasons.StaleDirectories, &reasons.UnknownDirectories, &reasons.FailedDirectories,
		&reasons.MissingDirectoryStatus, &reasons.OrphanDirectoryStatus, &reasons.ParentNotComplete, &reasons.PathMismatch, &reasons.MissingRoot); err != nil {
		return coverage, err
	}
	switch {
	case scanStatus == "running":
		coverage.Status = "scanning"
	case scanStatus == "complete" && coverage.IncompleteDirectories == 0:
		coverage.Status = "complete"
	case scanStatus != "":
		coverage.Status = "incomplete"
	}
	return coverage, nil
}

// SearchCloudInventory searches only the local inventory for one authorized account.
// Its coverage metadata makes zero matches distinguishable from an incomplete scan.
func (s *Store) SearchCloudInventory(account, query string, offset, limit int) ([]CloudInventoryEntry, int, CloudInventorySearchCoverage, error) {
	return s.SearchCloudInventoryAtGeneration(account, query, offset, limit, nil)
}

func (s *Store) SearchCloudInventoryAtGeneration(account, query string, offset, limit int, expected *int64) ([]CloudInventoryEntry, int, CloudInventorySearchCoverage, error) {
	return s.searchCloudInventory(account, query, offset, limit, expected, false, CloudInventorySearchFilter{Scope: "/", Kind: "all"})
}

// SearchCloudDirectoriesAtGeneration filters before SQL counting and paging.
func (s *Store) SearchCloudDirectoriesAtGeneration(account, query string, offset, limit int, expected *int64) ([]CloudInventoryEntry, int, CloudInventorySearchCoverage, error) {
	return s.searchCloudInventory(account, query, offset, limit, expected, true, CloudInventorySearchFilter{Scope: "/", Kind: "directory"})
}

type CloudInventorySearchFilter struct {
	Scope          string `json:"scope"`
	Kind           string `json:"kind"`
	Extensions     string `json:"extensions"`
	ModifiedFrom   int64  `json:"modified_from"`
	ModifiedBefore int64  `json:"modified_before"`
}

func (f CloudInventorySearchFilter) Valid() bool {
	return validInventoryPath(f.Scope) && len(f.Scope) <= 2048 && utf8.ValidString(f.Scope) && strings.IndexFunc(f.Scope, func(r rune) bool { return r < 32 || r == 127 }) < 0 && (f.Kind == "all" || f.Kind == "file" || f.Kind == "directory") && f.validExtraFilters()
}

func (s *Store) SearchCloudInventoryFilteredAtGeneration(account, query string, offset, limit int, expected *int64, filter CloudInventorySearchFilter) ([]CloudInventoryEntry, int, CloudInventorySearchCoverage, error) {
	return s.searchCloudInventory(account, query, offset, limit, expected, false, filter)
}

func (s *Store) searchCloudInventory(account, query string, offset, limit int, expected *int64, directoriesOnly bool, searchFilter CloudInventorySearchFilter) ([]CloudInventoryEntry, int, CloudInventorySearchCoverage, error) {
	items := []CloudInventoryEntry{}
	coverage := CloudInventorySearchCoverage{Status: "unknown"}
	query = strings.TrimSpace(query)
	if !searchFilter.Valid() || account == "" || len(account) > 128 || query == "" || utf8.RuneCountInString(query) > 128 || offset < 0 || offset > 1000000 || limit < 1 || limit > 100 {
		return nil, 0, coverage, fmt.Errorf("invalid cloud inventory search")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, coverage, os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, 0, coverage, err
	}
	defer tx.Rollback()
	if err = cloudInventoryGeneration(tx, account, &coverage.Generation); err != nil {
		return nil, 0, coverage, err
	}
	if expected != nil && *expected != coverage.Generation {
		return nil, 0, coverage, ErrCloudInventoryGenerationChanged
	}

	var scanStatus string
	var scanUpdated sql.NullInt64
	err = tx.QueryRow(`SELECT status,updated_at_utc FROM cloud_inventory_scan_state WHERE account_fingerprint=?`, account).Scan(&scanStatus, &scanUpdated)
	if err != nil && !errorsIsNoRows(err) {
		return nil, 0, coverage, err
	}
	var scanUpdatedAt *time.Time
	if scanUpdated.Valid {
		updated := time.UnixMicro(scanUpdated.Int64).UTC()
		scanUpdatedAt = &updated
	}
	coverage, err = cloudInventoryCoverageInTx(tx, account, scanStatus, scanUpdatedAt)
	if err != nil {
		return nil, 0, coverage, err
	}

	filter := ""
	if searchFilter.Kind == "directory" {
		filter = " AND is_dir=1"
	} else if searchFilter.Kind == "file" {
		filter = " AND is_dir=0"
	}
	args := []any{account}
	if searchFilter.Scope != "/" {
		lower, upper := cloudDirectoryPathRange(searchFilter.Scope)
		filter += " AND path>=? AND path<?"
		args = append(args, lower, upper)
	}
	if searchFilter.Extensions != "" {
		matches := []string{}
		for _, extension := range strings.Split(searchFilter.Extensions, ",") {
			matches = append(matches, "lower(name) LIKE ?")
			args = append(args, "%."+extension)
		}
		filter += " AND is_dir=0 AND (" + strings.Join(matches, " OR ") + ")"
	}
	if searchFilter.ModifiedFrom != 0 || searchFilter.ModifiedBefore != 0 {
		// Zero is an unknown provider timestamp, never an old modification date.
		filter += " AND provider_mtime>0"
		if searchFilter.ModifiedFrom != 0 {
			filter += " AND provider_mtime>=?"
			args = append(args, searchFilter.ModifiedFrom)
		}
		if searchFilter.ModifiedBefore != 0 {
			filter += " AND provider_mtime<?"
			args = append(args, searchFilter.ModifiedBefore)
		}
	}
	args = append(args, query, query)
	const match = `instr(lower(name),lower(?))>0 OR instr(lower(path),lower(?))>0`
	var total int
	if err = tx.QueryRow(`SELECT count(*) FROM cloud_inventory WHERE account_fingerprint=?`+filter+` AND (`+match+`)`, args...).Scan(&total); err != nil {
		return nil, 0, coverage, err
	}
	rows, err := tx.Query(`SELECT fs_id,path,name,is_dir,size_bytes,provider_mtime,content_md5 FROM cloud_inventory WHERE account_fingerprint=?`+filter+` AND (`+match+`) ORDER BY path,fs_id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, coverage, err
	}
	for rows.Next() {
		var item CloudInventoryEntry
		var isDir int
		if err = rows.Scan(&item.FsID, &item.Path, &item.Name, &isDir, &item.Size, &item.ProviderMtime, &item.ContentMD5); err != nil {
			_ = rows.Close()
			return nil, 0, coverage, err
		}
		item.Directory = isDir == 1
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, 0, coverage, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, coverage, err
	}
	for i := range items {
		// Target discovery needs paths, not recursive size statistics.
		if directoriesOnly || !items[i].Directory {
			continue
		}
		size, sizeErr := cloudDirectorySizeInTx(tx, account, items[i].Path, time.Now().UTC())
		if sizeErr != nil {
			return nil, 0, coverage, sizeErr
		}
		items[i].DirectorySizeBytes = &size.Bytes
		items[i].DirectorySizeComplete = &size.Complete
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, coverage, err
	}
	return items, total, coverage, nil
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
