package downloader

import (
	"context"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func (d *Downloader) PreviewEmptyCloudDirectoriesPage(ctx context.Context, offset int64, limit int, expected *int64) (taskstore.CloudEmptyDirectoryPage, error) {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return taskstore.CloudEmptyDirectoryPage{}, err
	}
	return d.queue.store.ListEmptyCloudDirectoryPage(client.Fingerprint(), d.QueueDirectory(), offset, limit, expected)
}
