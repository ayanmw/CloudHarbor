package downloader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

const duplicateHashInterval = 2 * time.Second
const duplicateScanRetryMax = time.Minute
const duplicateCandidateBatchSize = 100
const duplicateHashCheckpointBytes int64 = 16 << 20

func loadDuplicateCandidateBatch(store *taskstore.Store, account string, afterFsID int64) ([]taskstore.CloudInventoryEntry, error) {
	return store.ListCloudDuplicateHashCandidatePage(account, afterFsID, duplicateCandidateBatchSize)
}

func (d *Downloader) StartCloudDuplicateScan(ctx context.Context) (taskstore.CloudDuplicateScanState, error) {
	if err := d.reserveCloudInventoryScan("下载队列正在维护，暂不能启动重复扫描"); err != nil {
		return taskstore.CloudDuplicateScanState{}, err
	}
	d.duplicateScanMutex.Lock()
	if d.duplicateScanCancel != nil || d.duplicateScanStarting {
		d.duplicateScanMutex.Unlock()
		d.inventoryScanMutex.Lock()
		d.inventoryScanStarting = false
		d.inventoryScanMutex.Unlock()
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
			d.inventoryScanMutex.Lock()
			d.inventoryScanStarting = false
			d.inventoryScanMutex.Unlock()
		}
	}()
	stateInventory, err := d.CloudInventoryScanStatus()
	if err != nil {
		return taskstore.CloudDuplicateScanState{}, err
	}
	if stateInventory.Status != "complete" {
		return taskstore.CloudDuplicateScanState{}, ErrCloudInventoryIncomplete
	}
	inventoryClient, _, err := d.authorizedClient(d.ctx)
	if err != nil {
		return taskstore.CloudDuplicateScanState{}, err
	}
	coverage, err := d.queue.store.CloudDirectorySize(inventoryClient.Fingerprint(), "/")
	if err != nil {
		return taskstore.CloudDuplicateScanState{}, err
	}
	if !coverage.Complete {
		return taskstore.CloudDuplicateScanState{}, ErrCloudInventoryIncomplete
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
	refreshState, refreshErr := d.queue.store.LoadCloudInventoryRefreshState(account)
	if refreshErr == nil && (refreshState.Status == "running" || refreshState.Status == "paused") && refreshState.PendingCount > 0 {
		done()
		return taskstore.CloudDuplicateScanState{}, fmt.Errorf("网盘目录缓存刷新仍有待处理目录，请先完成或恢复刷新")
	}
	if refreshErr != nil && !errors.Is(refreshErr, sql.ErrNoRows) {
		done()
		return taskstore.CloudDuplicateScanState{}, refreshErr
	}
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
		estimate, e := d.queue.store.EstimateCloudDuplicateHashCandidates(account, 0)
		if e != nil {
			done()
			return state, e
		}
		state = taskstore.CloudDuplicateScanState{AccountFingerprint: account, RunID: id, Status: "running", Phase: "hashing", TotalFiles: estimate.Files, TotalBytes: estimate.EstimatedBytes, StartedAtUTC: now, UpdatedAtUTC: now}
	} else {
		estimate, e := d.queue.store.EstimateCloudDuplicateHashCandidates(account, state.NextFsID)
		if e != nil {
			done()
			return state, e
		}
		state.TotalFiles = state.ProcessedFiles + estimate.Files
		state.TotalBytes = state.ProcessedBytes + estimate.EstimatedBytes
	}
	state.Status = "running"
	now := time.Now().UTC()
	if state.RetryAtUTC != nil && state.RetryAtUTC.After(now) {
		state.Phase = "backoff"
	} else {
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
	d.inventoryScanMutex.Lock()
	d.inventoryScanCancel = cancel
	d.inventoryScanKind = "duplicate"
	d.inventoryScanStarting = false
	d.inventoryScanMutex.Unlock()
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
	state, err := d.queue.store.LoadCloudDuplicateScanState(client.Fingerprint())
	if errors.Is(err, sql.ErrNoRows) {
		return taskstore.CloudDuplicateScanState{AccountFingerprint: client.Fingerprint(), Status: "idle", Phase: "idle"}, nil
	}
	return state, err
}

func (d *Downloader) CloudDuplicateScanEventPage(ctx context.Context, before int64, limit int) ([]taskstore.CloudDuplicateScanEvent, int64, bool, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	return d.queue.store.CloudDuplicateScanEventPage(client.Fingerprint(), before, limit)
}

