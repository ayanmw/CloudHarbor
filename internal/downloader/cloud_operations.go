package downloader

import (
	"context"
	"errors"

	"web_browser_baidu_onekey_upload/internal/taskstore"
)

var ErrCloudOperationHistoryUnavailable = errors.New("remote operation history store unavailable")

func (d *Downloader) CloudOperationPage(ctx context.Context, filter taskstore.RemoteOperationFilter, offset, limit int) ([]taskstore.RemoteOperationSummary, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if d.queue == nil || d.queue.store == nil {
		return nil, 0, ErrCloudOperationHistoryUnavailable
	}
	return d.queue.store.RemoteOperationPageFiltered(filter, offset, limit)
}

func (d *Downloader) CloudOperationCursorPage(ctx context.Context, filter taskstore.RemoteOperationFilter, before *taskstore.RemoteOperationCursor, limit int) ([]taskstore.RemoteOperationSummary, int, *taskstore.RemoteOperationCursor, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, nil, err
	}
	if d.queue == nil || d.queue.store == nil {
		return nil, 0, nil, ErrCloudOperationHistoryUnavailable
	}
	return d.queue.store.RemoteOperationCursorPageFiltered(filter, before, limit)
}

func (d *Downloader) CloudOperationExport(ctx context.Context, filter taskstore.RemoteOperationFilter) ([]taskstore.RemoteOperationSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.queue == nil || d.queue.store == nil {
		return nil, ErrCloudOperationHistoryUnavailable
	}
	return d.queue.store.RemoteOperationExport(ctx, filter)
}

func (d *Downloader) CloudOperationEventPage(ctx context.Context, operationID string, filter taskstore.RemoteOperationEventFilter, beforeSequence int64, limit int) ([]taskstore.RemoteOperationEvent, int64, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, false, err
	}
	if d.queue == nil || d.queue.store == nil {
		return nil, 0, false, ErrCloudOperationHistoryUnavailable
	}
	return d.queue.store.RemoteOperationEventPage(operationID, filter, beforeSequence, limit)
}

func (d *Downloader) CloudOperationEvidenceExportFiltered(ctx context.Context, operationID string, filter taskstore.RemoteOperationEventFilter) (taskstore.RemoteOperationEvidenceBundle, error) {
	if err := ctx.Err(); err != nil {
		return taskstore.RemoteOperationEvidenceBundle{}, err
	}
	if d.queue == nil || d.queue.store == nil {
		return taskstore.RemoteOperationEvidenceBundle{}, ErrCloudOperationHistoryUnavailable
	}
	return d.queue.store.RemoteOperationEvidenceExportFiltered(ctx, operationID, filter)
}

func (d *Downloader) CloudOperationEvidenceExport(ctx context.Context, operationID string) (taskstore.RemoteOperationEvidenceBundle, error) {
	if err := ctx.Err(); err != nil {
		return taskstore.RemoteOperationEvidenceBundle{}, err
	}
	if d.queue == nil || d.queue.store == nil {
		return taskstore.RemoteOperationEvidenceBundle{}, ErrCloudOperationHistoryUnavailable
	}
	return d.queue.store.RemoteOperationEvidenceExport(ctx, operationID)
}
