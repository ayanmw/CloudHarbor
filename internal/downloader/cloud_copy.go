package downloader

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func (d *Downloader) observeConfirmedCopy(client *baiduapi.Client, request CloudMutationRequest, target, operationID string) error {
	source, err := client.GetFileInfo(request.Expected.FsID)
	if err != nil {
		return err
	}
	sourceAt := time.Now().UTC()
	if !sameCloudIdentity(request.Expected, source) {
		return fmt.Errorf("复制回执已确认，但来源身份发生变化")
	}
	entries, err := client.ListDir(request.DestinationDirectory)
	if err != nil {
		return err
	}
	targetAt := time.Now().UTC()
	var match *baiduapi.FileInfo
	for _, entry := range entries {
		if entry.Path != target {
			continue
		}
		if match != nil {
			return fmt.Errorf("复制目标路径身份不唯一")
		}
		item := entry
		match = &item
	}
	if match == nil || match.FsID <= 0 || match.FsID == source.FsID || match.ServerFilename != path.Base(target) || match.IsDir != source.IsDir || !validListingFile(request.DestinationDirectory, *match) || source.IsDir == 0 && (match.Size != source.Size || source.MD5 != "" && !strings.EqualFold(source.MD5, match.MD5)) {
		return fmt.Errorf("复制目标的新身份尚未核实")
	}
	return d.queue.store.RecordRemoteCopyObservation(operationID, cloudInventoryEntry(source), cloudInventoryEntry(*match), sourceAt, targetAt)
}

func (d *Downloader) finishConfirmedCopy(client *baiduapi.Client, request CloudMutationRequest, target string, result CloudMutationResult, epoch uint64) (CloudMutationResult, error) {
	observationErr := d.observeConfirmedCopy(client, request, target, result.OperationID)
	// A copied directory has new descendant identities. Invalidate the target
	// branch instead of inventing a recursive cache from the source snapshot.
	if err := d.queue.store.InvalidateCloudInventory(client.CloudCacheNamespace(), []string{request.DestinationDirectory, target}); err != nil {
		return result, fmt.Errorf("复制已确认，但目标缓存失效标记失败: %w", err)
	}
	if observationErr != nil {
		result.Warning = "复制回执已确认，但目标新身份尚未核实；目标缓存已失效，请刷新目标目录核对，勿重复复制"
		if err := d.queue.store.RecordRemoteCacheRefreshFailure(result.OperationID, errorSummary(observationErr.Error())); err != nil {
			return result, fmt.Errorf("复制已确认，但目标核对异常无法记录: %w", err)
		}
	}
	files, err := client.ListDir(d.QueueDirectory())
	if err == nil {
		err = d.queue.reconcileObserved(files, epoch, time.Now().UTC())
	}
	if err != nil {
		return result, fmt.Errorf("复制已确认，但下载队列核对失败: %w", err)
	}
	if observationErr != nil {
		// This records cache safety only, not a verified copy target identity.
		err = d.queue.store.MarkRemoteOperationCacheRecoveryComplete(result.OperationID)
	} else {
		err = d.queue.store.MarkRemoteOperationLocalReconciliationComplete(result.OperationID)
	}
	if err != nil {
		return result, fmt.Errorf("复制已确认，但本地同步证据无法持久化: %w", err)
	}
	return result, nil
}
