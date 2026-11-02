package downloader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

const cloudInventoryPageSize = 100
const cloudInventoryPageInterval = time.Second

func cloudInventoryPageBoundary(file baiduapi.FileInfo) *taskstore.CloudInventoryPageBoundary {
	return &taskstore.CloudInventoryPageBoundary{FsID: file.FsID, Path: file.Path, Name: file.ServerFilename, Directory: file.IsDir == 1, Size: file.Size, ProviderMtime: file.Mtime, ContentMD5: file.MD5}
}

func cloudInventoryPageBoundaryMatches(boundary *taskstore.CloudInventoryPageBoundary, file baiduapi.FileInfo) bool {
	return boundary != nil && boundary.FsID == file.FsID && boundary.Path == file.Path && boundary.Name == file.ServerFilename && boundary.Directory == (file.IsDir == 1) && boundary.Size == file.Size && boundary.ProviderMtime == file.Mtime && boundary.ContentMD5 == file.MD5
}

func (d *Downloader) reserveCloudInventoryScan(maintenanceError string) error {
	d.queue.mu.Lock()
	maintenance := d.queue.maintenance
	d.queue.mu.Unlock()
	if maintenance {
		return errors.New(maintenanceError)
	}
	if d.inventoryScanAdmissionHook != nil {
		d.inventoryScanAdmissionHook()
	}
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	if d.queue.maintenance {
		return errors.New(maintenanceError)
	}
	d.inventoryScanMutex.Lock()
	defer d.inventoryScanMutex.Unlock()
	if d.inventoryScanCancel != nil || d.inventoryScanStarting {
		return fmt.Errorf("网盘扫描已经在运行")
	}
	d.inventoryScanStarting = true
	return nil
}

func (d *Downloader) StartCloudInventoryScan(ctx context.Context, requestBudget int64) (taskstore.CloudScanState, error) {
	if requestBudget < 0 || requestBudget > taskstore.CloudInventoryRefreshRequestBudgetLimit {
		return taskstore.CloudScanState{}, fmt.Errorf("全盘扫描请求额度必须介于 0 和 %d 之间", taskstore.CloudInventoryRefreshRequestBudgetLimit)
	}
	if err := d.reserveCloudInventoryScan("下载队列正在维护，暂不能启动全盘扫描"); err != nil {
		return taskstore.CloudScanState{}, err
	}
	started := false
	defer func() {
		if !started {
			d.inventoryScanMutex.Lock()
			d.inventoryScanStarting = false
			d.inventoryScanMutex.Unlock()
		}
	}()
	// A scan outlives the HTTP request that starts it; bind it to the downloader
	// lifetime so a client disconnect after 202 Accepted does not pause the scan.
	scoped, done, err := d.startIntake(d.ctx)
	if err != nil {
		return taskstore.CloudScanState{}, err
	}
	client, _, err := d.authorizedClient(scoped)
	if err != nil {
		done()
		return taskstore.CloudScanState{}, err
	}
	account := client.Fingerprint()
	duplicateState, duplicateErr := d.queue.store.LoadCloudDuplicateScanState(account)
	if duplicateErr == nil && (duplicateState.Status == "running" || duplicateState.Status == "paused") {
		done()
		return taskstore.CloudScanState{}, fmt.Errorf("重复文件扫描仍有待处理哈希，请先完成或恢复扫描")
	}
	if duplicateErr != nil && !errors.Is(duplicateErr, sql.ErrNoRows) {
		done()
		return taskstore.CloudScanState{}, duplicateErr
	}
	refreshState, refreshErr := d.queue.store.LoadCloudInventoryRefreshState(account)
	if refreshErr == nil && (refreshState.Status == "running" || refreshState.Status == "paused") && refreshState.PendingCount > 0 {
		done()
		return taskstore.CloudScanState{}, fmt.Errorf("目录局部刷新仍有待处理目录，请先完成或恢复局部刷新")
	}
	if refreshErr != nil && !errors.Is(refreshErr, sql.ErrNoRows) {
		done()
		return taskstore.CloudScanState{}, refreshErr
	}
	state, err := d.queue.store.LoadCloudScanState(account)
	freshRun := false
	if errors.Is(err, sql.ErrNoRows) {
		runID, idErr := newSourceOperationID()
		if idErr != nil {
			done()
			return taskstore.CloudScanState{}, idErr
		}
		now := time.Now().UTC()
		state = taskstore.CloudScanState{AccountFingerprint: account, RunID: runID, Status: "running", Phase: "listing", CurrentDirectory: "/", RequestBudget: requestBudget, StartedAtUTC: now, UpdatedAtUTC: now}
		freshRun = true
	} else if err != nil {
		done()
		return taskstore.CloudScanState{}, err
	}
	if state.Status == "complete" {
		runID, idErr := newSourceOperationID()
		if idErr != nil {
			done()
			return taskstore.CloudScanState{}, idErr
		}
		now := time.Now().UTC()
		state = taskstore.CloudScanState{AccountFingerprint: account, RunID: runID, Status: "running", Phase: "listing", CurrentDirectory: "/", RequestBudget: requestBudget, StartedAtUTC: now, UpdatedAtUTC: now}
		freshRun = true
	}
	if freshRun {
		if err = d.queue.store.InvalidateCloudInventory(account, []string{"/"}); err != nil {
			done()
			return taskstore.CloudScanState{}, err
		}
		if err = d.queue.store.StartCloudScan(&state); err != nil {
			done()
			return taskstore.CloudScanState{}, err
		}
	} else if state.PendingCount == 0 {
		done()
		return taskstore.CloudScanState{}, fmt.Errorf("网盘扫描队列为空但状态尚未完成，需要重新核对扫描状态")
	}
	if !freshRun && requestBudget > 0 {
		maxInt64 := int64(^uint64(0) >> 1)
		if state.RequestBudget == 0 {
			if state.RequestsUsed > maxInt64-requestBudget {
				done()
				return taskstore.CloudScanState{}, fmt.Errorf("全盘扫描累计请求额度超出范围")
			}
			state.RequestBudget = state.RequestsUsed + requestBudget
		} else {
			if state.RequestBudget > maxInt64-requestBudget {
				done()
				return taskstore.CloudScanState{}, fmt.Errorf("全盘扫描累计请求额度超出范围")
			}
			state.RequestBudget += requestBudget
		}
	}
	if state.RequestBudget > 0 && state.RequestsUsed >= state.RequestBudget {
		done()
		return taskstore.CloudScanState{}, fmt.Errorf("请填写追加请求次数后继续全盘扫描")
	}
	now := time.Now().UTC()
	state.Status = "running"
	state.Phase = "listing"
	if state.RetryAtUTC != nil && state.RetryAtUTC.After(now) {
		state.Phase = "backoff"
	}
	state.UpdatedAtUTC = now
	if err = d.saveCloudScanState(state); err != nil {
		done()
		return taskstore.CloudScanState{}, err
	}
	workerCtx, cancel := context.WithCancel(scoped)
	d.inventoryScanMutex.Lock()
	if d.inventoryScanCancel != nil {
		d.inventoryScanMutex.Unlock()
		cancel()
		done()
		return taskstore.CloudScanState{}, fmt.Errorf("网盘扫描已经在运行")
	}
	d.inventoryScanCancel = cancel
	d.inventoryScanKind = "full"
	d.inventoryScanStarting = false
	d.inventoryScanMutex.Unlock()
	state.WorkerActive = true
	started = true
	go d.runCloudInventoryScan(workerCtx, state, done)
	return state, nil
}

