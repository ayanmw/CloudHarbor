package downloader

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

var ErrIntakeConflict = errors.New("入队项目已变化、目标冲突或存在待核对记录")
var ErrCloudInventoryIncomplete = errors.New("全盘列表缓存已失效或尚未完整，请先完成全盘扫描")
var ErrIntakeInvalid = errors.New("入队路径或请求无效")
var ErrIntakeStale = errors.New("入队预检已过期或认证已变化，请重新预检")
var ErrIntakeNotFound = errors.New("入队记录不存在")
var ErrIntakeUnconfirmed = errors.New("入队结果未知，目标隔离等待核对")

func validCloudPath(value string) bool {
	if value == "" || len(value) > 2048 || !strings.HasPrefix(value, "/") || path.Clean(value) != value || strings.Contains(value, "\\") {
		return false
	}
	for _, c := range value {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func intakeTarget(queue, source string) string { return path.Join(queue, path.Base(source)) }
func insideCloud(value, parent string) bool {
	return value == parent || strings.HasPrefix(value, parent+"/")
}
func validateIntakePaths(paths []string, queue string) error {
	if len(paths) < 1 || len(paths) > 20 || !validCloudPath(queue) || queue == "/" {
		return ErrIntakeInvalid
	}
	names := make(map[string]bool)
	for i, value := range paths {
		name := path.Base(value)
		if !validCloudPath(value) || value == "/" || insideCloud(value, queue) || insideCloud(queue, value) || !validName(name) || strings.EqualFold(name, stateDirectory) || strings.HasSuffix(name, ".可删除") {
			return ErrIntakeInvalid
		}
		key := strings.ToLower(name)
		if names[key] {
			return ErrIntakeConflict
		}
		names[key] = true
		for j := 0; j < i; j++ {
			if insideCloud(value, paths[j]) || insideCloud(paths[j], value) {
				return ErrIntakeInvalid
			}
		}
	}
	return nil
}
func (d *Downloader) startIntake(ctx context.Context) (context.Context, func(), error) {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || d.ctx.Err() != nil {
		return nil, nil, context.Canceled
	}
	if q.store == nil {
		return nil, nil, fmt.Errorf("persistent intake store unavailable")
	}
	if q.err != nil {
		return nil, nil, q.err
	}
	q.wg.Add(1)
	scoped, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(d.ctx, cancel)
	return scoped, func() { stop(); cancel(); q.wg.Done() }, nil
}

func (d *Downloader) startPathSensitiveOperation(ctx context.Context) (context.Context, func(), error) {
	d.pathOperations.RLock()
	scoped, done, err := d.startIntake(ctx)
	if err != nil {
		d.pathOperations.RUnlock()
		return nil, nil, err
	}
	d.queue.mu.Lock()
	maintenance := d.queue.maintenance
	d.queue.mu.Unlock()
	if maintenance {
		done()
		d.pathOperations.RUnlock()
		return nil, nil, fmt.Errorf("下载队列正在维护，请稍后重试")
	}
	return scoped, func() {
		done()
		d.pathOperations.RUnlock()
	}, nil
}

func checkedListing(client *baiduapi.Client, dir string) ([]baiduapi.FileInfo, error) {
	files, err := client.ListDir(dir)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if !validListingFile(dir, f) {
			return nil, ErrIntakeConflict
		}
	}
	return files, nil
}

func validListingFile(dir string, f baiduapi.FileInfo) bool {
	return f.FsID > 0 && f.Size >= 0 && validName(f.ServerFilename) && f.Path == path.Join(dir, f.ServerFilename) && (f.IsDir == 0 || f.IsDir == 1)
}
func (d *Downloader) previewIntakeItems(client *baiduapi.Client, paths []string, queueDir string) ([]IntakeItem, error) {
	if err := validateIntakePaths(paths, queueDir); err != nil {
		return nil, err
	}
	queued, err := checkedListing(client, queueDir)
	if err != nil {
		return nil, err
	}
	cache := make(map[string][]baiduapi.FileInfo)
	ids := make(map[int64]bool)
	result := make([]IntakeItem, 0, len(paths))
	for _, source := range paths {
		target := intakeTarget(queueDir, source)
		for _, f := range queued {
			if strings.EqualFold(f.ServerFilename, path.Base(target)) {
				return nil, ErrIntakeConflict
			}
		}
		parent := path.Dir(source)
		files, loaded := cache[parent]
		if !loaded {
			files, err = checkedListing(client, parent)
			if err != nil {
				return nil, err
			}
			cache[parent] = files
		}
		var found *baiduapi.FileInfo
		for _, f := range files {
			if f.Path == source {
				copy := f
				found = &copy
				break
			}
		}
		if found == nil || ids[found.FsID] {
			return nil, ErrIntakeConflict
		}
		ids[found.FsID] = true
		d.queue.mu.Lock()
		conflict := d.queue.isRemoteBlocked(found.FsID, found.Path) || d.queue.blockedIntakes[target] || d.queue.blockedSources[target] || d.queue.jobs[found.FsID] != nil
		d.queue.mu.Unlock()
		if conflict {
			return nil, ErrIntakeConflict
		}
		result = append(result, IntakeItem{Source: *found, SourceID: fmt.Sprint(found.FsID), Target: target, State: "ready"})
	}
	return result, nil
}
func (d *Downloader) PreviewIntake(ctx context.Context, paths []string) (Intake, error) {
	ctx, done, err := d.startPathSensitiveOperation(ctx)
	if err != nil {
		return Intake{}, err
	}
	defer done()
	d.cloudMutex.RLock()
	defer d.cloudMutex.RUnlock()
	client, epoch, err := d.authorizedClient(ctx)
	if err != nil {
		return Intake{}, err
	}
	queueDir := d.QueueDirectory()
	items, err := d.previewIntakeItems(client, paths, queueDir)
	if err != nil {
		d.reportAuthenticationFailure(err)
		return Intake{}, err
	}
	id, err := newIntakeID()
	if err != nil {
		return Intake{}, err
	}
	now := time.Now()
	r := intakeRecord{Version: 1, Epoch: epoch, Boot: d.intakeBoot, Intake: Intake{ID: id, State: "preview", QueueDir: queueDir, Mode: d.GetDownloadMode(), Created: now, Expires: now.Add(2 * time.Minute), Items: items}}
	if err = d.saveIntake(r); err != nil {
		return Intake{}, err
	}
	return cloneIntake(r).Intake, nil
}
func (d *Downloader) intakeClient(ctx context.Context, epoch uint64) (*baiduapi.Client, error) {
	client, current, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, err
	}
	if current != epoch {
		return nil, ErrIntakeStale
	}
	return client, nil
}
func (d *Downloader) findIntake(id string) (intakeRecord, error) {
	if !validIntakeID(id) {
		return intakeRecord{}, ErrIntakeInvalid
	}
	d.intakeMutex.RLock()
	defer d.intakeMutex.RUnlock()
	r, ok := d.intakes[id]
	if !ok {
		return intakeRecord{}, ErrIntakeNotFound
	}
	return cloneIntake(r), nil
}
func (d *Downloader) registerIntakeItem(r intakeRecord, item IntakeItem, destination baiduapi.FileInfo) error {
	destination = shareFileInfoStrings(destination)
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || d.ctx.Err() != nil {
		return context.Canceled
	}
	if q.err != nil {
		return q.err
	}
	if q.blockedSources[destination.Path] {
		return ErrIntakeConflict
	}
	if existing := q.jobs[destination.FsID]; existing != nil {
		if existing.Info != destination || existing.Mode != r.Mode {
			return ErrIntakeConflict
		}
		return nil
	}
	job := &queueJob{Version: 1, Info: destination, Mode: r.Mode, Status: "pending", Created: r.Created}
	if err := q.persist(job); err != nil {
		return err
	}
	q.jobs[destination.FsID] = job
	d.taskCatalogGeneration.Add(1)
	q.reflect(job)
	return nil
}
func sameMovedIdentity(source, target baiduapi.FileInfo) bool {
	return source.FsID == target.FsID && source.Size == target.Size && source.IsDir == target.IsDir && source.MD5 == target.MD5 && source.ServerFilename == target.ServerFilename
}
func (d *Downloader) checkIntakeOutcome(client *baiduapi.Client, item IntakeItem, queueDir string) (baiduapi.FileInfo, bool, bool, error) {
	sources, err := checkedListing(client, path.Dir(item.Source.Path))
	if err != nil {
		return baiduapi.FileInfo{}, false, false, err
	}
	targets, err := checkedListing(client, queueDir)
	if err != nil {
		return baiduapi.FileInfo{}, false, false, err
	}
	sourceExists, sourceSame := false, false
	for _, f := range sources {
		if f.Path == item.Source.Path {
			sourceExists = true
			sourceSame = f == item.Source
		}
	}
	var target *baiduapi.FileInfo
	for _, f := range targets {
		if strings.EqualFold(f.ServerFilename, path.Base(item.Target)) {
			copy := f
			target = &copy
			break
		}
	}
	if !sourceExists && target != nil && target.Path == item.Target && sameMovedIdentity(item.Source, *target) {
		return *target, true, false, nil
	}
	return baiduapi.FileInfo{}, false, sourceSame && target == nil, nil
}
func (d *Downloader) CommitIntake(ctx context.Context, id string) (Intake, error) {
	ctx, done, err := d.startPathSensitiveOperation(ctx)
	if err != nil {
		return Intake{}, err
	}
	defer done()
	d.cloudMutex.Lock()
	defer d.cloudMutex.Unlock()
	r, err := d.findIntake(id)
	if err != nil {
		return Intake{}, err
	}
	if r.State != "preview" {
		return r.Intake, nil
	}
	if r.Boot != d.intakeBoot || !time.Now().Before(r.Expires) {
		return r.Intake, ErrIntakeStale
	}
	client, err := d.intakeClient(ctx, r.Epoch)
	if err != nil {
		return r.Intake, err
	}
	paths := make([]string, len(r.Items))
	for i, item := range r.Items {
		paths[i] = item.Source.Path
	}
	queueDir := d.QueueDirectory()
	if queueDir != r.QueueDir {
		return r.Intake, ErrIntakeStale
	}
	fresh, err := d.previewIntakeItems(client, paths, r.QueueDir)
	if err != nil {
		d.reportAuthenticationFailure(err)
		return r.Intake, err
	}
	for i, item := range fresh {
		if item.Source != r.Items[i].Source {
			return r.Intake, ErrIntakeConflict
		}
	}
	if _, err = d.intakeClient(ctx, r.Epoch); err != nil {
		return r.Intake, err
	}
	r.State = "running"
	if err = d.saveIntake(r); err != nil {
		return r.Intake, err
	}
	for i := range r.Items {
		item := &r.Items[i]
		client, err = d.intakeClient(ctx, r.Epoch)
		if err != nil {
			break
		}
		item.State = "sending"
		if err = d.saveIntake(r); err != nil {
			return r.Intake, err
		}
		// Persistence may block while authorization is revoked. The operation
		// is still definitely unsent here, so check again before any network call.
		client, err = d.intakeClient(ctx, r.Epoch)
		if err != nil {
			item.State = "not_sent"
			item.Error = "authorization_changed_or_cancelled"
			break
		}
		moveErr := client.MoveFile(item.Source.Path, queueDir, path.Base(item.Target))
		var async *baiduapi.AsyncMoveError
		if moveErr == nil {
			item.SyncConfirmed = true
		}
		if errors.As(moveErr, &async) {
			item.Error = "async_move_unconfirmed"
			item.AsyncTaskID = async.TaskID
		}
		// A directory root can appear before an async operation finishes. Only
		// a durable synchronous receipt permits later root-identity recovery.
		if moveErr == nil || async != nil {
			if err = d.saveIntake(r); err != nil {
				return r.Intake, err
			}
		}
		d.reportAuthenticationFailure(moveErr)
		client, authErr := d.intakeClient(ctx, r.Epoch)
		var destination baiduapi.FileInfo
		var moved, unchanged bool
		checkErr := authErr
		if checkErr == nil {
			destination, moved, unchanged, checkErr = d.checkIntakeOutcome(client, *item, r.QueueDir)
		}
		if checkErr != nil {
			d.reportAuthenticationFailure(checkErr)
		}
		item.State = "unknown"
		item.Error = "move_unconfirmed"
		if async != nil {
			moved = false
			unchanged = false
			item.Error = "async_move_unconfirmed"
			item.AsyncTaskID = async.TaskID
		}
		if item.Source.IsDir == 1 && !item.SyncConfirmed {
			moved = false
			if async == nil {
				item.Error = "directory_completion_unconfirmed"
			}
		}
		if moved {
			if err = d.registerIntakeItem(r, *item, destination); err != nil {
				item.Error = "queue_registration_failed"
			} else {
				item.State = "moved"
				item.Error = ""
			}
		} else {
			var api *baiduapi.APIError
			if unchanged && errors.As(moveErr, &api) && api.Status == 200 && api.Code != 0 {
				item.State = "failed"
				item.Error = "provider_rejected"
			}
		}
		if err = d.saveIntake(r); err != nil {
			return r.Intake, err
		}
		if item.State == "unknown" {
			break
		}
	}
	for i := range r.Items {
		if r.Items[i].State == "ready" {
			r.Items[i].State = "not_sent"
			r.Items[i].Error = "batch_stopped"
		}
	}
	r.State = "finished"
	if err = d.saveIntake(r); err != nil {
		return r.Intake, err
	}
	return cloneIntake(r).Intake, nil
}

