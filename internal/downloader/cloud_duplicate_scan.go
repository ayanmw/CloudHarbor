package downloader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

const duplicateHashInterval = 2 * time.Second
const duplicateScanRetryMax = time.Minute
const duplicateCandidateBatchSize = 100

func loadDuplicateCandidateBatch(store *taskstore.Store, account string, afterFsID int64) ([]taskstore.CloudInventoryEntry, error) {
	return store.ListCloudDuplicateHashCandidatePage(account, afterFsID, duplicateCandidateBatchSize)
}

func (d *Downloader) StartCloudDuplicateScan(ctx context.Context) (taskstore.CloudDuplicateScanState, error) {
	d.duplicateScanMutex.Lock()
	if d.duplicateScanCancel != nil || d.duplicateScanStarting {
		d.duplicateScanMutex.Unlock()
		return taskstore.CloudDuplicateScanState{}, fmt.Errorf("重复文件扫描已在运行")
	}
	d.duplicateScanStarting = true
	d.duplicateScanMutex.Unlock()
	started := false
	defer func() {
		if !started {
			d.duplicateScanMutex.Lock()
			d.duplicateScanStarting = false
			d.duplicateScanMutex.Unlock()
		}
	}()
	stateInventory, err := d.CloudInventoryScanStatus()
	if err != nil {
		return taskstore.CloudDuplicateScanState{}, err
	}
	if stateInventory.Status != "complete" {
		return taskstore.CloudDuplicateScanState{}, fmt.Errorf("请先完成全盘列表扫描")
	}
	// A scan outlives the HTTP request that starts it; bind it to the downloader
	// lifetime so a client disconnect after 202 Accepted does not pause the scan.
	scoped, done, err := d.startIntake(d.ctx)
	if err != nil {
		return taskstore.CloudDuplicateScanState{}, err
	}
	client, _, err := d.authorizedClient(scoped)
	if err != nil {
		done()
		return taskstore.CloudDuplicateScanState{}, err
	}
	account := client.Fingerprint()
	state, err := d.queue.store.LoadCloudDuplicateScanState(account)
	fresh := errors.Is(err, sql.ErrNoRows)
	if err != nil && !fresh {
		done()
		return state, err
	}
	if fresh || state.Status == "complete" {
		id, e := newSourceOperationID()
		if e != nil {
			done()
			return state, e
		}
		now := time.Now().UTC()
		_, total, e := d.queue.store.ListCloudDuplicateHashCandidates(account, 0, 1)
		if e != nil {
			done()
			return state, e
		}
		state = taskstore.CloudDuplicateScanState{AccountFingerprint: account, RunID: id, Status: "running", Phase: "hashing", TotalFiles: int64(total), StartedAtUTC: now, UpdatedAtUTC: now}
	} else {
		_, total, e := d.queue.store.ListCloudDuplicateHashCandidates(account, state.NextFsID, 1)
		if e != nil {
			done()
			return state, e
		}
		state.TotalFiles = int64(total)
	}
	state.Status = "running"
	now := time.Now().UTC()
	if state.RetryAtUTC != nil && state.RetryAtUTC.After(now) {
		state.Phase = "backoff"
	} else {
		if state.RetryAtUTC != nil && state.RetryDelayMillis > 0 {
			state.RetryDelayMillis = int64(nextDuplicateRetryDelay(time.Duration(state.RetryDelayMillis)*time.Millisecond) / time.Millisecond)
		}
		state.Phase = "hashing"
		state.LastError = ""
		state.RetryAtUTC = nil
	}
	state.UpdatedAtUTC = now
	if err = d.queue.store.SaveCloudDuplicateScanState(state, true); err != nil {
		done()
		return state, err
	}
	wctx, cancel := context.WithCancel(scoped)
	d.duplicateScanMutex.Lock()
	d.duplicateScanCancel = cancel
	d.duplicateScanStarting = false
	d.duplicateScanMutex.Unlock()
	started = true
	go d.runCloudDuplicateScan(wctx, state, done)
	return state, nil
}
func (d *Downloader) PauseCloudDuplicateScan() {
	d.duplicateScanMutex.Lock()
	c := d.duplicateScanCancel
	d.duplicateScanMutex.Unlock()
	if c != nil {
		c()
	}
}
func (d *Downloader) CloudDuplicateScanStatus() (taskstore.CloudDuplicateScanState, error) {
	client, _, err := d.authorizedClient(d.ctx)
	if err != nil {
		return taskstore.CloudDuplicateScanState{}, err
	}
	return d.queue.store.LoadCloudDuplicateScanState(client.Fingerprint())
}
func (d *Downloader) CloudDuplicatePreview() ([]taskstore.CloudDuplicateGroup, error) {
	client, _, err := d.authorizedClient(d.ctx)
	if err != nil {
		return nil, err
	}
	return d.queue.store.ListVerifiedCloudDuplicateGroups(client.Fingerprint())
}

