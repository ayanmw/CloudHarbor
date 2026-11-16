package downloader

import (
	"context"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

// Retention is checked online in the same maintenance window as the deletion.
// Provider identity is checked again after hashing; a later external mutation
// remains subject to the provider's non-atomic metadata/write API boundary.
func verifyCleanupRetainedCopy(ctx context.Context, client *baiduapi.Client, expected baiduapi.FileInfo, digest string) error {
	current, err := client.GetFileInfo(expected.FsID)
	if err != nil {
		return err
	}
	if !sameCloudIdentity(expected, current) {
		return cleanupStale(errCleanupRetainedChanged)
	}
	actual, err := hashCloudFile(ctx, client, taskstore.CloudInventoryEntry{FsID: current.FsID, Path: current.Path, Name: current.ServerFilename, Size: current.Size, ProviderMtime: current.Mtime})
	if err != nil {
		return err
	}
	if actual != digest {
		return cleanupStale(errCleanupContentChanged)
	}
	current, err = client.GetFileInfo(expected.FsID)
	if err != nil {
		return err
	}
	if !sameCloudIdentity(expected, current) {
		return cleanupStale(errCleanupRetainedChanged)
	}
	return nil
}
