package downloader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/config"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func validateLocalMigrationPaths(source, target, state string) error {
	resolvedSource, err := resolveLocalPath(source)
	if err != nil {
		return fmt.Errorf("无法解析迁移源路径: %w", err)
	}
	resolvedTarget, err := resolveLocalPath(target)
	if err != nil {
		return fmt.Errorf("无法解析迁移目标路径: %w", err)
	}
	if sameLocalPath(resolvedSource, resolvedTarget) || pathsOverlap(resolvedSource, resolvedTarget) {
		return fmt.Errorf("迁移源目录与目标目录解析后相同或互相包含")
	}
	if state != "" {
		resolvedState, err := resolveLocalPath(state)
		if err != nil {
			return fmt.Errorf("无法解析程序状态目录: %w", err)
		}
		if pathsOverlap(resolvedSource, resolvedState) || pathsOverlap(resolvedTarget, resolvedState) {
			return fmt.Errorf("迁移目录解析后与程序状态目录重叠")
		}
	}
	return nil
}

func expectedLocalMigrationTombstone(source, id string) string {
	return source + ".nas-migration-" + id
}

func validateLocalMigrationRecordPaths(migration taskstore.LocalDirectoryMigration, state string) error {
	if !sameLocalPath(migration.TombstonePath, expectedLocalMigrationTombstone(migration.SourcePath, migration.ID)) {
		return fmt.Errorf("迁移隔离目录路径与迁移记录不匹配")
	}
	return validateLocalMigrationPaths(migration.SourcePath, migration.TargetPath, state)
}

func rejectUnsafeLocalMigrationResume(store *taskstore.Store, migration taskstore.LocalDirectoryMigration, cause error) error {
	now := time.Now().UTC()
	migration.Status = "failed"
	migration.LastError = fmt.Sprintf("无法安全恢复本地目录迁移: %v", cause)
	migration.FinishedAtUTC, migration.UpdatedAtUTC = now, now
	if err := store.UpdateLocalDirectoryMigration(migration, true); err != nil {
		return fmt.Errorf("记录本地迁移安全校验失败时出错: %w", err)
	}
	return nil
}

func validateLocalMigrationStartupConfig(migration taskstore.LocalDirectoryMigration, cfg *config.Config) error {
	if cfg.QueueDirExplicit && migration.DesiredQueuePath != cfg.QueueDir {
		return fmt.Errorf("迁移监控目录与当前启动配置锁定路径不同")
	}
	if cfg.LocalDownDirExplicit && !sameLocalPath(migration.TargetPath, cfg.LocalDownDir) {
		return fmt.Errorf("迁移下载目录与当前启动配置锁定路径不同")
	}
	return nil
}

func validateLocalPathOutsideState(path, state string) error {
	if state == "" {
		return nil
	}
	resolvedPath, err := resolveLocalPath(path)
	if err != nil {
		return fmt.Errorf("无法解析本地下载目录: %w", err)
	}
	resolvedState, err := resolveLocalPath(state)
	if err != nil {
		return fmt.Errorf("无法解析程序状态目录: %w", err)
	}
	if pathsOverlap(resolvedPath, resolvedState) {
		return fmt.Errorf("本地下载目录解析后与程序状态目录重叠")
	}
	return nil
}