func (d *Downloader) runCloudDuplicateScan(ctx context.Context, state taskstore.CloudDuplicateScanState, done func()) {
	defer done()
	defer func() { d.duplicateScanMutex.Lock(); d.duplicateScanCancel = nil; d.duplicateScanMutex.Unlock() }()
	retryDelay := time.Duration(state.RetryDelayMillis) * time.Millisecond
	if retryDelay <= 0 {
		retryDelay = d.duplicateScanRetryBase
	}
	if retryDelay <= 0 {
		retryDelay = 5 * time.Second
	}
	if state.Phase == "backoff" && state.RetryAtUTC != nil {
		if wait := time.Until(*state.RetryAtUTC); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				state.Status = "paused"
				state.Phase = "paused"
				state.UpdatedAtUTC = time.Now().UTC()
				_ = d.queue.store.SaveCloudDuplicateScanState(state, true)
				return
			case <-timer.C:
			}
		}
		retryDelay = nextDuplicateRetryDelay(retryDelay)
		state.Phase = "hashing"
		state.LastError = ""
		state.RetryAtUTC = nil
		state.RetryDelayMillis = int64(retryDelay / time.Millisecond)
		state.UpdatedAtUTC = time.Now().UTC()
		if err := d.queue.store.SaveCloudDuplicateScanState(state, true); err != nil {
			return
		}
	}
	var candidates []taskstore.CloudInventoryEntry
	for {
		if ctx.Err() != nil {
			state.Status = "paused"
			state.Phase = "paused"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.queue.store.SaveCloudDuplicateScanState(state, true)
			return
		}
		if len(candidates) == 0 {
			var err error
			candidates, err = loadDuplicateCandidateBatch(d.queue.store, state.AccountFingerprint, state.NextFsID)
			if err != nil {
				d.failDuplicateScan(&state, err)
				return
			}
		}
		if len(candidates) == 0 {
			state.Status = "complete"
			state.Phase = "complete"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.queue.store.SaveCloudDuplicateScanState(state, true)
			return
		}
		item := candidates[0]
		d.cloudMutex.RLock()
		client, _, err := d.authorizedClient(ctx)
		if err == nil && client.Fingerprint() != state.AccountFingerprint {
			err = ErrAuthenticationWaiting
		}
		if err == nil {
			var digest string
			digest, err = hashCloudFile(ctx, client, item)
			if err == nil {
				current, e := client.GetFileInfo(item.FsID)
				if e != nil {
					err = e
				} else if !sameInventoryIdentity(item, current) {
					err = ErrIntakeStale
				} else {
					err = d.queue.store.SaveVerifiedCloudContentHash(state.AccountFingerprint, item, digest, time.Now().UTC())
				}
			}
		}
		d.cloudMutex.RUnlock()
		if err != nil {
			if retryableDuplicateScanError(err) {
				if state.Phase != "backoff" {
					state.Status = "running"
					state.Phase = "backoff"
					state.LastError = errorSummary(err.Error())
					state.RetryDelayMillis = int64(retryDelay / time.Millisecond)
					retryAt := time.Now().UTC().Add(retryDelay)
					state.RetryAtUTC = &retryAt
					state.UpdatedAtUTC = time.Now().UTC()
					if saveErr := d.queue.store.SaveCloudDuplicateScanState(state, true); saveErr != nil {
						return
					}
				} else {
					state.LastError = errorSummary(err.Error())
					state.UpdatedAtUTC = time.Now().UTC()
					if saveErr := d.queue.store.SaveCloudDuplicateScanState(state, false); saveErr != nil {
						return
					}
				}
				timer := time.NewTimer(retryDelay)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					continue
				case <-timer.C:
				}
				retryDelay = nextDuplicateRetryDelay(retryDelay)
				state.Phase = "hashing"
				state.LastError = ""
				state.RetryAtUTC = nil
				state.RetryDelayMillis = int64(retryDelay / time.Millisecond)
				state.UpdatedAtUTC = time.Now().UTC()
				if saveErr := d.queue.store.SaveCloudDuplicateScanState(state, true); saveErr != nil {
					return
				}
				continue
			}
			d.failDuplicateScan(&state, err)
			return
		}
		retryDelay = d.duplicateScanRetryBase
		if retryDelay <= 0 {
			retryDelay = 5 * time.Second
		}
		state.RetryDelayMillis = 0
		state.RetryAtUTC = nil
		state.NextFsID = item.FsID
		state.ProcessedFiles++
		state.UpdatedAtUTC = time.Now().UTC()
		if err = d.queue.store.SaveCloudDuplicateScanState(state, false); err != nil {
			return
		}
		candidates = candidates[1:]
		t := time.NewTimer(duplicateHashInterval)
		select {
		case <-ctx.Done():
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
		case <-t.C:
		}
	}
}
func nextDuplicateRetryDelay(current time.Duration) time.Duration {
	next := current * 2
	if next > duplicateScanRetryMax {
		return duplicateScanRetryMax
	}
	return next
}
func retryableDuplicateScanError(err error) bool {
	return errors.Is(err, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) || baiduapi.IsRetryable(err)
}
func (d *Downloader) failDuplicateScan(s *taskstore.CloudDuplicateScanState, err error) {
	s.Status = "failed"
	s.Phase = "failed"
	s.LastError = errorSummary(err.Error())
	s.UpdatedAtUTC = time.Now().UTC()
	_ = d.queue.store.SaveCloudDuplicateScanState(*s, true)
}
func sameInventoryIdentity(c taskstore.CloudInventoryEntry, f baiduapi.FileInfo) bool {
	return c.FsID == f.FsID && c.Path == f.Path && c.Name == f.ServerFilename && c.Size == f.Size && c.ProviderMtime == f.Mtime && f.IsDir == 0
}
func hashCloudFile(ctx context.Context, c *baiduapi.Client, item taskstore.CloudInventoryEntry) (string, error) {
	info, err := c.GetFileInfo(item.FsID)
	if err != nil {
		return "", err
	}
	if !sameInventoryIdentity(item, info) {
		return "", ErrIntakeStale
	}
	link, err := c.GetDownloadLink(item.FsID)
	if err != nil {
		return "", err
	}
	body, n, err := c.WithContext(ctx).DownloadFileWithRange(link, 0)
	if err != nil {
		return "", err
	}
	defer body.Close()
	h := sha256.New()
	copied, err := io.Copy(h, body)
	if err != nil {
		return "", err
	}
	if copied != item.Size || (n > 0 && n != item.Size) {
		return "", fmt.Errorf("cloud file length does not match observed metadata")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