func (d *Downloader) PauseCloudInventoryScan() {
	d.inventoryScanMutex.Lock()
	cancel := d.inventoryScanCancel
	if d.inventoryScanKind != "full" {
		cancel = nil
	}
	d.inventoryScanMutex.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (d *Downloader) PauseCloudInventoryRefresh() {
	d.inventoryScanMutex.Lock()
	cancel := d.inventoryScanCancel
	if d.inventoryScanKind != "refresh" {
		cancel = nil
	}
	d.inventoryScanMutex.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (d *Downloader) CloudInventoryScanStatus() (taskstore.CloudScanState, error) {
	client, _, err := d.authorizedClient(d.ctx)
	if err != nil {
		return taskstore.CloudScanState{}, err
	}
	state, coverage, err := d.queue.store.LoadCloudInventoryScanOverview(client.Fingerprint())
	if err != nil {
		return state, err
	}
	state.InventoryCoverage = &coverage
	d.inventoryScanMutex.Lock()
	state.WorkerActive = d.inventoryScanKind == "full" && d.inventoryScanCancel != nil
	d.inventoryScanMutex.Unlock()
	return state, nil
}

func (d *Downloader) CloudScanHistoryRuns(ctx context.Context, before int64, filter taskstore.CloudScanRunFilter, limit int) ([]taskstore.CloudScanRun, int64, bool, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, 0, false, fmt.Errorf("invalid cloud scan history page")
	}
	runs, err := d.queue.store.ListCloudScanRunsFiltered(client.Fingerprint(), before, filter, limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	if len(runs) <= limit {
		return runs, 0, false, nil
	}
	page := runs[:limit]
	return page, page[len(page)-1].Sequence, true, nil
}

func (d *Downloader) CloudScanHistoryEvents(ctx context.Context, runID string, before int64, limit int) ([]taskstore.CloudScanEvent, int64, bool, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, 0, false, fmt.Errorf("invalid cloud scan event page")
	}
	events, err := d.queue.store.ListCloudScanEvents(client.Fingerprint(), runID, before, limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	if len(events) <= limit {
		return events, 0, false, nil
	}
	page := events[:limit]
	return page, page[len(page)-1].Sequence, true, nil
}

func (d *Downloader) CloudScanHistoryExport(ctx context.Context, runID string) (taskstore.CloudScanEvidenceBundle, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return taskstore.CloudScanEvidenceBundle{}, err
	}
	return d.queue.store.ExportCloudScanEvidence(ctx, client.Fingerprint(), runID)
}

func (d *Downloader) StartCloudInventoryRefresh(ctx context.Context, directory string, requestBudget int64) (taskstore.CloudInventoryRefreshState, error) {
	if directory == "" || len(directory) > 4096 || !strings.HasPrefix(directory, "/") || path.Clean(directory) != directory || strings.Contains(directory, "\\") {
		return taskstore.CloudInventoryRefreshState{}, fmt.Errorf("invalid cloud inventory refresh directory")
	}
	if requestBudget < 0 || requestBudget > taskstore.CloudInventoryRefreshRequestBudgetLimit {
		return taskstore.CloudInventoryRefreshState{}, fmt.Errorf("目录刷新请求额度必须在 0 到 %d 之间", taskstore.CloudInventoryRefreshRequestBudgetLimit)
	}
	if err := d.reserveCloudInventoryScan("下载队列正在维护，暂不能刷新网盘目录缓存"); err != nil {
		return taskstore.CloudInventoryRefreshState{}, err
	}
	started := false
	defer func() {
		if !started {
			d.inventoryScanMutex.Lock()
			d.inventoryScanStarting = false
			d.inventoryScanMutex.Unlock()
		}
	}()
	scoped, done, err := d.startIntake(d.ctx)
	if err != nil {
		return taskstore.CloudInventoryRefreshState{}, err
	}
	client, _, err := d.authorizedClient(scoped)
	if err != nil {
		done()
		return taskstore.CloudInventoryRefreshState{}, err
	}
	account := client.Fingerprint()
	duplicateState, duplicateErr := d.queue.store.LoadCloudDuplicateScanState(account)
	if duplicateErr == nil && (duplicateState.Status == "running" || duplicateState.Status == "paused") {
		done()
		return taskstore.CloudInventoryRefreshState{}, fmt.Errorf("重复文件扫描仍有待处理哈希，请先完成或恢复扫描")
	}
	if duplicateErr != nil && !errors.Is(duplicateErr, sql.ErrNoRows) {
		done()
		return taskstore.CloudInventoryRefreshState{}, duplicateErr
	}
	fullState, fullErr := d.queue.store.LoadCloudScanState(account)
	if fullErr == nil && (fullState.Status == "running" || fullState.Status == "paused") && fullState.PendingCount > 0 {
		done()
		return taskstore.CloudInventoryRefreshState{}, fmt.Errorf("全盘扫描仍有待处理目录，请先完成或恢复全盘扫描")
	}
	if fullErr != nil && !errors.Is(fullErr, sql.ErrNoRows) {
		done()
		return taskstore.CloudInventoryRefreshState{}, fullErr
	}
	state, stateErr := d.queue.store.LoadCloudInventoryRefreshState(account)
	freshRun := errors.Is(stateErr, sql.ErrNoRows)
	if stateErr != nil && !freshRun {
		done()
		return taskstore.CloudInventoryRefreshState{}, stateErr
	}
	if !freshRun && (state.Status == "running" || state.Status == "paused") && state.PendingCount > 0 {
		if state.RootPath != directory {
			done()
			return taskstore.CloudInventoryRefreshState{}, fmt.Errorf("已有目录刷新待恢复，当前范围是 %s；请先恢复该范围", state.RootPath)
		}
		if requestBudget > 0 {
			state.RequestBudgetAdded = requestBudget
			maxInt64 := int64(^uint64(0) >> 1)
			if state.RequestBudget == 0 {
				if state.RequestsUsed > maxInt64-requestBudget {
					done()
					return taskstore.CloudInventoryRefreshState{}, fmt.Errorf("目录刷新累计请求额度超出范围")
				}
				state.RequestBudget = state.RequestsUsed + requestBudget
			} else if state.RequestBudget > maxInt64-requestBudget {
				done()
				return taskstore.CloudInventoryRefreshState{}, fmt.Errorf("目录刷新累计请求额度超出范围")
			} else {
				state.RequestBudget += requestBudget
			}
		}
		if state.RequestBudget > 0 && state.RequestsUsed >= state.RequestBudget {
			done()
			return taskstore.CloudInventoryRefreshState{}, fmt.Errorf("请先在请求额度中填写追加次数，再继续刷新：%w", taskstore.ErrCloudInventoryRefreshRequestBudgetReached)
		}
		now := time.Now().UTC()
		state.Status = "running"
		state.Phase = "listing"
		if state.RetryAtUTC != nil && state.RetryAtUTC.After(now) {
			state.Phase = "backoff"
		}
		state.UpdatedAtUTC = now
		if err = d.queue.store.ResumeCloudInventoryRefreshState(state); err != nil {
			done()
			return taskstore.CloudInventoryRefreshState{}, err
		}
	} else if freshRun || state.Status == "complete" || state.Status == "failed" || state.PendingCount == 0 {
		runID, idErr := newSourceOperationID()
		if idErr != nil {
			done()
			return taskstore.CloudInventoryRefreshState{}, idErr
		}
		now := time.Now().UTC()
		state = taskstore.CloudInventoryRefreshState{AccountFingerprint: account, RunID: runID, RootPath: directory, Status: "running", Phase: "listing", CurrentDirectory: directory, RequestBudget: requestBudget, StartedAtUTC: now, UpdatedAtUTC: now}
		if err = d.queue.store.StartCloudInventoryRefresh(&state); err != nil {
			done()
			return taskstore.CloudInventoryRefreshState{}, err
		}
	} else {
		done()
		return taskstore.CloudInventoryRefreshState{}, fmt.Errorf("网盘目录刷新检查点状态无效")
	}
	workerCtx, cancel := context.WithCancel(scoped)
	d.inventoryScanMutex.Lock()
	if d.inventoryScanCancel != nil {
		d.inventoryScanMutex.Unlock()
		cancel()
		done()
		return taskstore.CloudInventoryRefreshState{}, fmt.Errorf("网盘扫描已经在运行")
	}
	d.inventoryScanCancel = cancel
	d.inventoryScanKind = "refresh"
	d.inventoryScanStarting = false
	d.inventoryScanMutex.Unlock()
	state.WorkerActive = true
	started = true
	go d.runCloudInventoryRefresh(workerCtx, state, done)
	return state, nil
}

func (d *Downloader) CloudInventoryRefreshStatus() (taskstore.CloudInventoryRefreshState, error) {
	client, _, err := d.authorizedClient(d.ctx)
	if err != nil {
		return taskstore.CloudInventoryRefreshState{}, err
	}
	state, err := d.queue.store.LoadCloudInventoryRefreshState(client.Fingerprint())
	if errors.Is(err, sql.ErrNoRows) {
		return taskstore.CloudInventoryRefreshState{AccountFingerprint: client.Fingerprint(), Status: "idle", Phase: "idle"}, nil
	}
	if err == nil {
		d.inventoryScanMutex.Lock()
		state.WorkerActive = d.inventoryScanKind == "refresh" && d.inventoryScanCancel != nil
		d.inventoryScanMutex.Unlock()
	}
	return state, err
}

func (d *Downloader) CloudInventoryRefreshEventPage(ctx context.Context, runID, kind, restartReason string, before int64, limit int) ([]taskstore.CloudInventoryRefreshEvent, int64, bool, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	if limit < 1 || limit > 100 {
		return nil, 0, false, fmt.Errorf("invalid cloud inventory refresh event page size")
	}
	events, err := d.queue.store.ListCloudInventoryRefreshEventsFiltered(client.Fingerprint(), runID, kind, restartReason, before, limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	if len(events) <= limit {
		return events, 0, false, nil
	}
	page := events[:limit]
	return page, page[len(page)-1].Sequence, true, nil
}

func (d *Downloader) CloudInventoryRefreshChangePage(ctx context.Context, runID string, eventSequence, before int64, limit int, kind, query string) (taskstore.CloudInventoryRefreshChangePage, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return taskstore.CloudInventoryRefreshChangePage{}, err
	}
	if eventSequence <= 0 || before < 0 || limit < 1 || limit > 50 {
		return taskstore.CloudInventoryRefreshChangePage{}, fmt.Errorf("invalid cloud inventory refresh change page")
	}
	return d.queue.store.QueryCloudInventoryRefreshChanges(client.Fingerprint(), runID, eventSequence, before, limit, kind, query)
}

func (d *Downloader) CloudInventoryRefreshChangesExport(ctx context.Context, runID string, eventSequence int64, kind, query string) (taskstore.CloudInventoryRefreshChangeExport, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return taskstore.CloudInventoryRefreshChangeExport{}, err
	}
	return d.queue.store.CloudInventoryRefreshChangesExport(ctx, client.Fingerprint(), runID, eventSequence, kind, query)
}

