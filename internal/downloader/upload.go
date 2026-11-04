package downloader

import (
	"context"
	"errors"
	"path"
	"strconv"
	"strings"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

// UploadStagedFile keeps the download queue paused only during namespace writes;
// transferring temporary blocks does not hold the cloud listing lock.
func (d *Downloader) UploadStagedFile(ctx context.Context, local, remote string, report func(string, int64) error) (baiduapi.UploadReceipt, error) {
	var empty baiduapi.UploadReceipt
	if !validCloudPath(remote) || remote == "/" {
		return empty, ErrIntakeInvalid
	}
	scoped, done, err := d.startIntake(ctx)
	if err != nil {
		return empty, err
	}
	defer done()
	client, epoch, err := d.authorizedClient(scoped)
	if err != nil {
		return empty, err
	}
	parent := path.Dir(remote)
	parts := strings.Split(strings.TrimPrefix(parent, "/"), "/")
	current := "/"
	for _, part := range parts {
		if part == "" {
			continue
		}
		next := path.Join(current, part)
		files, e := client.ListDir(current)
		if e != nil {
			return empty, e
		}
		found := false
		for _, f := range files {
			if f.ServerFilename == part {
				if f.IsDir != 1 {
					return empty, errors.New("上传父路径被同名文件占用")
				}
				found = true
				break
			}
		}
		if !found {
			if e = report("submitting", 0); e != nil {
				return empty, e
			}
			result, e := d.MutateCloud(scoped, CloudMutationRequest{Operation: "mkdir", DestinationDirectory: current, NewName: part})
			if e != nil {
				return empty, e
			}
			if result.Status != "confirmed" {
				return empty, baiduapi.ErrUploadUnconfirmed
			}
		}
		current = next
	}
	// Parent creation uses MutateCloud's own path gate. Acquiring another read
	// lock around it can deadlock when a path-settings writer is already waiting.
	fileCtx, fileDone, err := d.startPathSensitiveOperation(scoped)
	if err != nil {
		return empty, err
	}
	defer fileDone()
	scoped = fileCtx
	// Uploads share the same admission and persisted cursor protection as other
	// namespace writes; invalidating a cache alone cannot stop a paused scan publishing.
	if err = d.reserveCloudInventoryScan("下载队列正在维护，暂不能上传"); err != nil {
		return empty, err
	}
	defer func() { d.inventoryScanMutex.Lock(); d.inventoryScanStarting = false; d.inventoryScanMutex.Unlock() }()
	conflict, e := d.queue.store.CloudMutationScanConflict(client.Fingerprint(), []string{parent, remote})
	if e != nil {
		return empty, e
	}
	if conflict {
		return empty, errors.New("缓存或哈希扫描仍有待处理工作，请完成或恢复扫描后重试上传")
	}
	locked := false
	release := func() {
		if locked {
			d.cloudMutex.Unlock()
			d.EndMaintenance()
			locked = false
		}
	}
	defer release()
	receipt, err := client.UploadLocalFile(scoped, local, remote, func(phase string, sent int64) error {
		if _, e := d.intakeClient(scoped, epoch); e != nil {
			return e
		}
		if phase == "submitting" || phase == "finalizing" {
			conflict, e := d.queue.store.CloudMutationScanConflict(client.Fingerprint(), []string{parent, remote})
			if e != nil {
				return e
			}
			if conflict {
				return errors.New("上传目标仍有待恢复扫描，未提交远端")
			}

			if !locked {
				if e := d.BeginMaintenance(scoped); e != nil {
					return e
				}
				d.cloudMutex.Lock()
				locked = true
			}
			if d.queue.store != nil {
				if e := d.queue.store.InvalidateCloudInventory(client.Fingerprint(), []string{parent}); e != nil {
					return e
				}
			}
		} else if phase == "uploading" {
			release()
		}
		return report(phase, sent)
	})
	if err != nil {
		d.reportAuthenticationFailure(err)
		return empty, err
	}
	fsid, e := strconv.ParseInt(receipt.FsID, 10, 64)
	if e != nil || fsid <= 0 || !validCloudPath(receipt.Path) || path.Dir(receipt.Path) != parent {
		return empty, baiduapi.ErrUploadUnconfirmed
	}
	info, e := client.GetFileInfo(fsid)
	if e != nil || info.Path != receipt.Path || strconv.FormatInt(info.Size, 10) != receipt.Size || info.IsDir != 0 {
		return empty, baiduapi.ErrUploadUnconfirmed
	}
	return receipt, nil
}
