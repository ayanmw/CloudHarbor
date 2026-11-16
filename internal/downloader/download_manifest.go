package downloader

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

// Only the small pending batch is retained here. The preparation traversal has
// its own observed directory lists; this recorder never copies the entire tree.
type downloadManifestRecorder struct {
	store   *taskstore.Store
	id      string
	entries []taskstore.DownloadManifestEntry
}

type downloadManifestContextKey struct{}
type downloadManifestBinding struct {
	store *taskstore.Store
	id    string
}

func (r *downloadManifestRecorder) bind(ctx context.Context) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, downloadManifestContextKey{}, downloadManifestBinding{store: r.store, id: r.id})
}

// Preparation already published this root's current manifest. Read it once and
// bind its immutable ID to the worker context, never rediscover it per file.
func (d *Downloader) bindPreparedDownloadManifest(ctx context.Context, root baiduapi.FileInfo) (context.Context, error) {
	q := d.queue
	q.mu.Lock()
	store := q.store
	job := q.jobs[root.FsID]
	attempt := ""
	if job != nil {
		attempt = job.AttemptID
	}
	q.mu.Unlock()
	if store == nil || job == nil {
		return ctx, nil
	}
	m, err := store.CurrentDownloadManifest(ctx, root.FsID)
	if err != nil {
		return ctx, err
	}
	if m == nil || m.AttemptID != attempt || m.RootPath != root.Path {
		return ctx, fmt.Errorf("本次目录清单未完整发布，停止下载")
	}
	return context.WithValue(ctx, downloadManifestContextKey{}, downloadManifestBinding{store: store, id: m.ID}), nil
}

func recordDownloadManifestResult(ctx context.Context, info baiduapi.FileInfo, relative, kind, digest string) error {
	binding, ok := ctx.Value(downloadManifestContextKey{}).(downloadManifestBinding)
	if !ok {
		return nil
	}
	// Preparation stores slash-separated relative paths; Windows workers use
	// filepath.Join. Normalize only this new history key, leaving receipts intact.
	expected := taskstore.DownloadManifestEntry{FsID: info.FsID, Path: info.Path, Name: info.ServerFilename, IsDir: info.IsDir, Size: info.Size, Mtime: info.Mtime, CloudMD5: info.MD5, RelativePath: filepath.ToSlash(relative)}
	if err := binding.store.RecordDownloadManifestResult(ctx, binding.id, expected, taskstore.DownloadManifestResult{Kind: kind, SHA256: digest, RecordedAtUTC: time.Now().UTC()}); err != nil {
		return fmt.Errorf("保存本地文件完成证据失败: %w", err)
	}
	return nil
}

func recordDownloadManifestState(ctx context.Context, info baiduapi.FileInfo, relative, status, summary string) error {
	binding, ok := ctx.Value(downloadManifestContextKey{}).(downloadManifestBinding)
	if !ok {
		return nil
	}
	expected := taskstore.DownloadManifestEntry{FsID: info.FsID, Path: info.Path, Name: info.ServerFilename, IsDir: info.IsDir, Size: info.Size, Mtime: info.Mtime, CloudMD5: info.MD5, RelativePath: filepath.ToSlash(relative)}
	if err := binding.store.RecordDownloadManifestState(ctx, binding.id, expected, status, summary); err != nil {
		return fmt.Errorf("保存文件执行状态失败: %w", err)
	}
	return nil
}

func (d *Downloader) beginDownloadManifest(ctx context.Context, root baiduapi.FileInfo, relative string) (*downloadManifestRecorder, error) {
	q := d.queue
	q.mu.Lock()
	job := q.jobs[root.FsID]
	store := q.store
	attempt := ""
	if job != nil {
		attempt = job.AttemptID
	}
	q.mu.Unlock()
	if store == nil || job == nil {
		return nil, nil
	}
	id, err := newSourceOperationID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err = store.BeginDownloadManifest(ctx, taskstore.DownloadManifest{ID: id, RootID: root.FsID, RootPath: root.Path, AttemptID: attempt, StartedAtUTC: now}); err != nil {
		return nil, err
	}
	recorder := &downloadManifestRecorder{store: store, id: id, entries: make([]taskstore.DownloadManifestEntry, 0, taskstore.DownloadManifestBatchLimit)}
	if err = recorder.add(ctx, root, relative, now); err != nil {
		return nil, err
	}
	return recorder, nil
}

func (r *downloadManifestRecorder) add(ctx context.Context, info baiduapi.FileInfo, relative string, observed time.Time) error {
	if r == nil {
		return nil
	}
	r.entries = append(r.entries, taskstore.DownloadManifestEntry{FsID: info.FsID, Path: info.Path, Name: info.ServerFilename, IsDir: info.IsDir, Size: info.Size, Mtime: info.Mtime, CloudMD5: info.MD5, RelativePath: relative, ObservedAtUTC: observed})
	if len(r.entries) == taskstore.DownloadManifestBatchLimit {
		return r.flush(ctx)
	}
	return nil
}

func (r *downloadManifestRecorder) flush(ctx context.Context) error {
	if r == nil || len(r.entries) == 0 {
		return nil
	}
	if err := r.store.AppendDownloadManifestEntries(ctx, r.id, r.entries); err != nil {
		return err
	}
	r.entries = r.entries[:0]
	return nil
}

func (r *downloadManifestRecorder) publish(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if err := r.flush(ctx); err != nil {
		return err
	}
	return r.store.PublishDownloadManifest(ctx, r.id, time.Now().UTC())
}