func (d *Downloader) CloudDuplicateScanRuns(ctx context.Context, before int64, filter taskstore.CloudDuplicateScanRunFilter, limit int) ([]taskstore.CloudDuplicateScanRun, int64, bool, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	return d.queue.store.CloudDuplicateScanRunPage(client.Fingerprint(), before, filter, limit)
}

func (d *Downloader) CloudDuplicateScanRunsExport(ctx context.Context, filter taskstore.CloudDuplicateScanRunFilter) ([]taskstore.CloudDuplicateScanRun, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, err
	}
	return d.queue.store.ExportCloudDuplicateScanRuns(ctx, client.Fingerprint(), filter)
}

func (d *Downloader) CloudDuplicateScanRunEventPage(ctx context.Context, runID string, before int64, limit int) ([]taskstore.CloudDuplicateScanEvent, int64, bool, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	return d.queue.store.CloudDuplicateScanRunEvents(client.Fingerprint(), runID, before, limit)
}

func (d *Downloader) CloudDuplicateScanEvidenceExport(ctx context.Context, runID string) (taskstore.CloudDuplicateScanEvidenceBundle, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return taskstore.CloudDuplicateScanEvidenceBundle{}, err
	}
	return d.queue.store.CloudDuplicateScanEvidenceExport(ctx, client.Fingerprint(), runID)
}