func (d *Downloader) CloudInventoryRefreshRuns(ctx context.Context, before int64, limit int) ([]taskstore.CloudInventoryRefreshRun, int64, bool, error) {
	return d.CloudInventoryRefreshRunsFiltered(ctx, before, taskstore.CloudInventoryRefreshRunFilter{}, limit)
}

func (d *Downloader) CloudInventoryRefreshRunsFiltered(ctx context.Context, before int64, filter taskstore.CloudInventoryRefreshRunFilter, limit int) ([]taskstore.CloudInventoryRefreshRun, int64, bool, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, 0, false, fmt.Errorf("invalid cloud inventory refresh run page")
	}
	runs, err := d.queue.store.ListCloudInventoryRefreshRunsFiltered(client.Fingerprint(), before, filter, limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	if len(runs) <= limit {
		return runs, 0, false, nil
	}
	page := runs[:limit]
	return page, page[len(page)-1].StartSequence, true, nil
}

func (d *Downloader) CloudInventoryRefreshEvidenceExport(ctx context.Context, runID string) (taskstore.CloudInventoryRefreshEvidenceBundle, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return taskstore.CloudInventoryRefreshEvidenceBundle{}, err
	}
	return d.queue.store.CloudInventoryRefreshEvidenceExport(ctx, client.Fingerprint(), runID)
}

