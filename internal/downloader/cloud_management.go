package downloader

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type CloudMutationRequest struct {
	Operation             string             `json:"operation"`
	Expected              baiduapi.FileInfo  `json:"expected"`
	DestinationDirectory  string             `json:"destination_directory,omitempty"`
	NewName               string             `json:"new_name,omitempty"`
	RequireEmptyDirectory bool               `json:"require_empty_directory,omitempty"`
	ExpectedContentSHA256 string             `json:"expected_content_sha256,omitempty"`
	ExpectedRetained      *baiduapi.FileInfo `json:"expected_retained,omitempty"`
}

type CloudMutationResult struct {
	OperationID     string `json:"operation_id"`
	Status          string `json:"status"`
	ObjectID        int64  `json:"object_id"`
	SourcePath      string `json:"source_path,omitempty"`
	DestinationPath string `json:"destination_path,omitempty"`
	Warning         string `json:"warning,omitempty"`
}

func (d *Downloader) MutateCloud(ctx context.Context, request CloudMutationRequest) (CloudMutationResult, error) {
	target, err := validateCloudMutation(request)
	if err != nil {
		return CloudMutationResult{}, err
	}
	if d.cloudMutationAdmissionHook != nil {
		d.cloudMutationAdmissionHook()
	}
	scoped, done, err := d.startPathSensitiveOperation(ctx)
	if err != nil {
		return CloudMutationResult{}, err
	}
	defer done()
	queueDir := d.QueueDirectory()
	if request.Operation != "mkdir" && request.Expected.IsDir == 1 && insideCloud(queueDir, request.Expected.Path) {
		return CloudMutationResult{}, ErrIntakeInvalid
	}
	if (request.Operation == "move" || request.Operation == "copy") && request.Expected.IsDir == 1 && insideCloud(request.DestinationDirectory, request.Expected.Path) {
		return CloudMutationResult{}, ErrIntakeInvalid
	}
	var releaseAdmission func()
	groupedUpload := scoped.Value(uploadLeaseContextKey{}) != nil
	if groupedUpload {
		if request.Operation != "mkdir" {
			return CloudMutationResult{}, ErrIntakeInvalid
		}
		releaseAdmission, err = d.acquireUploadNamespace(scoped)
		if err != nil {
			return CloudMutationResult{}, err
		}
	} else {
		if err = d.reserveCloudInventoryScan("下载队列正在维护，暂不能执行网盘变更"); err != nil {
			return CloudMutationResult{}, err
		}
		releaseAdmission = func() { d.inventoryScanMutex.Lock(); d.inventoryScanStarting = false; d.inventoryScanMutex.Unlock() }
	}
	defer releaseAdmission()
	if groupedUpload {
		err = d.beginUploadMaintenance(scoped)
	} else {
		err = d.BeginMaintenance(scoped)
	}
	if err != nil {
		return CloudMutationResult{}, err
	}
	defer d.EndMaintenance()
	d.cloudMutex.Lock()
	defer d.cloudMutex.Unlock()
	client, epoch, err := d.authorizedCloudClient(scoped)
	if err != nil {
		return CloudMutationResult{}, err
	}
	account := client.CloudCacheNamespace()
	// Every mutation must respect unresolved source and destination evidence.
	// Acting on an ancestor can also move, remove or duplicate an isolated child.
	d.queue.mu.Lock()
	blocked := d.queue.isCloudMutationBlocked(request.Expected.FsID, request.Expected.Path)
	// An unresolved deletion can still be visible online. Such an object cannot
	// serve as the safety copy for deleting another duplicate.
	if retained := request.ExpectedRetained; retained != nil {
		blocked = blocked || d.queue.isCloudMutationBlocked(retained.FsID, retained.Path)
	}
	if target != "" {
		blocked = blocked || d.queue.isCloudMutationBlocked(0, target)
	}
	for _, paths := range []map[string]bool{d.queue.blockedRemotePaths, d.queue.blockedSources, d.queue.blockedIntakes, d.queue.blockedIntakeSources} {
		for uncertainPath := range paths {
			if uncertainPath == "" {
				continue
			}
			if (request.Expected.IsDir == 1 && request.Expected.Path != "" && insideCloud(uncertainPath, request.Expected.Path)) || (target != "" && insideCloud(uncertainPath, target)) {
				blocked = true
				break
			}
		}
	}
	d.queue.mu.Unlock()
	if blocked {
		return CloudMutationResult{}, fmt.Errorf("%w：%w", ErrIntakeConflict, errCloudMutationQuarantined)
	}
	if err = d.queue.store.InvalidateIdleDuplicateCheckpoint(account); err != nil {
		return CloudMutationResult{}, err
	}
	scanConflict, err := d.queue.store.CloudMutationScanConflict(account, mutationCachePaths(request, target))
	if err != nil {
		return CloudMutationResult{}, err
	}
	if scanConflict {
		return CloudMutationResult{}, fmt.Errorf("%w：云端缓存或重复文件扫描仍有待处理工作，请先完成或恢复扫描再执行远端操作", ErrIntakeConflict)
	}
	var beforeInfo baiduapi.FileInfo
	if request.Operation == "mkdir" {
		if err = verifyDirectory(client, request.DestinationDirectory); err != nil {
			return CloudMutationResult{}, err
		}
		if err = ensureNameAvailable(client, request.DestinationDirectory, request.NewName); err != nil {
			// Another group member can create this parent while we await the
			// namespace gate. Only a unique online directory can satisfy it;
			// unknown targets were rejected above, never inferred from existence.
			if groupedUpload && errors.Is(err, ErrIntakeConflict) {
				entries, readErr := client.ListDir(request.DestinationDirectory)
				if readErr != nil {
					return CloudMutationResult{}, readErr
				}
				var found baiduapi.FileInfo
				count := 0
				for _, entry := range entries {
					if strings.EqualFold(entry.ServerFilename, request.NewName) {
						found = entry
						count++
					}
				}
				if count == 1 && found.IsDir == 1 && found.FsID > 0 && found.Path == target && found.ServerFilename == request.NewName {
					return CloudMutationResult{Status: "existing", ObjectID: found.FsID, DestinationPath: target}, nil
				}
			}
			return CloudMutationResult{}, err
		}
	} else {
		current, readErr := client.GetFileInfo(request.Expected.FsID)
		if readErr != nil {
			return CloudMutationResult{}, readErr
		}
		if !sameCloudIdentity(request.Expected, current) {
			return CloudMutationResult{}, cleanupStale(errCleanupSourceChanged)
		}
		beforeInfo = current
		if request.ExpectedContentSHA256 != "" {
			if request.Operation != "delete" || request.Expected.IsDir != 0 || len(request.ExpectedContentSHA256) != 64 {
				return CloudMutationResult{}, ErrIntakeInvalid
			}
			if _, decodeErr := hex.DecodeString(request.ExpectedContentSHA256); decodeErr != nil {
				return CloudMutationResult{}, ErrIntakeInvalid
			}
			digest, hashErr := hashCloudFile(scoped, client, taskstore.CloudInventoryEntry{FsID: current.FsID, Path: current.Path, Name: current.ServerFilename, Size: current.Size, ProviderMtime: current.Mtime})
			if hashErr != nil {
				return CloudMutationResult{}, hashErr
			}
			if digest != request.ExpectedContentSHA256 {
				return CloudMutationResult{}, cleanupStale(errCleanupContentChanged)
			}
			current, readErr = client.GetFileInfo(request.Expected.FsID)
			if readErr != nil {
				return CloudMutationResult{}, readErr
			}
			if !sameCloudIdentity(request.Expected, current) {
				return CloudMutationResult{}, cleanupStale(errCleanupSourceChanged)
			}
		}
		if request.RequireEmptyDirectory {
			if request.Operation != "delete" || request.Expected.IsDir != 1 {
				return CloudMutationResult{}, ErrIntakeInvalid
			}
			children, listErr := client.ListDir(request.Expected.Path)
			if listErr != nil {
				return CloudMutationResult{}, listErr
			}
			if len(children) != 0 {
				return CloudMutationResult{}, fmt.Errorf("%w：%w", ErrIntakeConflict, errCleanupDirectoryNotEmpty)
			}
		}
		if request.Operation == "move" || request.Operation == "copy" {
			if err = verifyDirectory(client, request.DestinationDirectory); err != nil {
				return CloudMutationResult{}, err
			}
			if err = ensureNameAvailable(client, request.DestinationDirectory, path.Base(target)); err != nil {
				return CloudMutationResult{}, err
			}
		}
		if request.Operation == "rename" {
			if err = ensureNameAvailableExcluding(client, path.Dir(request.Expected.Path), request.NewName, request.Expected.FsID); err != nil {
				return CloudMutationResult{}, err
			}
		}
		if request.ExpectedRetained != nil {
			if err = verifyCleanupRetainedCopy(scoped, client, *request.ExpectedRetained, request.ExpectedContentSHA256); err != nil {
				return CloudMutationResult{}, err
			}
		}
		// Repeat the identity check after destination and content checks so a
		// changed path or object is not mutated based on an older listing.
		beforeInfo, err = client.GetFileInfo(request.Expected.FsID)
		if err != nil {
			return CloudMutationResult{}, err
		}
		if !sameCloudIdentity(request.Expected, beforeInfo) {
			return CloudMutationResult{}, cleanupStale(errCleanupSourceChanged)
		}
	}
	client, err = d.cloudIntakeClient(scoped, epoch)
	if err != nil {
		return CloudMutationResult{}, err
	}
	operationID, err := newSourceOperationID()
	if err != nil {
		return CloudMutationResult{}, err
	}
	objectID := request.Expected.FsID
	storeOperation := taskstore.RemoteOperation{ID: operationID, AccountFingerprint: client.Fingerprint(), Kind: request.Operation, ObjectID: objectID, SourcePath: request.Expected.Path, DestinationPath: target, Status: "prepared", CreatedAtUTC: time.Now().UTC()}
	preparedEvidence := map[string]any{"expected": request.Expected, "destination_directory": request.DestinationDirectory, "new_name": request.NewName, "auth_epoch": epoch, "cache_namespace": client.CloudCacheNamespace()}
	if request.ExpectedContentSHA256 != "" || request.RequireEmptyDirectory {
		preparedEvidence["cleanup_verification"] = map[string]any{"expected_content_sha256": request.ExpectedContentSHA256, "expected_retained": request.ExpectedRetained, "require_empty_directory": request.RequireEmptyDirectory, "checks_completed_at_utc": time.Now().UTC()}
	}
	storeOperation.Evidence, _ = json.Marshal(preparedEvidence)
	// Keep the owning upload batch's admission lock through prepared intent
	// persistence, then release it before any provider network request.
	releaseBatchAdmission := func(error) {}
	if groupedUpload {
		releaseBatchAdmission, err = baiduapi.AcquireUploadNamespaceAdmission(scoped, target, operationID)
		if err != nil {
			return CloudMutationResult{}, err
		}
	}
	if admission, ok := scoped.Value(cleanupAdmissionContextKey{}).(cleanupAdmission); ok {
		if request.Operation != "delete" {
			return CloudMutationResult{}, ErrIntakeInvalid
		}
		currentScope, scopeErr := d.cleanupScope(scoped)
		if scopeErr != nil {
			return CloudMutationResult{}, scopeErr
		}
		if currentScope != admission.Scope {
			return CloudMutationResult{}, ErrIntakeStale
		}
		err = d.commitCloudCacheObservation(scoped, epoch, func() error {
			return d.queue.store.PrepareCleanupOperation(scoped, admission.ID, admission.Ordinal, admission.Scope, storeOperation)
		})
	} else {
		err = d.queue.store.CreateRemoteOperation(storeOperation)
	}
	releaseBatchAdmission(err)
	if err != nil {
		return CloudMutationResult{}, err
	}
	switch request.Operation {
	case "mkdir":
		err = client.CreateDirectory(target)
	case "move", "copy":
		if request.Operation == "copy" {
			err = client.CopyFile(request.Expected.Path, request.DestinationDirectory, path.Base(target))
		} else {
			err = client.MoveFile(request.Expected.Path, request.DestinationDirectory, path.Base(target))
		}
	case "rename":
		err = client.RenameFile(request.Expected.Path, request.NewName)
	case "delete":
		err = client.DeleteFile(request.Expected.Path)
	}
	responseAt := time.Now().UTC()
	result := CloudMutationResult{OperationID: operationID, ObjectID: objectID, SourcePath: request.Expected.Path, DestinationPath: target}
	if err != nil {
		status := "unknown"
		var providerError *baiduapi.APIError
		if errors.As(err, &providerError) && providerError.Status < 500 && providerError.Code != 111 {
			status = "failed"
		}
		evidence, _ := json.Marshal(map[string]any{"error": errorSummary(err.Error()), "response_received": providerError != nil})
		observed := time.Time{}
		if providerError != nil {
			observed = responseAt
		}
		if persistErr := d.queue.store.UpdateRemoteOperation(operationID, status, evidence, observed); persistErr != nil {
			if status == "unknown" {
				result.Status = "unknown"
				result.Warning = "远端操作结果未知，SQLite 未能记录 unknown 状态；来源已在当前进程隔离，重启后会恢复"
				d.quarantineCloudMutationSource(objectID, request.Expected.Path, target)
				diagnostic := errorSummary(persistErr.Error())
				if invalidateErr := d.queue.store.InvalidateCloudInventory(client.CloudCacheNamespace(), mutationCachePaths(request, target)); invalidateErr != nil {
					result.Warning += "；本地缓存暂未能标为失效"
					diagnostic += "; cache invalidation failed: " + errorSummary(invalidateErr.Error())
				} else {
					result.Warning += "；本地缓存已标为失效"
				}
				if evidenceErr := d.queue.store.RecordRemoteOperationPersistenceFailure(operationID, diagnostic, observed); evidenceErr != nil {
					result.Warning += "；本地故障证据也未能持久化"
				}
			}
			return result, fmt.Errorf("远端操作结果无法持久化: %w", persistErr)
		}
		result.Status = status
		if status == "unknown" {
			invalidateErr := d.queue.store.InvalidateCloudInventory(client.CloudCacheNamespace(), mutationCachePaths(request, target))
			if invalidateErr != nil {
				result.Warning = "远端操作结果未知，且本地目录缓存暂未能标为失效；重启后会重试缓存恢复"
				if eventErr := d.queue.store.RecordRemoteCacheRefreshFailure(operationID, errorSummary(invalidateErr.Error())); eventErr != nil {
					result.Warning += "；缓存失败原因未能写入本地事件记录"
				}
			} else if markerErr := d.queue.store.MarkRemoteOperationCacheRecoveryComplete(operationID); markerErr != nil {
				result.Warning = "远端操作结果未知，目录缓存已失效，但恢复完成证据未能持久化；重启后会重试"
				if eventErr := d.queue.store.RecordRemoteCacheRefreshFailure(operationID, errorSummary(markerErr.Error())); eventErr != nil {
					result.Warning += "；缓存失败原因未能写入本地事件记录"
				}
			}
			d.quarantineCloudMutationSource(objectID, request.Expected.Path, target)
			if files, checkErr := client.ListDir(queueDir); checkErr == nil {
				_ = d.queue.reconcileObserved(files, epoch, time.Now().UTC())
			}
		}
		return result, err
	}
	evidence, _ := json.Marshal(map[string]any{"provider_response": "synchronous_success", "response_observed_at_utc": responseAt})
	if err = d.queue.store.UpdateRemoteOperation(operationID, "confirmed", evidence, responseAt); err != nil {
		if request.Operation == "copy" || request.Operation == "mkdir" {
			// A successful remote copy without a durable receipt must remain
			// quarantined now, as its prepared journal will be after restart.
			// mkdir has the same persistence window before restart recovery.
			result.Status = "unknown"
			result.Warning = "复制成功回执未能持久化，源和目标已隔离；请核对操作记录，不要重发"
			if request.Operation == "mkdir" {
				result.Warning = "新建目录成功回执未能持久化，目标已隔离；请核对操作记录，不要重发"
			}
			d.quarantineCloudMutationSource(objectID, request.Expected.Path, target)
		}
		return result, fmt.Errorf("远端操作已成功，但结果无法持久化: %w", err)
	}
	result.Status = "confirmed"
	if request.Operation == "copy" {
		return d.finishConfirmedCopy(client, request, target, result, epoch)
	}
	mutation := taskstore.CloudInventoryMutation{OperationID: operationID, Operation: request.Operation, ResponseObservedAtUTC: responseAt}
	if request.Operation != "mkdir" {
		mutation.Before = cloudInventoryEntry(beforeInfo)
	}
	var observationErr error
	cacheRecoveryDurable := true
	var afterInfo baiduapi.FileInfo
	var identityObservedAt time.Time
	switch request.Operation {
	case "mkdir":
		var entries []baiduapi.FileInfo
		entries, observationErr = client.ListDir(request.DestinationDirectory)
		if observationErr == nil {
			matches := make([]baiduapi.FileInfo, 0, 1)
			for _, entry := range entries {
				if entry.Path == target && entry.ServerFilename == path.Base(target) && entry.IsDir == 1 {
					matches = append(matches, entry)
				}
			}
			if len(matches) != 1 {
				observationErr = fmt.Errorf("无法唯一确认新建目录身份")
			} else {
				mutation.After = cloudInventoryEntry(matches[0])
			}
		}
	case "rename", "move":
		afterInfo, observationErr = client.GetFileInfo(request.Expected.FsID)
		if observationErr == nil {
			if afterInfo.FsID != request.Expected.FsID || afterInfo.Path != target || afterInfo.ServerFilename != path.Base(target) || afterInfo.IsDir != beforeInfo.IsDir {
				observationErr = fmt.Errorf("网盘操作后的对象身份或目标路径与请求不符")
			} else {
				mutation.After = cloudInventoryEntry(afterInfo)
				identityObservedAt = time.Now().UTC()
			}
		}
	}
	if observationErr == nil {
		var applied bool
		err = d.commitCloudCacheObservation(scoped, epoch, func() error {
			var applyErr error
			applied, applyErr = d.queue.store.ApplyConfirmedCloudMutation(account, mutation)
			return applyErr
		})
		if err != nil {
			observationErr = err
		} else if !applied {
			observationErr = fmt.Errorf("本地缓存覆盖或对象身份条件不满足")
		}
	}
	if observationErr != nil {
		invalidateErr := d.queue.store.InvalidateCloudInventory(account, mutationCachePaths(request, target))
		cacheRecoveryDurable = invalidateErr == nil
		result.Warning = "网盘操作已确认，但本地目录缓存未能增量更新，请重新核对或执行全盘扫描"
		diagnostic := errorSummary(observationErr.Error())
		if invalidateErr != nil {
			result.Warning += "；缓存失效标记也失败：" + errorSummary(invalidateErr.Error())
			diagnostic += "; cache invalidation failed: " + errorSummary(invalidateErr.Error())
		}
		if eventErr := d.queue.store.RecordRemoteCacheRefreshFailure(operationID, diagnostic); eventErr != nil {
			result.Warning += "；缓存失败原因未能写入本地事件记录：" + errorSummary(eventErr.Error())
		}
	}
	if request.Operation == "rename" || request.Operation == "move" {
		if err = d.queue.applyConfirmedCloudIdentityMutation(beforeInfo, afterInfo, operationID, responseAt, identityObservedAt); err != nil {
			return result, fmt.Errorf("网盘操作已确认，但本地待执行任务身份更新失败: %w", err)
		}
	} else if request.Operation == "delete" {
		if err = d.queue.cancelConfirmedCloudDelete(beforeInfo, operationID, responseAt); err != nil {
			return result, fmt.Errorf("网盘删除已确认，但本地待执行任务取消记录失败: %w", err)
		}
	}
	files, scanErr := client.ListDir(queueDir)
	if scanErr == nil {
		scanErr = d.queue.reconcileObserved(files, epoch, time.Now().UTC())
	}
	if scanErr != nil {
		return result, fmt.Errorf("远端操作已确认，下载队列核对失败: %w", scanErr)
	}
	_, cleanupBatch := scoped.Value(cleanupAdmissionContextKey{}).(cleanupAdmission)
	// A cleanup warning must stop the fixed batch, even if cache invalidation
	// succeeded. Withhold the continuation proof so restart stays conservative
	// when saving the warning itself also failed; keep the confirmed receipt.
	if cacheRecoveryDurable && (!cleanupBatch || result.Warning == "") {
		if err = d.queue.store.MarkRemoteOperationLocalReconciliationComplete(operationID); err != nil {
			return result, fmt.Errorf("远端操作已确认，但本地同步完成证据无法持久化: %w", err)
		}
	}
	return result, nil
}

