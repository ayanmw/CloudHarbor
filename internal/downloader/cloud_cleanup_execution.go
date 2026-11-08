package downloader

import (
	"context"
	"errors"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

var errCloudMutationQuarantined = errors.New("网盘项仍处于未知结果隔离")
var errCleanupDirectoryNotEmpty = errors.New("网盘目录已非空")
var errCleanupSourceChanged = errors.New("待删对象身份已变化")
var errCleanupRetainedChanged = errors.New("保留副本身份已变化")
var errCleanupContentChanged = errors.New("内容摘要已变化")

func cleanupStale(reason error) error { return errors.Join(ErrIntakeStale, reason) }

type cleanupAdmissionContextKey struct{}
type cleanupAdmission struct {
	ID      string
	Ordinal int
	Scope   taskstore.CleanupBatchScope
}

func (d *Downloader) cleanupScope(ctx context.Context) (taskstore.CleanupBatchScope, error) {
	client, _, err := d.authorizedCloudClient(ctx)
	if err != nil {
		return taskstore.CleanupBatchScope{}, err
	}
	account, err := d.queue.store.BoundAccountFingerprint()
	if err != nil {
		return taskstore.CleanupBatchScope{}, err
	}
	return taskstore.CleanupBatchScope{AccountFingerprint: account, ApplicationFingerprint: client.ApplicationFingerprint(), CacheNamespace: client.CloudCacheNamespace()}, nil
}

func cleanupFileIdentity(i taskstore.CleanupIdentity) baiduapi.FileInfo {
	dir := 0
	if i.Directory {
		dir = 1
	}
	return baiduapi.FileInfo{FsID: i.FsID, Path: i.Path, ServerFilename: i.Name, IsDir: dir, Size: i.Size, Mtime: i.Mtime, MD5: i.MD5}
}

// The caller owns worker serialization. The durable admission inside MutateCloud
// independently prevents replay and overlaps; no new delete implementation lives here.
func (d *Downloader) executeCleanupItem(ctx context.Context, id string, ordinal int) (CloudMutationResult, error) {
	scope, err := d.cleanupScope(ctx)
	if err != nil {
		return CloudMutationResult{}, err
	}
	manifest, err := d.queue.store.CleanupManifest(ctx, id, scope)
	if err != nil {
		return CloudMutationResult{}, err
	}
	batch, err := d.queue.store.CleanupBatch(ctx, id)
	if err != nil {
		return CloudMutationResult{}, err
	}
	if batch.StopRequested || (batch.Status != "queued" && batch.Status != "running") || ordinal < 0 || ordinal >= len(manifest.Items) {
		return CloudMutationResult{}, taskstore.ErrCleanupBatchConflict
	}
	item := manifest.Items[ordinal]
	request := CloudMutationRequest{Operation: "delete", Expected: cleanupFileIdentity(item.Expected), RequireEmptyDirectory: item.Kind == "empty", ExpectedContentSHA256: item.ExpectedSHA256}
	if item.Retained != nil {
		keep := cleanupFileIdentity(*item.Retained)
		request.ExpectedRetained = &keep
	}
	linked := context.WithValue(ctx, cleanupAdmissionContextKey{}, cleanupAdmission{ID: id, Ordinal: ordinal, Scope: scope})
	result, mutationErr := d.MutateCloud(linked, request)
	// Persist the actual outcome even when the caller cancelled while waiting.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if result.OperationID != "" {
		projectionErr := d.queue.store.ReconcileCleanupOperation(persistCtx, id, ordinal)
		return result, errors.Join(mutationErr, projectionErr)
	}
	if mutationErr == nil {
		return result, nil
	}
	if errors.Is(mutationErr, errCleanupDirectoryNotEmpty) || errors.Is(mutationErr, errCloudMutationQuarantined) {
		reason := "operation_conflict"
		if errors.Is(mutationErr, errCleanupDirectoryNotEmpty) {
			reason = "directory_not_empty"
		}
		return result, errors.Join(mutationErr, d.queue.store.FailCleanupPreflight(persistCtx, id, ordinal, scope, reason))
	}
	if errors.Is(mutationErr, context.Canceled) || errors.Is(mutationErr, context.DeadlineExceeded) || errors.Is(mutationErr, ErrAuthenticationWaiting) || errors.Is(mutationErr, ErrIntakeConflict) || errors.Is(mutationErr, taskstore.ErrCleanupBatchConflict) {
		return result, mutationErr
	}
	reason := "remote_read_failed"
	if errors.Is(mutationErr, ErrIntakeStale) {
		reason = "observation_changed"
	}
	// Real stale-manifest acceptance exposed that all identity failures were
	// collapsed into observation_changed, hiding which protected copy changed.
	switch {
	case errors.Is(mutationErr, errCleanupSourceChanged):
		reason = "source_identity_changed"
	case errors.Is(mutationErr, errCleanupRetainedChanged):
		reason = "retained_identity_changed"
	case errors.Is(mutationErr, errCleanupContentChanged):
		reason = "content_changed"
	}
	return result, errors.Join(mutationErr, d.queue.store.FailCleanupPreflight(persistCtx, id, ordinal, scope, reason))
}