func (d *Downloader) CloudInventoryRefreshRunSummariesExport(ctx context.Context, filter taskstore.CloudInventoryRefreshRunFilter) ([]taskstore.CloudInventoryRefreshRun, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, err
	}
	return d.queue.store.ExportCloudInventoryRefreshRunSummaries(ctx, client.Fingerprint(), filter)
}

func (d *Downloader) ListCachedCloud(ctx context.Context, directory string, offset, limit int, expected *int64) ([]taskstore.CloudInventoryEntry, taskstore.CloudInventoryDirectory, int, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, taskstore.CloudInventoryDirectory{}, 0, err
	}
	return d.queue.store.ListCloudInventoryAtGeneration(client.Fingerprint(), directory, offset, limit, expected)
}

func (d *Downloader) ListIncompleteCloudInventoryDirectories(ctx context.Context, beforePath, reason, query string, limit int, expected *int64) (taskstore.CloudInventoryIncompleteDirectoryPage, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return taskstore.CloudInventoryIncompleteDirectoryPage{}, err
	}
	return d.queue.store.ListCloudInventoryIncompleteDirectories(client.Fingerprint(), beforePath, reason, query, limit, expected)
}

func (d *Downloader) SearchCachedCloud(ctx context.Context, query string, offset, limit int, expected *int64) ([]taskstore.CloudInventoryEntry, int, taskstore.CloudInventorySearchCoverage, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, 0, taskstore.CloudInventorySearchCoverage{Status: "unknown"}, err
	}
	return d.queue.store.SearchCloudInventoryAtGeneration(client.Fingerprint(), query, offset, limit, expected)
}

