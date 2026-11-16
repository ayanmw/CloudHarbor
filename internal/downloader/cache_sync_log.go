package downloader

import (
	"context"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func (d *Downloader) CacheSyncChanges(ctx context.Context, run string) (taskstore.CacheSyncView, error) {
	client, _, err := d.authorizedCloudClient(ctx)
	if err != nil {
		return taskstore.CacheSyncView{}, err
	}
	return d.queue.store.CacheSyncChanges(client.CloudCacheNamespace(), run)
}
