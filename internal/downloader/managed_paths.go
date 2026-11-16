package downloader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

type ManagedPaths struct {
	QueueDir     string `json:"queue_dir"`
	LocalDownDir string `json:"local_download_dir"`
}

// ManagedPathSnapshot reports the effective configuration, not historical edit provenance.
type ManagedPathSnapshot struct {
	ManagedPaths
	QueueSource         string `json:"queue_source"`
	LocalDownloadSource string `json:"local_download_source"`
}

func (d *Downloader) GetManagedPathSnapshot() ManagedPathSnapshot {
	d.pathMu.RLock()
	defer d.pathMu.RUnlock()
	source := func(explicit, saved bool) string {
		if explicit {
			return "environment"
		}
		if saved {
			return "saved"
		}
		return "startup"
	}
	return ManagedPathSnapshot{ManagedPaths: ManagedPaths{QueueDir: d.queueDir, LocalDownDir: d.localDownDir}, QueueSource: source(d.cfg.QueueDirExplicit, d.queuePathSaved), LocalDownloadSource: source(d.cfg.LocalDownDirExplicit, d.localPathSaved)}
}

var ErrManagedPathsChanged = errors.New("当前路径配置已变化，请刷新后重新预览并确认")

type ManagedPathImpactTask struct {
	FsID         int64        `json:"fs_id"`
	Path         string       `json:"path"`
	Filename     string       `json:"server_filename"`
	Status       string       `json:"status"`
	Attempts     int          `json:"attempts"`
	DownloadMode DownloadMode `json:"download_mode"`
}

type ManagedPathImpact struct {
	CurrentQueueDir string                  `json:"current_queue_dir"`
	TargetQueueDir  string                  `json:"target_queue_dir"`
	Count           int                     `json:"count"`
	Offset          int                     `json:"offset"`
	Limit           int                     `json:"limit"`
	HasMore         bool                    `json:"has_more"`
	Tasks           []ManagedPathImpactTask `json:"tasks"`
	Digest          string                  `json:"digest"`
}

const managedPathImpactPageLimit = 50

type ManagedPathImpactConfirmationError struct {
	Impact ManagedPathImpact
	Stale  bool
}

func (e *ManagedPathImpactConfirmationError) Error() string {
	if e.Stale {
		return "队列在确认后发生变化，请核对最新受影响任务后再次确认"
	}
	return "切换监控目录会影响未执行任务，请先预览并确认"
}

// PreviewManagedPathChange snapshots unstarted queue roots that the target
// monitor directory will not list directly. The digest is revalidated before
// a confirmed settings write; it is not a remote identity proof.
func (d *Downloader) PreviewManagedPathChange(targetQueueDir string) (ManagedPathImpact, error) {
	return d.PreviewManagedPathChangePage(targetQueueDir, 0, managedPathImpactPageLimit)
}

func (d *Downloader) PreviewManagedPathChangePage(targetQueueDir string, offset, limit int) (ManagedPathImpact, error) {
	if !validCloudPath(targetQueueDir) || targetQueueDir == "/" {
		return ManagedPathImpact{}, fmt.Errorf("网盘监控目录必须是以 / 开头的非根目录")
	}
	if offset < 0 || limit < 1 || limit > managedPathImpactPageLimit {
		return ManagedPathImpact{}, fmt.Errorf("监控目录影响预览分页参数无效")
	}
	d.pathOperations.RLock()
	defer d.pathOperations.RUnlock()
	current := d.GetManagedPaths()
	return d.managedPathImpact(current.QueueDir, targetQueueDir, offset, limit), nil
}

func (d *Downloader) managedPathImpact(currentQueueDir, targetQueueDir string, offset, limit int) ManagedPathImpact {
	currentQueueDir, targetQueueDir = path.Clean(currentQueueDir), path.Clean(targetQueueDir)
	impact := ManagedPathImpact{
		CurrentQueueDir: currentQueueDir,
		TargetQueueDir:  targetQueueDir,
		Tasks:           make([]ManagedPathImpactTask, 0),
		Offset:          offset,
		Limit:           limit,
	}
	d.queue.mu.Lock()
	for _, job := range d.queue.jobs {
		if job == nil || (job.Status != "pending" && job.Status != "auth_wait" && job.Status != "retry_wait") {
			continue
		}
		jobPath := path.Clean(job.Info.Path)
		if path.Dir(jobPath) != currentQueueDir || path.Dir(jobPath) == targetQueueDir {
			continue
		}
		impact.Tasks = append(impact.Tasks, ManagedPathImpactTask{
			FsID: job.Info.FsID, Path: job.Info.Path, Filename: job.Info.ServerFilename,
			Status: job.Status, Attempts: job.Attempts, DownloadMode: job.Mode,
		})
	}
	d.queue.mu.Unlock()
	sort.Slice(impact.Tasks, func(i, j int) bool { return impact.Tasks[i].FsID < impact.Tasks[j].FsID })
	impact.Count = len(impact.Tasks)
	encoded, _ := json.Marshal(struct {
		CurrentQueueDir string                  `json:"current_queue_dir"`
		TargetQueueDir  string                  `json:"target_queue_dir"`
		Tasks           []ManagedPathImpactTask `json:"tasks"`
	}{impact.CurrentQueueDir, impact.TargetQueueDir, impact.Tasks})
	digest := sha256.Sum256(encoded)
	impact.Digest = hex.EncodeToString(digest[:])
	if offset > impact.Count {
		offset = impact.Count
	}
	impact.Offset = offset
	end := impact.Count
	if limit < impact.Count-offset {
		end = offset + limit
	}
	impact.HasMore = end < impact.Count
	impact.Tasks = impact.Tasks[offset:end]
	return impact
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
	return d.SetManagedPathsConfirmed(ctx, paths, migrateFiles, "")
}

