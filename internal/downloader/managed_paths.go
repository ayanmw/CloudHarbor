package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type ManagedPaths struct {
	QueueDir     string `json:"queue_dir"`
	LocalDownDir string `json:"local_download_dir"`
}

func (d *Downloader) GetManagedPaths() ManagedPaths {
	d.pathMu.RLock()
	defer d.pathMu.RUnlock()
	return ManagedPaths{QueueDir: d.queueDir, LocalDownDir: d.localDownDir}
}

func (d *Downloader) QueueDirectory() string {
	d.pathMu.RLock()
	defer d.pathMu.RUnlock()
	return d.queueDir
}

func (d *Downloader) LocalDownloadDirectory() string {
	d.pathMu.RLock()
	defer d.pathMu.RUnlock()
	return d.localDownDir
}

// SetManagedPaths changes runtime paths only after acquiring the queue gate.
// When requested, a local migration owns that gate until source verification and cleanup finish.
func (d *Downloader) SetManagedPaths(ctx context.Context, paths ManagedPaths, migrateFiles bool) (taskstore.LocalDirectoryMigration, error) {
	if !validCloudPath(paths.QueueDir) || paths.QueueDir == "/" {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("网盘监控目录必须是以 / 开头的非根目录")
	}
	local, err := filepath.Abs(filepath.Clean(strings.TrimSpace(paths.LocalDownDir)))
	if err != nil || strings.TrimSpace(paths.LocalDownDir) == "" {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("本地下载目录无效")
	}
	if d.cfg.StateDir != "" {
		stateRoot, absErr := filepath.Abs(d.cfg.StateDir)
		if absErr != nil || pathsOverlap(local, stateRoot) {
			return taskstore.LocalDirectoryMigration{}, fmt.Errorf("本地下载目录不能包含或位于程序状态目录内")
		}
	}
	current := d.GetManagedPaths()
	if current.QueueDir == paths.QueueDir && sameLocalPath(current.LocalDownDir, local) {
		return taskstore.LocalDirectoryMigration{}, nil
	}
	if d.cfg.QueueDirExplicit && paths.QueueDir != current.QueueDir {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("网盘监控目录由 .env 或启动环境锁定")
	}
	if d.cfg.LocalDownDirExplicit && !sameLocalPath(local, current.LocalDownDir) {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("本地下载目录由 .env 或启动环境锁定")
	}
	for _, intake := range d.GetIntakes() {
		if intake.State != "finished" {
			return taskstore.LocalDirectoryMigration{}, fmt.Errorf("存在未完成的网盘入队操作，暂不能修改路径")
		}
	}
	if err = d.TryBeginMaintenance(); err != nil {
		return taskstore.LocalDirectoryMigration{}, err
	}
	keepMaintenance := false
	defer func() {
		if !keepMaintenance {
			d.EndMaintenance()
		}
	}()
	if !d.scanMutex.TryLock() {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("下载目录扫描正在运行，请稍后重试")
	}
	defer d.scanMutex.Unlock()
	d.inventoryScanMutex.Lock()
	scanning := d.inventoryScanCancel != nil || d.inventoryScanStarting
	d.inventoryScanMutex.Unlock()
	if scanning {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("全盘缓存扫描运行时不能修改路径")
	}
	if migrateFiles && !sameLocalPath(current.LocalDownDir, local) {
		migration, startErr := d.beginLocalDirectoryMigration(current.LocalDownDir, local, paths.QueueDir)
		if startErr != nil {
			return taskstore.LocalDirectoryMigration{}, startErr
		}
		keepMaintenance = true
		return migration, nil
	}
	if err = os.MkdirAll(local, 0755); err != nil {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("无法创建本地下载目录: %w", err)
	}
	if err = d.saveAppSettings(map[string]any{"managed_queue_dir": paths.QueueDir, "managed_local_download_dir": local}); err != nil {
		return taskstore.LocalDirectoryMigration{}, err
	}
	d.pathMu.Lock()
	d.queueDir, d.localDownDir = paths.QueueDir, local
	d.pathMu.Unlock()
	return taskstore.LocalDirectoryMigration{}, nil
}

func (d *Downloader) saveAppSettings(updates map[string]any) error {
	d.settingsMu.Lock()
	defer d.settingsMu.Unlock()
	if d.queue.store == nil {
		return fmt.Errorf("应用设置存储不可用")
	}
	body, err := d.queue.store.LoadSettings()
	if err != nil {
		return err
	}
	settings := make(map[string]json.RawMessage)
	if len(body) > 0 {
		if err = json.Unmarshal(body, &settings); err != nil {
			return fmt.Errorf("应用设置损坏: %w", err)
		}
	}
	for key, value := range updates {
		encoded, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			return marshalErr
		}
		settings[key] = encoded
	}
	body, err = json.Marshal(settings)
	if err != nil {
		return err
	}
	return d.queue.store.SaveSettings(body)
}

func (d *Downloader) TryBeginMaintenance() error {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || q.err != nil {
		return fmt.Errorf("下载队列不可用")
	}
	if q.maintenance {
		return fmt.Errorf("下载队列正在维护")
	}
	if len(q.active) != 0 {
		return fmt.Errorf("有下载任务正在运行，暂不能修改路径")
	}
	q.maintenance = true
	q.signal()
	return nil
}

func sameLocalPath(a, b string) bool {
	aAbs, errA := filepath.Abs(a)
	bAbs, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	aAbs, bAbs = filepath.Clean(aAbs), filepath.Clean(bAbs)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(aAbs, bAbs)
	}
	return aAbs == bAbs
}

func pathsOverlap(a, b string) bool {
	a, errA := filepath.Abs(a)
	b, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return true
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	relAB, errAB := filepath.Rel(a, b)
	relBA, errBA := filepath.Rel(b, a)
	return (errAB == nil && (relAB == "." || (relAB != ".." && !strings.HasPrefix(relAB, ".."+string(filepath.Separator))))) ||
		(errBA == nil && (relBA == "." || (relBA != ".." && !strings.HasPrefix(relBA, ".."+string(filepath.Separator)))))
}
