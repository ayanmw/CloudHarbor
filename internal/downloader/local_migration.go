package downloader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func (d *Downloader) beginLocalDirectoryMigration(source, target, queueAfter string) (taskstore.LocalDirectoryMigration, error) {
	source, err := filepath.Abs(filepath.Clean(source))
	if err != nil {
		return taskstore.LocalDirectoryMigration{}, err
	}
	target, err = filepath.Abs(filepath.Clean(target))
	if err != nil {
		return taskstore.LocalDirectoryMigration{}, err
	}
	if sameLocalPath(source, target) || pathsOverlap(source, target) {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("迁移源目录与目标目录不能相同或互相包含")
	}
	if d.cfg.StateDir != "" && (pathsOverlap(source, d.cfg.StateDir) || pathsOverlap(target, d.cfg.StateDir)) {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("迁移目录不能包含或位于程序状态目录内")
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("无法访问原下载目录: %w", err)
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 || !sourceInfo.IsDir() {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("原下载路径必须是普通目录，不能是符号链接")
	}
	if err = os.MkdirAll(target, 0755); err != nil {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("无法创建目标下载目录: %w", err)
	}
	targetInfo, err := os.Lstat(target)
	if err != nil || targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.IsDir() {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("目标下载路径必须是普通目录，不能是符号链接")
	}
	latest, latestErr := d.queue.store.LoadLatestLocalDirectoryMigration()
	if latestErr != nil && latestErr != sql.ErrNoRows {
		return taskstore.LocalDirectoryMigration{}, latestErr
	}
	now := time.Now().UTC()
	if latestErr == nil && latest.SourcePath == source && latest.TargetPath == target && latest.DesiredQueuePath == queueAfter && latest.Status == "failed" {
		latest.Status, latest.LastError, latest.FinishedAtUTC, latest.UpdatedAtUTC = "running", "", time.Time{}, now
		if err = d.queue.store.UpdateLocalDirectoryMigration(latest, true); err != nil {
			return taskstore.LocalDirectoryMigration{}, err
		}
		d.launchLocalDirectoryMigration(latest)
		return latest, nil
	}
	if latestErr == nil && latest.Status == "running" {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("已有本地目录迁移正在运行")
	}
	id, err := newSourceOperationID()
	if err != nil {
		return taskstore.LocalDirectoryMigration{}, err
	}
	migration := taskstore.LocalDirectoryMigration{
		ID: id, SourcePath: source, TargetPath: target, DesiredQueuePath: queueAfter,
		TombstonePath: source + ".nas-migration-" + id,
		Status:        "running", Phase: "copying", StartedAtUTC: now, UpdatedAtUTC: now,
	}
	if err = d.queue.store.CreateLocalDirectoryMigration(migration); err != nil {
		return taskstore.LocalDirectoryMigration{}, err
	}
	d.launchLocalDirectoryMigration(migration)
	return migration, nil
}

func (d *Downloader) launchLocalDirectoryMigration(migration taskstore.LocalDirectoryMigration) {
	d.localMigrationMu.Lock()
	if d.localMigrationRunning {
		d.localMigrationMu.Unlock()
		return
	}
	d.localMigrationRunning, d.localMigrationPending = true, false
	d.localMigrationMu.Unlock()
	go d.runLocalDirectoryMigration(d.ctx, migration)
}

func (d *Downloader) SetLocalMigrationCompletionHandler(handler func()) {
	d.localMigrationMu.Lock()
	d.localMigrationComplete = handler
	d.localMigrationMu.Unlock()
}

func (d *Downloader) ResumeLocalDirectoryMigration() error {
	migration, err := d.queue.store.LoadActiveLocalDirectoryMigration()
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	d.localMigrationMu.Lock()
	running := d.localMigrationRunning
	d.localMigrationPending = false
	d.localMigrationMu.Unlock()
	if running {
		return fmt.Errorf("本地目录迁移已经启动")
	}
	d.queue.mu.Lock()
	if !d.queue.maintenance {
		if len(d.queue.active) != 0 {
			d.queue.mu.Unlock()
			return fmt.Errorf("无法恢复迁移：已有下载任务正在运行")
		}
		d.queue.maintenance = true
	}
	d.queue.mu.Unlock()
	d.launchLocalDirectoryMigration(migration)
	return nil
}

func (d *Downloader) LocalDirectoryMigrationStatus() (taskstore.LocalDirectoryMigration, error) {
	return d.queue.store.LoadLatestLocalDirectoryMigration()
}

func (d *Downloader) runLocalDirectoryMigration(ctx context.Context, migration taskstore.LocalDirectoryMigration) {
	completed := false
	defer func() {
		d.localMigrationMu.Lock()
		d.localMigrationRunning = false
		handler := d.localMigrationComplete
		d.localMigrationMu.Unlock()
		d.EndMaintenance()
		if completed && handler != nil {
			handler()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			migration.LastError = ""
			migration.UpdatedAtUTC = time.Now().UTC()
			_ = d.queue.store.UpdateLocalDirectoryMigration(migration, true)
			return
		}
		switch migration.Phase {
		case "copying":
			files, bytes, err := scanLocalDirectory(ctx, migration.SourcePath)
			if err != nil {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
			migration.TotalFiles, migration.TotalBytes = files, bytes
			migration.ProcessedFiles, migration.ProcessedBytes = 0, 0
			migration.UpdatedAtUTC = time.Now().UTC()
			if err = d.queue.store.UpdateLocalDirectoryMigration(migration, true); err != nil {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
			if err = copyLocalDirectory(ctx, migration, func(doneFiles, doneBytes int64) error {
				migration.ProcessedFiles, migration.ProcessedBytes = doneFiles, doneBytes
				migration.UpdatedAtUTC = time.Now().UTC()
				return d.queue.store.UpdateLocalDirectoryMigration(migration, false)
			}); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					migration.UpdatedAtUTC = time.Now().UTC()
					_ = d.queue.store.UpdateLocalDirectoryMigration(migration, true)
					return
				}
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
			migration.Phase = "verifying"
			migration.UpdatedAtUTC = time.Now().UTC()
			if err = d.queue.store.UpdateLocalDirectoryMigration(migration, true); err != nil {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
		case "verifying":
			if _, _, err := verifyLocalDirectory(ctx, migration.SourcePath, migration.TargetPath); err != nil {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
			migration.Phase = "retiring_source"
			migration.UpdatedAtUTC = time.Now().UTC()
			if err := d.queue.store.UpdateLocalDirectoryMigration(migration, true); err != nil {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
		case "retiring_source":
			if _, err := os.Lstat(migration.SourcePath); err == nil {
				if err = os.Rename(migration.SourcePath, migration.TombstonePath); err != nil {
					d.failLocalDirectoryMigration(&migration, fmt.Errorf("原目录仍被占用，请关闭占用程序后重试: %w", err))
					return
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				d.failLocalDirectoryMigration(&migration, err)
				return
			} else if _, tombstoneErr := os.Lstat(migration.TombstonePath); tombstoneErr != nil {
				d.failLocalDirectoryMigration(&migration, fmt.Errorf("原目录和迁移暂存目录都不存在，拒绝推断迁移结果"))
				return
			}
			migration.Phase = "source_retired"
			migration.UpdatedAtUTC = time.Now().UTC()
			if err := d.queue.store.UpdateLocalDirectoryMigration(migration, true); err != nil {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
		case "source_retired":
			if _, _, err := verifyLocalDirectory(ctx, migration.TombstonePath, migration.TargetPath); err != nil {
				if _, sourceErr := os.Lstat(migration.SourcePath); errors.Is(sourceErr, os.ErrNotExist) {
					if restoreErr := os.Rename(migration.TombstonePath, migration.SourcePath); restoreErr != nil {
						d.failLocalDirectoryMigration(&migration, fmt.Errorf("核验发现源目录变化且无法恢复原路径: %w", restoreErr))
						return
					}
				}
				migration.Phase, migration.UpdatedAtUTC = "copying", time.Now().UTC()
				if updateErr := d.queue.store.UpdateLocalDirectoryMigration(migration, true); updateErr != nil {
					d.failLocalDirectoryMigration(&migration, updateErr)
					return
				}
				continue
			}
			migration.Phase, migration.UpdatedAtUTC = "deleting_source", time.Now().UTC()
			if err := d.queue.store.UpdateLocalDirectoryMigration(migration, true); err != nil {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
		case "deleting_source":
			if _, err := os.Lstat(migration.SourcePath); err == nil {
				d.failLocalDirectoryMigration(&migration, fmt.Errorf("原路径在源目录隔离后被重新创建，拒绝删除"))
				return
			} else if !errors.Is(err, os.ErrNotExist) {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
			if err := os.RemoveAll(migration.TombstonePath); err != nil {
				d.failLocalDirectoryMigration(&migration, fmt.Errorf("无法清理已核验的原目录，请关闭占用程序后重试: %w", err))
				return
			}
			if err := d.saveAppSettings(map[string]any{"managed_queue_dir": migration.DesiredQueuePath, "managed_local_download_dir": migration.TargetPath}); err != nil {
				d.failLocalDirectoryMigration(&migration, fmt.Errorf("源目录已核验清理，但路径设置保存失败，可重试完成设置: %w", err))
				return
			}
			queueDir, localDir := migration.DesiredQueuePath, migration.TargetPath
			if d.cfg.QueueDirExplicit {
				queueDir = d.cfg.QueueDir
			}
			if d.cfg.LocalDownDirExplicit {
				localDir = d.cfg.LocalDownDir
			}
			d.pathMu.Lock()
			d.queueDir, d.localDownDir = queueDir, localDir
			d.pathMu.Unlock()
			migration.Status, migration.Phase = "completed", "complete"
			migration.LastError = ""
			migration.FinishedAtUTC, migration.UpdatedAtUTC = time.Now().UTC(), time.Now().UTC()
			if err := d.queue.store.UpdateLocalDirectoryMigration(migration, true); err != nil {
				return
			}
			completed = true
			return
		default:
			d.failLocalDirectoryMigration(&migration, fmt.Errorf("未知本地迁移阶段: %s", migration.Phase))
			return
		}
	}
}

func (d *Downloader) failLocalDirectoryMigration(m *taskstore.LocalDirectoryMigration, err error) {
	m.Status, m.LastError = "failed", err.Error()
	m.FinishedAtUTC, m.UpdatedAtUTC = time.Now().UTC(), time.Now().UTC()
	_ = d.queue.store.UpdateLocalDirectoryMigration(*m, true)
}

func scanLocalDirectory(ctx context.Context, root string) (int64, int64, error) {
	files, bytes := int64(0), int64(0)
	err := walkLocalDirectory(ctx, root, func(_ string, _ string, info os.FileInfo) error {
		if info.Mode().IsRegular() {
			files++
			bytes += info.Size()
		}
		return nil
	})
	return files, bytes, err
}

func copyLocalDirectory(ctx context.Context, migration taskstore.LocalDirectoryMigration, progress func(int64, int64) error) error {
	doneFiles, doneBytes := int64(0), int64(0)
	err := walkLocalDirectory(ctx, migration.SourcePath, func(source, relative string, info os.FileInfo) error {
		target := migration.TargetPath
		if relative != "." {
			target = filepath.Join(migration.TargetPath, relative)
		}
		if info.IsDir() {
			return ensureMigrationDirectory(target, info.Mode().Perm())
		}
		if err := copyMigrationFile(ctx, source, target, migration.ID, info); err != nil {
			return err
		}
		doneFiles++
		doneBytes += info.Size()
		return progress(doneFiles, doneBytes)
	})
	return err
}

func walkLocalDirectory(ctx context.Context, root string, visit func(string, string, os.FileInfo) error) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("源路径不是普通目录: %s", root)
	}
	return filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("源目录包含符号链接或特殊文件，已停止迁移: %s", name)
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if err = visit(name, relative, info); err != nil {
			return err
		}
		return nil
	})
}

func ensureMigrationDirectory(name string, mode os.FileMode) error {
	info, err := os.Lstat(name)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("目标路径类型冲突，未覆盖: %s", name)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.Mkdir(name, mode); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err = os.Lstat(name)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("创建目标目录后核验失败: %s", name)
	}
	return nil
}

func copyMigrationFile(ctx context.Context, source, target, migrationID string, sourceInfo os.FileInfo) error {
	if err := ensureMigrationDirectory(filepath.Dir(target), 0755); err != nil {
		return err
	}
	if equal, exists, err := equalRegularFiles(source, target); err != nil {
		return err
	} else if exists {
		if !equal {
			return fmt.Errorf("目标文件已存在且内容不同，未覆盖: %s", target)
		}
		return nil
	}
	nameHash := sha256.Sum256([]byte(target))
	temp := filepath.Join(filepath.Dir(target), ".nas-migrate-"+migrationID+"-"+hex.EncodeToString(nameHash[:8])+".part")
	_ = os.Remove(temp)
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, sourceInfo.Mode().Perm())
	if err != nil {
		in.Close()
		return err
	}
	hash := sha256.New()
	buffer := make([]byte, 1024*1024)
	var written int64
	for {
		if err = ctx.Err(); err != nil {
			break
		}
		var n int
		n, err = in.Read(buffer)
		if n > 0 {
			if _, writeErr := out.Write(buffer[:n]); writeErr != nil {
				err = writeErr
				break
			}
			_, _ = hash.Write(buffer[:n])
			written += int64(n)
		}
		if err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			break
		}
	}
	closeInErr, syncErr, closeOutErr := in.Close(), out.Sync(), out.Close()
	if err == nil {
		err = closeInErr
	}
	if err == nil {
		err = syncErr
	}
	if err == nil {
		err = closeOutErr
	}
	if err != nil {
		_ = os.Remove(temp)
		return err
	}
	current, err := os.Stat(source)
	if err != nil || !current.Mode().IsRegular() || current.Size() != sourceInfo.Size() || !current.ModTime().Equal(sourceInfo.ModTime()) || written != sourceInfo.Size() {
		_ = os.Remove(temp)
		return fmt.Errorf("源文件迁移期间发生变化: %s", source)
	}
	tempHash, err := hashMigrationFile(temp)
	if err != nil {
		_ = os.Remove(temp)
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), tempHash) {
		_ = os.Remove(temp)
		return fmt.Errorf("临时副本摘要核验失败: %s", source)
	}
	if err = os.Chtimes(temp, sourceInfo.ModTime(), sourceInfo.ModTime()); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if equal, exists, compareErr := equalRegularFiles(source, target); compareErr != nil {
		_ = os.Remove(temp)
		return compareErr
	} else if exists {
		_ = os.Remove(temp)
		if equal {
			return nil
		}
		return fmt.Errorf("目标文件在迁移期间出现冲突，未覆盖: %s", target)
	}
	if err = os.Link(temp, target); err == nil {
		return os.Remove(temp)
	}
	// Some NAS filesystems do not support hard links. Exclusive creation keeps
	// the no-overwrite guarantee; a crash during this fallback leaves the source
	// intact and causes the next run to stop on the partial target for inspection.
	out, createErr := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, sourceInfo.Mode().Perm())
	if createErr != nil {
		_ = os.Remove(temp)
		if errors.Is(createErr, os.ErrExist) {
			return fmt.Errorf("目标文件在迁移期间出现冲突，未覆盖: %s", target)
		}
		return createErr
	}
	copyErr := copyFileWithContext(ctx, out, temp)
	syncErr, closeErr := out.Sync(), out.Close()
	_ = os.Remove(temp)
	if copyErr != nil {
		_ = os.Remove(target)
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func copyFileWithContext(ctx context.Context, destination *os.File, source string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	buffer := make([]byte, 1024*1024)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, readErr := in.Read(buffer)
		if n > 0 {
			if _, err = destination.Write(buffer[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func equalRegularFiles(left, right string) (bool, bool, error) {
	info, err := os.Lstat(right)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, true, fmt.Errorf("目标文件不是普通文件，未覆盖: %s", right)
	}
	leftInfo, err := os.Lstat(left)
	if err != nil {
		return false, false, err
	}
	if leftInfo.Mode()&os.ModeSymlink != 0 || !leftInfo.Mode().IsRegular() {
		return false, false, fmt.Errorf("源文件不是普通文件: %s", left)
	}
	if info.Size() != leftInfo.Size() {
		return false, true, nil
	}
	leftHash, err := hashMigrationFile(left)
	if err != nil {
		return false, false, err
	}
	rightHash, err := hashMigrationFile(right)
	return strings.EqualFold(leftHash, rightHash), true, err
}

func hashMigrationFile(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyLocalDirectory(ctx context.Context, source, target string) (int64, int64, error) {
	files, bytes := int64(0), int64(0)
	err := walkLocalDirectory(ctx, source, func(name, relative string, info os.FileInfo) error {
		targetName := target
		if relative != "." {
			targetName = filepath.Join(target, relative)
		}
		if info.IsDir() {
			other, err := os.Lstat(targetName)
			if err != nil || other.Mode()&os.ModeSymlink != 0 || !other.IsDir() {
				return fmt.Errorf("目录核验失败: %s", targetName)
			}
			return nil
		}
		equal, exists, err := equalRegularFiles(name, targetName)
		if err != nil {
			return err
		}
		if !exists || !equal {
			return fmt.Errorf("文件核验失败: %s", targetName)
		}
		files++
		bytes += info.Size()
		return nil
	})
	return files, bytes, err
}
