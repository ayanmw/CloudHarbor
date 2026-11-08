package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/foldersync"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

// FolderSyncRemote deliberately reuses management writes and upload admission;
// directory synchronization is not a second unjournaled Baidu client.
func syncAccountScope(client *baiduapi.Client) string {
	return client.CloudCacheNamespace() + ":" + client.ApplicationFingerprint()
}

type FolderSyncRemote struct{ Downloader *Downloader }

func (a FolderSyncRemote) CachedAnalysis(ctx context.Context, roots []string) (foldersync.CachedAnalysisSource, error) {
	client, epoch, err := a.Downloader.authorizedCloudClient(ctx)
	if err != nil {
		return foldersync.CachedAnalysisSource{}, err
	}
	if a.Downloader.queue.store == nil {
		return foldersync.CachedAnalysisSource{}, fmt.Errorf("网盘缓存不可用")
	}
	var snapshot taskstore.CloudAnalysisSnapshot
	err = a.Downloader.commitCloudCacheObservation(ctx, epoch, func() error {
		var readErr error
		snapshot, readErr = a.Downloader.queue.store.CloudAnalysisSnapshot(ctx, client.CloudCacheNamespace(), roots)
		return readErr
	})
	if err != nil {
		return foldersync.CachedAnalysisSource{}, err
	}
	return foldersync.CachedAnalysisSource{Scope: syncAccountScope(client), Generation: strconv.FormatInt(snapshot.Generation, 10), ReadAt: snapshot.ReadAtUTC, Inputs: snapshot.Inputs}, nil
}

