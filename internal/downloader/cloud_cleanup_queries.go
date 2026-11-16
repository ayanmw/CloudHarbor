package downloader

import (
	"context"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func (d *Downloader) CloudCleanupBatches(ctx context.Context, offset, limit int) ([]taskstore.CleanupBatchSummary, int, error) {
	scope, err := d.cleanupScope(ctx)
	if err != nil {
		return nil, 0, err
	}
	return d.queue.store.CleanupBatchPage(ctx, scope, offset, limit)
}
func (d *Downloader) CloudCleanupBatch(ctx context.Context, id string) (taskstore.CleanupBatchRecord, error) {
	scope, err := d.cleanupScope(ctx)
	if err != nil {
		return taskstore.CleanupBatchRecord{}, err
	}
	if _, err = d.queue.store.CleanupManifest(ctx, id, scope); err != nil {
		return taskstore.CleanupBatchRecord{}, err
	}
	return d.queue.store.CleanupBatch(ctx, id)
}
func (d *Downloader) CloudCleanupItems(ctx context.Context, id string, offset, limit int) ([]taskstore.CleanupItemRecord, int, error) {
	scope, err := d.cleanupScope(ctx)
	if err != nil {
		return nil, 0, err
	}
	return d.queue.store.CleanupItemPage(ctx, id, scope, offset, limit)
}
func (d *Downloader) CloudCleanupEvents(ctx context.Context, id string, after int64, limit int) ([]taskstore.CleanupBatchEvent, bool, error) {
	scope, err := d.cleanupScope(ctx)
	if err != nil {
		return nil, false, err
	}
	return d.queue.store.CleanupEventPage(ctx, id, scope, after, limit)
}
func (d *Downloader) StopCloudCleanupBatch(ctx context.Context, id string) error {
	scope, err := d.cleanupScope(ctx)
	if err != nil {
		return err
	}
	if _, err = d.queue.store.CleanupManifest(ctx, id, scope); err != nil {
		return err
	}
	return d.queue.store.StopCleanupBatch(ctx, id)
}
