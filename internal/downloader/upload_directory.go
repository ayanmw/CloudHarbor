package downloader

import (
	"context"
	"errors"
	"path"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

// Upload and download state share one bound account directory. Upload startup
// restores these paths before worker readiness, including identity uncertainty
// after a confirmed mkdir; the original remote receipt remains unchanged.
func (d *Downloader) QuarantineUploadDirectory(remote string) error {
	if !validCloudPath(remote) || remote == "/" {
		return ErrIntakeInvalid
	}
	d.quarantineCloudMutationSource(0, "", remote)
	return nil
}

// EnsureUploadDirectory never claims an observed existing directory was created
// by this batch. New directories use the normal journal/maintenance/cache path.
func (d *Downloader) EnsureUploadDirectory(ctx context.Context, remote string) (CloudMutationResult, time.Time, error) {
	result := CloudMutationResult{Status: "failed", DestinationPath: remote}
	var observed time.Time
	if !validCloudPath(remote) || remote == "/" {
		return result, observed, ErrIntakeInvalid
	}
	scoped, done, err := d.startIntake(ctx)
	if err != nil {
		return result, observed, err
	}
	defer done()
	client, epoch, err := d.authorizedClient(scoped)
	if err != nil {
		return result, observed, err
	}
	scoped, releaseLease, err := d.useUploadLease(scoped)
	if err != nil {
		return result, observed, err
	}
	defer releaseLease()
	blockedPath := func() bool {
		d.queue.mu.Lock()
		defer d.queue.mu.Unlock()
		return d.queue.isRemoteBlocked(0, remote)
	}
	lookup := func(client *baiduapi.Client) (baiduapi.FileInfo, bool, error) {
		entries, e := client.ListDir(path.Dir(remote))
		if e != nil {
			return baiduapi.FileInfo{}, false, e
		}
		observed = time.Now().UTC()
		var found baiduapi.FileInfo
		count := 0
		for _, entry := range entries {
			if entry.ServerFilename == path.Base(remote) {
				found = entry
				count++
			}
		}
		if count == 0 {
			return found, false, nil
		}
		if count != 1 || found.IsDir != 1 || found.FsID <= 0 || found.Path != remote {
			return found, false, errors.New("目录目标被文件占用或远端身份不一致")
		}
		return found, true, nil
	}
	// Serialize observations with namespace writers, and recheck after the
	// listing in case another isolation source changed while it was pending.
	observe := func(client *baiduapi.Client) (baiduapi.FileInfo, bool, error) {
		d.cloudMutex.Lock()
		defer d.cloudMutex.Unlock()
		if blockedPath() {
			return baiduapi.FileInfo{}, false, ErrIntakeConflict
		}
		if e := verifyDirectory(client, path.Dir(remote)); e != nil {
			return baiduapi.FileInfo{}, false, e
		}
		entry, exists, e := lookup(client)
		if blockedPath() {
			return baiduapi.FileInfo{}, false, ErrIntakeConflict
		}
		if e == nil {
			_, e = d.intakeClient(scoped, epoch)
		}
		return entry, exists, e
	}
	found, exists, err := observe(client)
	if err != nil {
		if blockedPath() {
			result.Status = "unknown"
		}
		return result, observed, err
	}
	if exists {
		result.Status = "existing"
		result.ObjectID = found.FsID
		return result, observed, nil
	}
	result, err = d.MutateCloud(scoped, CloudMutationRequest{Operation: "mkdir", DestinationDirectory: path.Dir(remote), NewName: path.Base(remote)})
	if err != nil || (result.Status != "confirmed" && result.Status != "existing") {
		if result.OperationID == "" && !blockedPath() {
			result.Status = "failed"
		} else if result.Status != "failed" {
			result.Status = "unknown"
		}
		if result.Status == "unknown" {
			d.quarantineCloudMutationSource(0, "", remote)
		}
		result.DestinationPath = remote
		if err == nil {
			err = errors.New("目录创建尚未确认")
		}
		return result, observed, err
	}
	client, err = d.intakeClient(scoped, epoch)
	if err == nil {
		found, exists, err = observe(client)
	}
	if err == nil {
		_, err = d.intakeClient(scoped, epoch)
	}
	if err != nil || !exists {
		result.Status = "unknown"
		d.quarantineCloudMutationSource(0, "", remote)
		if err == nil {
			err = errors.New("目录创建回执已收到，但无法确认远端目录身份")
		}
		return result, observed, err
	}
	result.ObjectID = found.FsID
	return result, observed, nil
}