// resolveLocalPath resolves every existing ancestor before appending missing
// path components, so junction aliases are checked before target creation.
func resolveLocalPath(name string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(name))
	if err != nil {
		return "", err
	}
	probe := absolute
	missing := make([]string, 0, 4)
	for {
		resolved, resolveErr := resolveExistingLocalPath(probe)
		if resolveErr == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(resolveErr) {
			return "", resolveErr
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", resolveErr
		}
		missing = append(missing, filepath.Base(probe))
		probe = parent
	}
}

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
	latest, latestErr := d.queue.store.LoadLatestLocalDirectoryMigration()
	if latestErr != nil && latestErr != sql.ErrNoRows {
		return taskstore.LocalDirectoryMigration{}, latestErr
	}
	if latestErr == nil && latest.SourcePath == source && latest.TargetPath == target && latest.DesiredQueuePath == queueAfter && latest.Status == "failed" {
		if err = validateLocalMigrationRecordPaths(latest, d.cfg.StateDir); err != nil {
			return taskstore.LocalDirectoryMigration{}, fmt.Errorf("无法安全重试本地目录迁移: %w", err)
		}
	}
	sameFailedRetirement := latestErr == nil && latest.SourcePath == source && latest.TargetPath == target && latest.DesiredQueuePath == queueAfter &&
		latest.Status == "failed" && (latest.Phase == "retiring_source" || latest.Phase == "source_retired" || latest.Phase == "deleting_source")
	resumeAfterCleanup := false
	resumeWithTombstone := false
	sourceInfo, sourceErr := os.Lstat(source)
	if sourceErr != nil {
		if !sameFailedRetirement || !errors.Is(sourceErr, os.ErrNotExist) {
			return taskstore.LocalDirectoryMigration{}, fmt.Errorf("无法访问原下载目录: %w", sourceErr)
		}
		tombstoneInfo, tombstoneErr := os.Lstat(latest.TombstonePath)
		if errors.Is(tombstoneErr, os.ErrNotExist) && (strings.HasPrefix(latest.LastError, "源目录已核验清理，但路径设置保存失败:") || strings.HasPrefix(latest.LastError, "源目录已核验清理，但迁移完成记录保存失败")) {
			resumeAfterCleanup = true
		} else if tombstoneErr == nil && tombstoneInfo.Mode()&os.ModeSymlink == 0 && tombstoneInfo.IsDir() {
			resumeWithTombstone = true
			if latest.Phase == "retiring_source" {
				// The tombstone proves the rename succeeded before its checkpoint was saved.
				latest.Phase = "source_retired"
			}
		} else {
			return taskstore.LocalDirectoryMigration{}, fmt.Errorf("无法安全恢复：原下载目录不存在，迁移暂存目录状态不符合失败记录")
		}
	} else if sourceInfo.Mode()&os.ModeSymlink != 0 || !sourceInfo.IsDir() {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("原下载路径必须是普通目录，不能是符号链接")
	}
	if err = validateLocalMigrationPaths(source, target, d.cfg.StateDir); err != nil {
		return taskstore.LocalDirectoryMigration{}, err
	}
	if resumeAfterCleanup || resumeWithTombstone {
		targetInfo, targetErr := os.Lstat(target)
		if targetErr != nil || targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.IsDir() {
			return taskstore.LocalDirectoryMigration{}, fmt.Errorf("无法安全恢复路径设置：目标下载目录不可用")
		}
	} else {
		if err = os.MkdirAll(target, 0755); err != nil {
			return taskstore.LocalDirectoryMigration{}, fmt.Errorf("无法创建目标下载目录: %w", err)
		}
		targetInfo, targetErr := os.Lstat(target)
		if targetErr != nil || targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.IsDir() {
			return taskstore.LocalDirectoryMigration{}, fmt.Errorf("目标下载路径必须是普通目录，不能是符号链接")
		}
	}
	if err = validateLocalMigrationPaths(source, target, d.cfg.StateDir); err != nil {
		return taskstore.LocalDirectoryMigration{}, err
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
	if d.localMigrationRunning || d.localMigrationShutdown {
		d.localMigrationMu.Unlock()
		return
	}
	q := d.queue
	q.mu.Lock()
	if q.stopping || q.err != nil {
		q.mu.Unlock()
		d.localMigrationMu.Unlock()
		return
	}
	d.localMigrationRunning, d.localMigrationPending = true, false
	d.localMigrationWG.Add(1)
	q.mu.Unlock()
	d.localMigrationMu.Unlock()
	go func() {
		defer d.localMigrationWG.Done()
		d.runLocalDirectoryMigration(d.ctx, migration)
	}()
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
	rejectResume := func(cause error) error {
		if err := rejectUnsafeLocalMigrationResume(d.queue.store, migration, cause); err != nil {
			return err
		}
		// OpenDownloader gates the queue for every persisted active migration; once a refusal is durably recorded, no runner will release that gate.
		d.EndMaintenance()
		return nil
	}
	if err = validateLocalMigrationRecordPaths(migration, d.cfg.StateDir); err != nil {
		return rejectResume(err)
	}
	if err = validateLocalMigrationStartupConfig(migration, d.cfg); err != nil {
		return rejectResume(err)
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

func (d *Downloader) ExportLocalDirectoryMigrationEvidence(ctx context.Context) (taskstore.LocalDirectoryMigrationEvidenceBundle, error) {
	return d.queue.store.ExportLatestLocalDirectoryMigrationEvidence(ctx)
}

func (d *Downloader) ListLocalDirectoryMigrations(ctx context.Context, filter taskstore.LocalDirectoryMigrationFilter, offset, limit int) ([]taskstore.LocalDirectoryMigration, int, error) {
	return d.queue.store.ListFilteredLocalDirectoryMigrations(ctx, filter, offset, limit)
}

func (d *Downloader) ExportLocalDirectoryMigrationEvidenceByID(ctx context.Context, id string) (taskstore.LocalDirectoryMigrationEvidenceBundle, error) {
	return d.queue.store.ExportLocalDirectoryMigrationEvidence(ctx, id)
}

func (d *Downloader) ExportLocalDirectoryMigrationSummaries(ctx context.Context, filter taskstore.LocalDirectoryMigrationFilter) ([]taskstore.LocalDirectoryMigration, error) {
	return d.queue.store.ExportFilteredLocalDirectoryMigrationSummaries(ctx, filter)
}

func (d *Downloader) LocalDirectoryMigrationEventPage(ctx context.Context, id string, beforeSequence int64, limit int) ([]taskstore.LocalDirectoryMigrationEvidenceEvent, int64, bool, error) {
	return d.queue.store.LocalDirectoryMigrationEventPage(ctx, id, beforeSequence, limit)
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
			if d.localMigrationCopyStartHook != nil {
				d.localMigrationCopyStartHook()
			}
			if err = copyLocalDirectory(ctx, migration, func(doneFiles, doneBytes int64) error {
				if doneFiles > migration.TotalFiles {
					migration.TotalFiles = doneFiles
				}
				if doneBytes > migration.TotalBytes {
					migration.TotalBytes = doneBytes
				}
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
			migration.TotalFiles, migration.TotalBytes = migration.ProcessedFiles, migration.ProcessedBytes
			migration.Phase = "verifying"
			migration.UpdatedAtUTC = time.Now().UTC()
			if err = d.queue.store.UpdateLocalDirectoryMigration(migration, true); err != nil {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
		case "verifying":
			if _, _, err := verifyLocalDirectory(ctx, migration.SourcePath, migration.TargetPath); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					d.pauseLocalDirectoryMigration(&migration)
					return
				}
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
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					d.pauseLocalDirectoryMigration(&migration)
					return
				}
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
			digest, digestErr := hashLocalDirectory(ctx, migration.TargetPath)
			if digestErr != nil {
				if errors.Is(digestErr, context.Canceled) || errors.Is(digestErr, context.DeadlineExceeded) {
					d.pauseLocalDirectoryMigration(&migration)
					return
				}
				d.failLocalDirectoryMigration(&migration, fmt.Errorf("无法保存目标目录核验指纹: %w", digestErr))
				return
			}
			migration.VerifiedTargetSHA256 = digest
			migration.Phase, migration.UpdatedAtUTC = "deleting_source", time.Now().UTC()
			if err := d.queue.store.UpdateLocalDirectoryMigration(migration, true); err != nil {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
		case "deleting_source":
			if migration.VerifiedTargetSHA256 == "" {
				d.failLocalDirectoryMigration(&migration, fmt.Errorf("缺少目标目录核验指纹，拒绝清理隔离源目录；请恢复/核对目标内容后重新发起迁移"))
				return
			}
			targetDigest, err := hashLocalDirectory(ctx, migration.TargetPath)
			if err != nil || targetDigest != migration.VerifiedTargetSHA256 {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					d.pauseLocalDirectoryMigration(&migration)
					return
				}
				if err == nil {
					err = fmt.Errorf("目标目录内容与删除前核验结果不同")
				}
				d.failLocalDirectoryMigration(&migration, fmt.Errorf("删除隔离源目录前目标核验失败，已保留源数据: %w", err))
				return
			}
			if _, err := os.Lstat(migration.SourcePath); err == nil {
				d.failLocalDirectoryMigration(&migration, fmt.Errorf("原路径在源目录隔离后被重新创建，拒绝删除"))
				return
			} else if !errors.Is(err, os.ErrNotExist) {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
			if _, err := os.Lstat(migration.TombstonePath); err == nil {
				if _, _, err = verifyLocalDirectory(ctx, migration.TombstonePath, migration.TargetPath); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						d.pauseLocalDirectoryMigration(&migration)
						return
					}
					d.failLocalDirectoryMigration(&migration, fmt.Errorf("删除原目录前目标内容复核失败，已保留原目录: %w", err))
					return
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				d.failLocalDirectoryMigration(&migration, err)
				return
			}
			removeAll := os.RemoveAll
			if d.localMigrationRemoveAllHook != nil {
				removeAll = d.localMigrationRemoveAllHook
			}
			if err := removeAll(migration.TombstonePath); err != nil {
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
			previousQueueDir := d.GetManagedPaths().QueueDir
			if previousQueueDir != queueDir {
				d.invalidateQueueEligibility()
			}
			d.pathMu.Lock()
			d.queueDir, d.localDownDir = queueDir, localDir
			d.queuePathSaved, d.localPathSaved = true, true
			d.pathMu.Unlock()
			migration.Status, migration.Phase = "completed", "complete"
			migration.LastError = ""
			migration.FinishedAtUTC, migration.UpdatedAtUTC = time.Now().UTC(), time.Now().UTC()
			if err := d.queue.store.UpdateLocalDirectoryMigration(migration, true); err != nil {
				// The verified target and tombstone cleanup are already durable; keep this migration retryable if only the terminal checkpoint fails.
				migration.Phase = "deleting_source"
				d.failLocalDirectoryMigration(&migration, fmt.Errorf("源目录已核验清理，但迁移完成记录保存失败，可重试完成记录: %w", err))
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

func (d *Downloader) pauseLocalDirectoryMigration(m *taskstore.LocalDirectoryMigration) {
	m.Status, m.LastError = "running", ""
	m.FinishedAtUTC, m.UpdatedAtUTC = time.Time{}, time.Now().UTC()
	_ = d.queue.store.UpdateLocalDirectoryMigration(*m, true)
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
	nameHash := sha256.Sum256([]byte(target))
	temp := filepath.Join(filepath.Dir(target), ".nas-migrate-"+migrationID+"-"+hex.EncodeToString(nameHash[:8])+".part")
	resumePreparedPart := false
	if equal, exists, err := equalRegularFiles(source, target); err != nil {
		return err
	} else if exists {
		if !equal {
			if stagedEqual, stagedExists, stagedErr := equalRegularFiles(source, temp); stagedErr != nil {
				return stagedErr
			} else if !stagedExists || !stagedEqual {
				return fmt.Errorf("目标文件已存在且内容不同，未覆盖: %s", target)
			}
			partial, prefixErr := isStrictFilePrefix(target, temp)
			if prefixErr != nil {
				return prefixErr
			}
			if !partial {
				return fmt.Errorf("目标文件已存在且内容不同，未覆盖: %s", target)
			}
			// A verified same-migration staging file plus an exact shorter prefix
			// identifies the target as an interrupted fallback copy.
			if err = os.Remove(target); err != nil {
				return fmt.Errorf("无法清理已核验的迁移部分目标文件: %w", err)
			}
			resumePreparedPart = true
		} else {
			if err = os.Remove(temp); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("无法清理已完成迁移留下的临时文件: %w", err)
			}
			return nil
		}
	}
	if !resumePreparedPart {
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
		if err := os.Chtimes(temp, sourceInfo.ModTime(), sourceInfo.ModTime()); err != nil {
			_ = os.Remove(temp)
			return err
		}
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
	if linkErr := os.Link(temp, target); linkErr == nil {
		return os.Remove(temp)
	}
	// Some NAS filesystems do not support hard links. Exclusive creation keeps
	// the no-overwrite guarantee; a crash leaves the source and staged part intact
	// so a later run can resume only when the partial target matches its prefix.
	return copyMigrationPartToExclusiveTarget(ctx, target, temp, sourceInfo.Mode().Perm(), func(file *os.File) error {
		return file.Sync()
	}, func(file *os.File) error {
		return file.Close()
	}, os.Remove)
}

func copyMigrationPartToExclusiveTarget(ctx context.Context, target, temp string, mode os.FileMode, syncTarget, closeTarget func(*os.File) error, removeTarget func(string) error) error {
	out, createErr := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if createErr != nil {
		_ = os.Remove(temp)
		if errors.Is(createErr, os.ErrExist) {
			return fmt.Errorf("目标文件在迁移期间出现冲突，未覆盖: %s", target)
		}
		return createErr
	}
	copyErr := copyFileWithContext(ctx, out, temp)
	syncErr, closeErr := syncTarget(out), closeTarget(out)
	if copyErr != nil {
		removeErr := removeTarget(target)
		if removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
			_ = os.Remove(temp)
			return copyErr
		}
		return fmt.Errorf("复制失败且目标残片无法清理，已保留核验临时文件: %w", errors.Join(copyErr, removeErr))
	}
	if syncErr != nil {
		if closeErr != nil {
			return fmt.Errorf("目标文件同步失败: %v；关闭目标文件失败: %w", syncErr, closeErr)
		}
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	_ = os.Remove(temp)
	return nil
}

func isStrictFilePrefix(prefixPath, fullPath string) (bool, error) {
	prefixInfo, err := os.Lstat(prefixPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if prefixInfo.Mode()&os.ModeSymlink != 0 || !prefixInfo.Mode().IsRegular() {
		return false, fmt.Errorf("目标文件不是普通文件，未覆盖: %s", prefixPath)
	}
	fullInfo, err := os.Lstat(fullPath)
	if err != nil {
		return false, err
	}
	if fullInfo.Mode()&os.ModeSymlink != 0 || !fullInfo.Mode().IsRegular() {
		return false, fmt.Errorf("迁移临时文件不是普通文件: %s", fullPath)
	}
	if prefixInfo.Size() >= fullInfo.Size() {
		return false, nil
	}
	prefix, err := os.Open(prefixPath)
	if err != nil {
		return false, err
	}
	defer prefix.Close()
	full, err := os.Open(fullPath)
	if err != nil {
		return false, err
	}
	defer full.Close()
	buffer := make([]byte, 64*1024)
	other := make([]byte, len(buffer))
	for remaining := prefixInfo.Size(); remaining > 0; {
		chunk := int64(len(buffer))
		if chunk > remaining {
			chunk = remaining
		}
		if _, err = io.ReadFull(prefix, buffer[:chunk]); err != nil {
			return false, err
		}
		if _, err = io.ReadFull(full, other[:chunk]); err != nil {
			return false, err
		}
		for i := int64(0); i < chunk; i++ {
			if buffer[i] != other[i] {
				return false, nil
			}
		}
		remaining -= chunk
	}
	return true, nil
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

func hashLocalDirectory(ctx context.Context, root string) (string, error) {
	hash := sha256.New()
	writeField := func(value []byte) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write(value)
	}
	err := walkLocalDirectory(ctx, root, func(name, relative string, info os.FileInfo) error {
		writeField([]byte(filepath.ToSlash(relative)))
		if info.IsDir() {
			_, _ = hash.Write([]byte{'D'})
			return nil
		}
		_, _ = hash.Write([]byte{'F'})
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(info.Size()))
		_, _ = hash.Write(size[:])
		file, err := os.Open(name)
		if err != nil {
			return err
		}
		contentHash := sha256.New()
		buffer := make([]byte, 1024*1024)
		var copied int64
		for {
			if err = ctx.Err(); err != nil {
				break
			}
			var n int
			n, err = file.Read(buffer)
			if n > 0 {
				_, _ = contentHash.Write(buffer[:n])
				copied += int64(n)
			}
			if err == io.EOF {
				err = nil
				break
			}
			if err != nil {
				break
			}
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		current, err := os.Lstat(name)
		if err != nil || current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || current.Size() != info.Size() || !current.ModTime().Equal(info.ModTime()) || copied != info.Size() {
			return fmt.Errorf("核验期间文件发生变化: %s", name)
		}
		_, _ = hash.Write(contentHash.Sum(nil))
		return nil
	})
	if err != nil {
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