func (d *Downloader) GetCachedCloudDirectorySize(ctx context.Context, directory string) (taskstore.CloudDirectorySize, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return taskstore.CloudDirectorySize{}, err
	}
	return d.queue.store.CloudDirectorySize(client.Fingerprint(), directory)
}

func (d *Downloader) saveCloudScanState(state taskstore.CloudScanState) error {
	return d.queue.store.SaveCloudScanCheckpoint(state)
}

func (d *Downloader) runCloudInventoryScan(ctx context.Context, state taskstore.CloudScanState, done func()) {
	defer done()
	defer func() {
		d.inventoryScanMutex.Lock()
		d.inventoryScanCancel = nil
		d.inventoryScanKind = ""
		d.inventoryScanMutex.Unlock()
	}()
	backoff := inventoryRetryBase(state.RetryBackoffBaseMillis)
	if state.RetryBackoffBaseMillis == 0 {
		backoff = inventoryRetryBase(state.RetryDelayMillis)
	}
	for state.PendingCount > 0 {
		if err := ctx.Err(); err != nil {
			state.Status = "paused"
			state.Phase = "paused"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.saveCloudScanState(state)
			return
		}
		if !waitForInventoryRetry(ctx, state.RetryAtUTC) {
			state.Status, state.Phase = "paused", "paused"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.saveCloudScanState(state)
			return
		}
		if state.RetryAtUTC != nil {
			state.RetryAtUTC = nil
			state.Phase = "listing"
			state.UpdatedAtUTC = time.Now().UTC()
			if err := d.saveCloudScanState(state); err != nil {
				return
			}
		}
		directory, queueErr := d.queue.store.CloudScanQueueHead(state.AccountFingerprint, state.RunID)
		if queueErr != nil {
			state.Status, state.Phase = "failed", "failed"
			state.RetryDelayMillis, state.RetryBackoffBaseMillis, state.RetryAtUTC = 0, 0, nil
			state.LastError = errorSummary(queueErr.Error())
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.saveCloudScanState(state)
			return
		}
		if directory != state.CurrentDirectory {
			state.Status, state.Phase = "failed", "failed"
			state.RetryDelayMillis, state.RetryBackoffBaseMillis, state.RetryAtUTC = 0, 0, nil
			state.LastError = "持久化扫描队列与当前目录不一致"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.saveCloudScanState(state)
			return
		}
		d.cloudMutex.RLock()
		client, _, err := d.authorizedClient(ctx)
		if err == nil && client.Fingerprint() != state.AccountFingerprint {
			err = ErrAuthenticationWaiting
		}
		var files []structFileInfo
		var hasMore bool
		var nextBoundary *baiduapi.FileInfo
		boundaryMismatch := false
		if err == nil {
			reservedAt := time.Now().UTC()
			used, reserveErr := d.queue.store.ReserveCloudScanRequest(state.AccountFingerprint, state.RunID, reservedAt)
			if errors.Is(reserveErr, taskstore.ErrCloudInventoryScanRequestBudgetReached) {
				d.cloudMutex.RUnlock()
				state.Status, state.Phase = "paused", "request_budget_reached"
				state.UpdatedAtUTC = reservedAt
				_ = d.saveCloudScanState(state)
				return
			}
			if reserveErr != nil {
				d.cloudMutex.RUnlock()
				state.Status, state.Phase = "failed", "failed"
				state.LastError = errorSummary(reserveErr.Error())
				state.UpdatedAtUTC = reservedAt
				_ = d.saveCloudScanState(state)
				return
			}
			state.RequestsUsed, state.UpdatedAtUTC = used, reservedAt
			page, pageMore, boundary, pageErr := client.ListDirPageWithBoundary(directory, int(state.NextOffset), cloudInventoryPageSize)
			hasMore = pageMore
			nextBoundary = boundary
			err = pageErr
			if err == nil && state.NextOffset > 0 && (len(page) == 0 || !cloudInventoryPageBoundaryMatches(state.NextPageBoundary, page[0])) {
				boundaryMismatch = true
				err = fmt.Errorf("网盘目录分页边界发生变化；当前目录将从第一页重新核对")
			}
			if err == nil {
				files = make([]structFileInfo, 0, len(page))
				for _, f := range page {
					if !validListingFile(directory, f) {
						err = ErrIntakeConflict
						break
					}
					files = append(files, structFileInfo{fsid: f.FsID, path: f.Path, name: f.ServerFilename, isDir: f.IsDir == 1, size: f.Size, mtime: f.Mtime, md5: f.MD5})
				}
				if err == nil && boundary != nil && !validListingFile(directory, *boundary) {
					err = ErrIntakeConflict
				}
			}
		}
		if err != nil {
			d.cloudMutex.RUnlock()
			if ctx.Err() != nil {
				state.Status, state.Phase = "paused", "paused"
				state.LastError = ""
				state.UpdatedAtUTC = time.Now().UTC()
				_ = d.saveCloudScanState(state)
				return
			}
			if boundaryMismatch || (state.NextOffset > 0 && errors.Is(err, baiduapi.ErrDuplicatePageItem)) {
				reason := "page_boundary_mismatch"
				if errors.Is(err, baiduapi.ErrDuplicatePageItem) {
					reason = "provider_duplicate_page_id"
				}
				restarted, restartErr := d.queue.store.RestartCloudScanBoundary(state, time.Now().UTC(), reason)
				if restartErr == nil {
					state = restarted
					continue
				}
				state = restarted
				state.Status, state.Phase = "failed", "failed"
				state.LastError = errorSummary(restartErr.Error())
				state.RetryDelayMillis, state.RetryBackoffBaseMillis, state.RetryAtUTC = 0, 0, nil
				state.UpdatedAtUTC = time.Now().UTC()
				_ = d.saveCloudScanState(state)
				return
			}
			state.LastError = errorSummary(err.Error())
			retryable := errors.Is(err, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) || isCloudInventoryRetryable(err)
			delay := time.Duration(0)
			if retryable {
				state.Phase = "backoff"
				delay = inventoryRetryDelay(err, backoff)
				state.RetryDelayMillis = delay.Milliseconds()
				backoff = nextInventoryRetryBase(backoff)
				state.RetryBackoffBaseMillis = backoff.Milliseconds()
				retryAt := time.Now().UTC().Add(delay)
				state.RetryAtUTC = &retryAt
			} else {
				state.Status = "failed"
				state.Phase = "failed"
				state.RetryDelayMillis = 0
				state.RetryBackoffBaseMillis = 0
				state.RetryAtUTC = nil
				if boundaryMismatch || (state.NextOffset > 0 && errors.Is(err, baiduapi.ErrDuplicatePageItem)) {
					state.NextOffset = 0
					state.NextPageBoundary = nil
				}
			}
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.saveCloudScanState(state)
			if retryable {
				if !waitForInventoryRetry(ctx, state.RetryAtUTC) {
					continue
				}
				continue
			}
			return
		}
		backoff = 5 * time.Second
		state.RetryDelayMillis = 0
		state.RetryBackoffBaseMillis = 0
		state.RetryAtUTC = nil
		if hasMore && nextBoundary == nil {
			d.cloudMutex.RUnlock()
			state.Status, state.Phase = "failed", "failed"
			state.LastError = "分页响应缺少下一页边界项目"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.saveCloudScanState(state)
			return
		}
		pageState := state
		if nextBoundary == nil {
			pageState.NextPageBoundary = nil
		} else {
			pageState.NextPageBoundary = cloudInventoryPageBoundary(*nextBoundary)
		}
		observed := time.Now().UTC()
		entries := make([]taskstore.CloudInventoryEntry, 0, len(files))
		for _, f := range files {
			entries = append(entries, taskstore.CloudInventoryEntry{FsID: f.fsid, Path: f.path, Name: f.name, Directory: f.isDir, Size: f.size, ProviderMtime: f.mtime, ContentMD5: f.md5})
		}
		var next *int
		if hasMore {
			v := int(state.NextOffset) + len(entries)
			next = &v
		}
		if d.inventoryPageCommitHook != nil {
			d.inventoryPageCommitHook(directory)
		}
		if err = d.queue.store.UpsertCloudInventoryScanPage(&pageState, directory, entries, observed, next, nil); err != nil {
			d.cloudMutex.RUnlock()
			if state.NextOffset > 0 && errors.Is(err, taskstore.ErrCloudInventoryDuplicateAcrossPages) {
				restarted, restartErr := d.queue.store.RestartCloudScanBoundary(state, time.Now().UTC())
				if restartErr == nil {
					state = restarted
					continue
				}
				state = restarted
				state.Status, state.Phase = "failed", "failed"
				state.LastError = errorSummary(restartErr.Error())
				state.RetryDelayMillis, state.RetryBackoffBaseMillis, state.RetryAtUTC = 0, 0, nil
				state.UpdatedAtUTC = time.Now().UTC()
				_ = d.saveCloudScanState(state)
				return
			}
			state.Status = "failed"
			state.Phase = "failed"
			state.RetryDelayMillis, state.RetryBackoffBaseMillis, state.RetryAtUTC = 0, 0, nil
			state.LastError = errorSummary(err.Error())
			if state.NextOffset > 0 && errors.Is(err, taskstore.ErrCloudInventoryDuplicateAcrossPages) {
				state.NextOffset = 0
				state.NextPageBoundary = nil
			}
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.saveCloudScanState(state)
			return
		}
		state = pageState
		// Keep cache persistence ordered with remote mutations. Otherwise an old
		// listing can overwrite the stale mark written by a newer cloud change.
		d.cloudMutex.RUnlock()
		if state.Status == "complete" {
			return
		}
		timer := time.NewTimer(cloudInventoryPageInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			continue
		case <-timer.C:
		}
	}
}