func (d *Downloader) CloudDuplicateScanEstimate() (taskstore.CloudDuplicateHashCandidateEstimate, error) {
	client, _, err := d.authorizedClient(d.ctx)
	if err != nil {
		return taskstore.CloudDuplicateHashCandidateEstimate{}, err
	}
	state, err := d.queue.store.LoadCloudScanState(client.Fingerprint())
	if err != nil {
		return taskstore.CloudDuplicateHashCandidateEstimate{}, err
	}
	if state.Status != "complete" {
		return taskstore.CloudDuplicateHashCandidateEstimate{}, ErrCloudInventoryIncomplete
	}
	coverage, err := d.queue.store.CloudDirectorySize(client.Fingerprint(), "/")
	if err != nil {
		return taskstore.CloudDuplicateHashCandidateEstimate{}, err
	}
	if !coverage.Complete {
		return taskstore.CloudDuplicateHashCandidateEstimate{}, ErrCloudInventoryIncomplete
	}
	afterFsID := int64(0)
	duplicateState, stateErr := d.queue.store.LoadCloudDuplicateScanState(client.Fingerprint())
	if stateErr != nil && !errors.Is(stateErr, sql.ErrNoRows) {
		return taskstore.CloudDuplicateHashCandidateEstimate{}, stateErr
	}
	if stateErr == nil && duplicateState.Status != "complete" {
		afterFsID = duplicateState.NextFsID
	}
	return d.queue.store.EstimateCloudDuplicateHashCandidates(client.Fingerprint(), afterFsID)
}
func (d *Downloader) CloudDuplicatePreview(_ context.Context, offset, limit int) ([]taskstore.CloudDuplicateGroup, int, int, error) {
	client, _, err := d.authorizedClient(d.ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	return d.queue.store.ListVerifiedCloudDuplicateGroupsPage(client.Fingerprint(), offset, limit)
}

func (d *Downloader) runCloudDuplicateScan(ctx context.Context, state taskstore.CloudDuplicateScanState, done func()) {
	defer done()
	defer func() {
		d.duplicateScanMutex.Lock()
		d.duplicateScanCancel = nil
		d.duplicateScanMutex.Unlock()
		d.inventoryScanMutex.Lock()
		if d.inventoryScanKind == "duplicate" {
			d.inventoryScanCancel = nil
			d.inventoryScanKind = ""
		}
		d.inventoryScanMutex.Unlock()
	}()
	retryDelay := time.Duration(state.RetryBackoffBaseMillis) * time.Millisecond
	if retryDelay <= 0 {
		retryDelay = time.Duration(state.RetryDelayMillis) * time.Millisecond
	}
	if retryDelay <= 0 {
		retryDelay = d.duplicateScanRetryBase
	}
	if retryDelay <= 0 {
		retryDelay = 5 * time.Second
	}
	if state.Phase == "backoff" && state.RetryAtUTC != nil {
		if !waitForInventoryRetry(ctx, state.RetryAtUTC) {
			state.Status = "paused"
			state.Phase = "paused"
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.queue.store.SaveCloudDuplicateScanState(state, true)
			return
		}
		state.Phase = "hashing"
		state.LastError = ""
		state.RetryAtUTC = nil
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
			state.TotalFiles = state.ProcessedFiles
			state.TotalBytes = state.ProcessedBytes
			state.UpdatedAtUTC = time.Now().UTC()
			_ = d.queue.store.SaveCloudDuplicateScanState(state, true)
			return
		}
		item := candidates[0]
		if state.CurrentFsID != item.FsID || state.CurrentPath != item.Path || state.CurrentName != item.Name || state.CurrentSize != item.Size || state.CurrentProviderMtime != item.ProviderMtime || state.CurrentContentMD5 != item.ContentMD5 {
			if err := initializeDuplicateHashCheckpoint(&state, item); err != nil {
				return
			}
			state.Status, state.Phase, state.UpdatedAtUTC = "running", "hashing", time.Now().UTC()
			if err := d.queue.store.SaveCloudDuplicateScanState(state, false); err != nil {
				return
			}
		}
		d.cloudMutex.RLock()
		client, _, err := d.authorizedClient(ctx)
		if err == nil && client.Fingerprint() != state.AccountFingerprint {
			err = ErrAuthenticationWaiting
		}
		if err == nil {
			var digest string
			digest, err = hashCloudFileResumable(ctx, client, item, &state, func() error {
				state.UpdatedAtUTC = time.Now().UTC()
				return d.queue.store.SaveCloudDuplicateScanState(state, false)
			})
			if err == nil {
				current, e := client.GetFileInfo(item.FsID)
				if e != nil {
					if errors.Is(e, baiduapi.ErrFileMetadataMissing) {
						err = &cloudInventoryIdentityUnavailable{cause: e}
					} else {
						err = e
					}
				} else {
					err = cloudInventoryIdentityError(item, current)
					if err == nil {
						err = d.queue.store.CompleteCloudDuplicateCandidate(state.AccountFingerprint, state, item, digest, time.Now().UTC())
					}
				}
			}
		}
		reconciled := false
		var changed *cloudInventoryIdentityChanged
		var unavailable *cloudInventoryIdentityUnavailable
		if errors.As(err, &changed) || errors.As(err, &unavailable) {
			var observed *taskstore.CloudInventoryEntry
			var providerObservedAt time.Time
			if changed != nil {
				entry := taskstore.CloudInventoryEntry{FsID: changed.current.FsID, Path: changed.current.Path, Name: changed.current.ServerFilename, Directory: changed.current.IsDir != 0, Size: changed.current.Size, ProviderMtime: changed.current.Mtime, ContentMD5: changed.current.MD5}
				observed = &entry
				providerObservedAt = changed.observedAt
			}
			err = d.queue.store.ReconcileCloudDuplicateCandidateChange(state.AccountFingerprint, state, item, observed, providerObservedAt, time.Now().UTC())
			reconciled = err == nil
		}
		d.cloudMutex.RUnlock()
		if reconciled {
			return
		}
		if err != nil {
			if ctx.Err() != nil {
				state.Status = "paused"
				state.Phase = "paused"
				state.UpdatedAtUTC = time.Now().UTC()
				_ = d.queue.store.SaveCloudDuplicateScanState(state, true)
				return
			}
			if retryableDuplicateScanError(err) {
				waitDelay := duplicateScanRetryDelay(retryDelay, err)
				if state.Phase != "backoff" {
					state.Status = "running"
					state.Phase = "backoff"
					state.LastError = errorSummary(err.Error())
					state.RetryDelayMillis = int64(waitDelay / time.Millisecond)
					retryDelay = nextDuplicateRetryDelay(retryDelay)
					state.RetryBackoffBaseMillis = int64(retryDelay / time.Millisecond)
					retryAt := time.Now().UTC().Add(waitDelay)
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
				if state.RetryAtUTC == nil {
					retryAt := time.Now().UTC().Add(waitDelay)
					state.RetryAtUTC = &retryAt
				}
				if !waitForInventoryRetry(ctx, state.RetryAtUTC) {
					continue
				}
				retryDelay = time.Duration(state.RetryBackoffBaseMillis) * time.Millisecond
				state.Phase = "hashing"
				state.LastError = ""
				state.RetryAtUTC = nil
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
		state.RetryBackoffBaseMillis = 0
		state.RetryAtUTC = nil
		state.NextFsID = item.FsID
		state.ProcessedFiles++
		state.ProcessedBytes += item.Size
		clearDuplicateHashCheckpoint(&state)
		state.UpdatedAtUTC = time.Now().UTC()
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

func duplicateScanRetryDelay(local time.Duration, err error) time.Duration {
	delay := local
	var apiErr *baiduapi.APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > delay {
		delay = apiErr.RetryAfter
	}
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func retryableDuplicateScanError(err error) bool {
	return errors.Is(err, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) || baiduapi.IsRetryable(err)
}
func (d *Downloader) failDuplicateScan(s *taskstore.CloudDuplicateScanState, err error) {
	s.Status = "failed"
	s.Phase = "failed"
	s.LastError = errorSummary(err.Error())
	s.RetryDelayMillis = 0
	s.RetryBackoffBaseMillis = 0
	s.RetryAtUTC = nil
	s.UpdatedAtUTC = time.Now().UTC()
	_ = d.queue.store.SaveCloudDuplicateScanState(*s, true)
}
func sameInventoryIdentity(c taskstore.CloudInventoryEntry, f baiduapi.FileInfo) bool {
	return cloudInventoryIdentityError(c, f) == nil
}

type cloudInventoryIdentityChanged struct {
	current    baiduapi.FileInfo
	observedAt time.Time
}

func (e *cloudInventoryIdentityChanged) Error() string {
	return "cloud file identity changed during duplicate scan"
}

type cloudInventoryIdentityUnavailable struct{ cause error }

func (e *cloudInventoryIdentityUnavailable) Error() string {
	return "cloud file metadata did not uniquely identify the candidate"
}
func (e *cloudInventoryIdentityUnavailable) Unwrap() error { return e.cause }

func cloudInventoryIdentityError(c taskstore.CloudInventoryEntry, f baiduapi.FileInfo) error {
	if c.FsID != f.FsID || c.Path != f.Path || c.Name != f.ServerFilename || c.Size != f.Size || c.ProviderMtime != f.Mtime || f.IsDir != 0 {
		return &cloudInventoryIdentityChanged{current: f, observedAt: time.Now().UTC()}
	}
	if c.ContentMD5 != "" && f.MD5 != "" && !strings.EqualFold(c.ContentMD5, f.MD5) {
		return &cloudInventoryIdentityChanged{current: f, observedAt: time.Now().UTC()}
	}
	return nil
}
func clearDuplicateHashCheckpoint(state *taskstore.CloudDuplicateScanState) {
	state.CurrentFsID, state.CurrentPath, state.CurrentName = 0, "", ""
	state.CurrentSize, state.CurrentProviderMtime, state.CurrentBytes = 0, 0, 0
	state.CurrentContentMD5 = ""
	state.CurrentSHA256State = []byte{}
}

func initializeDuplicateHashCheckpoint(state *taskstore.CloudDuplicateScanState, item taskstore.CloudInventoryEntry) error {
	clearDuplicateHashCheckpoint(state)
	state.CurrentFsID, state.CurrentPath, state.CurrentName = item.FsID, item.Path, item.Name
	state.CurrentSize, state.CurrentProviderMtime, state.CurrentContentMD5 = item.Size, item.ProviderMtime, item.ContentMD5
	var err error
	state.CurrentSHA256State, err = sha256.New().(encoding.BinaryMarshaler).MarshalBinary()
	return err
}

func hashCloudFile(ctx context.Context, c *baiduapi.Client, item taskstore.CloudInventoryEntry) (string, error) {
	state := taskstore.CloudDuplicateScanState{}
	if err := initializeDuplicateHashCheckpoint(&state, item); err != nil {
		return "", err
	}
	return hashCloudFileResumable(ctx, c, item, &state, func() error { return nil })
}

func hashCloudFileResumable(ctx context.Context, c *baiduapi.Client, item taskstore.CloudInventoryEntry, state *taskstore.CloudDuplicateScanState, checkpoint func() error) (string, error) {
	info, err := c.GetFileInfo(item.FsID)
	if err != nil {
		if errors.Is(err, baiduapi.ErrFileMetadataMissing) {
			return "", &cloudInventoryIdentityUnavailable{cause: err}
		}
		return "", err
	}
	if err = cloudInventoryIdentityError(item, info); err != nil {
		return "", err
	}
	if state.CurrentFsID == item.FsID && state.CurrentBytes > 0 {
		if state.CurrentContentMD5 != "" && info.MD5 != "" && !strings.EqualFold(state.CurrentContentMD5, info.MD5) {
			return "", &cloudInventoryIdentityChanged{current: info, observedAt: time.Now().UTC()}
		}
		if (state.CurrentContentMD5 == "") != (info.MD5 == "") {
			// A legacy or incomplete checkpoint cannot prove that its saved prefix
			// belongs to the currently observed file; restart the hash from byte 0.
			state.CurrentBytes = 0
			state.CurrentSHA256State = nil
		}
	}
	h := sha256.New()
	offset := int64(0)
	if state.CurrentFsID == item.FsID && state.CurrentBytes > 0 && len(state.CurrentSHA256State) > 0 {
		unmarshaler, ok := h.(encoding.BinaryUnmarshaler)
		if ok && unmarshaler.UnmarshalBinary(state.CurrentSHA256State) == nil {
			offset = state.CurrentBytes
		}
	}
	if offset == 0 {
		if err = initializeDuplicateHashCheckpoint(state, item); err != nil {
			return "", err
		}
		state.CurrentContentMD5 = info.MD5
	}
	if offset == item.Size {
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	traceID, err := newSourceOperationID()
	if err != nil {
		return "", err
	}
	// Hash reads share the HTTP download helpers, but are not local downloads.
	c = c.WithDownloadTrace("hash-"+traceID, state.RunID, item.FsID, item.Path)
	link, err := c.GetDownloadLink(item.FsID)
	if err != nil {
		if errors.Is(err, baiduapi.ErrFileMetadataMissing) || errors.Is(err, baiduapi.ErrDownloadLinkMissing) {
			return "", &cloudInventoryIdentityUnavailable{cause: err}
		}
		return "", err
	}
	for attempt := 0; attempt < 2; attempt++ {
		body, length, downloadErr := c.WithContext(ctx).DownloadFileWithRange(link, offset)
		if errors.Is(downloadErr, baiduapi.ErrRangeIgnored) {
			offset = 0
			h.Reset()
			if err = initializeDuplicateHashCheckpoint(state, item); err != nil {
				return "", err
			}
			state.UpdatedAtUTC = time.Now().UTC()
			if err = checkpoint(); err != nil {
				return "", err
			}
			continue
		}
		if downloadErr != nil {
			return "", downloadErr
		}
		if length >= 0 && length != item.Size-offset {
			body.Close()
			return "", fmt.Errorf("cloud file response length does not match remaining size")
		}
		buf := make([]byte, 128*1024)
		nextCheckpoint := ((offset / duplicateHashCheckpointBytes) + 1) * duplicateHashCheckpointBytes
		for {
			if err = ctx.Err(); err != nil {
				break
			}
			n, readErr := body.Read(buf)
			if n > 0 {
				if int64(n) > item.Size-offset {
					err = fmt.Errorf("cloud file exceeds expected size")
					break
				}
				if _, err = h.Write(buf[:n]); err != nil {
					break
				}
				offset += int64(n)
				state.CurrentBytes = offset
				if offset >= nextCheckpoint {
					state.CurrentSHA256State, err = h.(encoding.BinaryMarshaler).MarshalBinary()
					if err != nil {
						break
					}
					state.UpdatedAtUTC = time.Now().UTC()
					if err = checkpoint(); err != nil {
						break
					}
					nextCheckpoint = ((offset / duplicateHashCheckpointBytes) + 1) * duplicateHashCheckpointBytes
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					err = readErr
				}
				break
			}
		}
		body.Close()
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		state.CurrentBytes = offset
		state.CurrentSHA256State, _ = h.(encoding.BinaryMarshaler).MarshalBinary()
		state.UpdatedAtUTC = time.Now().UTC()
		if saveErr := checkpoint(); saveErr != nil {
			return "", saveErr
		}
		if err != nil {
			return "", err
		}
		if offset != item.Size {
			return "", fmt.Errorf("cloud file length does not match observed metadata")
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	return "", fmt.Errorf("range resume was rejected after restart")
}
