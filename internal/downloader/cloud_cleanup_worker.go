package downloader

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

// Scope and receive time come from the backend. The caller only freezes the
// selected identities/preview observations and supplies the idempotency ID.
func (d *Downloader) CreateCloudCleanupBatch(ctx context.Context, id string, items []taskstore.CleanupBatchItem) (taskstore.CleanupBatchRecord, error) {
	scope, err := d.cleanupScope(ctx)
	if err != nil {
		return taskstore.CleanupBatchRecord{}, err
	}
	record, err := d.queue.store.CreateCleanupBatch(ctx, taskstore.CleanupBatchManifest{ID: id, AccountFingerprint: scope.AccountFingerprint, ApplicationFingerprint: scope.ApplicationFingerprint, CacheNamespace: scope.CacheNamespace, CreatedAtUTC: time.Now().UTC(), Items: items})
	if err == nil {
		d.startCleanupWorker()
	}
	return record, err
}

func (d *Downloader) startCleanupWorker() error {
	d.cleanupWorkerMu.Lock()
	defer d.cleanupWorkerMu.Unlock()
	if d.cleanupWorkerStarted {
		return nil
	}
	if d.ctx.Err() != nil || d.queue.store == nil {
		return context.Canceled
	}
	if err := d.StartBackground(func(context.Context) { d.runCleanupWorker() }); err != nil {
		return err
	}
	d.cleanupWorkerStarted = true
	return nil
}

func (d *Downloader) runCleanupWorker() {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-timer.C:
		}
		for d.ctx.Err() == nil {
			scope, err := d.cleanupScope(d.ctx)
			if err != nil {
				break
			}
			id, ordinal, err := d.queue.store.NextCleanupItem(d.ctx, scope)
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			if err != nil {
				break
			}
			if _, err = d.executeCleanupItem(d.ctx, id, ordinal); err != nil {
				break
			}
		}
		// Retry readiness/maintenance waits without a tight loop or extra provider
		// requests. Unknown/final items are never returned by the durable selector.
		timer.Reset(2 * time.Second)
	}
}

// Called after credential and upload isolation initialization. Readiness is
// checked by the worker on every attempt; Store.Open itself never sends writes.
func (d *Downloader) ResumeCloudCleanup() error { return d.startCleanupWorker() }