func (d *Downloader) runCloudInventoryRefresh(ctx context.Context, state taskstore.CloudInventoryRefreshState, done func()) {
	defer done()
	defer func() {
		d.inventoryScanMutex.Lock()
		d.inventoryScanCancel = nil
		d.inventoryScanKind = ""
		d.inventoryScanMutex.Unlock()
	}()
	backoff := inventoryRetryBase(state.RetryBackoffBaseMillis)
	if state.RetryBackoffBaseMillis == 0 {
		backoff = inventoryRetryBase(state.RetryDelayMillis)
	}
	for state.PendingCount > 0 {
		if err := ctx.Err(); err != nil {
			state.Status, state.Phase = "paused", "paused"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.queue.store.SaveCloudInventoryRefreshState(state)
			return
		}
		if !waitForInventoryRetry(ctx, state.RetryAtUTC) {
			state.Status, state.Phase = "paused", "paused"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.queue.store.SaveCloudInventoryRefreshState(state)
			return
		}
		if state.RetryAtUTC != nil {
			state.RetryAtUTC = nil
			state.Phase = "listing"
			state.UpdatedAtUTC = time.Now().UTC()
			if err := d.queue.store.SaveCloudInventoryRefreshState(state); err != nil {
				return
			}
		}
		directory, queueErr := d.queue.store.CloudInventoryRefreshQueueHead(state.AccountFingerprint, state.RunID)
		if queueErr != nil || directory != state.CurrentDirectory || !strings.HasPrefix(directory, state.RootPath) || (state.RootPath != "/" && directory != state.RootPath && !strings.HasPrefix(directory, state.RootPath+"/")) {
			state.Status, state.Phase = "failed", "failed"
			state.RetryDelayMillis, state.RetryBackoffBaseMillis, state.RetryAtUTC = 0, 0, nil
			if queueErr != nil {
				state.LastError = errorSummary(queueErr.Error())
			} else {
				state.LastError = "持久化局部刷新队列与当前目录不一致"
			}
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.queue.store.SaveCloudInventoryRefreshState(state)
			return
		}
		d.cloudMutex.RLock()
		client, _, err := d.authorizedClient(ctx)
		if err == nil && client.Fingerprint() != state.AccountFingerprint {
			err = ErrAuthenticationWaiting
		}
		var files []structFileInfo
		var hasMore bool
		var nextBoundary *baiduapi.FileInfo
		boundaryMismatch := false
		if err == nil {
			reservedAt := time.Now().UTC()
			used, reserveErr := d.queue.store.ReserveCloudInventoryRefreshRequest(state.AccountFingerprint, state.RunID, reservedAt)
			if errors.Is(reserveErr, taskstore.ErrCloudInventoryRefreshRequestBudgetReached) {
				d.cloudMutex.RUnlock()
				state.Status, state.Phase = "paused", "request_budget_reached"
				state.LastError = ""
				state.UpdatedAtUTC = reservedAt
				_ = d.queue.store.SaveCloudInventoryRefreshState(state)
				return
			}
			if reserveErr != nil {
				d.cloudMutex.RUnlock()
				state.Status, state.Phase = "failed", "failed"
				state.LastError = errorSummary(reserveErr.Error())
				state.UpdatedAtUTC = reservedAt
				_ = d.queue.store.SaveCloudInventoryRefreshState(state)
				return
			}
			state.RequestsUsed = used
			state.UpdatedAtUTC = reservedAt
			page, pageMore, boundary, pageErr := client.ListDirPageWithBoundary(directory, int(state.NextOffset), cloudInventoryPageSize)
			hasMore = pageMore
			nextBoundary = boundary
			err = pageErr
			if err == nil && state.NextOffset > 0 && (len(page) == 0 || !cloudInventoryPageBoundaryMatches(state.NextPageBoundary, page[0])) {
				boundaryMismatch = true
				err = fmt.Errorf("网盘目录分页边界发生变化；当前目录将从第一页重新核对")
			}
			if err == nil {
				files = make([]structFileInfo, 0, len(page))
				for _, f := range page {
					if !validListingFile(directory, f) {
						err = ErrIntakeConflict
						break
					}
					files = append(files, structFileInfo{fsid: f.FsID, path: f.Path, name: f.ServerFilename, isDir: f.IsDir == 1, size: f.Size, mtime: f.Mtime, md5: f.MD5})
				}
				if err == nil && boundary != nil && !validListingFile(directory, *boundary) {
					err = ErrIntakeConflict
				}
			}
		}
		if err != nil {
			d.cloudMutex.RUnlock()
			if ctx.Err() != nil {
				state.Status, state.Phase = "paused", "paused"
				state.LastError = ""
				state.UpdatedAtUTC = time.Now().UTC()
				_ = d.queue.store.SaveCloudInventoryRefreshState(state)
				return
			}
			// Full scans already treat duplicate provider IDs on an offset page as
			// observable pagination drift; scoped refreshes must recheck the same way.
			if state.NextOffset > 0 && (boundaryMismatch || errors.Is(err, baiduapi.ErrDuplicatePageItem)) {
				reason := "page_boundary_mismatch"
				var restartFSID *int64
				if errors.Is(err, baiduapi.ErrDuplicatePageItem) {
					reason = "provider_duplicate_page_id"
					var duplicate *baiduapi.DuplicatePageItemError
					if errors.As(err, &duplicate) {
						restartFSID = &duplicate.FsID
					}
				}
				restarted, restartErr := d.queue.store.RestartCloudInventoryRefreshBoundaryWithEvidence(state, time.Now().UTC(), reason, restartFSID)
				if restartErr == nil {
					state = restarted
					continue
				}
				state = restarted
				state.Status, state.Phase = "failed", "failed"
				state.LastError = errorSummary(restartErr.Error())
				state.RetryDelayMillis, state.RetryBackoffBaseMillis, state.RetryAtUTC = 0, 0, nil
				state.UpdatedAtUTC = time.Now().UTC()
				_ = d.queue.store.SaveCloudInventoryRefreshState(state)
				return
			}
			state.LastError = errorSummary(err.Error())
			retryable := errors.Is(err, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) || isCloudInventoryRetryable(err)
			delay := time.Duration(0)
			if retryable {
				state.Phase = "backoff"
				delay = inventoryRetryDelay(err, backoff)
				state.RetryDelayMillis = delay.Milliseconds()
				backoff = nextInventoryRetryBase(backoff)
				state.RetryBackoffBaseMillis = backoff.Milliseconds()
				retryAt := time.Now().UTC().Add(delay)
				state.RetryAtUTC = &retryAt
			} else {
				state.Status = "failed"
				state.Phase = "failed"
				state.RetryDelayMillis = 0
				state.RetryBackoffBaseMillis = 0
				state.RetryAtUTC = nil
				if boundaryMismatch {
					state.NextOffset = 0
					state.NextPageBoundary = nil
				}
			}
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.queue.store.SaveCloudInventoryRefreshState(state)
			if retryable {
				if !waitForInventoryRetry(ctx, state.RetryAtUTC) {
					continue
				}
				continue
			}
			return
		}
		backoff = 5 * time.Second
		state.RetryDelayMillis = 0
		state.RetryBackoffBaseMillis = 0
		state.RetryAtUTC = nil
		if hasMore && nextBoundary == nil {
			d.cloudMutex.RUnlock()
			state.Status, state.Phase = "failed", "failed"
			state.LastError = "分页响应缺少下一页边界项目"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.queue.store.SaveCloudInventoryRefreshState(state)
			return
		}
		pageState := state
		if nextBoundary == nil {
			pageState.NextPageBoundary = nil
		} else {
			pageState.NextPageBoundary = cloudInventoryPageBoundary(*nextBoundary)
		}
		observed := time.Now().UTC()
		entries := make([]taskstore.CloudInventoryEntry, 0, len(files))
		for _, f := range files {
			entries = append(entries, taskstore.CloudInventoryEntry{FsID: f.fsid, Path: f.path, Name: f.name, Directory: f.isDir, Size: f.size, ProviderMtime: f.mtime, ContentMD5: f.md5})
		}
		var next *int
		if hasMore {
			v := int(state.NextOffset) + len(entries)
			next = &v
		}
		if d.inventoryPageCommitHook != nil {
			d.inventoryPageCommitHook(directory)
		}
		if err = d.queue.store.UpsertCloudInventoryRefreshPage(&pageState, directory, entries, observed, next, nil); err != nil {
			d.cloudMutex.RUnlock()
			// A repeated ID across staged pages is the store-level equivalent of
			// a repeated provider page item, so retry this offset sequence from page one.
			if state.NextOffset > 0 && errors.Is(err, taskstore.ErrCloudInventoryDuplicateAcrossPages) {
				var duplicate *taskstore.CloudInventoryDuplicateAcrossPagesError
				if !errors.As(err, &duplicate) {
					state.Status, state.Phase = "failed", "failed"
					state.LastError = errorSummary(err.Error())
					state.UpdatedAtUTC = time.Now().UTC()
					_ = d.queue.store.SaveCloudInventoryRefreshState(state)
					return
				}
				restarted, restartErr := d.queue.store.RestartCloudInventoryRefreshBoundaryWithEvidence(state, time.Now().UTC(), "staged_page_duplicate_id", &duplicate.FsID)
				if restartErr == nil {
					state = restarted
					continue
				}
				state = restarted
				state.Status, state.Phase = "failed", "failed"
				state.LastError = errorSummary(restartErr.Error())
				state.RetryDelayMillis, state.RetryBackoffBaseMillis, state.RetryAtUTC = 0, 0, nil
				state.UpdatedAtUTC = time.Now().UTC()
				_ = d.queue.store.SaveCloudInventoryRefreshState(state)
				return
			}
			state.Status, state.Phase = "failed", "failed"
			state.RetryDelayMillis, state.RetryBackoffBaseMillis, state.RetryAtUTC = 0, 0, nil
			state.LastError = errorSummary(err.Error())
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.queue.store.SaveCloudInventoryRefreshState(state)
			return
		}
		state = pageState
		d.cloudMutex.RUnlock()
		if state.Status == "complete" {
			return
		}
		timer := time.NewTimer(cloudInventoryPageInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			continue
		case <-timer.C:
		}
	}
}

