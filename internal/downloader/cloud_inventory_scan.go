package downloader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

const cloudInventoryPageSize = 100
const cloudInventoryPageInterval = time.Second

func (d *Downloader) StartCloudInventoryScan(ctx context.Context) (taskstore.CloudScanState, error) {
	d.queue.mu.Lock()
	maintenance := d.queue.maintenance
	d.queue.mu.Unlock()
	if maintenance {
		return taskstore.CloudScanState{}, fmt.Errorf("下载队列正在维护，暂不能启动全盘扫描")
	}
	d.inventoryScanMutex.Lock()
	if d.inventoryScanCancel != nil || d.inventoryScanStarting {
		d.inventoryScanMutex.Unlock()
		return taskstore.CloudScanState{}, fmt.Errorf("网盘扫描已经在运行")
	}
	d.inventoryScanStarting = true
	d.inventoryScanMutex.Unlock()
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
	state, err := d.queue.store.LoadCloudScanState(account)
	freshRun := false
	if errors.Is(err, sql.ErrNoRows) {
		runID, idErr := newSourceOperationID()
		if idErr != nil {
			done()
			return taskstore.CloudScanState{}, idErr
		}
		now := time.Now().UTC()
		state = taskstore.CloudScanState{AccountFingerprint: account, RunID: runID, Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
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
		state = taskstore.CloudScanState{AccountFingerprint: account, RunID: runID, Status: "running", Phase: "listing", CurrentDirectory: "/", StartedAtUTC: now, UpdatedAtUTC: now}
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
	state.Status = "running"
	state.Phase = "listing"
	state.LastError = ""
	state.UpdatedAtUTC = time.Now().UTC()
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
	d.inventoryScanStarting = false
	d.inventoryScanMutex.Unlock()
	started = true
	go d.runCloudInventoryScan(workerCtx, state, done)
	return state, nil
}

func (d *Downloader) PauseCloudInventoryScan() {
	d.inventoryScanMutex.Lock()
	cancel := d.inventoryScanCancel
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
	return d.queue.store.LoadCloudScanState(client.Fingerprint())
}

func (d *Downloader) ListCachedCloud(ctx context.Context, directory string, offset, limit int) ([]taskstore.CloudInventoryEntry, taskstore.CloudInventoryDirectory, int, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, taskstore.CloudInventoryDirectory{}, 0, err
	}
	return d.queue.store.ListCloudInventory(client.Fingerprint(), directory, offset, limit)
}

func (d *Downloader) GetCachedCloudDirectorySize(ctx context.Context, directory string) (taskstore.CloudDirectorySize, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return taskstore.CloudDirectorySize{}, err
	}
	return d.queue.store.CloudDirectorySize(client.Fingerprint(), directory)
}

func (d *Downloader) saveCloudScanState(state taskstore.CloudScanState) error {
	return d.queue.store.SaveCloudScanState(state.AccountFingerprint, state.RunID, state.Status, state.Phase, state.CurrentDirectory, state.NextOffset, state.PagesProcessed, state.ItemsProcessed, state.StartedAtUTC, state.UpdatedAtUTC, state.LastError)
}

func (d *Downloader) runCloudInventoryScan(ctx context.Context, state taskstore.CloudScanState, done func()) {
	defer done()
	defer func() { d.inventoryScanMutex.Lock(); d.inventoryScanCancel = nil; d.inventoryScanMutex.Unlock() }()
	backoff := 5 * time.Second
	for state.PendingCount > 0 {
		if err := ctx.Err(); err != nil {
			state.Status = "paused"
			state.Phase = "paused"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.saveCloudScanState(state)
			return
		}
		directory, queueErr := d.queue.store.CloudScanQueueHead(state.AccountFingerprint, state.RunID)
		if queueErr != nil {
			state.Status, state.Phase = "failed", "failed"
			state.LastError = errorSummary(queueErr.Error())
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.saveCloudScanState(state)
			return
		}
		if directory != state.CurrentDirectory {
			state.Status, state.Phase = "failed", "failed"
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
		if err == nil {
			page, pageMore, pageErr := client.ListDirPage(directory, int(state.NextOffset), cloudInventoryPageSize)
			hasMore = pageMore
			err = pageErr
			if err == nil {
				files = make([]structFileInfo, 0, len(page))
				for _, f := range page {
					if !validListingFile(directory, f) {
						err = ErrIntakeConflict
						break
					}
					files = append(files, structFileInfo{fsid: f.FsID, path: f.Path, name: f.ServerFilename, isDir: f.IsDir == 1, size: f.Size, mtime: f.Mtime, md5: f.MD5})
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
			state.LastError = errorSummary(err.Error())
			state.Phase = "backoff"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.saveCloudScanState(state)
			if errors.Is(err, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) || isCloudInventoryRetryable(err) {
				timer := time.NewTimer(backoff)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					continue
				case <-timer.C:
				}
				if backoff < time.Minute {
					backoff *= 2
					if backoff > time.Minute {
						backoff = time.Minute
					}
				}
				continue
			}
			state.Status = "failed"
			state.Phase = "failed"
			_ = d.saveCloudScanState(state)
			return
		}
		backoff = 5 * time.Second
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
		children := make([]string, 0)
		if !hasMore {
			for _, entry := range entries {
				if entry.Directory {
					children = append(children, entry.Path)
				}
			}
		}
		if err = d.queue.store.UpsertCloudInventoryScanPage(&state, directory, entries, observed, next, children); err != nil {
			d.cloudMutex.RUnlock()
			state.Status = "failed"
			state.Phase = "failed"
			state.LastError = errorSummary(err.Error())
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.saveCloudScanState(state)
			return
		}
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

type structFileInfo struct {
	fsid        int64
	path, name  string
	isDir       bool
	size, mtime int64
	md5         string
}

func isCloudInventoryRetryable(err error) bool { return baiduapi.IsRetryable(err) }
