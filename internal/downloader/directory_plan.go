package downloader

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

// Prepare the local tree before transferring files. Reuse the observed lists
// for this attempt so preparation does not double the provider request count.
// This is an observation, not an atomic snapshot of the remote directory.
func (d *Downloader) prepareDirectoryTree(ctx context.Context, root baiduapi.FileInfo, relative string) (map[string][]baiduapi.FileInfo, error) {
	d.taskMutex.Lock()
	if task := d.tasks[root.FsID]; task != nil {
		task.DirectoryPrepared = false
	}
	d.taskMutex.Unlock()
	recorder, err := d.beginDownloadManifest(ctx, root, relative)
	if err != nil {
		return nil, err
	}
	lists := make(map[string][]baiduapi.FileInfo)
	type directory struct {
		info     baiduapi.FileInfo
		relative string
	}
	pending := []directory{{root, relative}}
	seen := map[int64]bool{}
	identities := map[int64]bool{root.FsID: true}
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[current.info.FsID] {
			return nil, fmt.Errorf("目录身份重复，停止准备本地目录")
		}
		seen[current.info.FsID] = true
		local, err := d.localPath(current.relative)
		if err != nil {
			return nil, err
		}
		d.beginTask(current.info, local)
		if err = ensureDirectory(local); err != nil {
			d.finish(current.info.FsID, err)
			return nil, err
		}
		client, _, err := d.authorizedClient(ctx)
		if err != nil {
			return nil, err
		}
		children, err := client.ListDir(current.info.Path)
		if err != nil {
			d.finish(current.info.FsID, err)
			return nil, err
		}
		lists[current.info.Path] = children
		observedAt := time.Now().UTC()
		for _, child := range children {
			if !validName(child.ServerFilename) || child.Path != path.Join(current.info.Path, child.ServerFilename) {
				return nil, fmt.Errorf("网盘子项路径或名称无效，停止准备本地目录")
			}
			if strings.HasSuffix(child.ServerFilename, ".可删除") {
				return nil, fmt.Errorf("发现旧版标记，无法验证本地文件；请移出队列目录或恢复原名后重试")
			}
			if identities[child.FsID] {
				return nil, fmt.Errorf("目录树中出现重复文件身份，停止准备下载")
			}
			identities[child.FsID] = true
			if child.IsDir == 1 {
				pending = append(pending, directory{child, path.Join(current.relative, child.ServerFilename)})
			} else {
				local, err := d.localPath(path.Join(current.relative, child.ServerFilename))
				if err != nil {
					return nil, err
				}
				d.preparePendingFile(child, local)
			}
			if err := recorder.add(ctx, child, path.Join(current.relative, child.ServerFilename), observedAt); err != nil {
				return nil, err
			}
		}
	}
	if err := recorder.publish(ctx); err != nil {
		return nil, err
	}
	d.taskMutex.Lock()
	if task := d.tasks[root.FsID]; task != nil {
		task.DirectoryPrepared = true
	}
	d.taskMutex.Unlock()
	return lists, nil
}

func (d *Downloader) preparePendingFile(info baiduapi.FileInfo, local string) {
	d.taskMutex.Lock()
	defer d.taskMutex.Unlock()
	if old := d.tasks[info.FsID]; old != nil && old.FileInfo == info && old.Status == "done" {
		return
	}
	d.tasks[info.FsID] = &Task{FileInfo: shareFileInfoStrings(info), LocalPath: local, Status: "pending"}
	d.taskCatalogGeneration.Add(1)
	d.taskStatsGeneration.Add(1)
}

func (d *Downloader) downloadLocalFolder(ctx context.Context, info baiduapi.FileInfo, relative string) error {
	lists, err := d.prepareDirectoryTree(ctx, info, relative)
	if err != nil {
		return err
	}
	ctx, err = d.bindPreparedDownloadManifest(ctx, info)
	if err != nil {
		return err
	}
	return d.downloadPreparedFolder(ctx, info, relative, lists)
}