type structFileInfo struct {
	fsid        int64
	path, name  string
	isDir       bool
	size, mtime int64
	md5         string
}

func isCloudInventoryRetryable(err error) bool { return baiduapi.IsRetryable(err) }

func inventoryRetryBase(delayMillis int64) time.Duration {
	if delayMillis <= 0 || delayMillis > int64(time.Hour/time.Millisecond) {
		return 5 * time.Second
	}
	return time.Duration(delayMillis) * time.Millisecond
}

func inventoryRetryDelay(err error, base time.Duration) time.Duration {
	if base <= 0 {
		base = 5 * time.Second
	}
	if base > time.Hour {
		base = time.Hour
	}
	var apiErr *baiduapi.APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > base {
		base = apiErr.RetryAfter
	}
	if base > time.Hour {
		return time.Hour
	}
	return base
}

func nextInventoryRetryBase(base time.Duration) time.Duration {
	next := base * 2
	if next > time.Minute || next <= 0 {
		return time.Minute
	}
	return next
}

func waitForInventoryRetry(ctx context.Context, retryAt *time.Time) bool {
	if retryAt == nil {
		return true
	}
	for {
		wait := utcInventoryRetryWait(*retryAt, time.Now().UTC())
		if wait <= 0 {
			return true
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return false
		case <-timer.C:
			// Re-read the UTC deadline after each bounded timer step. A wall-clock
			// adjustment must not leave a persisted retry sleeping on stale time.
		}
	}
}

func utcInventoryRetryWait(retryAt, now time.Time) time.Duration {
	wait := retryAt.Sub(now)
	if wait <= 0 {
		return 0
	}
	if wait > 30*time.Second {
		return 30 * time.Second
	}
	return wait
}