// Recheck reads provider state; it never repeats a move or guesses that an
// unobserved operation cannot still arrive later.
func (d *Downloader) RecheckIntake(ctx context.Context, id string) (Intake, error) {
	ctx, done, err := d.startPathSensitiveOperation(ctx)
	if err != nil {
		return Intake{}, err
	}
	defer done()
	d.cloudMutex.Lock()
	defer d.cloudMutex.Unlock()
	r, err := d.findIntake(id)
	if err != nil {
		return Intake{}, err
	}
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return r.Intake, err
	}
	for i := range r.Items {
		item := &r.Items[i]
		if item.State != "unknown" && item.State != "sending" {
			continue
		}
		if item.Error == "async_move_unconfirmed" || (item.Source.IsDir == 1 && !item.SyncConfirmed) {
			continue
		}
		destination, moved, _, checkErr := d.checkIntakeOutcome(client, *item, r.QueueDir)
		if checkErr != nil {
			d.reportAuthenticationFailure(checkErr)
			return r.Intake, checkErr
		}
		if moved {
			if err = d.registerIntakeItem(r, *item, destination); err != nil {
				return r.Intake, err
			}
			item.State = "moved"
			item.Error = ""
			if err = d.saveIntake(r); err != nil {
				return r.Intake, err
			}
		}
	}
	return cloneIntake(r).Intake, nil
}