// An optional expected snapshot is checked under the same path lock that selects
// the migration source. Legacy callers without a snapshot retain their behavior.
func (d *Downloader) SetManagedPathsConfirmed(ctx context.Context, paths ManagedPaths, migrateFiles bool, impactDigest string, expected ...ManagedPaths) (taskstore.LocalDirectoryMigration, error) {
	if len(expected) > 1 {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("路径前置条件只能提供一份")
	}
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
	d.pathOperations.Lock()
	defer d.pathOperations.Unlock()
	if err = ctx.Err(); err != nil {
		return taskstore.LocalDirectoryMigration{}, err
	}
	if d.managedPathsAdmissionHook != nil {
		d.managedPathsAdmissionHook()
	}
	current := d.GetManagedPaths()
	if len(expected) == 1 && (expected[0].QueueDir != current.QueueDir || strings.TrimSpace(expected[0].LocalDownDir) == "" || !sameLocalPath(expected[0].LocalDownDir, current.LocalDownDir)) {
		return taskstore.LocalDirectoryMigration{}, ErrManagedPathsChanged
	}
	samePaths := current.QueueDir == paths.QueueDir && sameLocalPath(current.LocalDownDir, local)
	var failedMigration taskstore.LocalDirectoryMigration
	resumeFailedMigration := false
	if samePaths && migrateFiles {
		failedMigration, err = d.queue.store.LoadLatestLocalDirectoryMigration()
		if err != nil && err != sql.ErrNoRows {
			return taskstore.LocalDirectoryMigration{}, err
		}
		resumeFailedMigration = err == nil && failedMigration.Status == "failed" &&
			failedMigration.TargetPath == local && failedMigration.DesiredQueuePath == paths.QueueDir
	}
	if samePaths && !resumeFailedMigration {
		return taskstore.LocalDirectoryMigration{}, nil
	}
	if d.cfg.QueueDirExplicit && paths.QueueDir != current.QueueDir {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("网盘监控目录由 .env 或启动环境锁定")
	}
	if d.cfg.LocalDownDirExplicit && !sameLocalPath(local, current.LocalDownDir) {
		return taskstore.LocalDirectoryMigration{}, fmt.Errorf("本地下载目录由 .env 或启动环境锁定")
	}
	if err = validateLocalPathOutsideState(local, d.cfg.StateDir); err != nil {
		return taskstore.LocalDirectoryMigration{}, err
	}
	for _, intake := range d.GetIntakes() {
		if intake.State == "preview" || intake.State == "running" {
			return taskstore.LocalDirectoryMigration{}, fmt.Errorf("存在未完成的网盘入队操作，暂不能修改路径")
		}
	}
	if d.managedPathsMaintenanceHook != nil {
		d.managedPathsMaintenanceHook()
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
	// A finishing migration publishes paths under queue maintenance rather than
	// pathOperations. Recheck after acquiring that gate before using the source.
	latest := d.GetManagedPaths()
	if latest.QueueDir != current.QueueDir || !sameLocalPath(latest.LocalDownDir, current.LocalDownDir) {
		return taskstore.LocalDirectoryMigration{}, ErrManagedPathsChanged
	}
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
	impact := d.managedPathImpact(current.QueueDir, paths.QueueDir, 0, managedPathImpactPageLimit)
	if (impact.Count > 0 && impactDigest == "") || (impactDigest != "" && impactDigest != impact.Digest) {
		return taskstore.LocalDirectoryMigration{}, &ManagedPathImpactConfirmationError{Impact: impact, Stale: impactDigest != ""}
	}
	if migrateFiles && (!sameLocalPath(current.LocalDownDir, local) || resumeFailedMigration) {
		source := current.LocalDownDir
		if resumeFailedMigration {
			source = failedMigration.SourcePath
		}
		migration, startErr := d.beginLocalDirectoryMigration(source, local, paths.QueueDir)
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
	d.queuePathSaved, d.localPathSaved = true, true
	d.pathMu.Unlock()
	if current.QueueDir != paths.QueueDir {
		d.invalidateQueueEligibility()
	}
	return taskstore.LocalDirectoryMigration{}, nil
}

// A successful scan is the only evidence that can authorize tasks from a
// monitoring directory. Discard in-memory eligibility when that directory
// changes, while retaining persisted task history for the next reconciliation.
func (d *Downloader) invalidateQueueEligibility() {
	q := d.queue
	q.mu.Lock()
	q.eligible = make(map[int64]*baiduapi.FileInfo)
	q.clearCandidates()
	q.mu.Unlock()
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
	if reason := d.managedPathQueueBlockReasonLocked(); reason != "" {
		return errors.New(reason)
	}
	q.maintenance = true
	q.signal()
	return nil
}

// ManagedPathQueueBlockReason is a read-only observation, not an admission lease.
// Saving still performs all path, intake, scan, and maintenance checks.
func (d *Downloader) ManagedPathQueueBlockReason() string {
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	return d.managedPathQueueBlockReasonLocked()
}

// Caller holds queue.mu, shared with the maintenance admission check above.
func (d *Downloader) managedPathQueueBlockReasonLocked() string {
	q := d.queue
	if q.stopping || q.err != nil {
		return "下载队列不可用"
	}
	if q.maintenance {
		return "下载队列正在维护"
	}
	if len(q.active) != 0 {
		return "有下载任务正在运行，暂不能修改路径"
	}
	return ""
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
