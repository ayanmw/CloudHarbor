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
	Operation             string            `json:"operation"`
	Expected              baiduapi.FileInfo `json:"expected"`
	DestinationDirectory  string            `json:"destination_directory,omitempty"`
	NewName               string            `json:"new_name,omitempty"`
	RequireEmptyDirectory bool              `json:"require_empty_directory,omitempty"`
	ExpectedContentSHA256 string            `json:"expected_content_sha256,omitempty"`
}

type CloudMutationResult struct {
	OperationID     string `json:"operation_id"`
	Status          string `json:"status"`
	ObjectID        int64  `json:"object_id"`
	SourcePath      string `json:"source_path,omitempty"`
	DestinationPath string `json:"destination_path,omitempty"`
}

func (d *Downloader) MutateCloud(ctx context.Context, request CloudMutationRequest) (CloudMutationResult, error) {
	target, err := validateCloudMutation(request)
	if err != nil {
		return CloudMutationResult{}, err
	}
	queueDir := d.QueueDirectory()
	if request.Operation != "mkdir" && request.Expected.IsDir == 1 && insideCloud(queueDir, request.Expected.Path) {
		return CloudMutationResult{}, ErrIntakeInvalid
	}
	if request.Operation == "move" && request.Expected.IsDir == 1 && insideCloud(request.DestinationDirectory, request.Expected.Path) {
		return CloudMutationResult{}, ErrIntakeInvalid
	}
	scoped, done, err := d.startIntake(ctx)
	if err != nil {
		return CloudMutationResult{}, err
	}
	defer done()
	if err = d.BeginMaintenance(scoped); err != nil {
		return CloudMutationResult{}, err
	}
	defer d.EndMaintenance()
	d.cloudMutex.Lock()
	defer d.cloudMutex.Unlock()
	client, epoch, err := d.authorizedClient(scoped)
	if err != nil {
		return CloudMutationResult{}, err
	}
	if request.Operation == "mkdir" {
		if err = verifyDirectory(client, request.DestinationDirectory); err != nil {
			return CloudMutationResult{}, err
		}
		if err = ensureNameAvailable(client, request.DestinationDirectory, request.NewName); err != nil {
			return CloudMutationResult{}, err
		}
	} else {
		current, readErr := client.GetFileInfo(request.Expected.FsID)
		if readErr != nil {
			return CloudMutationResult{}, readErr
		}
		if !sameCloudIdentity(request.Expected, current) {
			return CloudMutationResult{}, ErrIntakeStale
		}
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
				return CloudMutationResult{}, ErrIntakeStale
			}
			current, readErr = client.GetFileInfo(request.Expected.FsID)
			if readErr != nil {
				return CloudMutationResult{}, readErr
			}
			if !sameCloudIdentity(request.Expected, current) {
				return CloudMutationResult{}, ErrIntakeStale
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
				return CloudMutationResult{}, ErrIntakeConflict
			}
		}
		if request.Operation == "move" {
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
	}
	client, err = d.intakeClient(scoped, epoch)
	if err != nil {
		return CloudMutationResult{}, err
	}
	operationID, err := newSourceOperationID()
	if err != nil {
		return CloudMutationResult{}, err
	}
	objectID := request.Expected.FsID
	storeOperation := taskstore.RemoteOperation{ID: operationID, AccountFingerprint: client.Fingerprint(), Kind: request.Operation, ObjectID: objectID, SourcePath: request.Expected.Path, DestinationPath: target, Status: "prepared", CreatedAtUTC: time.Now().UTC()}
	storeOperation.Evidence, _ = json.Marshal(map[string]any{"expected": request.Expected, "destination_directory": request.DestinationDirectory, "new_name": request.NewName, "auth_epoch": epoch})
	if err = d.queue.store.CreateRemoteOperation(storeOperation); err != nil {
		return CloudMutationResult{}, err
	}
	switch request.Operation {
	case "mkdir":
		err = client.CreateDirectory(target)
	case "move":
		err = client.MoveFile(request.Expected.Path, request.DestinationDirectory, path.Base(target))
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
			return result, fmt.Errorf("远端操作结果无法持久化: %w", persistErr)
		}
		result.Status = status
		if status == "unknown" {
			_ = d.queue.store.InvalidateCloudInventory(client.Fingerprint(), mutationCachePaths(request, target))
			d.queue.mu.Lock()
			if objectID > 0 {
				d.queue.blockedRemoteIDs[objectID] = true
			}
			if request.Expected.Path != "" {
				d.queue.blockedRemotePaths[request.Expected.Path] = true
			}
			if target != "" {
				d.queue.blockedRemotePaths[target] = true
			}
			d.queue.mu.Unlock()
			if files, checkErr := client.ListDir(queueDir); checkErr == nil {
				_ = d.queue.reconcileObserved(files, epoch, time.Now().UTC())
			}
		}
		return result, err
	}
	evidence, _ := json.Marshal(map[string]any{"provider_response": "synchronous_success", "response_observed_at_utc": responseAt})
	if err = d.queue.store.UpdateRemoteOperation(operationID, "confirmed", evidence, responseAt); err != nil {
		return result, fmt.Errorf("远端操作已成功，但结果无法持久化: %w", err)
	}
	if err = d.queue.store.InvalidateCloudInventory(client.Fingerprint(), mutationCachePaths(request, target)); err != nil {
		return result, fmt.Errorf("远端操作已确认，目录缓存失效标记失败: %w", err)
	}
	files, scanErr := client.ListDir(queueDir)
	if scanErr == nil {
		scanErr = d.queue.reconcileObserved(files, epoch, time.Now().UTC())
	}
	result.Status = "confirmed"
	if scanErr != nil {
		return result, fmt.Errorf("远端操作已确认，下载队列核对失败: %w", scanErr)
	}
	return result, nil
}

func (d *Downloader) PreviewEmptyCloudDirectories(ctx context.Context) ([]taskstore.CloudInventoryEntry, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, err
	}
	state, err := d.queue.store.LoadCloudScanState(client.Fingerprint())
	if err != nil {
		return nil, err
	}
	if state.Status != "complete" {
		return nil, fmt.Errorf("%w：全盘缓存扫描尚未完整，不能安全生成空目录清理候选", ErrIntakeConflict)
	}
	return d.queue.store.ListCompleteEmptyCloudDirectories(client.Fingerprint())
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
	switch request.Operation {
	case "mkdir":
		if !validCloudPath(request.DestinationDirectory) || !validName(request.NewName) {
			return "", ErrIntakeInvalid
		}
		return path.Join(request.DestinationDirectory, request.NewName), nil
	case "move":
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