func (d *Downloader) quarantineCloudMutationSource(objectID int64, sourcePath, targetPath string) {
	d.queue.mu.Lock()
	if objectID > 0 {
		d.queue.blockedRemoteIDs[objectID] = true
	}
	if sourcePath != "" {
		d.queue.blockedRemotePaths[sourcePath] = true
	}
	if targetPath != "" {
		d.queue.blockedRemotePaths[targetPath] = true
	}
	d.queue.mu.Unlock()
}

func cloudInventoryEntry(info baiduapi.FileInfo) taskstore.CloudInventoryEntry {
	return taskstore.CloudInventoryEntry{
		FsID: info.FsID, Path: info.Path, Name: info.ServerFilename, Directory: info.IsDir == 1,
		Size: info.Size, ProviderMtime: info.Mtime, ContentMD5: info.MD5,
	}
}

func (d *Downloader) PreviewEmptyCloudDirectories(ctx context.Context) ([]taskstore.CloudInventoryEntry, error) {
	client, _, err := d.authorizedCloudClient(ctx)
	if err != nil {
		return nil, err
	}
	state, err := d.queue.store.LoadCloudScanState(client.CloudCacheNamespace())
	if err != nil {
		return nil, err
	}
	if state.Status != "complete" {
		return nil, fmt.Errorf("%w：全盘缓存扫描尚未完整，不能安全生成空目录清理候选", ErrIntakeConflict)
	}
	items, err := d.queue.store.ListCompleteEmptyCloudDirectories(client.CloudCacheNamespace())
	if err != nil {
		return nil, err
	}
	queueDir := d.QueueDirectory()
	filtered := items[:0]
	for _, item := range items {
		if insideCloud(item.Path, queueDir) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func mutationCachePaths(request CloudMutationRequest, target string) []string {
	paths := make([]string, 0, 3)
	if request.Expected.Path != "" {
		paths = append(paths, path.Dir(request.Expected.Path))
		paths = append(paths, request.Expected.Path)
	}
	if request.DestinationDirectory != "" {
		paths = append(paths, request.DestinationDirectory)
	}
	if target != "" {
		paths = append(paths, target)
	}
	return paths
}

func validateCloudMutation(request CloudMutationRequest) (string, error) {
	if retained := request.ExpectedRetained; retained != nil {
		if request.Operation != "delete" || request.Expected.IsDir != 0 || retained.IsDir != 0 || retained.FsID <= 0 || retained.FsID == request.Expected.FsID || retained.Path == request.Expected.Path || !validCloudPath(retained.Path) || retained.Path == "/" || !validName(retained.ServerFilename) || retained.Size != request.Expected.Size || len(request.ExpectedContentSHA256) != 64 {
			return "", ErrIntakeInvalid
		}
		if _, err := hex.DecodeString(request.ExpectedContentSHA256); err != nil {
			return "", ErrIntakeInvalid
		}
	}

	switch request.Operation {
	case "mkdir":
		if !validCloudPath(request.DestinationDirectory) || !validName(request.NewName) {
			return "", ErrIntakeInvalid
		}
		return path.Join(request.DestinationDirectory, request.NewName), nil
	case "move", "copy":
		if request.Expected.FsID <= 0 || !validCloudPath(request.Expected.Path) || !validCloudPath(request.DestinationDirectory) || request.Expected.Path == "/" {
			return "", ErrIntakeInvalid
		}
		name := request.NewName
		if name == "" {
			name = request.Expected.ServerFilename
		}
		if !validName(name) {
			return "", ErrIntakeInvalid
		}
		return path.Join(request.DestinationDirectory, name), nil
	case "rename":
		if request.Expected.FsID <= 0 || !validCloudPath(request.Expected.Path) || request.Expected.Path == "/" || !validName(request.NewName) {
			return "", ErrIntakeInvalid
		}
		return path.Join(path.Dir(request.Expected.Path), request.NewName), nil
	case "delete":
		if request.Expected.FsID <= 0 || !validCloudPath(request.Expected.Path) || request.Expected.Path == "/" {
			return "", ErrIntakeInvalid
		}
		if request.RequireEmptyDirectory && request.Expected.IsDir != 1 {
			return "", ErrIntakeInvalid
		}
		return "", nil
	default:
		return "", ErrIntakeInvalid
	}
}

func sameCloudIdentity(expected, current baiduapi.FileInfo) bool {
	return expected.FsID == current.FsID && expected.Path == current.Path && expected.ServerFilename == current.ServerFilename && expected.Size == current.Size && expected.IsDir == current.IsDir && expected.Mtime == current.Mtime && (expected.MD5 == "" || strings.EqualFold(expected.MD5, current.MD5))
}

func verifyDirectory(client *baiduapi.Client, directory string) error {
	if !validCloudPath(directory) {
		return ErrIntakeInvalid
	}
	_, err := client.ListDir(directory)
	return err
}

func ensureNameAvailable(client *baiduapi.Client, directory, name string) error {
	return ensureNameAvailableExcluding(client, directory, name, 0)
}

func ensureNameAvailableExcluding(client *baiduapi.Client, directory, name string, excludeID int64) error {
	items, err := client.ListDir(directory)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.FsID != excludeID && strings.EqualFold(item.ServerFilename, name) {
			return ErrIntakeConflict
		}
	}
	return nil
}