func (a FolderSyncRemote) Inspect(ctx context.Context, root string) (string, string, error) {
	d := a.Downloader
	if !validCloudPath(root) || root == "/" || insideCloud(root, d.QueueDirectory()) || insideCloud(d.QueueDirectory(), root) {
		return "", "", fmt.Errorf("同步网盘目录不能是根目录或与监控目录重叠")
	}
	client, _, err := d.authorizedCloudClient(ctx)
	if err != nil {
		return "", "", err
	}
	items, err := client.ListDir(path.Dir(root))
	if err != nil {
		return "", "", err
	}
	for _, e := range items {
		if e.Path == root && e.IsDir == 1 {
			return syncAccountScope(client), strconv.FormatInt(e.FsID, 10), nil
		}
	}
	return "", "", fmt.Errorf("远端根目录不存在，不能解释为删除所有内容")
}
func syncInfo(root string, e foldersync.Entry) (baiduapi.FileInfo, error) {
	id, err := strconv.ParseInt(e.ID, 10, 64)
	if err != nil || id <= 0 {
		return baiduapi.FileInfo{}, fmt.Errorf("同步远端身份无效")
	}
	isdir := 0
	if e.Directory {
		isdir = 1
	}
	return baiduapi.FileInfo{FsID: id, Path: path.Join(root, e.Path), ServerFilename: path.Base(e.Path), IsDir: isdir, Size: e.Size, Mtime: e.Mtime, MD5: e.MD5}, nil
}
func syncHash(ctx context.Context, client *baiduapi.Client, info baiduapi.FileInfo) (string, error) {
	item := taskstore.CloudInventoryEntry{FsID: info.FsID, Path: info.Path, Name: info.ServerFilename, Directory: info.IsDir == 1, Size: info.Size, ProviderMtime: info.Mtime, ContentMD5: info.MD5}
	state := taskstore.CloudDuplicateScanState{RunID: foldersync.ContextRunID(ctx)}
	if err := initializeDuplicateHashCheckpoint(&state, item); err != nil {
		return "", err
	}
	return hashCloudFileResumable(ctx, client, item, &state, func() error { return nil })
}
func (a FolderSyncRemote) Scan(ctx context.Context, c foldersync.Config, cache []foldersync.Entry) ([]foldersync.Entry, error) {
	client, _, err := a.Downloader.authorizedCloudClient(ctx)
	if err != nil {
		return nil, err
	}
	previous := map[string]foldersync.Entry{}
	for _, e := range cache {
		previous[e.ID] = e
	}
	result := []foldersync.Entry{}
	pending := []string{c.RemoteRoot}
	seen := map[int64]bool{}
	for len(pending) > 0 {
		directory := pending[0]
		pending = pending[1:]
		items, e := client.ListDir(directory)
		if e != nil {
			return nil, e
		}
		for _, info := range items {
			if info.Path == path.Join(c.RemoteRoot, ".nas-sync-history") {
				if info.IsDir != 1 {
					return nil, fmt.Errorf("远端保留名称被文件占用")
				}
				owners, e := client.ListDir(info.Path)
				if e != nil {
					return nil, e
				}
				owned := false
				for _, owner := range owners {
					if owner.ServerFilename == c.ID && owner.IsDir == 1 {
						owned = true
					}
				}
				if !owned {
					return nil, fmt.Errorf("远端隔离目录不属于该配置")
				}
				continue
			}
			if seen[info.FsID] || !strings.HasPrefix(info.Path, c.RemoteRoot+"/") {
				return nil, fmt.Errorf("远端遍历身份重复或路径越界")
			}
			seen[info.FsID] = true
			entry := foldersync.Entry{ID: strconv.FormatInt(info.FsID, 10), Path: strings.TrimPrefix(info.Path, c.RemoteRoot+"/"), Directory: info.IsDir == 1, Size: info.Size, Mtime: info.Mtime, MtimeUnit: "unix_seconds", MD5: info.MD5, Observed: time.Now().UTC()}
			if entry.Directory {
				entry.Size = 0
				pending = append(pending, info.Path)
			} else {
				old, ok := previous[entry.ID]
				if ok && old.SHA256 != "" && old.Path == entry.Path && old.Size == entry.Size && old.Mtime == entry.Mtime && old.MD5 != "" && strings.EqualFold(old.MD5, entry.MD5) {
					entry.SHA256 = old.SHA256
				} else {
					entry.SHA256, e = syncHash(ctx, client, info)
					if e != nil {
						return nil, e
					}
				}
			}
			result = append(result, entry)
		}
	}
	return result, nil
}
func (a FolderSyncRemote) guard(ctx context.Context, c foldersync.Config, e *foldersync.Entry, verifyContent bool) (*baiduapi.Client, baiduapi.FileInfo, error) {
	scope, rootID, err := a.Inspect(ctx, c.RemoteRoot)
	if err != nil {
		return nil, baiduapi.FileInfo{}, err
	}
	if scope != c.Account || rootID != c.RemoteID {
		return nil, baiduapi.FileInfo{}, fmt.Errorf("同步账号或根目录身份变化")
	}
	client, _, err := a.Downloader.authorizedCloudClient(ctx)
	if err != nil {
		return nil, baiduapi.FileInfo{}, err
	}
	if e == nil {
		return client, baiduapi.FileInfo{}, nil
	}
	expected, err := syncInfo(c.RemoteRoot, *e)
	if err != nil {
		return nil, expected, err
	}
	actual, err := client.GetFileInfo(expected.FsID)
	if err != nil {
		return nil, actual, err
	}
	if e.Directory {
		if actual.FsID != expected.FsID || actual.Path != expected.Path || actual.IsDir != 1 {
			return nil, actual, ErrIntakeStale
		}
	} else {
		if !sameCloudIdentity(expected, actual) {
			return nil, actual, ErrIntakeStale
		}
		if verifyContent {
			digest, e2 := syncHash(ctx, client, actual)
			if e2 != nil {
				return nil, actual, e2
			}
			if digest != e.SHA256 {
				return nil, actual, fmt.Errorf("同步前远端内容变化")
			}
		}
	}
	return client, actual, nil
}
func (a FolderSyncRemote) Apply(ctx context.Context, c foldersync.Config, s foldersync.Step, run string, log func(string, any) error) (output any, outputErr error) {
	effect := false
	defer func() {
		if outputErr != nil && !effect {
			outputErr = &foldersync.NotStartedError{Err: outputErr}
		}
	}()
	client, expected, err := a.guard(ctx, c, s.Remote, true)
	if err != nil {
		return nil, err
	}
	d := a.Downloader
	mutate := func(request CloudMutationRequest) (CloudMutationResult, error) {
		if err := log("remote_intent", request); err != nil {
			return CloudMutationResult{}, err
		}
		result, e := d.MutateCloud(ctx, request)
		if result.OperationID != "" || result.Status == "confirmed" {
			effect = true
		}
		if recordErr := log("remote_result", result); recordErr != nil {
			return result, recordErr
		}
		if e != nil {
			return result, e
		}
		if result.Status != "confirmed" && result.Status != "existing" {
			return result, fmt.Errorf("远端操作未确认")
		}
		return result, nil
	}
	archive := func(backup string) (CloudMutationResult, error) {
		current := c.RemoteRoot
		relative := strings.TrimPrefix(path.Dir(backup), c.RemoteRoot+"/")
		for _, part := range strings.Split(relative, "/") {
			current = path.Join(current, part)
			if err := log("remote_backup_directory_intent", map[string]string{"path": current}); err != nil {
				return CloudMutationResult{}, err
			}
			result, _, e := d.EnsureUploadDirectory(ctx, current)
			if result.OperationID != "" || result.Status == "confirmed" {
				effect = true
			}
			if e != nil {
				return CloudMutationResult{}, e
			}
			if err := log("remote_backup_directory_result", result); err != nil {
				return CloudMutationResult{}, err
			}
		}
		return mutate(CloudMutationRequest{Operation: "move", Expected: expected, DestinationDirectory: path.Dir(backup), NewName: path.Base(backup)})
	}

	target := path.Join(c.RemoteRoot, s.Path)
	switch s.Action {
	case "remote_archive":
		if s.Remote == nil || s.Remote.Path != s.Path {
			return nil, fmt.Errorf("归档来源缺少固定身份")
		}
		backup, e := syncRemoteArchivePath(c, run, s.Path)
		if e != nil {
			return nil, e
		}
		moved, e := archive(backup)
		if e != nil {
			return moved, e
		}
		actual, e := client.GetFileInfo(expected.FsID)
		if e != nil {
			return moved, e
		}
		if actual.FsID != expected.FsID || actual.Path != backup || actual.IsDir != expected.IsDir {
			return moved, fmt.Errorf("归档根身份或位置无法确认；保留等待核对")
		}
		return map[string]any{"source_path": expected.Path, "archive_path": backup, "move_receipt": moved, "root_identity_verified_at_utc": time.Now().UTC()}, nil

	case "remote_mkdir":
		return mutate(CloudMutationRequest{Operation: "mkdir", DestinationDirectory: path.Dir(target), NewName: path.Base(target)})
	case "remote_move":
		return mutate(CloudMutationRequest{Operation: "move", Expected: expected, DestinationDirectory: path.Dir(target), NewName: path.Base(target)})
	case "remote_delete":
		return mutate(CloudMutationRequest{Operation: "delete", Expected: expected, RequireEmptyDirectory: s.Remote.Directory})
	case "upload":
		if s.Local == nil {
			return nil, fmt.Errorf("上传缺少本地身份")
		}
		if s.Remote != nil {
			// Preserve the previous remote object before replacement. These
			// namespace calls retain ordinary operation IDs and unknown isolation.
			backup, e := syncRemoteArchivePath(c, run, s.Remote.Path)
			if e != nil {
				return nil, e
			}
			if _, err = archive(backup); err != nil {
				return nil, err
			}
		}
		local := filepath.Join(c.LocalRoot, filepath.FromSlash(s.Local.Path))
		receipt, err := d.UploadStagedFile(ctx, local, target, func(phase string, sent int64) error {
			if phase == "submitting" || phase == "uploading" || phase == "finalizing" {
				effect = true
			}
			return log("upload_phase", map[string]any{"phase": phase, "sent": strconv.FormatInt(sent, 10), "path": s.Path})
		}, nil)
		if err != nil {
			return receipt, err
		}
		if err = log("upload_receipt", receipt); err != nil {
			return receipt, err
		}
		if receipt.Path != target {
			return receipt, fmt.Errorf("上传目标发生冲突改名，需核对")
		}
		id, err := strconv.ParseInt(receipt.FsID, 10, 64)
		if err != nil {
			return receipt, err
		}
		uploaded, err := client.GetFileInfo(id)
		if err != nil {
			return receipt, err
		}
		digest, err := syncHash(ctx, client, uploaded)
		if err != nil {
			return receipt, err
		}
		if digest != s.Local.SHA256 {
			return receipt, fmt.Errorf("上传后内容与计划不一致")
		}
		return receipt, nil
	default:
		return nil, fmt.Errorf("未知远端同步动作")
	}
}
func (a FolderSyncRemote) Download(ctx context.Context, c foldersync.Config, e foldersync.Entry, target string) error {
	client, info, err := a.guard(ctx, c, &e, false)
	if err != nil {
		return err
	}
	client = client.WithDownloadTrace("sync-"+e.ID, foldersync.ContextRunID(ctx), info.FsID, info.Path)
	link, err := client.GetDownloadLink(info.FsID)
	if err != nil {
		return err
	}
	body, length, err := client.DownloadFileWithRange(link, 0)
	if err != nil {
		return err
	}
	defer body.Close()
	if length >= 0 && length != e.Size {
		return fmt.Errorf("同步下载响应大小不符")
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(body, e.Size+1))
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if n != e.Size || hex.EncodeToString(hash.Sum(nil)) != e.SHA256 {
		return fmt.Errorf("同步下载内容校验失败")
	}
	_, _, err = a.guard(ctx, c, &e, false)
	return err
}
