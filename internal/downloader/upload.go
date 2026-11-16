package downloader

import (
	"context"
	"errors"
	"path"
	"strconv"
	"strings"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func (d *Downloader) uploadScopeContext(ctx context.Context, epoch uint64) (context.Context, error) {
	d.clientMutex.RLock()
	managed := d.authManaged
	d.clientMutex.RUnlock()
	if !managed {
		return ctx, nil
	}
	if _, err := d.intakeClient(ctx, epoch); err != nil {
		return nil, err
	}
	account, err := d.queue.store.BoundAccountFingerprint()
	if err != nil {
		return nil, err
	}
	client, err := d.intakeClient(ctx, epoch)
	if err != nil {
		return nil, err
	}
	// Legacy token-only uploads lack an application binding and remain unscoped.
	if client.AppKey == "" {
		return ctx, nil
	}
	return baiduapi.WithUploadAccountScope(ctx, baiduapi.UploadAccountScope{AccountFingerprint: account, CredentialFingerprint: client.Fingerprint(), AppKeyFingerprint: client.ApplicationFingerprint(), CheckedAtUTC: time.Now().UTC()}), nil
}

// UploadStagedFile keeps the download queue paused only during namespace writes;
// transferring temporary blocks does not hold the cloud listing lock.
func (d *Downloader) UploadStagedFile(ctx context.Context, local, remote string, report func(string, int64) error, observe func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
	var empty baiduapi.UploadReceipt
	if !validCloudPath(remote) || remote == "/" {
		return empty, ErrIntakeInvalid
	}
	scoped, done, err := d.startIntake(ctx)
	if err != nil {
		return empty, err
	}
	defer done()
	client, epoch, err := d.authorizedCloudClient(scoped)
	if err != nil {
		return empty, err
	}
	scoped, err = d.uploadScopeContext(scoped, epoch)
	if err != nil {
		return empty, err
	}
	// Parallel admission review found ordinary file uploads could bypass an
	// unknown parent directory simply because it already appeared in a listing.
	targetBlocked := func() bool { d.queue.mu.Lock(); defer d.queue.mu.Unlock(); return d.queue.isRemoteBlocked(0, remote) }
	if targetBlocked() {
		return empty, baiduapi.ErrUploadUnconfirmed
	}
	scoped, releaseLease, err := d.useUploadLease(scoped)
	if err != nil {
		return empty, err
	}
	defer releaseLease()
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
			// Recovery review found the fresh-upload parent creation path could
			// recreate a deleted target before validating the original session.
			if _, resuming := baiduapi.UploadSessionResumeFromContext(scoped); resuming {
				return empty, baiduapi.ErrUploadUnconfirmed
			}
			if e = report("submitting", 0); e != nil {
				return empty, e
			}
			result, _, e := d.EnsureUploadDirectory(scoped, next)
			if e != nil {
				return empty, e
			}
			if result.Status != "confirmed" && result.Status != "existing" {
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
	if err = d.queue.store.InvalidateIdleDuplicateCheckpoint(client.CloudCacheNamespace()); err != nil {
		return empty, err
	}
	conflict, e := d.queue.store.CloudMutationScanConflict(client.CloudCacheNamespace(), []string{parent, remote})
	if e != nil {
		return empty, e
	}
	if conflict {
		return empty, errors.New("缓存或哈希扫描仍有待处理工作，请完成或恢复扫描后重试上传")
	}
	locked := false
	var leaveNamespace func()
	release := func() {
		if locked {
			d.cloudMutex.Unlock()
			d.EndMaintenance()
			leaveNamespace()
			locked = false
		}
	}
	defer release()
	upload := func(phase string, sent int64) error {
		if _, e := d.intakeClient(scoped, epoch); e != nil {
			return e
		}
		if phase == "submitting" || phase == "finalizing" {
			conflict, e := d.queue.store.CloudMutationScanConflict(client.CloudCacheNamespace(), []string{parent, remote})
			if e != nil {
				return e
			}
			if conflict {
				return errors.New("上传目标仍有待恢复扫描，未提交远端")
			}

			if !locked {
				leave, e := d.acquireUploadNamespace(scoped)
				if e != nil {
					return e
				}
				if _, e = d.intakeClient(scoped, epoch); e != nil {
					leave()
					return e
				}
				if e = d.beginUploadMaintenance(scoped); e != nil {
					leave()
					return e
				}
				leaveNamespace = leave
				d.cloudMutex.Lock()
				locked = true
			}
			if targetBlocked() {
				return baiduapi.ErrUploadUnconfirmed
			}
			if _, e := d.intakeClient(scoped, epoch); e != nil {
				return e
			}

			if d.queue.store != nil {
				if e := d.queue.store.InvalidateCloudInventory(client.CloudCacheNamespace(), []string{parent}); e != nil {
					return e
				}
			}
		} else if phase == "uploading" {
			release()
		}
		return report(phase, sent)
	}
	var receipt baiduapi.UploadReceipt
	if source, ok := baiduapi.UploadSessionResumeFromContext(scoped); ok {
		receipt, err = client.ResumeUploadLocalFileMeasured(scoped, local, remote, source, upload, observe)
	} else {
		receipt, err = client.UploadLocalFileMeasured(scoped, local, remote, upload, observe)
	}
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

// VerifyUploadReceipt reads only the identity in a saved creation receipt. The
// same account gate as uploading applies; a credential epoch change invalidates
// the observation instead of confirming it under a different authorization.
func (d *Downloader) VerifyUploadReceipt(ctx context.Context, original baiduapi.UploadAccountScope, receipt baiduapi.UploadReceipt) (baiduapi.UploadReceiptVerification, error) {
	var empty baiduapi.UploadReceiptVerification
	scoped, done, err := d.startIntake(ctx)
	if err != nil {
		return empty, err
	}
	defer done()
	client, epoch, err := d.authorizedCloudClient(scoped)
	if err != nil {
		return empty, err
	}
	scoped, err = d.uploadScopeContext(scoped, epoch)
	if err != nil {
		return empty, err
	}
	result, err := client.VerifyUploadReceipt(scoped, original, receipt)
	if err != nil {
		d.reportAuthenticationFailure(err)
		return empty, err
	}
	if _, err = d.intakeClient(scoped, epoch); err != nil {
		return empty, err
	}
	return result, nil
}
